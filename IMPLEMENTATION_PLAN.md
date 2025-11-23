# CacheFS Phase 4 & 5 Implementation Plan

## Executive Summary

This document outlines the detailed implementation plan for **Phase 4: Performance Optimization** and **Phase 5: Monitoring and Observability** of the cachefs project.

**Current Status:**
- ✅ Phase 1: Core Infrastructure (Complete)
- ✅ Phase 2: Advanced Eviction (Complete)
- ✅ Phase 3: Write-Back Support (Complete)
- ✅ Quick Wins: Examples, RemoveAll, Phase 3 Benchmarks (Complete)
- 🔄 Phase 4: Performance Optimization (In Planning)
- 📋 Phase 5: Monitoring & Observability (In Planning)

**Performance Baseline:**
- Cache read hit: 292 ns/op, 200 B/op, 4 allocs/op
- Cache read miss: 920 ns/op, 904 B/op, 7 allocs/op
- Write-through: 147 ns/op, 128 B/op, 2 allocs/op
- Write-back: 75 µs/op, 200 KB/op, 4 allocs/op
- Metadata cache hit: 19.6 ns/op, 0 B/op, 0 allocs/op

---

## Phase 4: Performance Optimization

### Overview

Phase 4 focuses on improving cache performance through reduced lock contention, memory efficiency, and better concurrency. The optimizations are ordered from easiest to most complex.

### 4.1: Memory Pooling for Cache Entries

**Objective:** Reduce GC pressure and memory allocations by reusing cache entry objects.

**Benefits:**
- 20-30% reduction in allocations
- Lower GC pause times
- Better memory locality
- Improved sustained throughput

**Implementation Details:**

1. **Add sync.Pool for cache entries**
   ```go
   var cacheEntryPool = sync.Pool{
       New: func() interface{} {
           return &cacheEntry{}
       },
   }
   ```

2. **Add sync.Pool for metadata entries**
   ```go
   var metadataEntryPool = sync.Pool{
       New: func() interface{} {
           return &metadataEntry{}
       },
   }
   ```

3. **Update cache entry allocation**
   - Replace `&cacheEntry{...}` with `getCacheEntry()`
   - Initialize fields after getting from pool
   - Add reset method to clear entry state

4. **Update cache entry deallocation**
   - Call `putCacheEntry(entry)` in `removeEntry()`
   - Reset entry before returning to pool
   - Handle data buffer pooling separately

5. **Add buffer pooling for large data**
   - Use sync.Pool for []byte buffers
   - Bucket by size (1KB, 4KB, 16KB, 64KB, etc.)
   - Only pool buffers above threshold (e.g., 1KB)

**Files to Modify:**
- `types.go` - Add pool variables and helper functions
- `cachefs.go` - Update entry creation/deletion
- `file.go` - Update Read/Write to use pooled buffers

**Testing Strategy:**
- Benchmark allocations before/after: `go test -bench=. -benchmem`
- Target: 30% reduction in B/op and allocs/op
- Verify no memory leaks with `-memprofile`
- Test with varying entry sizes

**Success Criteria:**
- ✅ Allocations reduced by 20-30%
- ✅ All existing tests pass
- ✅ Benchmarks show no performance regression
- ✅ Memory profile shows pool reuse

**Estimated Effort:** 2-3 hours

---

### 4.2: Batch Operations Support

**Objective:** Add batch methods to reduce lock acquisition overhead for bulk operations.

**Benefits:**
- Single lock acquisition for multiple operations
- Better cache coherency
- Reduced overhead for bulk invalidations
- Improved performance for mass updates

**Implementation Details:**

1. **Add BatchInvalidate method**
   ```go
   func (c *CacheFS) BatchInvalidate(paths []string) int
   ```
   - Lock once, invalidate all paths
   - Return count of invalidated entries
   - More efficient than calling Invalidate() in loop

2. **Add BatchFlush method**
   ```go
   func (c *CacheFS) BatchFlush(paths []string) error
   ```
   - Flush specific dirty entries
   - Lock once, flush all
   - Return first error encountered

