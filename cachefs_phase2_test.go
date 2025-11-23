package cachefs

import (
	"os"
	"testing"
	"time"
)

func TestEvictionLFU(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithEvictionPolicy(EvictionLFU),
		WithMaxEntries(2),
	)

	// Create 3 files
	backing.files["/file1.txt"] = []byte("A")
	backing.files["/file2.txt"] = []byte("B")
	backing.files["/file3.txt"] = []byte("C")

	// Read file1 once
	file1, _ := cache.OpenFile("/file1.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 1)
	file1.Read(buf)
	file1.Close()

	// Read file2 multiple times (higher frequency)
	for i := 0; i < 5; i++ {
		file2, _ := cache.OpenFile("/file2.txt", os.O_RDONLY, 0644)
		buf := make([]byte, 1)
		file2.Read(buf)
		file2.Close()
	}

	// Read file3 - should evict file1 (lowest frequency)
	file3, _ := cache.OpenFile("/file3.txt", os.O_RDONLY, 0644)
	buf3 := make([]byte, 1)
	file3.Read(buf3)
	file3.Close()

	// Check that we have at most 2 entries
	if cache.stats.Entries() > 2 {
		t.Errorf("expected at most 2 entries, got %d", cache.stats.Entries())
	}

	// Check that at least one eviction occurred
	if cache.stats.Evictions() < 1 {
		t.Errorf("expected at least 1 eviction, got %d", cache.stats.Evictions())
	}

	// file1 should not be in cache (lowest frequency)
	cache.mu.RLock()
	_, file1Exists := cache.entries["/file1.txt"]
	cache.mu.RUnlock()

	if file1Exists {
		t.Error("file1 should have been evicted (lowest frequency)")
	}
}

func TestEvictionTTL(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithEvictionPolicy(EvictionTTL),
		WithTTL(50*time.Millisecond),
		WithMaxEntries(2),
	)

	// Create 2 files
	backing.files["/file1.txt"] = []byte("A")
	backing.files["/file2.txt"] = []byte("B")

	// Read file1
	file1, _ := cache.OpenFile("/file1.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 1)
	file1.Read(buf)
	file1.Close()

	// Wait a bit
	time.Sleep(30 * time.Millisecond)

	// Read file2
	file2, _ := cache.OpenFile("/file2.txt", os.O_RDONLY, 0644)
	buf2 := make([]byte, 1)
	file2.Read(buf2)
	file2.Close()

	// Wait for file1 to expire
	time.Sleep(30 * time.Millisecond)

	// Read file1 again - should be a miss due to expiration
	initialMisses := cache.stats.Misses()
	file1Again, _ := cache.OpenFile("/file1.txt", os.O_RDONLY, 0644)
	buf1Again := make([]byte, 1)
	file1Again.Read(buf1Again)
	file1Again.Close()

	// Should have triggered a cache miss
	if cache.stats.Misses() <= initialMisses {
		t.Error("expected cache miss after TTL expiration")
	}
}

func TestEvictionHybrid(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithEvictionPolicy(EvictionHybrid),
		WithMaxEntries(3),
	)

	// Create and read multiple files
	for i := 1; i <= 4; i++ {
		name := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[name] = []byte{byte('A' + i - 1)}

		// File 2 gets accessed more frequently
		accessCount := 1
		if i == 2 {
			accessCount = 5
		}

		for j := 0; j < accessCount; j++ {
			file, _ := cache.OpenFile(name, os.O_RDONLY, 0644)
			buf := make([]byte, 1)
			file.Read(buf)
			file.Close()
		}
	}

	// Should have evicted at least one entry
	if cache.stats.Evictions() < 1 {
		t.Errorf("expected at least 1 eviction, got %d", cache.stats.Evictions())
	}

	// File 2 should still be in cache (high access count)
	cache.mu.RLock()
	_, file2Exists := cache.entries["/file2.txt"]
	cache.mu.RUnlock()

	if !file2Exists {
		t.Error("file2 should still be in cache (high access count)")
	}
}

func TestCleanExpired(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithTTL(50*time.Millisecond),
	)

	// Add file to cache
	backing.files["/file.txt"] = []byte("test")
	file, _ := cache.OpenFile("/file.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 4)
	file.Read(buf)
	file.Close()

	// Should have 1 entry
	if cache.stats.Entries() != 1 {
		t.Errorf("expected 1 entry, got %d", cache.stats.Entries())
	}

	// Wait for expiration
	time.Sleep(60 * time.Millisecond)

	// Clean expired entries
	cache.CleanExpired()

	// Should have 0 entries now
	if cache.stats.Entries() != 0 {
		t.Errorf("expected 0 entries after cleaning expired, got %d", cache.stats.Entries())
	}
}

