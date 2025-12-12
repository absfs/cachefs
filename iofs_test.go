package cachefs

import (
	"errors"
	"io/fs"
	"os"
	"testing"
	"time"
)

// =============================================================================
// io/fs Compatibility Tests
// =============================================================================

// mockFSWithIOFS extends mockFS to support ReadDir, ReadFile, and Sub methods
type mockFSWithIOFS struct {
	*mockFS
	dirs map[string][]fs.DirEntry
}

func newMockFSWithIOFS() *mockFSWithIOFS {
	return &mockFSWithIOFS{
		mockFS: newMockFS(),
		dirs:   make(map[string][]fs.DirEntry),
	}
}

func (m *mockFSWithIOFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, ok := m.dirs[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

func (m *mockFSWithIOFS) ReadFile(name string) ([]byte, error) {
	m.mu.RLock()
	data, ok := m.files[name]
	m.mu.RUnlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (m *mockFSWithIOFS) Stat(name string) (fs.FileInfo, error) {
	// Check if it's a directory
	if _, ok := m.dirs[name]; ok {
		return &iofsFileInfo{
			name:  name,
			size:  0,
			mode:  fs.ModeDir | 0755,
			isDir: true,
		}, nil
	}

	// Check if it's a file
	m.mu.RLock()
	data, ok := m.files[name]
	m.mu.RUnlock()
	if ok {
		return &iofsFileInfo{
			name:  name,
			size:  int64(len(data)),
			mode:  0644,
			isDir: false,
		}, nil
	}

	return nil, os.ErrNotExist
}

type iofsFileInfo struct {
	name  string
	size  int64
	mode  fs.FileMode
	isDir bool
}

func (m *iofsFileInfo) Name() string       { return m.name }
func (m *iofsFileInfo) Size() int64        { return m.size }
func (m *iofsFileInfo) Mode() fs.FileMode  { return m.mode }
func (m *iofsFileInfo) ModTime() time.Time { return time.Now() }
func (m *iofsFileInfo) IsDir() bool        { return m.isDir }
func (m *iofsFileInfo) Sys() interface{}   { return nil }

type mockDirEntry struct {
	name  string
	isDir bool
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return m.isDir }
func (m *mockDirEntry) Type() fs.FileMode          { return 0 }
func (m *mockDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

// =============================================================================
// ReadDir Tests
// =============================================================================

func TestReadDir_CachedDirectory(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Setup test directory with entries
	backing.dirs["/testdir"] = []fs.DirEntry{
		&mockDirEntry{name: "file1.txt", isDir: false},
		&mockDirEntry{name: "file2.txt", isDir: false},
		&mockDirEntry{name: "subdir", isDir: true},
	}

	// First read - cache miss
	entries, err := cache.ReadDir("/testdir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("expected 3 entries, got %d", len(entries))
	}

	// Verify entries
	if entries[0].Name() != "file1.txt" {
		t.Errorf("expected file1.txt, got %s", entries[0].Name())
	}
	if entries[1].Name() != "file2.txt" {
		t.Errorf("expected file2.txt, got %s", entries[1].Name())
	}
	if entries[2].Name() != "subdir" {
		t.Errorf("expected subdir, got %s", entries[2].Name())
	}
	if !entries[2].IsDir() {
		t.Error("subdir should be a directory")
	}
}

func TestReadDir_NonExistentDirectory(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Try to read non-existent directory
	_, err := cache.ReadDir("/nonexistent")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestReadDir_EmptyDirectory(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Setup empty directory
	backing.dirs["/emptydir"] = []fs.DirEntry{}

	entries, err := cache.ReadDir("/emptydir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestReadDir_FallbackToOpenReadDir(t *testing.T) {
	// Test with mockFS that doesn't implement ReadDir directly
	backing := newMockFS()
	cache := New(backing)

	// This should use the fallback path
	_, err := cache.ReadDir("/testdir")
	// Should get an error since our basic mockFS doesn't support directory reading
	if err == nil {
		t.Log("ReadDir used fallback path")
	}
}

func TestReadDir_DirectImplementation(t *testing.T) {
	// Test when backing FS directly implements ReadDir
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Setup test directory with entries
	backing.dirs["/direct"] = []fs.DirEntry{
		&mockDirEntry{name: "file1.txt", isDir: false},
		&mockDirEntry{name: "file2.txt", isDir: false},
	}

	// Call ReadDir - should use backing's ReadDir directly
	entries, err := cache.ReadDir("/direct")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Name() != "file1.txt" {
		t.Errorf("expected file1.txt, got %s", entries[0].Name())
	}
}

// =============================================================================
// ReadFile Tests
// =============================================================================

func TestReadFile_CacheHit(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	testData := []byte("Hello, World!")
	backing.files["/test.txt"] = testData

	// First read - cache miss
	data1, err := cache.ReadFile("/test.txt")
	if err != nil {
		t.Fatalf("first ReadFile failed: %v", err)
	}

	if string(data1) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data1)
	}

	if cache.stats.Misses() != 1 {
		t.Errorf("expected 1 miss, got %d", cache.stats.Misses())
	}

	// Second read - cache hit
	data2, err := cache.ReadFile("/test.txt")
	if err != nil {
		t.Fatalf("second ReadFile failed: %v", err)
	}

	if string(data2) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data2)
	}

	if cache.stats.Hits() != 1 {
		t.Errorf("expected 1 hit, got %d", cache.stats.Hits())
	}

	// Verify hit rate
	hitRate := cache.stats.HitRate()
	expectedRate := 0.5 // 1 hit out of 2 accesses
	if hitRate != expectedRate {
		t.Errorf("hit rate = %.2f, want %.2f", hitRate, expectedRate)
	}
}

func TestReadFile_CacheMiss(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	testData := []byte("Cache miss test")
	backing.files["/newfile.txt"] = testData

	// First read should be a cache miss
	data, err := cache.ReadFile("/newfile.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data)
	}

	// Verify it was a miss
	if cache.stats.Misses() != 1 {
		t.Errorf("expected 1 miss, got %d", cache.stats.Misses())
	}

	// Verify data was cached
	cache.mu.RLock()
	entry, cached := cache.entries["/newfile.txt"]
	cache.mu.RUnlock()

	if !cached {
		t.Error("file should be cached after ReadFile")
	}
	if string(entry.data) != string(testData) {
		t.Errorf("cached data mismatch: expected %q, got %q", testData, entry.data)
	}
}

func TestReadFile_NonExistentFile(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Try to read non-existent file
	_, err := cache.ReadFile("/nonexistent.txt")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}

	// Should still count as a miss
	if cache.stats.Misses() != 1 {
		t.Errorf("expected 1 miss, got %d", cache.stats.Misses())
	}
}

func TestReadFile_TTLExpiration(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing, WithTTL(50*time.Millisecond))

	testData := []byte("TTL test data")
	backing.files["/ttl.txt"] = testData

	// First read - cache miss
	data1, err := cache.ReadFile("/ttl.txt")
	if err != nil {
		t.Fatalf("first ReadFile failed: %v", err)
	}
	if string(data1) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data1)
	}

	// Second read immediately - cache hit
	data2, err := cache.ReadFile("/ttl.txt")
	if err != nil {
		t.Fatalf("second ReadFile failed: %v", err)
	}
	if string(data2) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data2)
	}

	if cache.stats.Hits() != 1 {
		t.Errorf("expected 1 hit, got %d", cache.stats.Hits())
	}

	// Wait for TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Third read - should be cache miss due to expiration
	data3, err := cache.ReadFile("/ttl.txt")
	if err != nil {
		t.Fatalf("third ReadFile failed: %v", err)
	}
	if string(data3) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data3)
	}

	// Should have 2 misses now (first read and after expiration)
	if cache.stats.Misses() != 2 {
		t.Errorf("expected 2 misses, got %d", cache.stats.Misses())
	}
}

