# Cache-Aside Product Catalog

**Status:** Planned (build order #1)
**Stack:** Go, Redis

## Problem
Read-heavy catalog service demonstrating the cache-aside pattern under real load: cache stampede protection, TTL/invalidation strategy, and measurable hit/miss ratio.

## Planned deliverables
- Cache-aside read path with stampede protection (singleflight / lock-based)
- Invalidation strategy on writes
- Metrics: hit/miss ratio, latency with vs without cache
- `scripts/simulate.sh` — end-to-end script that spins up the service and drives realistic read/write traffic to exercise cache behavior
- Architecture diagram + tradeoffs section in README