3. **Add BatchStats method**
   ```go
   func (c *CacheFS) BatchStats(paths []string) map[string]EntryStats
   ```
   - Get stats for multiple entries
   - Single read lock
   - Return per-path statistics

4. **Add Prefetch method**
   ```go
   func (c *CacheFS) Prefetch(paths []string) error
   ```
   - Pre-load multiple files into cache
   - Useful for cache warming
   - Parallel loading with worker pool

**Files to Modify:**
- `cachefs.go` - Add batch methods
- `batch.go` - New file for batch operations
- `types.go` - Add EntryStats type

**Testing Strategy:**
- Unit tests for each batch operation
- Benchmark batch vs loop operations
- Test with 1, 10, 100, 1000 paths
- Verify correct error handling

**Success Criteria:**
- ✅ Batch operations 2-5x faster than loops
- ✅ All batch operations properly handle errors
- ✅ Documentation includes usage examples
- ✅ Thread-safe under concurrent access

**Estimated Effort:** 3-4 hours

---

### 4.3: Lock-Free Stats Operations

**Objective:** Use atomic operations for statistics to eliminate Stats mutex contention.

**Benefits:**
- Zero lock contention for stats updates
- Better concurrent read performance
- Simplified stats code
- Lower latency for cache operations

**Implementation Details:**

1. **Convert Stats fields to atomic types**
   ```go
   type Stats struct {
       hits      atomic.Uint64
       misses    atomic.Uint64
       evictions atomic.Uint64
       bytesUsed atomic.Uint64
       entries   atomic.Uint64
   }
   ```

2. **Remove Stats mutex**
   - Delete `mu sync.RWMutex` from Stats
   - No locking needed for atomic operations

3. **Update all stats methods**
   - `recordHit()` → `s.hits.Add(1)`
   - `recordMiss()` → `s.misses.Add(1)`
   - `Hits()` → `return s.hits.Load()`
   - etc.

4. **Add atomic operations for bytes tracking**
   - Use `atomic.Uint64` for bytesUsed
   - Atomic add/subtract in addBytes/removeBytes

5. **Update HitRate calculation**
   - Load hits and misses atomically
   - Calculate ratio without locks
   - Handle zero division safely

**Files to Modify:**
- `types.go` - Update Stats struct
- All files calling stats methods

**Testing Strategy:**
- Concurrent stats update test
- Verify accuracy under high contention
- Benchmark before/after
- Race detector: `go test -race`

**Success Criteria:**
- ✅ No mutex in Stats struct
- ✅ All stats operations use atomics
- ✅ 10-20% faster stats operations
- ✅ No data races in tests

**Estimated Effort:** 2 hours

---

### 4.4: Sharded Cache Structure

**Objective:** Split cache into multiple shards to reduce lock contention on concurrent access.

**Benefits:**
- 2-10x improvement for concurrent workloads
- Better scalability on multi-core systems
- Lower lock contention
- Improved cache locality per shard

**Implementation Details:**

1. **Create cacheShard struct** ✅ (Already done in shard.go)
   ```go
   type cacheShard struct {
       mu              sync.RWMutex
       entries         map[string]*cacheEntry
       lruHead         *cacheEntry
       lruTail         *cacheEntry
       metadataEntries map[string]*metadataEntry
       bytesUsed       uint64
       entryCount      uint64
   }
   ```

2. **Update CacheFS to use shards**
   - Replace global entries map with shard array
   - Add shardCount configuration (default 32)
   - Use FNV hash to select shard by path
   - Remove global mu (each shard has own lock)

3. **Implement shard selection**
   ```go
   func (c *CacheFS) getShard(path string) *cacheShard
   ```
   - Hash path using fnv.New32a()
   - Modulo by shard count
   - Return appropriate shard

4. **Refactor all cache operations**
   - `Remove()` - Get shard, lock, remove
   - `Invalidate()` - Get shard, lock, invalidate
   - `Stat()` - Get shard for metadata
   - `Clear()` - Lock all shards, clear each
   - `Flush()` - Lock all shards, flush each
   - `InvalidatePrefix()` - Check all shards (multi-lock)

