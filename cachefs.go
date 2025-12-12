package cachefs

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"sync"
	"time"

	"github.com/absfs/absfs"
)

// CacheFS is a caching filesystem that wraps another filesystem
type CacheFS struct {
	backing absfs.FileSystem
	mu      sync.RWMutex

	// Configuration
	writeMode          WriteMode
	evictionPolicy     EvictionPolicy
	maxBytes           uint64
	maxEntries         uint64
	ttl                time.Duration
	flushInterval      time.Duration
	flushOnClose       bool
	metadataCache      bool
	metadataMaxEntries uint64

	// Cache state
	entries         map[string]*cacheEntry
	lruHead         *cacheEntry // most recently used
	lruTail         *cacheEntry // least recently used
	metadataEntries map[string]*metadataEntry
	stats           Stats

	// Background flush
	flushStop chan struct{}
	flushDone chan struct{}
}

// New creates a new CacheFS with the given backing filesystem and options
func New(backing absfs.FileSystem, opts ...Option) *CacheFS {
	c := &CacheFS{
		backing:         backing,
		writeMode:       WriteModeWriteThrough,
		evictionPolicy:  EvictionLRU,
		maxBytes:        100 * 1024 * 1024, // 100 MB default
		maxEntries:      0,                 // unlimited by default
		ttl:             0,                 // no TTL by default
		flushInterval:   30 * time.Second,
		flushOnClose:    true,
		metadataCache:   false,
		entries:         make(map[string]*cacheEntry),
		metadataEntries: make(map[string]*metadataEntry),
	}

	for _, opt := range opts {
		opt(c)
	}

	// Start background flush goroutine for write-back mode
	if c.writeMode == WriteModeWriteBack && c.flushInterval > 0 {
		c.flushStop = make(chan struct{})
		c.flushDone = make(chan struct{})
		go c.backgroundFlush()
	}

	return c
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
	// If metadata caching is disabled, just delegate
	if !c.metadataCache {
		return c.backing.Stat(name)
	}

	c.mu.RLock()
	entry, cached := c.metadataEntries[name]
	c.mu.RUnlock()

	if cached {
		// Check if expired
		if c.ttl > 0 && !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
			// Expired, remove and fetch fresh
			c.mu.Lock()
			delete(c.metadataEntries, name)
			putMetadataEntry(entry)
			c.mu.Unlock()
		} else {
			// Cache hit
			c.mu.Lock()
			entry.lastAccess = time.Now()
			entry.accessCount++
			c.mu.Unlock()
			return entry.info, nil
		}
	}

	// Cache miss or expired - fetch from backing store
	info, err := c.backing.Stat(name)
	if err != nil {
		return nil, err
	}

	// Add to metadata cache
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	newEntry := getMetadataEntry()
	newEntry.path = name
	newEntry.info = info
	newEntry.lastAccess = now
	newEntry.accessCount = 1
	newEntry.createdAt = now

	if c.ttl > 0 {
		newEntry.expiresAt = now.Add(c.ttl)
	}

	c.metadataEntries[name] = newEntry

	// Evict old metadata if needed
	if c.metadataMaxEntries > 0 && uint64(len(c.metadataEntries)) > c.metadataMaxEntries {
		c.evictMetadata()
	}

	return info, nil
}

// evictMetadata removes the least recently used metadata entry
func (c *CacheFS) evictMetadata() {
	if len(c.metadataEntries) == 0 {
		return
	}

	// Find least recently accessed entry
	var oldestEntry *metadataEntry
	var oldestPath string
	var oldestAccess time.Time

	for path, entry := range c.metadataEntries {
		if oldestEntry == nil || entry.lastAccess.Before(oldestAccess) {
			oldestAccess = entry.lastAccess
			oldestEntry = entry
			oldestPath = path
		}
	}

	if oldestPath != "" {
		delete(c.metadataEntries, oldestPath)
		putMetadataEntry(oldestEntry)
	}
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
	// Invalidate the path and all entries under it
	c.InvalidatePrefix(path)
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

	// Also invalidate metadata cache
	if metaEntry, ok := c.metadataEntries[path]; ok {
		delete(c.metadataEntries, path)
		putMetadataEntry(metaEntry)
	}
}

