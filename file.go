package cachefs

import (
	"bytes"
	"io"
	"io/fs"
	"time"

	"github.com/absfs/absfs"
)

// cachedFile wraps a file and provides caching functionality
type cachedFile struct {
	cfs      *CacheFS
	file     absfs.File
	path     string
	buffer   *bytes.Buffer
	position int64
	flags    int
	mode     fs.FileMode
	closed   bool
}

// Name returns the name of the file
func (f *cachedFile) Name() string {
	return f.path
}

// Sync commits the file to stable storage
func (f *cachedFile) Sync() error {
	return f.file.Sync()
}

// ReadAt reads from the file at the specified offset
func (f *cachedFile) ReadAt(b []byte, off int64) (n int, err error) {
	return f.file.ReadAt(b, off)
}

// WriteAt writes to the file at the specified offset
func (f *cachedFile) WriteAt(b []byte, off int64) (n int, err error) {
	return f.file.WriteAt(b, off)
}

// Read reads from the cached file
func (f *cachedFile) Read(p []byte) (n int, err error) {
	if f.closed {
		return 0, fs.ErrClosed
	}

	// Check if we have cached data
	f.cfs.mu.RLock()
	entry, cached := f.cfs.entries[f.path]
	f.cfs.mu.RUnlock()

	if cached {
		// Check if entry is expired
		if f.cfs.ttl > 0 && !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
			// Entry expired - remove it and treat as cache miss
			f.cfs.mu.Lock()
			f.cfs.removeEntry(entry)
			f.cfs.mu.Unlock()
			cached = false
		} else {
			// Cache hit - read from cache
			f.cfs.stats.recordHit()

			// Update access time and move to head of LRU
			f.cfs.mu.Lock()
			entry.lastAccess = time.Now()
			entry.accessCount++
			f.cfs.moveToHead(entry)
			f.cfs.mu.Unlock()

			// Read from cached data
			if f.buffer == nil {
				f.buffer = bytes.NewBuffer(entry.data)
			}
			return f.buffer.Read(p)
		}
	}

	// Cache miss - read from backing file
	f.cfs.stats.recordMiss()

	// Read entire file to cache it
	data, err := io.ReadAll(f.file)
	if err != nil {
		return 0, err
	}

	// Create cache entry from pool
	now := time.Now()
	newEntry := getCacheEntry()
	newEntry.path = f.path
	newEntry.data = data
	newEntry.modTime = now
	newEntry.size = int64(len(data))
	newEntry.dirty = false
	newEntry.lastAccess = now
	newEntry.accessCount = 1
	newEntry.createdAt = now

	if f.cfs.ttl > 0 {
		newEntry.expiresAt = now.Add(f.cfs.ttl)
	}

	// Add to cache
	f.cfs.mu.Lock()
	f.cfs.entries[f.path] = newEntry
	f.cfs.moveToHead(newEntry)
	f.cfs.stats.addBytes(uint64(len(data)))
	f.cfs.stats.incEntries()

	// Evict if needed
	if err := f.cfs.evictIfNeeded(); err != nil {
		f.cfs.mu.Unlock()
		return 0, err
	}
	f.cfs.mu.Unlock()

	// Read from buffer
	f.buffer = bytes.NewBuffer(data)
	return f.buffer.Read(p)
}

