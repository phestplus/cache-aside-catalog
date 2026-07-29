// Package metrics exposes Prometheus instrumentation for the cache-aside layer.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	CacheRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "catalog_cache_requests_total",
			Help: "Total cache lookups, labeled by outcome (hit|miss).",
		},
		[]string{"outcome"},
	)

	StoreCallsCoalesced = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "catalog_store_calls_coalesced_total",
			Help: "Concurrent cache-miss requests collapsed into a single store call by singleflight.",
		},
		[]string{},
	)

	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "catalog_get_product_duration_seconds",
			Help:    "GetProduct latency, labeled by outcome (hit|miss).",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"outcome"},
	)
)

// Registry is a dedicated Prometheus registry (rather than the global
// default) so tests can spin up isolated instances without collector
// registration panics.
func NewRegistry() *prometheus.Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(CacheRequests, StoreCallsCoalesced, RequestDuration)
	return r
}