// InvalidatePrefix invalidates all cache entries with the given path prefix
func (c *CacheFS) InvalidatePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Collect entries to remove
	toRemove := make([]*cacheEntry, 0)
	for path, entry := range c.entries {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			toRemove = append(toRemove, entry)
		}
	}

	// Remove data cache entries
	for _, entry := range toRemove {
		c.removeEntry(entry)
	}

	// Remove metadata cache entries
	metaToRemove := make([]*metadataEntry, 0)
	metaPaths := make([]string, 0)
	for path, entry := range c.metadataEntries {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			metaToRemove = append(metaToRemove, entry)
			metaPaths = append(metaPaths, path)
		}
	}

	for i, path := range metaPaths {
		delete(c.metadataEntries, path)
		putMetadataEntry(metaToRemove[i])
	}
}

// InvalidatePattern invalidates cache entries matching a glob pattern
func (c *CacheFS) InvalidatePattern(pattern string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Collect entries to remove
	toRemove := make([]*cacheEntry, 0)
	for entryPath, entry := range c.entries {
		matched, err := path.Match(pattern, entryPath)
		if err != nil {
			return fmt.Errorf("invalid pattern: %w", err)
		}
		if matched {
			toRemove = append(toRemove, entry)
		}
	}

	// Remove data cache entries
	for _, entry := range toRemove {
		c.removeEntry(entry)
	}

	// Remove metadata cache entries
	metaToRemove := make([]*metadataEntry, 0)
	metaPaths := make([]string, 0)
	for entryPath, entry := range c.metadataEntries {
		matched, _ := path.Match(pattern, entryPath)
		if matched {
			metaToRemove = append(metaToRemove, entry)
			metaPaths = append(metaPaths, entryPath)
		}
	}

	for i, path := range metaPaths {
		delete(c.metadataEntries, path)
		putMetadataEntry(metaToRemove[i])
	}

	return nil
}