5. **Update eviction logic**
   - Move LRU/LFU/TTL/Hybrid to per-shard
   - Evict from shard when shard exceeds limit
   - Global limit divided across shards
   - Rebalancing optional for future

6. **Update background flush**
   - Iterate all shards
   - Lock each shard independently
   - Flush dirty entries per shard

7. **Add shard configuration**
   ```go
   func WithShardCount(count int) Option
   ```

**Files to Modify:**
- `shard.go` - Shard structure and methods ✅
- `cachefs.go` - Refactor all ~30 methods
- `file.go` - Update Read/Write to use shards
- `options.go` - Add WithShardCount ✅
- `types.go` - Update constants

**Testing Strategy:**
- All existing tests must pass
- Concurrent access benchmark
- Test with 1, 8, 32, 64, 128 shards
- Verify even distribution across shards
- Memory usage shouldn't increase significantly

**Success Criteria:**
- ✅ All 32 tests pass with sharding
- ✅ 2-5x improvement on concurrent benchmarks
- ✅ Linear scaling up to CPU count
- ✅ No deadlocks or race conditions

**Estimated Effort:** 6-8 hours (most complex)

**Note:** This was started but reverted. Skeleton code exists in git history.

---

## Phase 5: Monitoring and Observability

### Overview

Phase 5 adds comprehensive metrics, monitoring capabilities, and performance validation to make cachefs production-ready.

### 5.1: Detailed Metrics Collection

**Objective:** Provide detailed operational metrics for monitoring and tuning.

**Benefits:**
- Deep visibility into cache behavior
- Identify performance bottlenecks
- Enable data-driven tuning
- Production monitoring support

**Implementation Details:**

1. **Add DetailedStats struct**
   ```go
   type DetailedStats struct {
       // Existing basic stats
       Hits, Misses, Evictions uint64
       BytesUsed, Entries       uint64

       // New detailed metrics
       EvictionsByPolicy map[EvictionPolicy]uint64
       FlushCount        uint64
       FlushErrors       uint64
       DirtyEntries      uint64

       // Latency tracking
       ReadLatencyP50    time.Duration
       ReadLatencyP95    time.Duration
       ReadLatencyP99    time.Duration
       WriteLatencyP50   time.Duration
       WriteLatencyP95   time.Duration
       WriteLatencyP99   time.Duration

       // Per-operation counters
       ReadOps, WriteOps       uint64
       InvalidateOps           uint64
       EvictOps                uint64
   }
   ```

2. **Add latency histogram**
   - Use simple histogram with buckets
   - Track read/write latencies
   - Calculate percentiles on demand
   - Optional: use HdrHistogram for accuracy

3. **Add per-eviction-policy counters**
   - Track which policy evicted entry
   - Useful for hybrid strategy tuning

4. **Add export methods**
   ```go
   func (c *CacheFS) DetailedStats() *DetailedStats
   func (c *CacheFS) ExportPrometheus() string
   func (c *CacheFS) ExportJSON() ([]byte, error)
   ```

5. **Add metrics collection option**
   ```go
   func WithDetailedMetrics(enable bool) Option
   ```
   - Disabled by default (minimal overhead)
   - Enable for production monitoring

**Files to Modify:**
- `types.go` - Add DetailedStats and histogram
- `metrics.go` - New file for metrics collection
- `cachefs.go` - Add metrics tracking hooks
- `file.go` - Add latency measurement
- `options.go` - Add WithDetailedMetrics

**Testing Strategy:**
- Verify metrics accuracy
- Test histogram percentile calculation
- Benchmark overhead (should be <5%)
- Test Prometheus export format

**Success Criteria:**
- ✅ All metrics accurately tracked
- ✅ <5% performance overhead when enabled
- ✅ Prometheus-compatible export
- ✅ JSON export for custom monitoring

**Estimated Effort:** 4-5 hours

---

### 5.2: Cache Warming Strategies

**Objective:** Provide mechanisms to pre-populate cache for optimal performance.

