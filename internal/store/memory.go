package store

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/ekelestephen/cache-aside-catalog/internal/model"
)

// InMemoryStore simulates a slow relational database with an in-process map
// plus artificial latency. This keeps the demo self-contained (no Postgres
// container needed) while the project's actual subject under test, the
// cache-aside layer in front of it, talks to a real Redis instance.
type InMemoryStore struct {
	mu       sync.RWMutex
	products map[string]model.Product
	latency  time.Duration
	jitter   time.Duration
}

func NewInMemoryStore(latency, jitter time.Duration) *InMemoryStore {
	return &InMemoryStore{
		products: make(map[string]model.Product),
		latency:  latency,
		jitter:   jitter,
	}
}

func (s *InMemoryStore) simulateIO() {
	d := s.latency
	if s.jitter > 0 {
		d += time.Duration(rand.Int63n(int64(s.jitter)))
	}
	time.Sleep(d)
}

func (s *InMemoryStore) GetProduct(ctx context.Context, id string) (model.Product, error) {
	s.simulateIO()
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.products[id]
	if !ok {
		return model.Product{}, ErrNotFound
	}
	return p, nil
}

func (s *InMemoryStore) ListProducts(ctx context.Context) ([]model.Product, error) {
	s.simulateIO()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Product, 0, len(s.products))
	for _, p := range s.products {
		out = append(out, p)
	}
	return out, nil
}

func (s *InMemoryStore) CreateProduct(ctx context.Context, p model.Product) error {
	s.simulateIO()
	s.mu.Lock()
	defer s.mu.Unlock()
	p.UpdatedAt = time.Now().UTC()
	s.products[p.ID] = p
	return nil
}

func (s *InMemoryStore) UpdateProduct(ctx context.Context, id string, in model.UpdateProductInput) (model.Product, error) {
	s.simulateIO()
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[id]
	if !ok {
		return model.Product{}, ErrNotFound
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Description != nil {
		p.Description = *in.Description
	}
	if in.PriceCents != nil {
		p.PriceCents = *in.PriceCents
	}
	p.UpdatedAt = time.Now().UTC()
	s.products[id] = p
	return p, nil
}
