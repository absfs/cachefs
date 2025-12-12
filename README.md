# cachefs

[![Go Reference](https://pkg.go.dev/badge/github.com/absfs/cachefs.svg)](https://pkg.go.dev/github.com/absfs/cachefs)
[![Go Report Card](https://goreportcard.com/badge/github.com/absfs/cachefs)](https://goreportcard.com/report/github.com/absfs/cachefs)
[![CI](https://github.com/absfs/cachefs/actions/workflows/ci.yml/badge.svg)](https://github.com/absfs/cachefs/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A sophisticated caching filesystem implementation for the AbsFS ecosystem, providing enterprise-grade write-through, write-back, and write-around caching with advanced eviction policies.

## Overview

`cachefs` extends beyond the basic caching capabilities of `corfs` (copy-on-read filesystem) by implementing a full-featured cache layer with multiple write modes, configurable eviction policies, and intelligent cache invalidation strategies. It's designed for scenarios requiring high performance, memory efficiency, and fine-grained control over caching behavior.

### Key Differentiators from corfs

- **Multiple Write Modes**: Write-through, write-back, and write-around policies
- **Advanced Eviction**: LRU (Least Recently Used), LFU (Least Frequently Used), and TTL-based eviction
- **Memory Management**: Configurable cache size limits with automatic eviction
- **Cache Invalidation**: Time-based, event-based, and manual invalidation strategies
- **Metadata Caching**: Separate control over data and metadata caching
- **Performance Monitoring**: Built-in metrics and statistics tracking
- **Async Write-Back**: Background write-back with configurable flush intervals

## Features

### Cache Policies

#### Eviction Policies

- **LRU (Least Recently Used)**: Evicts least recently accessed items
- **LFU (Least Frequently Used)**: Evicts least frequently accessed items
- **TTL (Time-To-Live)**: Automatic expiration based on age
- **Hybrid**: Combination of LRU/LFU with TTL constraints

#### Write Modes

- **Write-Through**: Synchronous writes to both cache and backing store
- **Write-Back**: Asynchronous writes with configurable flush intervals
- **Write-Around**: Bypass cache on writes, only cache on reads

### Memory Management

- Configurable maximum cache size (bytes or entry count)
- Automatic eviction when limits are reached
- Memory pressure detection and adaptive behavior
- Separate limits for data and metadata caches

### Symbolic Link Support

- **CacheFS**: Implements `absfs.FileSystem` (no symlink methods)
- **SymlinkCacheFS**: Implements `absfs.SymlinkFileSystem` with full symlink support
- Use `NewSymlinkFS()` when your backing filesystem supports symlinks

### Cache Invalidation

- **Time-Based**: TTL expiration with configurable durations
- **Event-Based**: Invalidation on file system events
- **Manual**: Explicit cache clear operations
- **Pattern-Based**: Invalidate by path patterns or prefixes

## Implementation Phases

### Phase 1: Core Infrastructure

- Basic cache entry structure
- LRU eviction implementation
- Write-through mode
- Simple memory limit enforcement
- Basic statistics tracking

### Phase 2: Advanced Eviction

- LFU eviction policy
- TTL-based expiration
- Hybrid eviction strategies
- Metadata caching separate from data

### Phase 3: Write-Back Support

- Async write-back implementation
- Background flush scheduler
- Write-back queue management
- Dirty entry tracking and persistence

### Phase 4: Performance Optimization

- Lock-free data structures where possible
- Sharded cache to reduce contention
- Batch operations support
- Memory pooling for cache entries

### Phase 5: Monitoring and Observability

- Detailed metrics (hit rate, eviction rate, etc.)
- Cache warming strategies
- Performance benchmarking suite
- Comparison benchmarks vs corfs

## API Design

### Basic Usage

```go
package main

import (
    "github.com/absfs/absfs"
    "github.com/absfs/cachefs"
    "github.com/absfs/memfs"
)

func main() {
    // Create backing filesystem
    backing, _ := memfs.NewFS()

    // Create cache with default settings (write-through, LRU, 100MB limit)
    cache := cachefs.New(backing)

    // Use as standard absfs.FileSystem
    file, err := cache.OpenFile("/data/file.txt", os.O_RDWR|os.O_CREATE, 0644)
    if err != nil {
        panic(err)
    }
    defer file.Close()

    // Reads and writes are automatically cached
    data := make([]byte, 1024)
    n, err := file.Read(data)
}
```

### With Symlink Support

```go
package main

import (
    "github.com/absfs/cachefs"
    "github.com/absfs/memfs"
)

func main() {
    // Create backing filesystem with symlink support
    backing, _ := memfs.NewFS()

    // Create SymlinkCacheFS for full symlink support
    cache := cachefs.NewSymlinkFS(backing)

    // All symlink operations are passed through to backing
    cache.Symlink("/target/file.txt", "/link.txt")
    target, _ := cache.Readlink("/link.txt")
    info, _ := cache.Lstat("/link.txt")
}
```

### Advanced Configuration

```go
// Create cache with custom configuration
cache := cachefs.New(backing,
    cachefs.WithWriteMode(cachefs.WriteModeWriteBack),
    cachefs.WithEvictionPolicy(cachefs.EvictionLRU),
    cachefs.WithMaxBytes(500 * 1024 * 1024), // 500 MB
    cachefs.WithTTL(5 * time.Minute),
    cachefs.WithFlushInterval(30 * time.Second),
    cachefs.WithMetadataCache(true),
)

// Access cache statistics
stats := cache.Stats()
fmt.Printf("Hit Rate: %.2f%%\n", stats.HitRate()*100)
fmt.Printf("Evictions: %d\n", stats.Evictions)
fmt.Printf("Memory Used: %d bytes\n", stats.BytesUsed)
```

### Write Mode Configuration

```go
// Write-through: synchronous, always consistent
wt := cachefs.New(backing,
    cachefs.WithWriteMode(cachefs.WriteModeWriteThrough),
)

// Write-back: async writes, better performance, risk of data loss
wb := cachefs.New(backing,
    cachefs.WithWriteMode(cachefs.WriteModeWriteBack),
    cachefs.WithFlushInterval(10 * time.Second),
    cachefs.WithFlushOnClose(true),
)

// Write-around: bypass cache on writes, cache only reads
wa := cachefs.New(backing,
    cachefs.WithWriteMode(cachefs.WriteModeWriteAround),
)
```

### Eviction Policy Configuration

```go
// LRU: good for temporal locality
lru := cachefs.New(backing,
    cachefs.WithEvictionPolicy(cachefs.EvictionLRU),
    cachefs.WithMaxEntries(10000),
)

// LFU: good for frequency-based access patterns
lfu := cachefs.New(backing,
    cachefs.WithEvictionPolicy(cachefs.EvictionLFU),
    cachefs.WithMaxBytes(1024 * 1024 * 1024), // 1 GB
)

// TTL: automatic expiration
ttl := cachefs.New(backing,
    cachefs.WithEvictionPolicy(cachefs.EvictionTTL),
    cachefs.WithTTL(10 * time.Minute),
)

// Hybrid: combine LRU with TTL
hybrid := cachefs.New(backing,
    cachefs.WithEvictionPolicy(cachefs.EvictionHybrid),
    cachefs.WithTTL(15 * time.Minute),
    cachefs.WithMaxBytes(512 * 1024 * 1024),
)
```

### Cache Invalidation

```go
// Invalidate specific path
cache.Invalidate("/data/stale.txt")

// Invalidate by pattern
cache.InvalidatePattern("/data/*.tmp")

// Invalidate by prefix
cache.InvalidatePrefix("/cache/")

// Clear entire cache
cache.Clear()

// Flush dirty entries (write-back mode)
cache.Flush()
```

### Statistics and Monitoring

```go
// Get current statistics
stats := cache.Stats()
fmt.Printf("Hits: %d, Misses: %d, Hit Rate: %.2f%%\n",
    stats.Hits, stats.Misses, stats.HitRate()*100)

// Reset statistics
cache.ResetStats()

// Export metrics
metrics := cache.Metrics()
// metrics can be exposed via Prometheus, statsd, etc.
```

## Memory Management

### Size Limits

```go
// Limit by total bytes
cache := cachefs.New(backing,
    cachefs.WithMaxBytes(1024 * 1024 * 1024), // 1 GB
)

// Limit by entry count
cache := cachefs.New(backing,
    cachefs.WithMaxEntries(50000),
)

// Limit both
cache := cachefs.New(backing,
    cachefs.WithMaxBytes(500 * 1024 * 1024),
    cachefs.WithMaxEntries(100000),
)
```

### Separate Data and Metadata Caches

```go
// Configure separate limits for metadata
cache := cachefs.New(backing,
    cachefs.WithMaxBytes(1024 * 1024 * 1024), // 1 GB for data
    cachefs.WithMetadataCache(true),
    cachefs.WithMetadataMaxEntries(100000), // Cache lots of metadata
)
```

### Memory Pressure Handling

```go
// Adaptive behavior under memory pressure
cache := cachefs.New(backing,
    cachefs.WithMemoryPressureHandler(func(used, limit uint64) {
        // Custom logic when approaching memory limits
        // e.g., reduce TTL, increase eviction rate
    }),
)
```

## Cache Invalidation Strategies

### Time-Based Invalidation

```go
// Global TTL for all entries
cache := cachefs.New(backing,
    cachefs.WithTTL(5 * time.Minute),
)

// Per-path TTL configuration
cache.SetPathTTL("/dynamic/*", 30 * time.Second)
cache.SetPathTTL("/static/*", 1 * time.Hour)
```

### Event-Based Invalidation

```go
// Invalidate on file system events
cache := cachefs.New(backing,
    cachefs.WithInvalidateOnEvent(true),
)

// Custom event handlers
cache.OnFileModified(func(path string) {
    cache.Invalidate(path)
})
```

### Write-Based Invalidation

```go
// Write-through: no invalidation needed (always consistent)
wt := cachefs.New(backing,
    cachefs.WithWriteMode(cachefs.WriteModeWriteThrough),
)

// Write-around: invalidate on write (cache only reads)
wa := cachefs.New(backing,
    cachefs.WithWriteMode(cachefs.WriteModeWriteAround),
)

// Write-back: invalidate on flush
wb := cachefs.New(backing,
    cachefs.WithWriteMode(cachefs.WriteModeWriteBack),
    cachefs.WithInvalidateOnFlush(true),
)
```

## Performance Benchmarks vs corfs

Expected performance characteristics:

### Read Performance

- **Cache Hit**: 10-100x faster than backing store (memory access)
- **Cache Miss**: Slightly slower than direct access (overhead of cache check)
- **vs corfs**: Similar cache hit performance, better eviction strategies

### Write Performance

- **Write-Through**: Similar to direct writes (synchronous)
- **Write-Back**: 10-50x faster than direct writes (async)
- **Write-Around**: Same as direct writes (bypass cache)
- **vs corfs**: Write-back mode significantly faster, write-through comparable

### Memory Efficiency

- **LRU/LFU**: Better memory utilization than naive caching
- **TTL**: Automatic cleanup of stale entries
- **vs corfs**: More sophisticated memory management, configurable limits

### Benchmark Suite

```go
// Example benchmark results (target metrics)
BenchmarkCacheFS_ReadHit-8         10000000    120 ns/op
BenchmarkCacheFS_ReadMiss-8         1000000   1500 ns/op
BenchmarkCacheFS_WriteThrough-8     1000000   1800 ns/op
BenchmarkCacheFS_WriteBack-8       10000000    200 ns/op
BenchmarkCacheFS_LRU_Eviction-8     5000000    350 ns/op
BenchmarkCacheFS_LFU_Eviction-8     5000000    380 ns/op

// Comparison with corfs
BenchmarkCorFS_ReadHit-8           10000000    130 ns/op
BenchmarkCorFS_ReadMiss-8           1000000   1450 ns/op
BenchmarkCorFS_Write-8              1000000   1750 ns/op
```

## Use Cases

### High-Read Workloads

```go
// Web server serving static assets
cache := cachefs.New(osfs.New(),
    cachefs.WithEvictionPolicy(cachefs.EvictionLRU),
    cachefs.WithMaxBytes(2 * 1024 * 1024 * 1024), // 2 GB
    cachefs.WithTTL(1 * time.Hour),
)
```

### Write-Heavy with Batch Processing

```go
// Log aggregation with periodic flush
cache := cachefs.New(backing,
    cachefs.WithWriteMode(cachefs.WriteModeWriteBack),
    cachefs.WithFlushInterval(5 * time.Minute),
    cachefs.WithMaxBytes(500 * 1024 * 1024),
)
```

### Mixed Read/Write with Strong Consistency

```go
// Database-like workload
cache := cachefs.New(backing,
    cachefs.WithWriteMode(cachefs.WriteModeWriteThrough),
    cachefs.WithEvictionPolicy(cachefs.EvictionHybrid),
    cachefs.WithMaxEntries(100000),
    cachefs.WithTTL(10 * time.Minute),
)
```

## Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run benchmarks
go test -bench=. -benchmem

# Compare with corfs
go test -bench=. -benchmem > cachefs.bench
cd ../corfs
go test -bench=. -benchmem > corfs.bench
benchcmp corfs.bench ../cachefs/cachefs.bench
```

## Contributing

Contributions are welcome! Please ensure:

- All tests pass
- Code coverage remains above 80%
- Benchmarks show no performance regressions
- Documentation is updated

## License

MIT License - See LICENSE file for details

## Related Projects

- [absfs](https://github.com/absfs/absfs) - Core filesystem abstraction
- [corfs](https://github.com/absfs/corfs) - Copy-on-read filesystem
- [memfs](https://github.com/absfs/memfs) - In-memory filesystem
- [osfs](https://github.com/absfs/osfs) - Operating system filesystem wrapper