**Benefits:**
- Reduce cold start latency
- Predictable performance
- Support for common access patterns
- Better cache hit rates

**Implementation Details:**

1. **Add WarmCache method**
   ```go
   func (c *CacheFS) WarmCache(paths []string, opts ...WarmOption) error
   ```
   - Pre-load list of files
   - Parallel loading with worker pool
   - Progress callback support
   - Error handling per file

2. **Add WarmCacheFromPattern method**
   ```go
   func (c *CacheFS) WarmCacheFromPattern(pattern string) error
   ```
   - Load all files matching glob pattern
   - Recursive directory support
   - Size limits to prevent overflow

3. **Add WarmCacheFromFile method**
   ```go
   func (c *CacheFS) WarmCacheFromFile(listPath string) error
   ```
   - Read newline-separated list of paths
   - Load each path into cache
   - Skip non-existent files

4. **Add priority-based warming**
   ```go
   type WarmPriority int
   const (
       WarmPriorityHigh WarmPriority = iota
       WarmPriorityNormal
       WarmPriorityLow
   )
   ```
   - High priority: load first, prevent eviction
   - Normal: regular cache behavior
   - Low: load if space available

5. **Add warming progress tracking**
   ```go
   type WarmProgress struct {
       Total     int
       Completed int
       Errors    int
       BytesRead uint64
   }
   ```
   - Progress callback for monitoring
   - Error collection

6. **Add background warming**
   ```go
   func (c *CacheFS) WarmCacheAsync(paths []string, done chan<- error)
   ```
   - Non-blocking cache warming
   - Runs in background goroutine
   - Completion notification

**Files to Modify:**
- `warming.go` - New file for cache warming
- `cachefs.go` - Add warming methods
- `types.go` - Add WarmProgress, WarmPriority

**Testing Strategy:**
- Test warming 1, 100, 1000 files
- Verify parallel loading works
- Test error handling
- Benchmark warming speed
- Test with eviction during warming

**Success Criteria:**
- ✅ Can warm 1000 files in <1 second
- ✅ Parallel loading utilizes all cores
- ✅ Graceful error handling
- ✅ Progress tracking accurate

**Estimated Effort:** 3-4 hours

---

### 5.3: Comparison Benchmarks vs corfs

**Objective:** Validate performance claims against corfs (copy-on-read filesystem).

**Benefits:**
- Demonstrate value proposition
- Validate README claims
- Identify areas for improvement
- Marketing/documentation material

**Implementation Details:**

1. **Set up corfs dependency**
   ```go
   require github.com/absfs/corfs v0.0.0-latest
   ```
   - Add to go.mod
   - Import for comparison tests

2. **Create comparison benchmark suite**
   ```go
   // comparison_bench_test.go

   func BenchmarkComparison_ReadHit(b *testing.B)
   func BenchmarkComparison_ReadMiss(b *testing.B)
   func BenchmarkComparison_WriteThrough(b *testing.B)
   func BenchmarkComparison_SequentialReads(b *testing.B)
   func BenchmarkComparison_RandomAccess(b *testing.B)
   func BenchmarkComparison_EvictionLRU(b *testing.B)
   func BenchmarkComparison_ConcurrentReads(b *testing.B)
   ```

3. **Each benchmark tests both**
   - Sub-benchmark for cachefs
   - Sub-benchmark for corfs
   - Same test data and operations
   - Same cache size limits

4. **Add comparison documentation**
   ```markdown
   # Performance Comparison: cachefs vs corfs

   ## Methodology
   ## Results
   ## Analysis
   ```

5. **Generate comparison report**
   ```bash
   go test -bench=Comparison -benchmem > comparison.txt
   benchstat baseline.txt comparison.txt
   ```

6. **Create visualization**
   - Optional: Python script for charts
   - Compare key metrics side-by-side
   - Include in README

**Files to Create:**
- `comparison_bench_test.go` - Benchmark suite
- `COMPARISON.md` - Results and analysis
- `scripts/compare.sh` - Automation script

**Testing Strategy:**
- Run on same hardware
- Multiple runs for consistency
- Statistical significance testing
- Document test environment

