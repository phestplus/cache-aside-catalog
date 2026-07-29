package catalog_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/phestplus/cache-aside-catalog/internal/cache"
	"github.com/phestplus/cache-aside-catalog/internal/catalog"
	"github.com/phestplus/cache-aside-catalog/internal/model"
	"github.com/phestplus/cache-aside-catalog/internal/store"
)

// countingStore wraps InMemoryStore and counts GetProduct calls, so tests
// can assert on how many times the "database" was actually hit. That
// count is the whole point of the stampede-protection test below.
type countingStore struct {
	*store.InMemoryStore
	getCalls int64
}

func (s *countingStore) GetProduct(ctx context.Context, id string) (model.Product, error) {
	atomic.AddInt64(&s.getCalls, 1)
	return s.InMemoryStore.GetProduct(ctx, id)
}

func newTestService(t *testing.T, latency time.Duration) (*catalog.Service, *countingStore) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cs := &countingStore{InMemoryStore: store.NewInMemoryStore(latency, 0)}
	svc := catalog.NewService(cs, cache.NewRedisCache(rdb), time.Minute, 0)
	return svc, cs
}

func TestGetProduct_MissThenHit(t *testing.T) {
	svc, cs := newTestService(t, 0)
	ctx := context.Background()

	created, err := svc.CreateProduct(ctx, model.CreateProductInput{Name: "Widget", PriceCents: 999})
	if err != nil {
		t.Fatalf("CreateProduct failed: %v", err)
	}

	// First read is a cache miss and must hit the store.
	got, err := svc.GetProduct(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetProduct (miss) failed: %v", err)
	}
	if got.Name != "Widget" {
		t.Fatalf("expected name Widget, got %q", got.Name)
	}
	if cs.getCalls != 1 {
		t.Fatalf("expected 1 store call after first read, got %d", cs.getCalls)
	}

	// Second read should be served from cache, no additional store call.
	if _, err := svc.GetProduct(ctx, created.ID); err != nil {
		t.Fatalf("GetProduct (hit) failed: %v", err)
	}
	if cs.getCalls != 1 {
		t.Fatalf("expected store call count to stay at 1 on cache hit, got %d", cs.getCalls)
	}
}

func TestGetProduct_NotFound(t *testing.T) {
	svc, _ := newTestService(t, 0)
	if _, err := svc.GetProduct(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("expected an error for a missing product, got nil")
	}
}

// TestGetProduct_StampedeProtection is the core claim of this project: N
// concurrent requests for the same cold key must collapse into exactly one
// store call, not N.
func TestGetProduct_StampedeProtection(t *testing.T) {
	const concurrency = 50
	// Latency wide enough that all goroutines are guaranteed to be
	// in-flight and blocked on the same singleflight call before it
	// resolves.
	svc, cs := newTestService(t, 100*time.Millisecond)
	ctx := context.Background()

	created, err := svc.CreateProduct(ctx, model.CreateProductInput{Name: "Gadget", PriceCents: 4999})
	if err != nil {
		t.Fatalf("CreateProduct failed: %v", err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, concurrency)
	for range concurrency {
		wg.Go(func() {
			if _, err := svc.GetProduct(ctx, created.ID); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent GetProduct failed: %v", err)
	}

	if cs.getCalls != 1 {
		t.Fatalf("expected exactly 1 store call for %d concurrent misses on the same key, got %d", concurrency, cs.getCalls)
	}
}

func TestUpdateProduct_InvalidatesCache(t *testing.T) {
	svc, cs := newTestService(t, 0)
	ctx := context.Background()

	created, err := svc.CreateProduct(ctx, model.CreateProductInput{Name: "Original", PriceCents: 100})
	if err != nil {
		t.Fatalf("CreateProduct failed: %v", err)
	}

	// Warm the cache.
	if _, err := svc.GetProduct(ctx, created.ID); err != nil {
		t.Fatalf("GetProduct (warm) failed: %v", err)
	}
	if cs.getCalls != 1 {
		t.Fatalf("expected 1 store call after warming cache, got %d", cs.getCalls)
	}

	newName := "Updated"
	if _, err := svc.UpdateProduct(ctx, created.ID, model.UpdateProductInput{Name: &newName}); err != nil {
		t.Fatalf("UpdateProduct failed: %v", err)
	}

	// The cache entry must have been invalidated, so this read is a miss
	// again and returns the new value.
	got, err := svc.GetProduct(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetProduct (post-update) failed: %v", err)
	}
	if got.Name != newName {
		t.Fatalf("expected updated name %q, got %q", newName, got.Name)
	}
	if cs.getCalls != 2 {
		t.Fatalf("expected 2 store calls total (cache should've been invalidated by update), got %d", cs.getCalls)
	}
}