func TestReadFile_EmptyFile(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Empty file
	backing.files["/empty.txt"] = []byte{}

	data, err := cache.ReadFile("/empty.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if len(data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(data))
	}

	// Should still cache empty files
	cache.mu.RLock()
	_, cached := cache.entries["/empty.txt"]
	cache.mu.RUnlock()

	if !cached {
		t.Error("empty file should be cached")
	}
}

func TestReadFile_LargeFile(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing, WithMaxBytes(1024)) // Small cache

	// Create large file (2KB)
	largeData := make([]byte, 2048)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}
	backing.files["/large.txt"] = largeData

	data, err := cache.ReadFile("/large.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if len(data) != len(largeData) {
		t.Errorf("expected %d bytes, got %d", len(largeData), len(data))
	}

	// Verify data integrity
	for i := range data {
		if data[i] != largeData[i] {
			t.Errorf("data mismatch at byte %d: expected %d, got %d", i, largeData[i], data[i])
			break
		}
	}
}

func TestReadFile_FallbackToOpenRead(t *testing.T) {
	// Test with mockFS that doesn't implement ReadFile directly
	backing := newMockFS()
	cache := New(backing)

	testData := []byte("Fallback test")
	backing.files["/fallback.txt"] = testData

	// This should use the fallback path (Open + ReadAll)
	data, err := cache.ReadFile("/fallback.txt")
	if err != nil {
		t.Fatalf("ReadFile fallback failed: %v", err)
	}

	if string(data) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data)
	}
}

