// Package store defines the source-of-truth persistence layer for products.
package store

import (
	"context"
	"errors"

	"github.com/ekelestephen/cache-aside-catalog/internal/model"
)

var ErrNotFound = errors.New("product not found")

// Store is the source of truth the cache sits in front of. A real deployment
// would back this with Postgres/MySQL; the interface boundary is what makes
// swapping the implementation a non-invasive change (see InMemoryStore).
type Store interface {
	GetProduct(ctx context.Context, id string) (model.Product, error)
	ListProducts(ctx context.Context) ([]model.Product, error)
	CreateProduct(ctx context.Context, p model.Product) error
	UpdateProduct(ctx context.Context, id string, in model.UpdateProductInput) (model.Product, error)
}
