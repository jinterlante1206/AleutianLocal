# HLD Queries - Future Enhancements

This document tracks planned enhancements for HLD query operations based on the GR-19c post-implementation review.

## 1. BadgerDB Caching Layer (DB-H1, DB-H2, DB-H3, DB-M1, DB-M2)

### Status: Planned
### Priority: High
### Effort: 8-12 hours

**Description:**
Add persistent caching of LCA results and path decompositions using BadgerDB.

**Implementation Plan:**

```go
// Add to HLDecomposition struct:
type HLDecomposition struct {
    // ... existing fields ...

    cache *badger.DB  // BadgerDB instance for caching
    cacheEnabled bool // Whether caching is enabled
}

// Cache key format:
// "lca:{graphHash}:{u}:{v}" -> lca node ID
// "path:{graphHash}:{u}:{v}" -> []PathSegment (encoded)
// "dist:{graphHash}:{u}:{v}" -> distance (int)

// TTL Strategy:
// - LCA results: 1 hour (frequently accessed, stable)
// - Path decompositions: 30 minutes (larger, less stable)
// - Distance queries: 1 hour (small, stable)

// Cache invalidation:
// - Include graph hash in cache key
// - Invalidate all entries when graph changes
// - Periodic cleanup of expired entries
```

**Benefits:**
- 10-100x speedup for repeated queries
- Reduced CPU load during analysis
- Persistent cache across service restarts

**Challenges:**
- Cache invalidation when graph changes
- Memory management for large graphs
- Serialization overhead for PathSegment slices

**Related Files:**
- `services/trace/graph/hld_queries.go` - Add cache lookups
- `services/trace/graph/hld.go` - Add cache field and initialization
- `services/trace/graph/hld_cache.go` - New file for cache operations

---

## 2. CRS Integration (CRS-H1, CRS-H2, CRS-M1, CRS-M2, CRS-M3)

### Status: Partially Complete
### Priority: High
### Effort: 4-6 hours

**Description:**
Full integration with Code Reasoning State for replay and debugging.

**Current State:**
- HLD construction already records TraceSteps via `GraphAnalytics.BuildHLDIterative()`
- CRSGraphAdapter provides high-level graph queries
- Missing: Query-level TraceStep recording, replay hooks, correlation IDs

**Implementation Plan:**

### CRS-H1: TraceStep Recording

Record HLD queries as CRS TraceSteps at the GraphAnalytics level:

```go
// In GraphAnalytics wrapper methods:
func (ga *GraphAnalytics) LCAWithCRS(ctx context.Context, u, v string) (string, error) {
    start := time.Now()
    lca, err := ga.hld.LCA(ctx, u, v)
    duration := time.Since(start)

    // Record TraceStep
    traceStep := crs.NewTraceStepBuilder().
        WithAction("lca_query").
        WithTarget(fmt.Sprintf("%s,%s", u, v)).
        WithTool("HLDecomposition.LCA").
        WithDuration(duration).
        WithMetadata("lca", lca).
        WithMetadata("error", err != nil).
        Build()

    crs.RecordTraceStep(ctx, traceStep)
    return lca, err
}
```

### CRS-H2: Replay Hooks

Add replay mode for deterministic debugging:

```go
type HLDecomposition struct {
    // ... existing fields ...

    replayMode bool
    replayCache map[string]interface{}  // Pre-recorded results
}

func (hld *HLDecomposition) LCA(ctx context.Context, u, v string) (string, error) {
    if hld.replayMode {
        key := fmt.Sprintf("lca:%s:%s", u, v)
        if cached, ok := hld.replayCache[key]; ok {
            return cached.(string), nil
        }
    }
    // ... normal execution ...
}
```

### CRS-M1, M2, M3: Metadata and Correlation

```go
// Extract correlation ID from context
func getCorrelationID(ctx context.Context) string {
    if cid, ok := ctx.Value(correlationIDKey).(string); ok {
        return cid
    }
    return ""
}

// Add to all log statements:
slog.InfoContext(ctx, "LCA computed",
    slog.String("correlation_id", getCorrelationID(ctx)),
    slog.String("caller", getCallerFromContext(ctx)),
    // ... other fields ...
)
```

**Benefits:**
- Full query traceability
- Deterministic replay for debugging
- Query provenance tracking

**Related Files:**
- `services/trace/analytics/analytics.go` - Add CRS wrapper methods
- `services/trace/graph/hld_queries.go` - Add replay hooks
- `services/trace/agent/mcts/crs/` - Use existing CRS infrastructure

---