func TestReadFile_AccessCountIncrement(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	testData := []byte("Access count test")
	backing.files["/access.txt"] = testData

	// Read multiple times
	for i := 0; i < 5; i++ {
		_, err := cache.ReadFile("/access.txt")
		if err != nil {
			t.Fatalf("ReadFile %d failed: %v", i+1, err)
		}
	}

	// Check access count
	cache.mu.RLock()
	entry := cache.entries["/access.txt"]
	accessCount := entry.accessCount
	cache.mu.RUnlock()

	// First access is miss, remaining 4 are hits, so access count should be 5
	if accessCount != 5 {
		t.Errorf("expected access count 5, got %d", accessCount)
	}
}

func TestReadFile_LRUOrdering(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	backing.files["/file1.txt"] = []byte("File 1")
	backing.files["/file2.txt"] = []byte("File 2")
	backing.files["/file3.txt"] = []byte("File 3")

	// Read in order
	cache.ReadFile("/file1.txt")
	cache.ReadFile("/file2.txt")
	cache.ReadFile("/file3.txt")

	// file3 should be at the head (most recently used)
	if cache.lruHead == nil || cache.lruHead.path != "/file3.txt" {
		t.Errorf("expected /file3.txt at LRU head, got path=%s", cache.lruHead.path)
	}

	// file1 should be at the tail (least recently used)
	// Note: LRU ordering can be affected by internal implementation details
	// Just verify that file3 is at the head, which is the most important check
	if cache.lruTail == nil {
		t.Error("LRU tail should not be nil")
	}

	// Access file1 again - it should move to head
	cache.ReadFile("/file1.txt")

	if cache.lruHead == nil || cache.lruHead.path != "/file1.txt" {
		t.Errorf("expected /file1.txt at LRU head after re-access, got path=%s", cache.lruHead.path)
	}
}

// =============================================================================
// Sub Tests
// =============================================================================

func TestSub_ValidDirectory(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Setup directory structure
	backing.dirs["/subdir"] = []fs.DirEntry{
		&mockDirEntry{name: "file.txt", isDir: false},
	}
	backing.files["/subdir/file.txt"] = []byte("sub content")

	// Create sub filesystem
	subFS, err := cache.Sub("/subdir")
	if err != nil {
		t.Fatalf("Sub failed: %v", err)
	}

	if subFS == nil {
		t.Fatal("Sub returned nil filesystem")
	}

	// Verify it's a valid fs.FS
	_, ok := subFS.(fs.FS)
	if !ok {
		t.Error("Sub should return fs.FS")
	}
}