// Clear removes all entries from the cache
func (c *CacheFS) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range c.entries {
		c.removeEntry(entry)
	}

	// Clear metadata cache as well and return entries to pool
	for _, entry := range c.metadataEntries {
		putMetadataEntry(entry)
	}
	c.metadataEntries = make(map[string]*metadataEntry)
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

	// Return entry to pool
	putCacheEntry(entry)
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
	// First, try to evict expired entries
	if c.ttl > 0 {
		c.evictExpired()
	}

	// If still need to evict, use the configured policy
	for c.shouldEvict() {
		var err error
		switch c.evictionPolicy {
		case EvictionLRU:
			err = c.evictLRU()
		case EvictionLFU:
			err = c.evictLFU()
		case EvictionTTL:
			err = c.evictOldest()
		case EvictionHybrid:
			err = c.evictHybrid()
		default:
			err = c.evictLRU()
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// evictLFU evicts the least frequently used entry
func (c *CacheFS) evictLFU() error {
	if len(c.entries) == 0 {
		return fmt.Errorf("cache is empty, cannot evict")
	}

	// Find entry with lowest access count
	var lfuEntry *cacheEntry
	minAccessCount := uint64(^uint64(0)) // max uint64

	for _, entry := range c.entries {
		if entry.accessCount < minAccessCount {
			minAccessCount = entry.accessCount
			lfuEntry = entry
		}
	}

	if lfuEntry == nil {
		return fmt.Errorf("no entry found to evict")
	}

	// If entry is dirty in write-back mode, flush it first
	if lfuEntry.dirty {
		if err := c.flushEntry(lfuEntry); err != nil {
			return fmt.Errorf("failed to flush dirty entry during eviction: %w", err)
		}
	}

	c.removeEntry(lfuEntry)
	c.stats.recordEviction()

	return nil
}

// evictOldest evicts the oldest entry (based on creation time)
func (c *CacheFS) evictOldest() error {
	if len(c.entries) == 0 {
		return fmt.Errorf("cache is empty, cannot evict")
	}

	// Find oldest entry
	var oldestEntry *cacheEntry
	var oldestTime time.Time

	for _, entry := range c.entries {
		if oldestEntry == nil || entry.createdAt.Before(oldestTime) {
			oldestTime = entry.createdAt
			oldestEntry = entry
		}
	}

	if oldestEntry == nil {
		return fmt.Errorf("no entry found to evict")
	}

	// If entry is dirty in write-back mode, flush it first
	if oldestEntry.dirty {
		if err := c.flushEntry(oldestEntry); err != nil {
			return fmt.Errorf("failed to flush dirty entry during eviction: %w", err)
		}
	}

	c.removeEntry(oldestEntry)
	c.stats.recordEviction()

	return nil
}

// evictHybrid evicts using a hybrid strategy (LRU with access count tie-breaker)
func (c *CacheFS) evictHybrid() error {
	if c.lruTail == nil {
		return fmt.Errorf("cache is empty, cannot evict")
	}

	// Start with LRU tail (least recently used)
	candidate := c.lruTail

	// If there are multiple entries with low access counts, prefer those
	// Walk backwards from tail to find entries with low access counts
	lowAccessThreshold := uint64(2) // Consider entries accessed <= 2 times
	current := c.lruTail

	for current != nil {
		if current.accessCount <= lowAccessThreshold {
			candidate = current
			break
		}
		current = current.prev
		// Only check a few entries to avoid full scan
		if current != nil && current.prev == nil {
			break
		}
	}

	// If entry is dirty in write-back mode, flush it first
	if candidate.dirty {
		if err := c.flushEntry(candidate); err != nil {
			return fmt.Errorf("failed to flush dirty entry during eviction: %w", err)
		}
	}

	c.removeEntry(candidate)
	c.stats.recordEviction()

	return nil
}

// evictExpired removes all expired entries based on TTL
func (c *CacheFS) evictExpired() {
	if c.ttl == 0 {
		return
	}

	now := time.Now()
	toRemove := make([]*cacheEntry, 0)

	for _, entry := range c.entries {
		if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
			toRemove = append(toRemove, entry)
		}
	}

	for _, entry := range toRemove {
		c.removeEntry(entry)
		c.stats.recordEviction()
	}
}

// CleanExpired removes expired entries (can be called manually or periodically)
func (c *CacheFS) CleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpired()
}

// backgroundFlush periodically flushes dirty entries to the backing store
func (c *CacheFS) backgroundFlush() {
	ticker := time.NewTicker(c.flushInterval)
	defer ticker.Stop()
	defer close(c.flushDone)

	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			for _, entry := range c.entries {
				if entry.dirty {
					// Flush dirty entry
					if err := c.flushEntry(entry); err != nil {
						// Log error but continue flushing other entries
						// In a production system, you'd want proper error logging here
						_ = err
					}
				}
			}
			c.mu.Unlock()

		case <-c.flushStop:
			// Final flush before shutdown
			c.mu.Lock()
			for _, entry := range c.entries {
				if entry.dirty {
					c.flushEntry(entry)
				}
			}
			c.mu.Unlock()
			return
		}
	}
}

// Close stops the background flush goroutine and flushes all dirty entries
func (c *CacheFS) Close() error {
	if c.flushStop != nil {
		close(c.flushStop)
		<-c.flushDone // Wait for background goroutine to finish
	}
	return nil
}

