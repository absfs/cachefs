package cachefs_test

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/absfs/absfs"
	"github.com/absfs/cachefs"
)

// Simple in-memory filesystem for examples
type simpleFS struct {
	files map[string][]byte
}

func newSimpleFS() *simpleFS {
	return &simpleFS{files: make(map[string][]byte)}
}

func (s *simpleFS) Chdir(dir string) error               { return nil }
func (s *simpleFS) Getwd() (string, error)               { return "/", nil }
func (s *simpleFS) TempDir() string                      { return "/tmp" }
func (s *simpleFS) Open(name string) (absfs.File, error) { return s.OpenFile(name, os.O_RDONLY, 0) }
func (s *simpleFS) Create(name string) (absfs.File, error) {
	return s.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
}
func (s *simpleFS) Mkdir(name string, perm os.FileMode) error    { return nil }
func (s *simpleFS) MkdirAll(name string, perm os.FileMode) error { return nil }
func (s *simpleFS) Remove(name string) error                     { delete(s.files, name); return nil }
func (s *simpleFS) RemoveAll(path string) error                  { delete(s.files, path); return nil }
func (s *simpleFS) Stat(name string) (os.FileInfo, error)        { return nil, os.ErrNotExist }
func (s *simpleFS) Rename(oldname, newname string) error {
	s.files[newname] = s.files[oldname]
	delete(s.files, oldname)
	return nil
}
func (s *simpleFS) Chmod(name string, mode os.FileMode) error                   { return nil }
func (s *simpleFS) Chtimes(name string, atime time.Time, mtime time.Time) error { return nil }
func (s *simpleFS) Chown(name string, uid, gid int) error                       { return nil }
func (s *simpleFS) Truncate(name string, size int64) error                      { return nil }
func (s *simpleFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	return &simpleFile{fs: s, name: name}, nil
}
func (s *simpleFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return nil, nil
}
func (s *simpleFS) ReadFile(name string) ([]byte, error) {
	data, ok := s.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}
func (s *simpleFS) Sub(dir string) (fs.FS, error) {
	return absfs.FilerToFS(s, dir)
}

type simpleFile struct {
	fs   *simpleFS
	name string
	pos  int64
}

func (f *simpleFile) Name() string { return f.name }
func (f *simpleFile) Read(p []byte) (n int, err error) {
	data := f.fs.files[f.name]
	if f.pos >= int64(len(data)) {
		return 0, io.EOF
	}
	n = copy(p, data[f.pos:])
	f.pos += int64(n)
	if f.pos >= int64(len(data)) && n < len(p) {
		err = io.EOF
	}
	return
}
func (f *simpleFile) Write(p []byte) (n int, err error) {
	f.fs.files[f.name] = append(f.fs.files[f.name], p...)
	return len(p), nil
}
func (f *simpleFile) Close() error                                 { return nil }
func (f *simpleFile) Sync() error                                  { return nil }
func (f *simpleFile) Stat() (os.FileInfo, error)                   { return nil, nil }
func (f *simpleFile) Readdir(int) ([]os.FileInfo, error)           { return nil, nil }
func (f *simpleFile) Readdirnames(int) ([]string, error)           { return nil, nil }
func (f *simpleFile) ReadDir(int) ([]fs.DirEntry, error)           { return nil, nil }
func (f *simpleFile) Seek(offset int64, whence int) (int64, error) { f.pos = offset; return f.pos, nil }
func (f *simpleFile) ReadAt(b []byte, off int64) (n int, err error) {
	data := f.fs.files[f.name]
	return copy(b, data[off:]), nil
}
func (f *simpleFile) WriteAt(b []byte, off int64) (n int, err error) { return len(b), nil }
func (f *simpleFile) Truncate(size int64) error                      { return nil }
func (f *simpleFile) WriteString(s string) (n int, err error)        { return f.Write([]byte(s)) }

// Example_basic demonstrates basic usage of cachefs
func Example_basic() {
	// Create backing filesystem
	backing := newSimpleFS()

	// Create cache with default settings (write-through, LRU, 100MB limit)
	cache := cachefs.New(backing)

	// Write a file
	backing.files["/data/file.txt"] = []byte("Hello, CacheFS!")

	// Read the file (will be cached)
	file, _ := cache.OpenFile("/data/file.txt", os.O_RDONLY, 0644)
	data := make([]byte, 15)
	n, _ := file.Read(data)
	file.Close()

	fmt.Printf("Read: %s\n", string(data[:n]))

	// Second read will be a cache hit
	file2, _ := cache.OpenFile("/data/file.txt", os.O_RDONLY, 0644)
	data2 := make([]byte, 15)
	file2.Read(data2)
	file2.Close()

	// Check statistics
	stats := cache.Stats()
	fmt.Printf("Hits: %d, Misses: %d\n", stats.Hits(), stats.Misses())

	// Output:
	// Read: Hello, CacheFS!
	// Hits: 1, Misses: 1
}

// Example_advanced demonstrates advanced configuration
func Example_advanced() {
	backing := newSimpleFS()

	// Create cache with custom configuration
	cache := cachefs.New(backing,
		cachefs.WithWriteMode(cachefs.WriteModeWriteThrough),
		cachefs.WithEvictionPolicy(cachefs.EvictionLRU),
		cachefs.WithMaxBytes(1024*1024), // 1 MB
		cachefs.WithTTL(5*time.Minute),
		cachefs.WithMaxEntries(1000),
	)

	// Use the cache
	backing.files["/config.txt"] = []byte("configuration data")
	file, _ := cache.OpenFile("/config.txt", os.O_RDONLY, 0644)
	data := make([]byte, 18)
	file.Read(data)
	file.Close()

	fmt.Printf("Data: %s\n", string(data))
	fmt.Printf("Cache hit rate: %.0f%%\n", cache.Stats().HitRate()*100)

	// Output:
	// Data: configuration data
	// Cache hit rate: 0%
}

// Example_eviction demonstrates cache eviction
func Example_eviction() {
	backing := newSimpleFS()

	// Small cache that will trigger eviction
	cache := cachefs.New(backing,
		cachefs.WithMaxBytes(100),
	)

	// Add files that exceed cache size
	backing.files["/file1.txt"] = make([]byte, 60)
	backing.files["/file2.txt"] = make([]byte, 60)

	// Read first file
	file1, _ := cache.OpenFile("/file1.txt", os.O_RDONLY, 0644)
	buf1 := make([]byte, 60)
	file1.Read(buf1)
	file1.Close()

	fmt.Printf("Entries after file1: %d\n", cache.Stats().Entries())

	// Read second file - should trigger eviction of file1
	file2, _ := cache.OpenFile("/file2.txt", os.O_RDONLY, 0644)
	buf2 := make([]byte, 60)
	file2.Read(buf2)
	file2.Close()

	fmt.Printf("Entries after file2: %d\n", cache.Stats().Entries())
	fmt.Printf("Total evictions: %d\n", cache.Stats().Evictions())

	// Output:
	// Entries after file1: 1
	// Entries after file2: 1
	// Total evictions: 1
}