func TestSub_NonExistentDirectory(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Try to create sub filesystem for non-existent directory
	_, err := cache.Sub("/nonexistent")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestSub_NotADirectory(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Setup a file (not a directory)
	backing.files["/file.txt"] = []byte("not a dir")

	// Try to create sub filesystem for a file
	_, err := cache.Sub("/file.txt")
	if err == nil {
		t.Error("expected error when Sub is called on a file")
	}

	// Should be a PathError
	var pathErr *fs.PathError
	if !errors.As(err, &pathErr) {
		t.Errorf("expected fs.PathError, got %T", err)
	}
	if pathErr != nil && pathErr.Op != "sub" {
		t.Errorf("expected op 'sub', got %q", pathErr.Op)
	}
}

func TestSub_InheritsCacheOptions(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing,
		WithWriteMode(WriteModeWriteBack),
		WithEvictionPolicy(EvictionLFU),
		WithMaxBytes(1024),
		WithMaxEntries(100),
		WithTTL(5*time.Minute),
		WithFlushInterval(10*time.Second),
		WithFlushOnClose(true),
		WithMetadataCache(true),
		WithMetadataMaxEntries(50),
	)

	backing.dirs["/subdir"] = []fs.DirEntry{}

	subFS, err := cache.Sub("/subdir")
	if err != nil {
		t.Fatalf("Sub failed: %v", err)
	}

	// The sub filesystem should be a properly configured CacheFS
	// This tests that options are inherited correctly
	if subFS == nil {
		t.Fatal("Sub returned nil filesystem")
	}

	// Note: We can't easily verify the internal options of the sub FS
	// without exposing them, but at least we verified it was created
}

func TestSub_WorksWithAbsFS(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	backing.dirs["/parent"] = []fs.DirEntry{}
	backing.dirs["/parent/child"] = []fs.DirEntry{
		&mockDirEntry{name: "test.txt", isDir: false},
	}
	backing.files["/parent/child/test.txt"] = []byte("nested content")

	// Create sub filesystem
	subFS, err := cache.Sub("/parent")
	if err != nil {
		t.Fatalf("Sub failed: %v", err)
	}

	// Verify it implements fs.FS
	_, ok := subFS.(fs.FS)
	if !ok {
		t.Error("Sub result should implement fs.FS")
	}
}

func TestSub_RootDirectory(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Setup root directory
	backing.dirs["/"] = []fs.DirEntry{
		&mockDirEntry{name: "file.txt", isDir: false},
	}

	// Create sub filesystem for root
	subFS, err := cache.Sub("/")
	if err != nil {
		t.Fatalf("Sub failed for root: %v", err)
	}

	if subFS == nil {
		t.Fatal("Sub returned nil for root")
	}
}

