// Command simulate drives realistic traffic against a running instance of
// the catalog API to demonstrate, end to end, the three things this project
// claims to do: cache-aside reads work, concurrent misses on the same key
// get collapsed into one store call (stampede protection), and writes
// invalidate the cache. It's meant to be run against the docker-compose
// stack by scripts/simulate.sh, not as a unit test — the point is to prove
// the behavior over the network against real Redis, not against mocks.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type product struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
}

func main() {
	baseURL := flag.String("base-url", envOr("BASE_URL", "http://localhost:8080"), "catalog API base URL")
	stampedeConcurrency := flag.Int("stampede-concurrency", 50, "number of concurrent requests fired at one cold key")
	readWorkloadRequests := flag.Int("read-requests", 500, "number of requests in the read-heavy workload phase")
	catalogSize := flag.Int("catalog-size", 20, "number of products to seed for the read-heavy workload")
	flag.Parse()

	fmt.Println("== 1. Waiting for the service to become healthy ==")
	if err := waitHealthy(*baseURL, 30*time.Second); err != nil {
		fatalf("service never became healthy: %v", err)
	}
	fmt.Println("service is up")

	fmt.Println("\n== 2. Stampede protection: seeding one product, then firing concurrent cold reads ==")
	target, err := createProduct(*baseURL, "Stampede Test Widget", 1999)
	if err != nil {
		fatalf("failed to create product: %v", err)
	}
	before, err := fetchMetrics(*baseURL)
	if err != nil {
		fatalf("failed to fetch metrics: %v", err)
	}

	start := time.Now()
	var wg sync.WaitGroup
	errs := make(chan error, *stampedeConcurrency)
	for range *stampedeConcurrency {
		wg.Go(func() {
			if _, _, err := getProduct(*baseURL, target.ID); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		fmt.Fprintf(os.Stderr, "request error: %v\n", err)
	}
	elapsed := time.Since(start)

	after, err := fetchMetrics(*baseURL)
	if err != nil {
		fatalf("failed to fetch metrics: %v", err)
	}
	coalesced := after.storeCallsCoalesced - before.storeCallsCoalesced
	fmt.Printf("fired %d concurrent requests for the same cold key in %s\n", *stampedeConcurrency, elapsed)
	fmt.Printf("store calls coalesced by singleflight: %.0f (expected %d — every request but the first waited on it instead of hitting the store)\n",
		coalesced, *stampedeConcurrency-1)
	if int(coalesced) != *stampedeConcurrency-1 {
		fmt.Println("WARNING: coalesced count did not match expectation — stampede protection may not be working")
	}

	fmt.Println("\n== 3. Read-heavy workload: seeding a small catalog and hammering it to measure hit ratio ==")
	ids := make([]string, 0, *catalogSize)
	for i := 0; i < *catalogSize; i++ {
		p, err := createProduct(*baseURL, fmt.Sprintf("Catalog Item %d", i), int64(500+i*37))
		if err != nil {
			fatalf("failed to seed product %d: %v", i, err)
		}
		ids = append(ids, p.ID)
	}
	beforeWorkload, err := fetchMetrics(*baseURL)
	if err != nil {
		fatalf("failed to fetch metrics: %v", err)
	}
	var readWg sync.WaitGroup
	sem := make(chan struct{}, 20)
	for i := 0; i < *readWorkloadRequests; i++ {
		id := ids[i%len(ids)]
		sem <- struct{}{}
		readWg.Go(func() {
			defer func() { <-sem }()
			_, _, _ = getProduct(*baseURL, id)
		})
	}
	readWg.Wait()
	afterWorkload, err := fetchMetrics(*baseURL)
	if err != nil {
		fatalf("failed to fetch metrics: %v", err)
	}
	hits := afterWorkload.cacheHits - beforeWorkload.cacheHits
	misses := afterWorkload.cacheMisses - beforeWorkload.cacheMisses
	total := hits + misses
	ratio := 0.0
	if total > 0 {
		ratio = hits / total * 100
	}
	fmt.Printf("issued %d reads across %d distinct products: %.0f hits, %.0f misses (%.1f%% hit ratio)\n",
		*readWorkloadRequests, *catalogSize, hits, misses, ratio)
	fmt.Println("(first pass over each id is always a miss; the ratio approaches (catalog_size-1)/catalog_size as more requests land after warm-up)")

	fmt.Println("\n== 4. Write invalidation: updating a product and confirming the next read reflects it ==")
	newName := "Renamed After Update"
	if err := updateProduct(*baseURL, target.ID, newName); err != nil {
		fatalf("update failed: %v", err)
	}
	got, _, err := getProduct(*baseURL, target.ID)
	if err != nil {
		fatalf("post-update read failed: %v", err)
	}
	if got.Name != newName {
		fatalf("expected updated name %q after invalidation, got %q", newName, got.Name)
	}
	fmt.Printf("read-after-write returned the updated name (%q) — cache invalidation on update works\n", got.Name)

	fmt.Println("\nAll simulation phases completed successfully.")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func waitHealthy(base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for /healthz, last error: %v", lastErr)
}

func createProduct(base, name string, priceCents int64) (product, error) {
	body, _ := json.Marshal(map[string]any{"name": name, "price_cents": priceCents})
	resp, err := http.Post(base+"/products", "application/json", bytes.NewReader(body))
	if err != nil {
		return product{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return product{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}
	var p product
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return product{}, err
	}
	return p, nil
}

func getProduct(base, id string) (product, time.Duration, error) {
	start := time.Now()
	resp, err := http.Get(base + "/products/" + id)
	if err != nil {
		return product{}, 0, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return product{}, elapsed, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}
	var p product
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return product{}, elapsed, err
	}
	return p, elapsed, nil
}

func updateProduct(base, id, name string) error {
	body, _ := json.Marshal(map[string]any{"name": name})
	req, err := http.NewRequest(http.MethodPut, base+"/products/"+id, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

type metricsSnapshot struct {
	cacheHits           float64
	cacheMisses         float64
	storeCallsCoalesced float64
}

var metricLineRe = regexp.MustCompile(`^(\w+)(\{[^}]*\})?\s+([0-9eE+\-.]+)$`)

// fetchMetrics does a minimal scrape of the Prometheus text exposition
// format — full parsing isn't needed for three counters, and avoiding an
// extra dependency keeps this load-generator a single self-contained file.
func fetchMetrics(base string) (metricsSnapshot, error) {
	resp, err := http.Get(base + "/metrics")
	if err != nil {
		return metricsSnapshot{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return metricsSnapshot{}, err
	}

	var snap metricsSnapshot
	for line := range strings.SplitSeq(string(body), "\n") {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		m := metricLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, labels, valStr := m[1], m[2], m[3]
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		switch {
		case name == "catalog_cache_requests_total" && strings.Contains(labels, `outcome="hit"`):
			snap.cacheHits = val
		case name == "catalog_cache_requests_total" && strings.Contains(labels, `outcome="miss"`):
			snap.cacheMisses = val
		case name == "catalog_store_calls_coalesced_total":
			snap.storeCallsCoalesced = val
		}
	}
	return snap, nil
}
