package cachefs

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DetailedStats contains comprehensive cache statistics and metrics
type DetailedStats struct {
	// Basic counters
	Hits      uint64 `json:"hits"`
	Misses    uint64 `json:"misses"`
	Evictions uint64 `json:"evictions"`
	BytesUsed uint64 `json:"bytes_used"`
	Entries   uint64 `json:"entries"`

	// Hit rate
	HitRate float64 `json:"hit_rate"`

	// Write-back specific
	FlushCount   uint64 `json:"flush_count,omitempty"`
	FlushErrors  uint64 `json:"flush_errors,omitempty"`
	DirtyEntries uint64 `json:"dirty_entries,omitempty"`

	// Per-operation counters
	ReadOps       uint64 `json:"read_ops,omitempty"`
	WriteOps      uint64 `json:"write_ops,omitempty"`
	InvalidateOps uint64 `json:"invalidate_ops,omitempty"`

	// Configuration
	MaxBytes       uint64 `json:"max_bytes"`
	MaxEntries     uint64 `json:"max_entries"`
	TTL            string `json:"ttl,omitempty"`
	WriteMode      string `json:"write_mode"`
	EvictionPolicy string `json:"eviction_policy"`
}

// DetailedStats returns comprehensive statistics about the cache
func (c *CacheFS) DetailedStats() *DetailedStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := &DetailedStats{
		Hits:      c.stats.Hits(),
		Misses:    c.stats.Misses(),
		Evictions: c.stats.Evictions(),
		BytesUsed: c.stats.BytesUsed(),
		Entries:   c.stats.Entries(),
		HitRate:   c.stats.HitRate(),

		MaxBytes:   c.maxBytes,
		MaxEntries: c.maxEntries,

		WriteMode:      writeModeString(c.writeMode),
		EvictionPolicy: evictionPolicyString(c.evictionPolicy),
	}

	if c.ttl > 0 {
		stats.TTL = c.ttl.String()
	}

	// Count dirty entries if in write-back mode
	if c.writeMode == WriteModeWriteBack {
		dirtyCount := uint64(0)
		for _, entry := range c.entries {
			if entry.dirty {
				dirtyCount++
			}
		}
		stats.DirtyEntries = dirtyCount
	}

	return stats
}

// ExportJSON exports cache statistics as JSON
func (c *CacheFS) ExportJSON() ([]byte, error) {
	stats := c.DetailedStats()
	return json.MarshalIndent(stats, "", "  ")
}

// ExportPrometheus exports cache statistics in Prometheus format
func (c *CacheFS) ExportPrometheus() string {
	stats := c.DetailedStats()

	var b strings.Builder

	// Basic metrics
	fmt.Fprintf(&b, "# HELP cachefs_hits_total Total number of cache hits\n")
	fmt.Fprintf(&b, "# TYPE cachefs_hits_total counter\n")
	fmt.Fprintf(&b, "cachefs_hits_total %d\n\n", stats.Hits)

	fmt.Fprintf(&b, "# HELP cachefs_misses_total Total number of cache misses\n")
	fmt.Fprintf(&b, "# TYPE cachefs_misses_total counter\n")
	fmt.Fprintf(&b, "cachefs_misses_total %d\n\n", stats.Misses)

	fmt.Fprintf(&b, "# HELP cachefs_evictions_total Total number of cache evictions\n")
	fmt.Fprintf(&b, "# TYPE cachefs_evictions_total counter\n")
	fmt.Fprintf(&b, "cachefs_evictions_total %d\n\n", stats.Evictions)

	fmt.Fprintf(&b, "# HELP cachefs_hit_rate Cache hit rate (0.0 to 1.0)\n")
	fmt.Fprintf(&b, "# TYPE cachefs_hit_rate gauge\n")
	fmt.Fprintf(&b, "cachefs_hit_rate %.4f\n\n", stats.HitRate)

	fmt.Fprintf(&b, "# HELP cachefs_bytes_used Current bytes used by cache\n")
	fmt.Fprintf(&b, "# TYPE cachefs_bytes_used gauge\n")
	fmt.Fprintf(&b, "cachefs_bytes_used %d\n\n", stats.BytesUsed)

	fmt.Fprintf(&b, "# HELP cachefs_entries Current number of cache entries\n")
	fmt.Fprintf(&b, "# TYPE cachefs_entries gauge\n")
	fmt.Fprintf(&b, "cachefs_entries %d\n\n", stats.Entries)

	if stats.DirtyEntries > 0 {
		fmt.Fprintf(&b, "# HELP cachefs_dirty_entries Number of dirty entries (write-back mode)\n")
		fmt.Fprintf(&b, "# TYPE cachefs_dirty_entries gauge\n")
		fmt.Fprintf(&b, "cachefs_dirty_entries %d\n\n", stats.DirtyEntries)
	}

	// Capacity metrics
	fmt.Fprintf(&b, "# HELP cachefs_max_bytes Maximum cache size in bytes\n")
	fmt.Fprintf(&b, "# TYPE cachefs_max_bytes gauge\n")
	fmt.Fprintf(&b, "cachefs_max_bytes %d\n\n", stats.MaxBytes)

	if stats.MaxEntries > 0 {
		fmt.Fprintf(&b, "# HELP cachefs_max_entries Maximum number of cache entries\n")
		fmt.Fprintf(&b, "# TYPE cachefs_max_entries gauge\n")
		fmt.Fprintf(&b, "cachefs_max_entries %d\n\n", stats.MaxEntries)
	}

	return b.String()
}

// Helper functions to convert enums to strings
func writeModeString(mode WriteMode) string {
	switch mode {
	case WriteModeWriteThrough:
		return "write-through"
	case WriteModeWriteBack:
		return "write-back"
	case WriteModeWriteAround:
		return "write-around"
	default:
		return "unknown"
	}
}

func evictionPolicyString(policy EvictionPolicy) string {
	switch policy {
	case EvictionLRU:
		return "lru"
	case EvictionLFU:
		return "lfu"
	case EvictionTTL:
		return "ttl"
	case EvictionHybrid:
		return "hybrid"
	default:
		return "unknown"
	}
}

// Snapshot captures a point-in-time view of cache statistics
type Snapshot struct {
	Timestamp time.Time
	Stats     *DetailedStats
}

// TakeSnapshot captures current cache statistics with a timestamp
func (c *CacheFS) TakeSnapshot() *Snapshot {
	return &Snapshot{
		Timestamp: time.Now(),
		Stats:     c.DetailedStats(),
	}
}

// CompareSnapshots returns the delta between two snapshots
func CompareSnapshots(before, after *Snapshot) map[string]int64 {
	delta := make(map[string]int64)

	delta["hits"] = int64(after.Stats.Hits) - int64(before.Stats.Hits)
	delta["misses"] = int64(after.Stats.Misses) - int64(before.Stats.Misses)
	delta["evictions"] = int64(after.Stats.Evictions) - int64(before.Stats.Evictions)
	delta["bytes_used"] = int64(after.Stats.BytesUsed) - int64(before.Stats.BytesUsed)
	delta["entries"] = int64(after.Stats.Entries) - int64(before.Stats.Entries)

	return delta
}
