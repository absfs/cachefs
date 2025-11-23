package cachefs

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDetailedStats(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithWriteMode(WriteModeWriteBack),
		WithEvictionPolicy(EvictionLRU),
		WithMaxBytes(1024),
		WithTTL(5*time.Minute),
	)

	// Perform some operations
	backing.files["/file1.txt"] = []byte("test data 1")
	backing.files["/file2.txt"] = []byte("test data 2")

	file, _ := cache.OpenFile("/file1.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 11)
	file.Read(buf)
	file.Close()

	// Get detailed stats
	stats := cache.DetailedStats()

	if stats.Hits != 0 {
		t.Errorf("expected 0 hits, got %d", stats.Hits)
	}

	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}

	if stats.Entries != 1 {
		t.Errorf("expected 1 entry, got %d", stats.Entries)
	}

	if stats.WriteMode != "write-back" {
		t.Errorf("expected write-back mode, got %s", stats.WriteMode)
	}

	if stats.EvictionPolicy != "lru" {
		t.Errorf("expected lru policy, got %s", stats.EvictionPolicy)
	}

	if stats.MaxBytes != 1024 {
		t.Errorf("expected max bytes 1024, got %d", stats.MaxBytes)
	}

	if stats.TTL != "5m0s" {
		t.Errorf("expected TTL 5m0s, got %s", stats.TTL)
	}
}

func TestExportJSON(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Perform some operations
	backing.files["/file.txt"] = []byte("test")
	file, _ := cache.OpenFile("/file.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 4)
	file.Read(buf)
	file.Close()

	// Export as JSON
	jsonData, err := cache.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON error: %v", err)
	}

	// Verify it's valid JSON
	var stats DetailedStats
	err = json.Unmarshal(jsonData, &stats)
	if err != nil {
		t.Fatalf("JSON unmarshal error: %v", err)
	}

	if stats.Hits != 0 {
		t.Errorf("expected 0 hits in JSON, got %d", stats.Hits)
	}

	if stats.Misses != 1 {
		t.Errorf("expected 1 miss in JSON, got %d", stats.Misses)
	}
}

func TestExportPrometheus(t *testing.T) {
	backing := newMockFS()
	cache := New(backing, WithMaxBytes(2048))

	// Perform some operations
	backing.files["/file.txt"] = []byte("test")
	file, _ := cache.OpenFile("/file.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 4)
	file.Read(buf)
	file.Close()

	// Export as Prometheus
	prom := cache.ExportPrometheus()

	// Verify Prometheus format
	if !strings.Contains(prom, "cachefs_hits_total 0") {
		t.Error("expected cachefs_hits_total 0 in Prometheus output")
	}

	if !strings.Contains(prom, "cachefs_misses_total 1") {
		t.Error("expected cachefs_misses_total 1 in Prometheus output")
	}

	if !strings.Contains(prom, "# HELP cachefs_hits_total") {
		t.Error("expected HELP comment in Prometheus output")
	}

	if !strings.Contains(prom, "# TYPE cachefs_hits_total counter") {
		t.Error("expected TYPE comment in Prometheus output")
	}

	if !strings.Contains(prom, "cachefs_max_bytes 2048") {
		t.Error("expected max_bytes in Prometheus output")
	}
}

func TestTakeSnapshot(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Take initial snapshot
	snap1 := cache.TakeSnapshot()

	if snap1.Timestamp.IsZero() {
		t.Error("snapshot timestamp should not be zero")
	}

	if snap1.Stats == nil {
		t.Fatal("snapshot stats should not be nil")
	}

	// Perform operations
	backing.files["/file.txt"] = []byte("test")
	file, _ := cache.OpenFile("/file.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 4)
	file.Read(buf)
	file.Close()

	// Take second snapshot
	time.Sleep(1 * time.Millisecond) // Ensure different timestamp
	snap2 := cache.TakeSnapshot()

	if !snap2.Timestamp.After(snap1.Timestamp) {
		t.Error("second snapshot should have later timestamp")
	}

	if snap2.Stats.Entries != 1 {
		t.Errorf("expected 1 entry in second snapshot, got %d", snap2.Stats.Entries)
	}
}

func TestCompareSnapshots(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Take initial snapshot
	snap1 := cache.TakeSnapshot()

	// Perform operations
	for i := 0; i < 5; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("test data")
		file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
		buf := make([]byte, 9)
		file.Read(buf)
		file.Close()
	}

	// Take second snapshot
	snap2 := cache.TakeSnapshot()

	// Compare snapshots
	delta := CompareSnapshots(snap1, snap2)

	if delta["hits"] != 0 {
		t.Errorf("expected 0 additional hits, got %d", delta["hits"])
	}

	if delta["misses"] != 5 {
		t.Errorf("expected 5 additional misses, got %d", delta["misses"])
	}

	if delta["entries"] != 5 {
		t.Errorf("expected 5 additional entries, got %d", delta["entries"])
	}
}

func TestWriteModeString(t *testing.T) {
	tests := []struct {
		mode     WriteMode
		expected string
	}{
		{WriteModeWriteThrough, "write-through"},
		{WriteModeWriteBack, "write-back"},
		{WriteModeWriteAround, "write-around"},
	}

	for _, tt := range tests {
		result := writeModeString(tt.mode)
		if result != tt.expected {
			t.Errorf("writeModeString(%v) = %s, want %s", tt.mode, result, tt.expected)
		}
	}
}

func TestEvictionPolicyString(t *testing.T) {
	tests := []struct {
		policy   EvictionPolicy
		expected string
	}{
		{EvictionLRU, "lru"},
		{EvictionLFU, "lfu"},
		{EvictionTTL, "ttl"},
		{EvictionHybrid, "hybrid"},
	}

	for _, tt := range tests {
		result := evictionPolicyString(tt.policy)
		if result != tt.expected {
			t.Errorf("evictionPolicyString(%v) = %s, want %s", tt.policy, result, tt.expected)
		}
	}
}

func TestDirtyEntriesCount(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithWriteMode(WriteModeWriteBack),
		WithFlushInterval(1000000),
		WithFlushOnClose(false),
	)

	// Write to multiple files
	for i := 0; i < 3; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		file, _ := cache.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
		file.Write([]byte("dirty data"))
		file.Close()
	}

	// Get stats
	stats := cache.DetailedStats()

	if stats.DirtyEntries != 3 {
		t.Errorf("expected 3 dirty entries, got %d", stats.DirtyEntries)
	}
}