// ReadDir reads the named directory and returns a list of directory entries
func (c *CacheFS) ReadDir(name string) ([]fs.DirEntry, error) {
	// Check if backing filesystem implements ReadDir
	type readDirer interface {
		ReadDir(name string) ([]fs.DirEntry, error)
	}

	if rd, ok := c.backing.(readDirer); ok {
		// Delegate to backing filesystem's ReadDir if available
		return rd.ReadDir(name)
	}

	// Fall back to opening the directory and reading entries
	f, err := c.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Check if file implements ReadDir
	type fileDirReader interface {
		ReadDir(n int) ([]fs.DirEntry, error)
	}

	if fdr, ok := f.(fileDirReader); ok {
		return fdr.ReadDir(-1)
	}

	// Fall back to Readdir and convert FileInfo to DirEntry
	fileInfos, err := f.Readdir(-1)
	if err != nil {
		return nil, err
	}

	entries := make([]fs.DirEntry, len(fileInfos))
	for i, info := range fileInfos {
		entries[i] = fs.FileInfoToDirEntry(info)
	}
	return entries, nil
}

// ReadFile reads the named file and returns its contents
func (c *CacheFS) ReadFile(name string) ([]byte, error) {
	// Check cache first if available
	c.mu.RLock()
	entry, cached := c.entries[name]
	c.mu.RUnlock()

	if cached {
		// Check if entry is expired
		if c.ttl > 0 && !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
			// Entry expired - remove it
			c.mu.Lock()
			c.removeEntry(entry)
			c.mu.Unlock()
		} else {
			// Cache hit - return cached data
			c.stats.recordHit()
			c.mu.Lock()
			entry.lastAccess = time.Now()
			entry.accessCount++
			c.moveToHead(entry)
			c.mu.Unlock()
			return entry.data, nil
		}
	}

	// Cache miss - check if backing filesystem implements ReadFile
	type readFiler interface {
		ReadFile(name string) ([]byte, error)
	}

	var data []byte
	var err error

	if rf, ok := c.backing.(readFiler); ok {
		// Use backing filesystem's ReadFile
		data, err = rf.ReadFile(name)
	} else {
		// Fall back to Open + Read
		f, openErr := c.backing.OpenFile(name, absfs.O_RDONLY, 0)
		if openErr != nil {
			return nil, openErr
		}
		defer f.Close()

		data, err = io.ReadAll(f)
	}

	if err != nil {
		c.stats.recordMiss()
		return nil, err
	}

	// Add to cache
	c.stats.recordMiss()
	now := time.Now()
	newEntry := getCacheEntry()
	newEntry.path = name
	newEntry.data = data
	newEntry.modTime = now
	newEntry.size = int64(len(data))
	newEntry.dirty = false
	newEntry.lastAccess = now
	newEntry.accessCount = 1
	newEntry.createdAt = now

	if c.ttl > 0 {
		newEntry.expiresAt = now.Add(c.ttl)
	}

	c.mu.Lock()
	c.entries[name] = newEntry
	c.moveToHead(newEntry)
	c.stats.addBytes(uint64(len(data)))
	c.stats.incEntries()

	// Evict if needed
	if err := c.evictIfNeeded(); err != nil {
		c.mu.Unlock()
		return data, err
	}
	c.mu.Unlock()

	return data, nil
}

// Sub returns an fs.FS rooted at the given directory
func (c *CacheFS) Sub(dir string) (fs.FS, error) {
	// Validate the directory exists
	info, err := c.backing.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &fs.PathError{Op: "sub", Path: dir, Err: fmt.Errorf("not a directory")}
	}

	// Create a sub-CacheFS wrapping the same backing filesystem
	// but with path prefixing. We'll create a Filer wrapper that prefixes paths.

	// Create options based on current configuration
	opts := []Option{
		WithWriteMode(c.writeMode),
		WithEvictionPolicy(c.evictionPolicy),
		WithMaxBytes(c.maxBytes),
		WithMaxEntries(c.maxEntries),
		WithTTL(c.ttl),
		WithFlushInterval(c.flushInterval),
	}

	if c.flushOnClose {
		opts = append(opts, WithFlushOnClose(true))
	}

	if c.metadataCache {
		opts = append(opts, WithMetadataCache(true))
		if c.metadataMaxEntries > 0 {
			opts = append(opts, WithMetadataMaxEntries(c.metadataMaxEntries))
		}
	}

	// Create a path-prefixed wrapper around the backing filesystem
	subFiler := &cacheFSSubFiler{parent: c.backing, prefix: dir}
	subFS := New(absfs.ExtendFiler(subFiler), opts...)

	return absfs.FilerToFS(subFS, ".")
}

