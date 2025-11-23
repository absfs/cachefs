package cachefs

import (
	"io/fs"
	"sync"
	"sync/atomic"
	"time"
)

// Pools for reducing allocations and GC pressure
var (
	cacheEntryPool = sync.Pool{
		New: func() interface{} {
			return &cacheEntry{}
		},
	}

	metadataEntryPool = sync.Pool{
		New: func() interface{} {
			return &metadataEntry{}
		},
	}
)

// getCacheEntry gets a cache entry from the pool
func getCacheEntry() *cacheEntry {
	return cacheEntryPool.Get().(*cacheEntry)
}

// putCacheEntry returns a cache entry to the pool after resetting it
func putCacheEntry(e *cacheEntry) {
	// Reset the entry to avoid keeping references
	e.path = ""
	e.data = nil
	e.modTime = time.Time{}
	e.size = 0
	e.dirty = false
	e.lastAccess = time.Time{}
	e.prev = nil
	e.next = nil
	e.accessCount = 0
	e.createdAt = time.Time{}
	e.expiresAt = time.Time{}

	cacheEntryPool.Put(e)
}

// getMetadataEntry gets a metadata entry from the pool
func getMetadataEntry() *metadataEntry {
	return metadataEntryPool.Get().(*metadataEntry)
}

// putMetadataEntry returns a metadata entry to the pool after resetting it
func putMetadataEntry(e *metadataEntry) {
	// Reset the entry to avoid keeping references
	e.path = ""
	e.info = nil
	e.lastAccess = time.Time{}
	e.accessCount = 0
	e.createdAt = time.Time{}
	e.expiresAt = time.Time{}

	metadataEntryPool.Put(e)
}

// WriteMode defines the cache write behavior
type WriteMode int

const (
	// WriteModeWriteThrough writes to both cache and backing store synchronously
	WriteModeWriteThrough WriteMode = iota
	// WriteModeWriteBack writes to cache immediately and backing store asynchronously
	WriteModeWriteBack
	// WriteModeWriteAround bypasses cache on writes, only caches reads
	WriteModeWriteAround
)

// EvictionPolicy defines how entries are evicted when cache is full
type EvictionPolicy int

const (
	// EvictionLRU evicts least recently used entries
	EvictionLRU EvictionPolicy = iota
	// EvictionLFU evicts least frequently used entries
	EvictionLFU
	// EvictionTTL evicts entries based on time-to-live
	EvictionTTL
	// EvictionHybrid combines LRU/LFU with TTL
	EvictionHybrid
)

// cacheEntry represents a single cached item
type cacheEntry struct {
	path      string
	data      []byte
	modTime   time.Time
	size      int64
	dirty     bool // true if write-back mode and not yet flushed

	// LRU fields
	lastAccess time.Time
	prev       *cacheEntry
	next       *cacheEntry

	// LFU fields
	accessCount uint64

	// TTL fields
	createdAt time.Time
	expiresAt time.Time
}

// metadataEntry represents cached file metadata
type metadataEntry struct {
	path        string
	info        fs.FileInfo
	lastAccess  time.Time
	accessCount uint64
	createdAt   time.Time
	expiresAt   time.Time
}

// Stats represents cache statistics using lock-free atomic operations
type Stats struct {
	hits      atomicUint64
	misses    atomicUint64
	evictions atomicUint64
	bytesUsed atomicUint64
	entries   atomicUint64
}

// atomicUint64 wraps sync/atomic operations for uint64
type atomicUint64 struct {
	value uint64
}

func (a *atomicUint64) Load() uint64 {
	return atomic.LoadUint64(&a.value)
}

func (a *atomicUint64) Store(val uint64) {
	atomic.StoreUint64(&a.value, val)
}

func (a *atomicUint64) Add(delta uint64) uint64 {
	return atomic.AddUint64(&a.value, delta)
}

func (a *atomicUint64) Sub(delta uint64) uint64 {
	// Subtraction is done by adding the two's complement
	return atomic.AddUint64(&a.value, ^uint64(delta-1))
}

// HitRate returns the cache hit rate as a value between 0 and 1
func (s *Stats) HitRate() float64 {
	hits := s.hits.Load()
	misses := s.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

// Hits returns the number of cache hits
func (s *Stats) Hits() uint64 {
	return s.hits.Load()
}

// Misses returns the number of cache misses
func (s *Stats) Misses() uint64 {
	return s.misses.Load()
}

// Evictions returns the number of evicted entries
func (s *Stats) Evictions() uint64 {
	return s.evictions.Load()
}

// BytesUsed returns the total bytes used by cache
func (s *Stats) BytesUsed() uint64 {
	return s.bytesUsed.Load()
}

// Entries returns the number of cached entries
func (s *Stats) Entries() uint64 {
	return s.entries.Load()
}

func (s *Stats) recordHit() {
	s.hits.Add(1)
}

func (s *Stats) recordMiss() {
	s.misses.Add(1)
}

func (s *Stats) recordEviction() {
	s.evictions.Add(1)
}

func (s *Stats) addBytes(n uint64) {
	s.bytesUsed.Add(n)
}

func (s *Stats) removeBytes(n uint64) {
	s.bytesUsed.Sub(n)
}

func (s *Stats) incEntries() {
	s.entries.Add(1)
}

func (s *Stats) decEntries() {
	s.entries.Sub(1)
}

func (s *Stats) reset() {
	s.hits.Store(0)
	s.misses.Store(0)
	s.evictions.Store(0)
}
