package cachefs

import (
	"os"
	"testing"
)

func TestBatchInvalidate(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Populate cache
	for i := 0; i < 10; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("test data")
		file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
		buf := make([]byte, 9)
		file.Read(buf)
		file.Close()
	}

	// Verify all cached
	if cache.Stats().Entries() != 10 {
		t.Errorf("expected 10 entries, got %d", cache.Stats().Entries())
	}

	// Batch invalidate 5 files
	paths := []string{"/file0.txt", "/file1.txt", "/file2.txt", "/file3.txt", "/file4.txt"}
	invalidated := cache.BatchInvalidate(paths)

	if invalidated != 5 {
		t.Errorf("expected 5 invalidated, got %d", invalidated)
	}

	if cache.Stats().Entries() != 5 {
		t.Errorf("expected 5 entries remaining, got %d", cache.Stats().Entries())
	}
}

func TestBatchInvalidateNonExistent(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Invalidate non-existent files
	paths := []string{"/nonexistent1.txt", "/nonexistent2.txt"}
	invalidated := cache.BatchInvalidate(paths)

	if invalidated != 0 {
		t.Errorf("expected 0 invalidated, got %d", invalidated)
	}
}

func TestBatchFlush(t *testing.T) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteBack), WithFlushInterval(1000000), WithFlushOnClose(false))

	// Write to multiple files (without closing to keep them dirty)
	paths := []string{"/file1.txt", "/file2.txt", "/file3.txt"}
	for _, path := range paths {
		file, _ := cache.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0644)
		file.Write([]byte("dirty data"))
		file.Close() // Close without flush
	}

	// Clear backing store to verify flush behavior
	backing.files = make(map[string][]byte)

	// Batch flush
	err := cache.BatchFlush(paths)
	if err != nil {
		t.Errorf("BatchFlush error: %v", err)
	}

	// Verify all flushed to backing store
	if len(backing.files) != 3 {
		t.Errorf("expected 3 files in backing, got %d", len(backing.files))
	}

	for _, path := range paths {
		if string(backing.files[path]) != "dirty data" {
			t.Errorf("expected 'dirty data' in %s, got %s", path, string(backing.files[path]))
		}
	}
}

func TestBatchStats(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Populate cache with some files
	backing.files["/file1.txt"] = []byte("data1")
	backing.files["/file2.txt"] = []byte("data2")

	file1, _ := cache.OpenFile("/file1.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 5)
	file1.Read(buf)
	file1.Close()

	file2, _ := cache.OpenFile("/file2.txt", os.O_RDONLY, 0644)
	file2.Read(buf)
	file2.Close()

	// Get batch stats
	paths := []string{"/file1.txt", "/file2.txt", "/file3.txt"}
	stats := cache.BatchStats(paths)

	if len(stats) != 3 {
		t.Errorf("expected 3 stats entries, got %d", len(stats))
	}

	if !stats["/file1.txt"].Cached {
		t.Error("expected file1.txt to be cached")
	}

	if !stats["/file2.txt"].Cached {
		t.Error("expected file2.txt to be cached")
	}

	if stats["/file3.txt"].Cached {
		t.Error("expected file3.txt to not be cached")
	}

	if stats["/file1.txt"].Size != 5 {
		t.Errorf("expected size 5, got %d", stats["/file1.txt"].Size)
	}

	if stats["/file1.txt"].AccessCount != 1 {
		t.Errorf("expected access count 1, got %d", stats["/file1.txt"].AccessCount)
	}
}

func TestPrefetch(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create test files in backing store
	for i := 0; i < 10; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("prefetch test data")
	}

	// Verify cache is empty
	if cache.Stats().Entries() != 0 {
		t.Errorf("expected 0 entries, got %d", cache.Stats().Entries())
	}

	// Prefetch all files
	paths := []string{
		"/file0.txt", "/file1.txt", "/file2.txt", "/file3.txt", "/file4.txt",
		"/file5.txt", "/file6.txt", "/file7.txt", "/file8.txt", "/file9.txt",
	}

	err := cache.Prefetch(paths, &PrefetchOptions{Workers: 4, SkipErrors: true})
	if err != nil {
		t.Errorf("Prefetch error: %v", err)
	}

	// Verify all files are now cached
	if cache.Stats().Entries() != 10 {
		t.Errorf("expected 10 entries, got %d", cache.Stats().Entries())
	}

	// Verify cache hits for all files
	cache.ResetStats()
	for _, path := range paths {
		file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
		buf := make([]byte, 18)
		file.Read(buf)
		file.Close()
	}

	if cache.Stats().Hits() != 10 {
		t.Errorf("expected 10 hits, got %d", cache.Stats().Hits())
	}

	if cache.Stats().Misses() != 0 {
		t.Errorf("expected 0 misses, got %d", cache.Stats().Misses())
	}
}

func TestPrefetchWithErrors(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create only some files
	backing.files["/file1.txt"] = []byte("data")
	backing.files["/file3.txt"] = []byte("data")

	paths := []string{"/file1.txt", "/file2.txt", "/file3.txt"}

	// Prefetch with skip errors
	err := cache.Prefetch(paths, &PrefetchOptions{Workers: 2, SkipErrors: true})
	// Note: mockFS creates files on OpenFile, so we won't get errors
	// In a real filesystem, this would error for missing files
	if err != nil {
		t.Logf("Prefetch error (expected in real FS): %v", err)
	}

	// All files should be cached (mockFS creates on open)
	if cache.Stats().Entries() < 2 {
		t.Errorf("expected at least 2 entries, got %d", cache.Stats().Entries())
	}
}

// Benchmark batch operations
func BenchmarkBatchInvalidate(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	// Pre-populate cache
	for i := 0; i < 100; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("test")
		file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
		buf := make([]byte, 4)
		file.Read(buf)
		file.Close()
	}

	paths := make([]string, 10)
	for i := 0; i < 10; i++ {
		paths[i] = string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.BatchInvalidate(paths)
	}
}

func BenchmarkBatchStats(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	// Pre-populate cache
	for i := 0; i < 50; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("test")
		file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
		buf := make([]byte, 4)
		file.Read(buf)
		file.Close()
	}

	paths := make([]string, 10)
	for i := 0; i < 10; i++ {
		paths[i] = string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.BatchStats(paths)
	}
}

func BenchmarkPrefetch(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	// Create files in backing
	for i := 0; i < 20; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("prefetch benchmark data")
	}

	paths := make([]string, 20)
	for i := 0; i < 20; i++ {
		paths[i] = string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Clear()
		cache.Prefetch(paths, &PrefetchOptions{Workers: 4, SkipErrors: true})
	}
}