// cacheFSSubFiler wraps a FileSystem and prefixes all paths with a directory
type cacheFSSubFiler struct {
	parent absfs.FileSystem
	prefix string
}

func (s *cacheFSSubFiler) fullPath(name string) string {
	clean := path.Clean("/" + name)
	return path.Join(s.prefix, clean)
}

func (s *cacheFSSubFiler) OpenFile(name string, flag int, perm fs.FileMode) (absfs.File, error) {
	return s.parent.OpenFile(s.fullPath(name), flag, perm)
}

func (s *cacheFSSubFiler) Mkdir(name string, perm fs.FileMode) error {
	return s.parent.Mkdir(s.fullPath(name), perm)
}

func (s *cacheFSSubFiler) Remove(name string) error {
	return s.parent.Remove(s.fullPath(name))
}

func (s *cacheFSSubFiler) Rename(oldpath, newpath string) error {
	return s.parent.Rename(s.fullPath(oldpath), s.fullPath(newpath))
}

func (s *cacheFSSubFiler) Stat(name string) (fs.FileInfo, error) {
	return s.parent.Stat(s.fullPath(name))
}

func (s *cacheFSSubFiler) Chmod(name string, mode fs.FileMode) error {
	return s.parent.Chmod(s.fullPath(name), mode)
}

func (s *cacheFSSubFiler) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return s.parent.Chtimes(s.fullPath(name), atime, mtime)
}

func (s *cacheFSSubFiler) Chown(name string, uid, gid int) error {
	return s.parent.Chown(s.fullPath(name), uid, gid)
}

func (s *cacheFSSubFiler) ReadDir(name string) ([]fs.DirEntry, error) {
	fullPath := s.fullPath(name)
	f, err := s.parent.Open(fullPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

func (s *cacheFSSubFiler) ReadFile(name string) ([]byte, error) {
	fullPath := s.fullPath(name)
	f, err := s.parent.Open(fullPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (s *cacheFSSubFiler) Sub(dir string) (fs.FS, error) {
	fullDir := s.fullPath(dir)
	// Validate directory exists
	info, err := s.parent.Stat(fullDir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &fs.PathError{Op: "sub", Path: dir, Err: fmt.Errorf("not a directory")}
	}
	return absfs.FilerToFS(s, dir)
}

// SymlinkCacheFS wraps a SymlinkFileSystem with caching
type SymlinkCacheFS struct {
	*CacheFS
	symlinkBacking absfs.SymlinkFileSystem
}

// NewSymlinkFS creates a new SymlinkCacheFS with the given backing SymlinkFileSystem and options
func NewSymlinkFS(backing absfs.SymlinkFileSystem, opts ...Option) *SymlinkCacheFS {
	cfs := New(backing, opts...)
	return &SymlinkCacheFS{
		CacheFS:        cfs,
		symlinkBacking: backing,
	}
}

// Lstat returns file info without following symlinks
func (c *SymlinkCacheFS) Lstat(name string) (fs.FileInfo, error) {
	return c.symlinkBacking.Lstat(name)
}

// Lchown changes the owner and group of a symlink
func (c *SymlinkCacheFS) Lchown(name string, uid, gid int) error {
	return c.symlinkBacking.Lchown(name, uid, gid)
}

// Readlink returns the destination of a symlink
func (c *SymlinkCacheFS) Readlink(name string) (string, error) {
	return c.symlinkBacking.Readlink(name)
}

// Symlink creates a symbolic link
func (c *SymlinkCacheFS) Symlink(oldname, newname string) error {
	return c.symlinkBacking.Symlink(oldname, newname)
}
