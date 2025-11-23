package cachefs

import (
	"fmt"
	"io/fs"
	"sync"
	"time"

	"github.com/absfs/absfs"
)

// CacheFS is a caching filesystem that wraps another filesystem
type CacheFS struct {
	backing absfs.FileSystem
	mu      sync.RWMutex

	// Configuration
	writeMode           WriteMode
	evictionPolicy      EvictionPolicy
	maxBytes            uint64
	maxEntries          uint64
	ttl                 time.Duration
	flushInterval       time.Duration
	flushOnClose        bool
	metadataCache       bool
	metadataMaxEntries  uint64

	// Cache state
	entries   map[string]*cacheEntry
	lruHead   *cacheEntry // most recently used
	lruTail   *cacheEntry // least recently used
	stats     Stats
}

// New creates a new CacheFS with the given backing filesystem and options
func New(backing absfs.FileSystem, opts ...Option) *CacheFS {
	c := &CacheFS{
		backing:        backing,
		writeMode:      WriteModeWriteThrough,
		evictionPolicy: EvictionLRU,
		maxBytes:       100 * 1024 * 1024, // 100 MB default
		maxEntries:     0,                  // unlimited by default
		ttl:            0,                  // no TTL by default
		flushInterval:  30 * time.Second,
		flushOnClose:   true,
		metadataCache:  false,
		entries:        make(map[string]*cacheEntry),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Separator returns the path separator for the filesystem
func (c *CacheFS) Separator() uint8 {
	return c.backing.Separator()
}

// OpenFile opens a file with the specified flags and permissions
func (c *CacheFS) OpenFile(name string, flag int, perm fs.FileMode) (absfs.File, error) {
	// Open the backing file
	file, err := c.backing.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}

	// Wrap in cached file
	return &cachedFile{
		cfs:   c,
		file:  file,
		path:  name,
		flags: flag,
		mode:  perm,
	}, nil
}

// Mkdir creates a directory
func (c *CacheFS) Mkdir(name string, perm fs.FileMode) error {
	return c.backing.Mkdir(name, perm)
}

// Remove removes a file or empty directory
func (c *CacheFS) Remove(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove from cache if present
	if entry, ok := c.entries[name]; ok {
		c.removeEntry(entry)
	}

	return c.backing.Remove(name)
}

// Stat returns file information
func (c *CacheFS) Stat(name string) (fs.FileInfo, error) {
	// For now, delegate to backing filesystem
	// Metadata caching will be added in Phase 2
	return c.backing.Stat(name)
}

// Rename renames a file or directory
func (c *CacheFS) Rename(oldname, newname string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove old entry from cache if present
	if entry, ok := c.entries[oldname]; ok {
		c.removeEntry(entry)
	}

	// Remove new entry from cache if present (it will be overwritten)
	if entry, ok := c.entries[newname]; ok {
		c.removeEntry(entry)
	}

	return c.backing.Rename(oldname, newname)
}

// ListSeparator returns the list separator for the filesystem
func (c *CacheFS) ListSeparator() uint8 {
	return c.backing.ListSeparator()
}

// Chdir changes the current working directory
func (c *CacheFS) Chdir(dir string) error {
	return c.backing.Chdir(dir)
}

// Getwd returns the current working directory
func (c *CacheFS) Getwd() (string, error) {
	return c.backing.Getwd()
}

// TempDir returns the temporary directory
func (c *CacheFS) TempDir() string {
	return c.backing.TempDir()
}

// Open opens a file for reading
func (c *CacheFS) Open(name string) (absfs.File, error) {
	return c.OpenFile(name, absfs.O_RDONLY, 0)
}

// Create creates or truncates a file
func (c *CacheFS) Create(name string) (absfs.File, error) {
	return c.OpenFile(name, absfs.O_RDWR|absfs.O_CREATE|absfs.O_TRUNC, 0666)
}

// MkdirAll creates a directory and all parent directories
func (c *CacheFS) MkdirAll(name string, perm fs.FileMode) error {
	return c.backing.MkdirAll(name, perm)
}

// RemoveAll removes a path and all children
func (c *CacheFS) RemoveAll(path string) error {
	// TODO: invalidate all entries under this path
	return c.backing.RemoveAll(path)
}

// Truncate changes the size of a file
func (c *CacheFS) Truncate(name string, size int64) error {
	c.mu.Lock()
	if entry, ok := c.entries[name]; ok {
		c.removeEntry(entry)
	}
	c.mu.Unlock()

	return c.backing.Truncate(name, size)
}

// Chmod changes file permissions
func (c *CacheFS) Chmod(name string, mode fs.FileMode) error {
	return c.backing.Chmod(name, mode)
}

// Chtimes changes file access and modification times
func (c *CacheFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return c.backing.Chtimes(name, atime, mtime)
}

// Chown changes file owner and group
func (c *CacheFS) Chown(name string, uid, gid int) error {
	return c.backing.Chown(name, uid, gid)
}

// Stats returns the current cache statistics
func (c *CacheFS) Stats() *Stats {
	return &c.stats
}

// ResetStats resets all cache statistics
func (c *CacheFS) ResetStats() {
	c.stats.reset()
}

// Invalidate removes a specific path from the cache
func (c *CacheFS) Invalidate(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, ok := c.entries[path]; ok {
		c.removeEntry(entry)
	}
}

// Clear removes all entries from the cache
func (c *CacheFS) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range c.entries {
		c.removeEntry(entry)
	}
}

