package cachefs

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/absfs/absfs"
)

// mockFS is a simple in-memory filesystem for testing
type mockFS struct {
	mu       sync.RWMutex
	files    map[string][]byte
	cwd      string
	globFunc func(pattern string) ([]string, error)
}

func newMockFS() *mockFS {
	return &mockFS{
		files: make(map[string][]byte),
		cwd:   "/",
	}
}

func (m *mockFS) Separator() uint8 {
	return '/'
}

func (m *mockFS) ListSeparator() uint8 {
	return ':'
}

func (m *mockFS) Chdir(dir string) error {
	m.cwd = dir
	return nil
}

func (m *mockFS) Getwd() (string, error) {
	return m.cwd, nil
}

func (m *mockFS) TempDir() string {
	return "/tmp"
}

func (m *mockFS) Open(name string) (absfs.File, error) {
	return m.OpenFile(name, os.O_RDONLY, 0)
}

func (m *mockFS) Create(name string) (absfs.File, error) {
	return m.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
}

func (m *mockFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	return &mockFile{
		fs:   m,
		name: name,
		flag: flag,
		perm: perm,
		pos:  0,
	}, nil
}

func (m *mockFS) Mkdir(name string, perm os.FileMode) error {
	return nil
}

func (m *mockFS) MkdirAll(name string, perm os.FileMode) error {
	return nil
}

func (m *mockFS) Remove(name string) error {
	m.mu.Lock()
	delete(m.files, name)
	m.mu.Unlock()
	return nil
}

func (m *mockFS) RemoveAll(path string) error {
	m.mu.Lock()
	delete(m.files, path)
	m.mu.Unlock()
	return nil
}

func (m *mockFS) Stat(name string) (os.FileInfo, error) {
	return nil, os.ErrNotExist
}

func (m *mockFS) Rename(oldname, newname string) error {
	if data, ok := m.files[oldname]; ok {
		m.files[newname] = data
		delete(m.files, oldname)
	}
	return nil
}

func (m *mockFS) Chmod(name string, mode os.FileMode) error {
	return nil
}

func (m *mockFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return nil
}

func (m *mockFS) Chown(name string, uid, gid int) error {
	return nil
}

func (m *mockFS) Truncate(name string, size int64) error {
	if data, ok := m.files[name]; ok {
		if int64(len(data)) > size {
			m.files[name] = data[:size]
		}
	}
	return nil
}

func (m *mockFS) Glob(pattern string) ([]string, error) {
	// Use custom glob function if provided
	if m.globFunc != nil {
		return m.globFunc(pattern)
	}

	// Simple glob implementation for testing
	var matches []string
	for path := range m.files {
		// For now, just return all files
		// Real implementation would match the pattern
		matches = append(matches, path)
	}
	return matches, nil
}

type mockFile struct {
	fs   *mockFS
	name string
	flag int
	perm os.FileMode
	pos  int64
}

func (f *mockFile) Name() string {
	return f.name
}

func (f *mockFile) Read(p []byte) (n int, err error) {
	f.fs.mu.RLock()
	data, ok := f.fs.files[f.name]
	f.fs.mu.RUnlock()
	if !ok {
		return 0, io.EOF
	}

	if f.pos >= int64(len(data)) {
		return 0, io.EOF
	}

	n = copy(p, data[f.pos:])
	f.pos += int64(n)
	return n, nil
}

func (f *mockFile) Write(p []byte) (n int, err error) {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()

	data, ok := f.fs.files[f.name]
	if !ok {
		data = make([]byte, 0)
	}

	// Extend if necessary
	needed := int(f.pos) + len(p)
	if needed > len(data) {
		newData := make([]byte, needed)
		copy(newData, data)
		data = newData
	}

	n = copy(data[f.pos:], p)
	f.pos += int64(n)
	f.fs.files[f.name] = data[:int(f.pos)]
	return n, nil
}

func (f *mockFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.pos = offset
	case io.SeekCurrent:
		f.pos += offset
	case io.SeekEnd:
		f.fs.mu.RLock()
		data := f.fs.files[f.name]
		f.fs.mu.RUnlock()
		f.pos = int64(len(data)) + offset
	}
	return f.pos, nil
}

func (f *mockFile) Close() error {
	return nil
}

func (f *mockFile) Sync() error {
	return nil
}

func (f *mockFile) Stat() (os.FileInfo, error) {
	return nil, nil
}

func (f *mockFile) Readdir(n int) ([]os.FileInfo, error) {
	return nil, nil
}

func (f *mockFile) Readdirnames(n int) ([]string, error) {
	return nil, nil
}

func (f *mockFile) ReadAt(b []byte, off int64) (n int, err error) {
	f.fs.mu.RLock()
	data, ok := f.fs.files[f.name]
	f.fs.mu.RUnlock()
	if !ok {
		return 0, io.EOF
	}

	if off >= int64(len(data)) {
		return 0, io.EOF
	}

	n = copy(b, data[off:])
	return n, nil
}

