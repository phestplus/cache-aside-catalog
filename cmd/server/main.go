// Command server runs the cache-aside product catalog API.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/phestplus/cache-aside-catalog/internal/cache"
	"github.com/phestplus/cache-aside-catalog/internal/catalog"
	"github.com/phestplus/cache-aside-catalog/internal/metrics"
	"github.com/phestplus/cache-aside-catalog/internal/store"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDurationOr(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func main() {
	addr := envOr("PORT", "8080")
	redisAddr := envOr("REDIS_ADDR", "localhost:6379")
	ttl := envDurationOr("CACHE_TTL", 30*time.Second)
	ttlJitter := envDurationOr("CACHE_TTL_JITTER", 10*time.Second)
	storeLatencyMS := envIntOr("STORE_LATENCY_MS", 80)
	storeJitterMS := envIntOr("STORE_JITTER_MS", 40)

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		cancel()
		log.Fatalf("could not connect to redis at %s: %v", redisAddr, err)
	}
	cancel()

	memStore := store.NewInMemoryStore(
		time.Duration(storeLatencyMS)*time.Millisecond,
		time.Duration(storeJitterMS)*time.Millisecond,
	)
	redisCache := cache.NewRedisCache(redisClient)
	svc := catalog.NewService(memStore, redisCache, ttl, ttlJitter)
	handler := catalog.NewHandler(svc)

	registry := metrics.NewRegistry()

	mux := http.NewServeMux()
	handler.Register(mux)
	mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	srv := &http.Server{
		Addr:              ":" + addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("cache-aside-catalog listening on :%s (redis=%s, ttl=%s+jitter %s)", addr, redisAddr, ttl, ttlJitter)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	log.Println("shutting down")
	_ = srv.Shutdown(shutdownCtx)
}