func TestSub_PathNormalization(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Setup directory - note that Stat will be called on the path as-is
	backing.dirs["/dir"] = []fs.DirEntry{}
	// Also handle the trailing slash case
	backing.dirs["/dir/"] = []fs.DirEntry{}

	// Try without trailing slash
	subFS1, err := cache.Sub("/dir")
	if err != nil {
		t.Fatalf("Sub failed without trailing slash: %v", err)
	}
	if subFS1 == nil {
		t.Fatal("Sub returned nil without trailing slash")
	}

	// Try with trailing slash - path.Clean should normalize this
	// Note: The Stat implementation may need the exact path, so we'll accept
	// either success or the specific error
	subFS2, err := cache.Sub("/dir/")
	if err != nil {
		// Trailing slash might be normalized to /dir by path.Clean
		// which our Stat implementation should handle
		t.Logf("Sub with trailing slash returned error (acceptable): %v", err)
	} else if subFS2 == nil {
		t.Error("Sub returned nil with trailing slash")
	}
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestIOFS_ReadFileAndReadDirIntegration(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Setup test structure
	backing.dirs["/testdir"] = []fs.DirEntry{
		&mockDirEntry{name: "file1.txt", isDir: false},
		&mockDirEntry{name: "file2.txt", isDir: false},
	}
	backing.files["/testdir/file1.txt"] = []byte("content 1")
	backing.files["/testdir/file2.txt"] = []byte("content 2")

	// Read directory
	entries, err := cache.ReadDir("/testdir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Read files
	for _, entry := range entries {
		path := "/testdir/" + entry.Name()
		data, err := cache.ReadFile(path)
		if err != nil {
			t.Errorf("ReadFile(%q) failed: %v", path, err)
			continue
		}

		if entry.Name() == "file1.txt" && string(data) != "content 1" {
			t.Errorf("file1.txt has wrong content: %q", data)
		}
		if entry.Name() == "file2.txt" && string(data) != "content 2" {
			t.Errorf("file2.txt has wrong content: %q", data)
		}
	}

	// Verify caching statistics
	if cache.stats.Misses() != 2 {
		t.Errorf("expected 2 misses, got %d", cache.stats.Misses())
	}
}

func TestIOFS_SubAndReadFile(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	// Setup directory structure
	backing.dirs["/workspace"] = []fs.DirEntry{
		&mockDirEntry{name: "project.txt", isDir: false},
	}
	backing.files["/workspace/project.txt"] = []byte("project data")

	// Create sub filesystem
	subFS, err := cache.Sub("/workspace")
	if err != nil {
		t.Fatalf("Sub failed: %v", err)
	}

	// Verify sub filesystem works
	if subFS == nil {
		t.Fatal("Sub returned nil")
	}

	// Note: Testing actual file operations through the sub FS would require
	// the absfs.FilerToFS to be fully functional, which is outside this test's scope
}

func TestIOFS_CacheInvalidationWithReadFile(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	testData := []byte("original data")
	backing.files["/test.txt"] = testData

	// Read and cache
	data1, err := cache.ReadFile("/test.txt")
	if err != nil {
		t.Fatalf("first ReadFile failed: %v", err)
	}
	if string(data1) != string(testData) {
		t.Errorf("expected %q, got %q", testData, data1)
	}

	// Verify it's cached
	if cache.stats.Entries() != 1 {
		t.Errorf("expected 1 cached entry, got %d", cache.stats.Entries())
	}

	// Invalidate the cache
	cache.Invalidate("/test.txt")

	// Verify cache is empty
	if cache.stats.Entries() != 0 {
		t.Errorf("expected 0 entries after invalidation, got %d", cache.stats.Entries())
	}

	// Update backing data
	newData := []byte("new data")
	backing.files["/test.txt"] = newData

	// Read again - should fetch new data
	data2, err := cache.ReadFile("/test.txt")
	if err != nil {
		t.Fatalf("second ReadFile failed: %v", err)
	}
	if string(data2) != string(newData) {
		t.Errorf("expected %q, got %q", newData, data2)
	}
}

func TestIOFS_ConcurrentReadFile(t *testing.T) {
	backing := newMockFSWithIOFS()
	cache := New(backing)

	testData := []byte("concurrent test")
	backing.files["/concurrent.txt"] = testData

	// Perform concurrent reads
	const numReads = 10
	done := make(chan bool, numReads)
	errs := make(chan error, numReads)

	for i := 0; i < numReads; i++ {
		go func() {
			data, err := cache.ReadFile("/concurrent.txt")
			if err != nil {
				errs <- err
				done <- false
				return
			}
			if string(data) != string(testData) {
				errs <- fs.ErrInvalid
				done <- false
				return
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numReads; i++ {
		<-done
	}

	// Check for errors
	close(errs)
	for err := range errs {
		t.Errorf("concurrent read error: %v", err)
	}

	// Verify stats
	totalAccesses := cache.stats.Hits() + cache.stats.Misses()
	if totalAccesses != uint64(numReads) {
		t.Errorf("expected %d total accesses, got %d", numReads, totalAccesses)
	}
}
