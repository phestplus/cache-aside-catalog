package catalog

import (
	"context"
	"encoding/json"
	"math/rand"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/ekelestephen/cache-aside-catalog/internal/cache"
	"github.com/ekelestephen/cache-aside-catalog/internal/metrics"
	"github.com/ekelestephen/cache-aside-catalog/internal/model"
	"github.com/ekelestephen/cache-aside-catalog/internal/store"
)

// Service implements the cache-aside read pattern: check the cache first,
// fall back to the store on a miss, then populate the cache for next time.
//
// Two problems come with that pattern once you add real concurrency and
// scale, and this service exists to prove I handled both rather than just
// describe them:
//
//  1. Cache stampede: if 500 requests for the same cold key arrive at once,
//     a naive implementation sends 500 identical requests to the store at
//     the same moment. I use singleflight to collapse concurrent misses for
//     the same key into one store call — the other 499 callers just wait for
//     that one call's result.
//  2. Synchronized expiry: if every key gets the exact same TTL, keys that
//     were written around the same time all expire at the same moment,
//     recreating the stampede problem on a timer. I add random jitter on top
//     of the base TTL so expirations spread out instead of landing together.
type Service struct {
	store     store.Store
	cache     cache.Cache
	ttl       time.Duration
	ttlJitter time.Duration
	sf        singleflight.Group
}

func NewService(s store.Store, c cache.Cache, ttl, ttlJitter time.Duration) *Service {
	return &Service{store: s, cache: c, ttl: ttl, ttlJitter: ttlJitter}
}

func productKey(id string) string {
	return "product:" + id
}

func (s *Service) jitteredTTL() time.Duration {
	if s.ttlJitter <= 0 {
		return s.ttl
	}
	return s.ttl + time.Duration(rand.Int63n(int64(s.ttlJitter)))
}

func (s *Service) GetProduct(ctx context.Context, id string) (model.Product, error) {
	start := time.Now()
	key := productKey(id)

	if raw, found, err := s.cache.Get(ctx, key); err == nil && found {
		var p model.Product
		if json.Unmarshal([]byte(raw), &p) == nil {
			metrics.CacheRequests.WithLabelValues("hit").Inc()
			metrics.RequestDuration.WithLabelValues("hit").Observe(time.Since(start).Seconds())
			return p, nil
		}
	}
	// A cache read error is treated as a miss rather than a failure — the
	// store is the source of truth, so the correct behavior when the cache
	// is unavailable is to serve from the store, not to error out.
	metrics.CacheRequests.WithLabelValues("miss").Inc()

	v, err, shared := s.sf.Do(key, func() (any, error) {
		p, err := s.store.GetProduct(ctx, id)
		if err != nil {
			return model.Product{}, err
		}
		if data, err := json.Marshal(p); err == nil {
			_ = s.cache.Set(ctx, key, string(data), s.jitteredTTL())
		}
		return p, nil
	})
	if shared {
		metrics.StoreCallsCoalesced.WithLabelValues().Inc()
	}
	if err != nil {
		return model.Product{}, err
	}
	metrics.RequestDuration.WithLabelValues("miss").Observe(time.Since(start).Seconds())
	return v.(model.Product), nil
}

func (s *Service) ListProducts(ctx context.Context) ([]model.Product, error) {
	return s.store.ListProducts(ctx)
}

func (s *Service) CreateProduct(ctx context.Context, in model.CreateProductInput) (model.Product, error) {
	p := model.Product{
		ID:          newID(),
		Name:        in.Name,
		Description: in.Description,
		PriceCents:  in.PriceCents,
	}
	if err := s.store.CreateProduct(ctx, p); err != nil {
		return model.Product{}, err
	}
	return p, nil
}

// UpdateProduct writes through to the store, then invalidates the cache
// entry rather than updating it in place. Deleting is simpler and safer
// than trying to keep a cached copy in sync with every possible partial
// update — the next read just costs one cache miss.
func (s *Service) UpdateProduct(ctx context.Context, id string, in model.UpdateProductInput) (model.Product, error) {
	p, err := s.store.UpdateProduct(ctx, id, in)
	if err != nil {
		return model.Product{}, err
	}
	_ = s.cache.Delete(ctx, productKey(id))
	return p, nil
}