## 3. Rate Limiting and Circuit Breaker (A-M4, A-M5)

### Status: Planned
### Priority: Medium
### Effort: 4-6 hours

**Description:**
Protect against query flooding and cascading failures.

**Implementation:**

```go
import (
    "golang.org/x/time/rate"
    "github.com/sony/gobreaker"
)

var (
    // A-M4: Rate limiter
    lcaLimiter = rate.NewLimiter(1000, 100)  // 1000 QPS, burst 100

    // A-M5: Circuit breaker
    lcaCircuit = gobreaker.NewCircuitBreaker(gobreaker.Settings{
        Name:        "LCA",
        MaxRequests: 10,
        Interval:    60 * time.Second,
        Timeout:     60 * time.Second,
        ReadyToTrip: func(counts gobreaker.Counts) bool {
            failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
            return counts.Requests >= 3 && failureRatio >= 0.6
        },
    })
)

func (hld *HLDecomposition) LCA(ctx context.Context, u, v string) (string, error) {
    // Rate limit
    if !lcaLimiter.Allow() {
        return "", errors.New("LCA rate limit exceeded")
    }

    // Circuit breaker
    result, err := lcaCircuit.Execute(func() (interface{}, error) {
        return hld.computeLCA(ctx, u, v)
    })

    if err != nil {
        return "", err
    }
    return result.(string), nil
}
```

**Benefits:**
- Prevents DoS via query flooding
- Protects against cascading failures
- Automatic recovery from transient issues

**Challenges:**
- Tuning rate limits for different workloads
- Avoiding false positives in circuit breaker
- Metrics and alerting integration

---

## 4. Query Timeout Enforcement (A-M6)

### Status: Planned
### Priority: Medium
### Effort: 2 hours

**Description:**
Enforce maximum query timeout to prevent runaway queries.

**Implementation:**

```go
const (
    DefaultLCATimeout = 5 * time.Second
    DefaultPathDecompositionTimeout = 10 * time.Second
)

func (hld *HLDecomposition) LCA(ctx context.Context, u, v string) (string, error) {
    // Add timeout if not already present
    if _, hasDeadline := ctx.Deadline(); !hasDeadline {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, DefaultLCATimeout)
        defer cancel()
    }

    // ... rest of implementation ...
}
```

**Benefits:**
- Prevents slow queries from hanging
- Predictable worst-case latency
- Better resource utilization

**Challenges:**
- Choosing appropriate timeout values
- Handling partial results on timeout
- User experience for legitimate slow queries

---

## 5. Query Complexity Estimation (A-M7)

### Status: Planned
### Priority: Low
### Effort: 4 hours

**Description:**
Estimate query cost before execution to enable prioritization and rejection.

**Implementation:**

```go
func (hld *HLDecomposition) EstimateLCACost(u, v string) int {
    uIdx := hld.nodeToIdx[u]
    vIdx := hld.nodeToIdx[v]

    // Estimate based on depth difference
    // Worst case: O(depth[u] + depth[v])
    depthDiff := abs(hld.depth[uIdx] - hld.depth[vIdx])
    maxDepth := max(hld.depth[uIdx], hld.depth[vIdx])

    // Conservative estimate: deeper depth = higher cost
    return depthDiff + maxDepth
}

func (hld *HLDecomposition) LCA(ctx context.Context, u, v string) (string, error) {
    // Reject expensive queries
    cost := hld.EstimateLCACost(u, v)
    if cost > maxAllowedCost {
        return "", fmt.Errorf("query too expensive: cost=%d, max=%d", cost, maxAllowedCost)
    }

    // ... rest of implementation ...
}
```

**Benefits:**
- Query prioritization (serve cheap queries first)
- Proactive rejection of expensive queries
- Better SLA compliance

**Use Cases:**
- Multi-tenant systems with query quotas
- Interactive tools needing fast response
- Background batch processing

---

## 6. Batch Query API (A-M8)

### Status: Planned
### Priority: Low
### Effort: 6-8 hours

**Description:**
Optimize for batch workloads with parallel query execution.

**Implementation:**

```go
func (hld *HLDecomposition) BatchLCA(ctx context.Context, pairs [][2]string) ([]string, []error, error) {
    results := make([]string, len(pairs))
    errors := make([]error, len(pairs))

    var wg sync.WaitGroup
    sem := make(chan struct{}, runtime.NumCPU()) // Limit concurrency

    for i, pair := range pairs {
        wg.Add(1)
        go func(idx int, u, v string) {
            defer wg.Done()

            sem <- struct{}{}        // Acquire
            defer func() { <-sem }() // Release

            lca, err := hld.LCA(ctx, u, v)
            results[idx] = lca
            errors[idx] = err
        }(i, pair[0], pair[1])
    }

    wg.Wait()
    return results, errors, nil
}
```