func (f *mockFile) WriteAt(b []byte, off int64) (n int, err error) {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()

	data, ok := f.fs.files[f.name]
	if !ok {
		data = make([]byte, 0)
	}

	// Extend if necessary
	needed := int(off) + len(b)
	if needed > len(data) {
		newData := make([]byte, needed)
		copy(newData, data)
		data = newData
	}

	n = copy(data[off:], b)
	f.fs.files[f.name] = data
	return n, nil
}

func (f *mockFile) Truncate(size int64) error {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()

	data := f.fs.files[f.name]
	if int64(len(data)) > size {
		f.fs.files[f.name] = data[:size]
	}
	return nil
}

func (f *mockFile) WriteString(s string) (n int, err error) {
	return f.Write([]byte(s))
}

func TestNew(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	if cache == nil {
		t.Fatal("New returned nil")
	}

	if cache.writeMode != WriteModeWriteThrough {
		t.Error("default write mode should be WriteModeWriteThrough")
	}

	if cache.evictionPolicy != EvictionLRU {
		t.Error("default eviction policy should be EvictionLRU")
	}

	if cache.maxBytes != 100*1024*1024 {
		t.Error("default maxBytes should be 100MB")
	}
}

func TestWithOptions(t *testing.T) {
	backing := newMockFS()
	cache := New(backing,
		WithWriteMode(WriteModeWriteBack),
		WithEvictionPolicy(EvictionLFU),
		WithMaxBytes(500*1024*1024),
		WithMaxEntries(10000),
		WithTTL(5*time.Minute),
	)

	if cache.writeMode != WriteModeWriteBack {
		t.Error("write mode not set correctly")
	}

	if cache.evictionPolicy != EvictionLFU {
		t.Error("eviction policy not set correctly")
	}

	if cache.maxBytes != 500*1024*1024 {
		t.Error("maxBytes not set correctly")
	}

	if cache.maxEntries != 10000 {
		t.Error("maxEntries not set correctly")
	}

	if cache.ttl != 5*time.Minute {
		t.Error("TTL not set correctly")
	}
}

func TestCacheReadHit(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Write test data to backing store
	testData := []byte("Hello, World!")
	backing.files["/test.txt"] = testData

	// First read - should be a cache miss
	file1, err := cache.OpenFile("/test.txt", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}

	data1 := make([]byte, len(testData))
	n1, err := file1.Read(data1)
	if err != nil && err != io.EOF {
		t.Fatalf("failed to read file: %v", err)
	}
	file1.Close()

	if n1 != len(testData) || !bytes.Equal(data1, testData) {
		t.Errorf("first read got %q, want %q", data1[:n1], testData)
	}

	if cache.stats.Misses() != 1 {
		t.Errorf("expected 1 miss, got %d", cache.stats.Misses())
	}

	// Second read - should be a cache hit
	file2, err := cache.OpenFile("/test.txt", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}

	data2 := make([]byte, len(testData))
	n2, err := file2.Read(data2)
	if err != nil && err != io.EOF {
		t.Fatalf("failed to read file: %v", err)
	}
	file2.Close()

	if n2 != len(testData) || !bytes.Equal(data2, testData) {
		t.Errorf("second read got %q, want %q", data2[:n2], testData)
	}

	if cache.stats.Hits() != 1 {
		t.Errorf("expected 1 hit, got %d", cache.stats.Hits())
	}

	hitRate := cache.stats.HitRate()
	expectedRate := 0.5 // 1 hit out of 2 total accesses
	if hitRate != expectedRate {
		t.Errorf("hit rate = %.2f, want %.2f", hitRate, expectedRate)
	}
}

func TestCacheWriteThrough(t *testing.T) {
	backing := newMockFS()
	cache := New(backing, WithWriteMode(WriteModeWriteThrough))

	testData := []byte("Hello, Cache!")

	// Write to cache
	file, err := cache.OpenFile("/test.txt", os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}

	n, err := file.Write(testData)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	file.Close()

	if n != len(testData) {
		t.Errorf("wrote %d bytes, want %d", n, len(testData))
	}

	// Verify data was written to backing store
	backingData := backing.files["/test.txt"]
	if !bytes.Equal(backingData, testData) {
		t.Errorf("backing store has %q, want %q", backingData, testData)
	}
}

func TestEvictionByMaxBytes(t *testing.T) {
	backing := newMockFS()
	cache := New(backing, WithMaxBytes(100)) // Small cache

	// Write data larger than cache
	data1 := make([]byte, 60)
	data2 := make([]byte, 60)
	for i := range data1 {
		data1[i] = 'A'
	}
	for i := range data2 {
		data2[i] = 'B'
	}

	backing.files["/file1.txt"] = data1
	backing.files["/file2.txt"] = data2

	// Read first file - should cache it
	file1, err := cache.OpenFile("/file1.txt", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file1: %v", err)
	}
	buf1 := make([]byte, len(data1))
	file1.Read(buf1)
	file1.Close()

	if cache.stats.Entries() != 1 {
		t.Errorf("expected 1 entry, got %d", cache.stats.Entries())
	}

	// Read second file - should evict first file
	file2, err := cache.OpenFile("/file2.txt", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file2: %v", err)
	}
	buf2 := make([]byte, len(data2))
	file2.Read(buf2)
	file2.Close()

	if cache.stats.Evictions() < 1 {
		t.Errorf("expected at least 1 eviction, got %d", cache.stats.Evictions())
	}
}

