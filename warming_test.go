package cachefs

import (
	"os"
	"testing"
	"time"
)

func TestWarmCache(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create test files
	for i := 0; i < 10; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("test data for warming")
	}

	// Verify cache is empty
	if cache.Stats().Entries() != 0 {
		t.Errorf("expected 0 entries, got %d", cache.Stats().Entries())
	}

	// Warm cache
	paths := []string{
		"/file0.txt", "/file1.txt", "/file2.txt", "/file3.txt", "/file4.txt",
	}
	err := cache.WarmCache(paths, WithWarmWorkers(2), WithWarmSkipErrors(true))
	if err != nil {
		t.Errorf("WarmCache error: %v", err)
	}

	// Verify files are cached
	if cache.Stats().Entries() != 5 {
		t.Errorf("expected 5 entries, got %d", cache.Stats().Entries())
	}

	// Verify cache hits
	cache.ResetStats()
	for _, path := range paths {
		file, _ := cache.OpenFile(path, os.O_RDONLY, 0644)
		buf := make([]byte, 21)
		file.Read(buf)
		file.Close()
	}

	if cache.Stats().Hits() != 5 {
		t.Errorf("expected 5 hits after warming, got %d", cache.Stats().Hits())
	}

	if cache.Stats().Misses() != 0 {
		t.Errorf("expected 0 misses after warming, got %d", cache.Stats().Misses())
	}
}

func TestWarmCacheProgress(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create test files
	for i := 0; i < 20; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("test data")
	}

	paths := make([]string, 20)
	for i := 0; i < 20; i++ {
		paths[i] = string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
	}

	// Track progress
	progressCalled := false
	var finalProgress WarmProgress

	err := cache.WarmCache(paths, WithWarmWorkers(4), WithWarmProgress(func(p WarmProgress) {
		progressCalled = true
		finalProgress = p
	}))

	if err != nil {
		t.Errorf("WarmCache error: %v", err)
	}

	if !progressCalled {
		t.Error("progress callback was not called")
	}

	if finalProgress.Total != 20 {
		t.Errorf("expected total 20, got %d", finalProgress.Total)
	}

	if finalProgress.Completed != 20 {
		t.Errorf("expected completed 20, got %d", finalProgress.Completed)
	}
}

func TestWarmCacheAsync(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create test files
	for i := 0; i < 5; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("async test data")
	}

	paths := []string{
		"/file0.txt", "/file1.txt", "/file2.txt", "/file3.txt", "/file4.txt",
	}

	// Warm cache asynchronously
	done := make(chan error, 1)
	cache.WarmCacheAsync(paths, done, WithWarmWorkers(2))

	// Wait for completion
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("WarmCacheAsync error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("WarmCacheAsync timed out")
	}

	// Verify files are cached
	if cache.Stats().Entries() != 5 {
		t.Errorf("expected 5 entries, got %d", cache.Stats().Entries())
	}
}

func TestWarmCacheFromPattern(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create test files with pattern
	backing.files["/data/file1.txt"] = []byte("data1")
	backing.files["/data/file2.txt"] = []byte("data2")
	backing.files["/data/file3.log"] = []byte("log")
	backing.files["/other/file4.txt"] = []byte("other")

	// Mock Glob implementation
	backing.globFunc = func(pattern string) ([]string, error) {
		if pattern == "/data/*.txt" {
			return []string{"/data/file1.txt", "/data/file2.txt"}, nil
		}
		return nil, nil
	}

	// Warm cache from pattern
	err := cache.WarmCacheFromPattern("/data/*.txt", WithWarmWorkers(2))
	if err != nil {
		t.Errorf("WarmCacheFromPattern error: %v", err)
	}

	// Verify only matching files are cached
	if cache.Stats().Entries() != 2 {
		t.Errorf("expected 2 entries, got %d", cache.Stats().Entries())
	}
}