// Write writes to the cached file
func (f *cachedFile) Write(p []byte) (n int, err error) {
	if f.closed {
		return 0, fs.ErrClosed
	}

	switch f.cfs.writeMode {
	case WriteModeWriteThrough:
		// Write to backing store first
		n, err = f.file.Write(p)
		if err != nil {
			return n, err
		}

		// Update cache if entry exists
		f.cfs.mu.Lock()
		if entry, ok := f.cfs.entries[f.path]; ok {
			// For simplicity, invalidate the cache entry on write
			// A more sophisticated implementation would update the cached data
			f.cfs.removeEntry(entry)
		}
		f.cfs.mu.Unlock()

		return n, nil

	case WriteModeWriteBack:
		// Write to cache, mark as dirty
		f.cfs.mu.Lock()
		defer f.cfs.mu.Unlock()

		entry, ok := f.cfs.entries[f.path]
		if !ok {
			// Create new cache entry from pool
			now := time.Now()
			entry = getCacheEntry()
			entry.path = f.path
			entry.data = make([]byte, 0)
			entry.modTime = now
			entry.size = 0
			entry.dirty = true
			entry.lastAccess = now
			entry.accessCount = 1
			entry.createdAt = now

			if f.cfs.ttl > 0 {
				entry.expiresAt = now.Add(f.cfs.ttl)
			}

			f.cfs.entries[f.path] = entry
			f.cfs.moveToHead(entry)
			f.cfs.stats.incEntries()
		}

		// Append data to cache entry
		entry.data = append(entry.data, p...)
		entry.size = int64(len(entry.data))
		entry.modTime = time.Now()
		entry.dirty = true

		// Update stats
		f.cfs.stats.addBytes(uint64(len(p)))

		// Evict if needed
		if err := f.cfs.evictIfNeeded(); err != nil {
			return 0, err
		}

		return len(p), nil

	case WriteModeWriteAround:
		// Bypass cache, write directly to backing store
		f.cfs.mu.Lock()
		if entry, ok := f.cfs.entries[f.path]; ok {
			f.cfs.removeEntry(entry)
		}
		f.cfs.mu.Unlock()

		return f.file.Write(p)

	default:
		return f.file.Write(p)
	}
}

// Seek sets the file position
func (f *cachedFile) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, fs.ErrClosed
	}

	// If we have a buffer, seek in it
	if f.buffer != nil {
		switch whence {
		case io.SeekStart:
			f.position = offset
		case io.SeekCurrent:
			f.position += offset
		case io.SeekEnd:
			f.position = int64(f.buffer.Len()) + offset
		}
		// Reset buffer to position
		f.cfs.mu.RLock()
		if entry, ok := f.cfs.entries[f.path]; ok {
			f.buffer = bytes.NewBuffer(entry.data[f.position:])
		}
		f.cfs.mu.RUnlock()
		return f.position, nil
	}

	return f.file.Seek(offset, whence)
}

// Close closes the file
func (f *cachedFile) Close() error {
	if f.closed {
		return fs.ErrClosed
	}
	f.closed = true

	// Flush if needed
	if f.cfs.flushOnClose && f.cfs.writeMode == WriteModeWriteBack {
		f.cfs.mu.Lock()
		if entry, ok := f.cfs.entries[f.path]; ok && entry.dirty {
			if err := f.cfs.flushEntry(entry); err != nil {
				f.cfs.mu.Unlock()
				return err
			}
		}
		f.cfs.mu.Unlock()
	}

	return f.file.Close()
}

// Stat returns file info
func (f *cachedFile) Stat() (fs.FileInfo, error) {
	return f.file.Stat()
}

// Readdir reads directory entries
func (f *cachedFile) Readdir(n int) ([]fs.FileInfo, error) {
	return f.file.Readdir(n)
}

// Readdirnames reads directory entry names
func (f *cachedFile) Readdirnames(n int) ([]string, error) {
	return f.file.Readdirnames(n)
}

// ReadDir reads directory entries from the file
func (f *cachedFile) ReadDir(n int) ([]fs.DirEntry, error) {
	// Check if the underlying file implements ReadDir
	type dirReader interface {
		ReadDir(n int) ([]fs.DirEntry, error)
	}

	if dr, ok := f.file.(dirReader); ok {
		return dr.ReadDir(n)
	}

	// Fall back to Readdir and convert FileInfo to DirEntry
	fileInfos, err := f.file.Readdir(n)
	if err != nil {
		return nil, err
	}

	entries := make([]fs.DirEntry, len(fileInfos))
	for i, info := range fileInfos {
		entries[i] = fs.FileInfoToDirEntry(info)
	}
	return entries, nil
}

// Truncate changes the file size
func (f *cachedFile) Truncate(size int64) error {
	// Invalidate cache entry
	f.cfs.mu.Lock()
	if entry, ok := f.cfs.entries[f.path]; ok {
		f.cfs.removeEntry(entry)
	}
	f.cfs.mu.Unlock()

	return f.file.Truncate(size)
}

// WriteString writes a string to the file
func (f *cachedFile) WriteString(s string) (n int, err error) {
	return f.Write([]byte(s))
}