func TestMetadataCache(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithMetadataCache(true),
		WithMetadataMaxEntries(2),
	)

	// First Stat call - should be a cache miss
	_, err := cache.Stat("/file.txt")
	if err == nil {
		t.Error("expected error for non-existent file")
	}

	// With metadata caching disabled, it should just delegate
	cache2 := New(backing, WithMetadataCache(false))
	_, err = cache2.Stat("/file.txt")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestMetadataCacheEviction(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithMetadataCache(true),
		WithMetadataMaxEntries(2),
	)

	// Try to cache metadata for 3 files (more than limit)
	for i := 1; i <= 3; i++ {
		name := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		cache.Stat(name) // Will fail but still gets cached as attempt
	}

	// Should have at most 2 metadata entries due to eviction
	cache.mu.RLock()
	metaCount := len(cache.metadataEntries)
	cache.mu.RUnlock()

	if metaCount > 2 {
		t.Errorf("expected at most 2 metadata entries, got %d", metaCount)
	}
}

func TestInvalidatePrefix(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create files with different prefixes
	backing.files["/data/file1.txt"] = []byte("A")
	backing.files["/data/file2.txt"] = []byte("B")
	backing.files["/other/file3.txt"] = []byte("C")

	// Read all files to cache them
	for _, name := range []string{"/data/file1.txt", "/data/file2.txt", "/other/file3.txt"} {
		file, _ := cache.OpenFile(name, os.O_RDONLY, 0644)
		buf := make([]byte, 1)
		file.Read(buf)
		file.Close()
	}

	// Should have 3 entries
	if cache.stats.Entries() != 3 {
		t.Errorf("expected 3 entries, got %d", cache.stats.Entries())
	}

	// Invalidate /data prefix
	cache.InvalidatePrefix("/data")

	// Should have 1 entry left
	if cache.stats.Entries() != 1 {
		t.Errorf("expected 1 entry after prefix invalidation, got %d", cache.stats.Entries())
	}

	// Check that /other/file3.txt is still cached
	cache.mu.RLock()
	_, exists := cache.entries["/other/file3.txt"]
	cache.mu.RUnlock()

	if !exists {
		t.Error("/other/file3.txt should still be in cache")
	}
}

func TestInvalidatePattern(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create files
	backing.files["/file1.txt"] = []byte("A")
	backing.files["/file2.txt"] = []byte("B")
	backing.files["/file3.log"] = []byte("C")

	// Read all files to cache them
	for _, name := range []string{"/file1.txt", "/file2.txt", "/file3.log"} {
		file, _ := cache.OpenFile(name, os.O_RDONLY, 0644)
		buf := make([]byte, 1)
		file.Read(buf)
		file.Close()
	}

	// Should have 3 entries
	if cache.stats.Entries() != 3 {
		t.Errorf("expected 3 entries, got %d", cache.stats.Entries())
	}

	// Invalidate /*.txt pattern (full path pattern)
	err := cache.InvalidatePattern("/*.txt")
	if err != nil {
		t.Fatalf("InvalidatePattern failed: %v", err)
	}

	// Should have 1 entry left (.log file)
	if cache.stats.Entries() != 1 {
		t.Errorf("expected 1 entry after pattern invalidation, got %d", cache.stats.Entries())
	}

	// Check that .log file is still cached
	cache.mu.RLock()
	_, exists := cache.entries["/file3.log"]
	cache.mu.RUnlock()

	if !exists {
		t.Error("/file3.log should still be in cache")
	}
}

func TestTTLExpiration(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithTTL(50*time.Millisecond),
	)

	// Add file to cache
	backing.files["/file.txt"] = []byte("test data")
	file, _ := cache.OpenFile("/file.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 9)
	file.Read(buf)
	file.Close()

	// Should be a cache hit immediately
	initialHits := cache.stats.Hits()
	file2, _ := cache.OpenFile("/file.txt", os.O_RDONLY, 0644)
	buf2 := make([]byte, 9)
	file2.Read(buf2)
	file2.Close()

	if cache.stats.Hits() <= initialHits {
		t.Error("expected cache hit")
	}

	// Wait for TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Should be a cache miss now
	initialMisses := cache.stats.Misses()
	file3, _ := cache.OpenFile("/file.txt", os.O_RDONLY, 0644)
	buf3 := make([]byte, 9)
	file3.Read(buf3)
	file3.Close()

	// Eviction should have removed the expired entry
	if cache.stats.Misses() <= initialMisses {
		t.Error("expected cache miss after TTL expiration")
	}
}