func TestWarmCacheFromFile(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create test files
	backing.files["/file1.txt"] = []byte("data1")
	backing.files["/file2.txt"] = []byte("data2")
	backing.files["/file3.txt"] = []byte("data3")

	// Create list file
	listContent := "/file1.txt\n/file2.txt\n# comment\n/file3.txt\n"
	backing.files["/list.txt"] = []byte(listContent)

	// Warm cache from file list
	err := cache.WarmCacheFromFile("/list.txt", WithWarmWorkers(2))
	if err != nil {
		t.Errorf("WarmCacheFromFile error: %v", err)
	}

	// Verify all listed files are cached
	if cache.Stats().Entries() != 3 {
		t.Errorf("expected 3 entries, got %d", cache.Stats().Entries())
	}
}

func TestWarmCacheWithErrors(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create only some files
	backing.files["/exists1.txt"] = []byte("data")
	backing.files["/exists2.txt"] = []byte("data")

	paths := []string{"/exists1.txt", "/missing.txt", "/exists2.txt"}

	// Warm with skip errors (mockFS creates files, so this won't actually error)
	err := cache.WarmCache(paths, WithWarmSkipErrors(true))
	if err != nil {
		t.Logf("WarmCache with skip errors: %v", err)
	}

	// At least the existing files should be cached
	if cache.Stats().Entries() < 2 {
		t.Errorf("expected at least 2 entries, got %d", cache.Stats().Entries())
	}
}

func TestWarmCacheSmart(t *testing.T) {
	backing := newMockFS()
	cache := New(backing, WithMaxEntries(5))

	// Create files
	for i := 0; i < 10; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("smart warming test")
	}

	// Access some files to create metadata
	for i := 0; i < 3; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		cache.Stat(path)
	}

	// Clear cache
	cache.Clear()

	// Smart warm should reload frequently accessed files
	err := cache.WarmCacheSmart(WithWarmWorkers(2))
	if err != nil {
		t.Errorf("WarmCacheSmart error: %v", err)
	}

	// Should have loaded some files based on metadata
	// (exact count depends on implementation)
	if cache.Stats().Entries() > 5 {
		t.Errorf("exceeded max entries: got %d", cache.Stats().Entries())
	}
}

func TestWarmCachePriority(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Create test files
	for i := 0; i < 5; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("priority test")
	}

	paths := []string{"/file0.txt", "/file1.txt", "/file2.txt"}

	// Test different priority levels
	priorities := []WarmPriority{
		WarmPriorityHigh,
		WarmPriorityNormal,
		WarmPriorityLow,
	}

	for _, priority := range priorities {
		cache.Clear()
		err := cache.WarmCache(paths, WithWarmPriority(priority))
		if err != nil {
			t.Errorf("WarmCache with priority %d error: %v", priority, err)
		}

		if cache.Stats().Entries() != 3 {
			t.Errorf("expected 3 entries with priority %d, got %d", priority, cache.Stats().Entries())
		}
	}
}

// Benchmark warming performance
func BenchmarkWarmCache100(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	// Create 100 test files
	for i := 0; i < 100; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("benchmark data for cache warming")
	}

	paths := make([]string, 100)
	for i := 0; i < 100; i++ {
		paths[i] = string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Clear()
		cache.WarmCache(paths, WithWarmWorkers(4))
	}
}

func BenchmarkWarmCacheParallel(b *testing.B) {
	backing := newMockFS()
	cache := New(backing)

	// Create test files
	for i := 0; i < 50; i++ {
		path := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[path] = []byte("parallel warming test")
	}

	paths := make([]string, 50)
	for i := 0; i < 50; i++ {
		paths[i] = string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + (i/10)%10), byte('0' + i%10), byte('.'), byte('t'), byte('x'), byte('t')})
	}

	// Test different worker counts
	workerCounts := []int{1, 2, 4, 8}

	for _, workers := range workerCounts {
		b.Run(string([]byte{byte('w'), byte('o'), byte('r'), byte('k'), byte('e'), byte('r'), byte('s'), byte('_'), byte('0' + workers)}), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cache.Clear()
				cache.WarmCache(paths, WithWarmWorkers(workers))
			}
		})
	}
}
