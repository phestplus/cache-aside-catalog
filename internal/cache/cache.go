// Package cache defines the caching layer sitting in front of the store.
package cache

import (
	"context"
	"time"
)

// Cache is a simple string key/value cache with TTL. Values are pre-serialized
// (JSON) by the caller so this interface stays storage-agnostic.
type Cache interface {
	Get(ctx context.Context, key string) (value string, found bool, err error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}