**Benefits:**
- 5-10x speedup for batch workloads
- Better CPU utilization
- Simplified caller code

**Use Cases:**
- Bulk graph analysis
- Report generation
- Data export/migration

---

## 7. Concurrent Access Benchmarks (A-M3)

### Status: Planned
### Priority: Medium
### Effort: 2 hours

**Description:**
Add benchmarks for concurrent query patterns.

**Implementation:**

```go
func BenchmarkLCA_Concurrent(b *testing.B) {
    hld := buildLargeHLD(b, 10000)
    ctx := context.Background()

    b.RunParallel(func(pb *testing.PB) {
        i := 0
        for pb.Next() {
            u := fmt.Sprintf("node_%d", i%5000)
            v := fmt.Sprintf("node_%d", (i+2500)%10000)
            _, _ = hld.LCA(ctx, u, v)
            i++
        }
    })
}

func BenchmarkLCA_ContentionHigh(b *testing.B) {
    hld := buildLargeHLD(b, 10000)
    ctx := context.Background()

    // All goroutines query same pair (high stats contention)
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _, _ = hld.LCA(ctx, "node_1000", "node_5000")
        }
    })
}
```

**Benefits:**
- Identify concurrency bottlenecks
- Validate thread safety
- Performance regression detection

---

## 8. Version Field and Schema Migration (A-M1)

### Status: Planned
### Priority: Low
### Effort: 2 hours

**Description:**
Add version field to support schema evolution.

**Implementation:**

```go
const HLDSchemaVersion = 1

type HLDecomposition struct {
    version int  // Schema version (current: 1)
    // ... existing fields ...
}

func (hld *HLDecomposition) Serialize() ([]byte, error) {
    data := struct {
        Version int
        // ... serialized fields ...
    }{
        Version: HLDSchemaVersion,
        // ...
    }
    return json.Marshal(data)
}

func Deserialize(data []byte) (*HLDecomposition, error) {
    var wrapper struct {
        Version int
        // ...
    }
    json.Unmarshal(data, &wrapper)

    switch wrapper.Version {
    case 1:
        return deserializeV1(data)
    default:
        return nil, fmt.Errorf("unsupported HLD version: %d", wrapper.Version)
    }
}
```

---

## 9. Graph Change Detection (A-M2)

### Status: Planned
### Priority: Medium
### Effort: 3 hours

**Description:**
Detect when graph has changed since HLD construction.

**Implementation:**

```go
type HLDecomposition struct {
    graphHash string  // Hash of graph when HLD built
    // ... existing fields ...
}

func BuildHLD(ctx context.Context, g *Graph, root string) (*HLDecomposition, error) {
    hld := &HLDecomposition{
        graphHash: g.Hash(),  // Compute graph hash
        // ...
    }
    // ... build HLD ...
    return hld, nil
}

func (hld *HLDecomposition) ValidateGraphHash(g *Graph) error {
    currentHash := g.Hash()
    if currentHash != hld.graphHash {
        return errors.New("graph has changed since HLD construction")
    }
    return nil
}
```

**Benefits:**
- Prevents stale HLD from returning incorrect results
- Automatic HLD rebuild detection
- Debugging aid for cache invalidation

---

## Priority Summary

### Must Have (Before Production):
- BadgerDB caching (DB-H1, H2, H3)
- CRS TraceStep recording (CRS-H1)

### Should Have (Production Hardening):
- Rate limiting (A-M4)
- Circuit breaker (A-M5)
- Timeout enforcement (A-M6)
- Graph change detection (A-M2)
- Concurrent benchmarks (A-M3)

### Nice to Have (Future Optimization):
- Replay hooks (CRS-H2)
- Query complexity estimation (A-M7)
- Batch API (A-M8)
- Version field (A-M1)

---

## Testing Plan

Each enhancement should include:
1. Unit tests for happy path
2. Unit tests for error cases
3. Integration tests with GraphAnalytics
4. Benchmarks for performance impact
5. Load tests for concurrency
6. Documentation updates

---

## References

- GR-19c Post-Implementation Review: `/tmp/gr_19c_post_implementation_review.md`
- CLAUDE.md: `/Users/jin/GolandProjects/AleutianFOSS/CLAUDE.md`
- CRS Package: `services/trace/agent/mcts/crs/`
- Graph Analytics: `services/trace/analytics/`