func TestEvictionByMaxEntries(t *testing.T) {
	backing := newMockFS()
	cache := New(backing, WithMaxEntries(2))

	// Create 3 small files
	backing.files["/file1.txt"] = []byte("A")
	backing.files["/file2.txt"] = []byte("B")
	backing.files["/file3.txt"] = []byte("C")

	// Read all three files
	for i := 1; i <= 3; i++ {
		name := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		file, err := cache.OpenFile(name, os.O_RDONLY, 0644)
		if err != nil {
			t.Fatalf("failed to open %s: %v", name, err)
		}
		buf := make([]byte, 1)
		file.Read(buf)
		file.Close()
	}

	if cache.stats.Evictions() < 1 {
		t.Errorf("expected at least 1 eviction, got %d", cache.stats.Evictions())
	}

	if cache.stats.Entries() > 2 {
		t.Errorf("expected at most 2 entries, got %d", cache.stats.Entries())
	}
}

func TestInvalidate(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	testData := []byte("test data")
	backing.files["/test.txt"] = testData

	// Read to populate cache
	file, err := cache.OpenFile("/test.txt", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	buf := make([]byte, len(testData))
	file.Read(buf)
	file.Close()

	if cache.stats.Entries() != 1 {
		t.Errorf("expected 1 entry, got %d", cache.stats.Entries())
	}

	// Invalidate
	cache.Invalidate("/test.txt")

	if cache.stats.Entries() != 0 {
		t.Errorf("expected 0 entries after invalidation, got %d", cache.stats.Entries())
	}
}

func TestClear(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	// Add multiple files to cache
	for i := 1; i <= 3; i++ {
		name := string([]byte{byte('/'), byte('f'), byte('i'), byte('l'), byte('e'), byte('0' + i), byte('.'), byte('t'), byte('x'), byte('t')})
		backing.files[name] = []byte{byte('A' + i - 1)}

		file, err := cache.OpenFile(name, os.O_RDONLY, 0644)
		if err != nil {
			t.Fatalf("failed to open %s: %v", name, err)
		}
		buf := make([]byte, 1)
		file.Read(buf)
		file.Close()
	}

	if cache.stats.Entries() != 3 {
		t.Errorf("expected 3 entries, got %d", cache.stats.Entries())
	}

	// Clear cache
	cache.Clear()

	if cache.stats.Entries() != 0 {
		t.Errorf("expected 0 entries after clear, got %d", cache.stats.Entries())
	}
}

func TestResetStats(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	backing.files["/test.txt"] = []byte("test")

	// Generate some stats
	file, _ := cache.OpenFile("/test.txt", os.O_RDONLY, 0644)
	buf := make([]byte, 4)
	file.Read(buf)
	file.Close()

	if cache.stats.Misses() == 0 {
		t.Error("expected some misses before reset")
	}

	cache.ResetStats()

	if cache.stats.Hits() != 0 || cache.stats.Misses() != 0 {
		t.Error("stats not reset properly")
	}
}

func TestRemove(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	testData := []byte("test data")
	backing.files["/test.txt"] = testData

	// Read to populate cache
	file, err := cache.OpenFile("/test.txt", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	buf := make([]byte, len(testData))
	file.Read(buf)
	file.Close()

	if cache.stats.Entries() != 1 {
		t.Errorf("expected 1 entry, got %d", cache.stats.Entries())
	}

	// Remove file
	err = cache.Remove("/test.txt")
	if err != nil {
		t.Fatalf("failed to remove file: %v", err)
	}

	// Should be removed from cache too
	if cache.stats.Entries() != 0 {
		t.Errorf("expected 0 entries after remove, got %d", cache.stats.Entries())
	}

	// Should be removed from backing store
	if _, ok := backing.files["/test.txt"]; ok {
		t.Error("file still exists in backing store")
	}
}

func TestRename(t *testing.T) {
	backing := newMockFS()
	cache := New(backing)

	testData := []byte("test data")
	backing.files["/old.txt"] = testData

	// Read to populate cache
	file, err := cache.OpenFile("/old.txt", os.O_RDONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	buf := make([]byte, len(testData))
	file.Read(buf)
	file.Close()

	if cache.stats.Entries() != 1 {
		t.Errorf("expected 1 entry, got %d", cache.stats.Entries())
	}

	// Rename file
	err = cache.Rename("/old.txt", "/new.txt")
	if err != nil {
		t.Fatalf("failed to rename file: %v", err)
	}

	// Old entry should be removed from cache
	cache.mu.RLock()
	_, oldExists := cache.entries["/old.txt"]
	cache.mu.RUnlock()

	if oldExists {
		t.Error("old entry still in cache after rename")
	}
}
