package cachefs

import (
	"sync"
	"time"
)

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

// Stats represents cache statistics
type Stats struct {
	mu sync.RWMutex

	hits      uint64
	misses    uint64
	evictions uint64
	bytesUsed uint64
	entries   uint64
}

// HitRate returns the cache hit rate as a value between 0 and 1
func (s *Stats) HitRate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := s.hits + s.misses
	if total == 0 {
		return 0.0
	}
	return float64(s.hits) / float64(total)
}

// Hits returns the number of cache hits
func (s *Stats) Hits() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hits
}

// Misses returns the number of cache misses
func (s *Stats) Misses() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.misses
}

// Evictions returns the number of evicted entries
func (s *Stats) Evictions() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.evictions
}

// BytesUsed returns the total bytes used by cache
func (s *Stats) BytesUsed() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bytesUsed
}

// Entries returns the number of cached entries
func (s *Stats) Entries() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries
}

func (s *Stats) recordHit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits++
}

func (s *Stats) recordMiss() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.misses++
}

func (s *Stats) recordEviction() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictions++
}

func (s *Stats) addBytes(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bytesUsed += n
}

func (s *Stats) removeBytes(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bytesUsed -= n
}

func (s *Stats) incEntries() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries++
}

func (s *Stats) decEntries() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries--
}

func (s *Stats) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits = 0
	s.misses = 0
	s.evictions = 0
}