**Success Criteria:**
- ✅ Comprehensive benchmark coverage
- ✅ Results match README claims
- ✅ Documentation complete
- ✅ Repeatable methodology

**Estimated Effort:** 3-4 hours

---

## Implementation Timeline

### Recommended Order

**Week 1: Foundation**
- Day 1-2: Phase 4.1 - Memory Pooling (2-3 hours)
- Day 2-3: Phase 4.3 - Lock-Free Stats (2 hours)
- Day 3-4: Phase 4.2 - Batch Operations (3-4 hours)

**Week 2: Advanced Optimization**
- Day 1-3: Phase 4.4 - Sharded Cache (6-8 hours)
- Day 4-5: Testing and benchmarking

**Week 3: Observability**
- Day 1-2: Phase 5.1 - Detailed Metrics (4-5 hours)
- Day 2-3: Phase 5.2 - Cache Warming (3-4 hours)
- Day 4: Phase 5.3 - Comparison Benchmarks (3-4 hours)
- Day 5: Documentation and polish

**Total Estimated Effort:** 26-34 hours

---

## Success Metrics

### Phase 4 Goals
- ✅ 30% reduction in memory allocations
- ✅ 2-5x improvement in concurrent workloads
- ✅ No performance regression in single-threaded workloads
- ✅ All existing tests pass
- ✅ No new race conditions

### Phase 5 Goals
- ✅ Comprehensive metrics collection
- ✅ Production-ready monitoring
- ✅ Cache warming capability
- ✅ Performance validation vs corfs
- ✅ Complete documentation

---

## Risk Assessment

### High Risk
- **Sharded Cache**: Most complex, could introduce bugs
  - Mitigation: Extensive testing, incremental rollout

### Medium Risk
- **Memory Pooling**: Potential for memory leaks
  - Mitigation: Memory profiling, careful testing

### Low Risk
- **Lock-Free Stats**: Simple atomic operations
- **Batch Operations**: Additive feature
- **Metrics/Warming**: Separate from core functionality

---

## Rollback Plan

Each phase is independent and can be reverted if issues arise:

1. **Memory Pooling**: Remove pools, revert to normal allocation
2. **Lock-Free Stats**: Revert to mutex-based stats
3. **Batch Operations**: Remove new methods, no breaking changes
4. **Sharded Cache**: Revert to single map with global lock
5. **Metrics**: Disable detailed metrics, keep basic stats
6. **Warming**: Remove warming methods, cache still works

---

## Testing Strategy

### For Each Phase

1. **Unit Tests**
   - Test new functionality
   - Test edge cases
   - Test error handling

2. **Benchmark Tests**
   - Compare before/after performance
   - Test with varying sizes
   - Test concurrent access

3. **Integration Tests**
   - Test with real-world scenarios
   - Test all write modes
   - Test all eviction policies

4. **Race Detection**
   ```bash
   go test -race ./...
   ```

5. **Memory Profiling**
   ```bash
   go test -memprofile=mem.prof
   go tool pprof mem.prof
   ```

6. **Coverage**
   ```bash
   go test -coverprofile=coverage.out
   go tool cover -html=coverage.out
   ```

---

## Documentation Updates

### For Each Phase

1. **README.md**: Update features and examples
2. **API Documentation**: Add godoc comments
3. **CHANGELOG.md**: Document changes
4. **IMPLEMENTATION_PLAN.md**: Update status
5. **Example Code**: Add usage examples

---

## Questions to Resolve

1. **Sharding Strategy**: Fixed shard count or dynamic?
2. **Memory Pooling**: What buffer sizes to pool?
3. **Metrics Format**: Prometheus only or support multiple formats?
4. **Cache Warming**: Should it block or be async by default?
5. **corfs Comparison**: Which version of corfs to benchmark against?

---

## Next Steps

1. Review and approve this plan
2. Start with Phase 4.1 (Memory Pooling)
3. Implement, test, benchmark, commit each phase
4. Update baseline benchmarks after each phase
5. Maintain backward compatibility throughout

**Ready to begin implementation!**