// Flush flushes all dirty entries to the backing store (write-back mode)
func (c *CacheFS) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstError error
	for _, entry := range c.entries {
		if entry.dirty {
			if err := c.flushEntry(entry); err != nil && firstError == nil {
				firstError = err
			}
		}
	}
	return firstError
}

// removeEntry removes an entry from the cache and updates statistics
func (c *CacheFS) removeEntry(entry *cacheEntry) {
	// Remove from map
	delete(c.entries, entry.path)

	// Remove from LRU list
	c.removeLRU(entry)

	// Update statistics
	c.stats.removeBytes(uint64(len(entry.data)))
	c.stats.decEntries()
}

// removeLRU removes an entry from the LRU linked list
func (c *CacheFS) removeLRU(entry *cacheEntry) {
	if entry.prev != nil {
		entry.prev.next = entry.next
	} else {
		c.lruHead = entry.next
	}

	if entry.next != nil {
		entry.next.prev = entry.prev
	} else {
		c.lruTail = entry.prev
	}

	entry.prev = nil
	entry.next = nil
}

// moveToHead moves an entry to the head of the LRU list (most recently used)
func (c *CacheFS) moveToHead(entry *cacheEntry) {
	// Remove from current position
	c.removeLRU(entry)

	// Add to head
	entry.next = c.lruHead
	entry.prev = nil

	if c.lruHead != nil {
		c.lruHead.prev = entry
	}
	c.lruHead = entry

	if c.lruTail == nil {
		c.lruTail = entry
	}
}

// evictLRU evicts the least recently used entry
func (c *CacheFS) evictLRU() error {
	if c.lruTail == nil {
		return fmt.Errorf("cache is empty, cannot evict")
	}

	entry := c.lruTail

	// If entry is dirty in write-back mode, flush it first
	if entry.dirty {
		if err := c.flushEntry(entry); err != nil {
			return fmt.Errorf("failed to flush dirty entry during eviction: %w", err)
		}
	}

	c.removeEntry(entry)
	c.stats.recordEviction()

	return nil
}

// flushEntry writes a dirty entry to the backing store
func (c *CacheFS) flushEntry(entry *cacheEntry) error {
	if !entry.dirty {
		return nil
	}

	// Write to backing store
	file, err := c.backing.OpenFile(entry.path, absfs.O_WRONLY|absfs.O_CREATE|absfs.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for flush: %w", err)
	}
	defer file.Close()

	_, err = file.Write(entry.data)
	if err != nil {
		return fmt.Errorf("failed to write during flush: %w", err)
	}

	entry.dirty = false
	return nil
}

// shouldEvict checks if eviction is needed based on cache limits
func (c *CacheFS) shouldEvict() bool {
	stats := c.stats.BytesUsed()
	entries := c.stats.Entries()

	if c.maxBytes > 0 && stats >= c.maxBytes {
		return true
	}

	if c.maxEntries > 0 && entries >= c.maxEntries {
		return true
	}

	return false
}

// evictIfNeeded evicts entries if cache limits are exceeded
func (c *CacheFS) evictIfNeeded() error {
	for c.shouldEvict() {
		if err := c.evictLRU(); err != nil {
			return err
		}
	}
	return nil
}
