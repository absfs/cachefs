package cachefs

import (
	"sync"
)

// BatchInvalidate invalidates multiple paths in a single lock acquisition.
// Returns the number of entries actually invalidated.
func (c *CacheFS) BatchInvalidate(paths []string) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	invalidated := 0

	for _, path := range paths {
		// Invalidate cache entry
		if entry, ok := c.entries[path]; ok {
			c.removeEntry(entry)
			invalidated++
		}

		// Invalidate metadata entry
		if metaEntry, ok := c.metadataEntries[path]; ok {
			delete(c.metadataEntries, path)
			putMetadataEntry(metaEntry)
		}
	}

	return invalidated
}

// BatchFlush flushes specific dirty entries to the backing store.
// Returns the first error encountered, but attempts to flush all entries.
func (c *CacheFS) BatchFlush(paths []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstError error

	for _, path := range paths {
		if entry, ok := c.entries[path]; ok && entry.dirty {
			if err := c.flushEntry(entry); err != nil && firstError == nil {
				firstError = err
			}
		}
	}

	return firstError
}

// EntryStats contains statistics for a single cache entry
type EntryStats struct {
	Path        string
	Size        int64
	Dirty       bool
	AccessCount uint64
	Cached      bool
}

// BatchStats returns statistics for multiple entries in a single lock acquisition.
// Entries not in cache will have Cached=false.
func (c *CacheFS) BatchStats(paths []string) map[string]EntryStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := make(map[string]EntryStats, len(paths))

	for _, path := range paths {
		if entry, ok := c.entries[path]; ok {
			stats[path] = EntryStats{
				Path:        path,
				Size:        entry.size,
				Dirty:       entry.dirty,
				AccessCount: entry.accessCount,
				Cached:      true,
			}
		} else {
			stats[path] = EntryStats{
				Path:   path,
				Cached: false,
			}
		}
	}

	return stats
}

// PrefetchOptions configures the Prefetch operation
type PrefetchOptions struct {
	// Workers is the number of concurrent workers to use for prefetching.
	// If 0, defaults to number of CPUs.
	Workers int

	// SkipErrors continues prefetching even if some files fail to load.
	// Errors are still returned but don't stop the operation.
	SkipErrors bool
}

// Prefetch pre-loads multiple files into the cache in parallel.
// Useful for cache warming scenarios.
func (c *CacheFS) Prefetch(paths []string, opts *PrefetchOptions) error {
	if opts == nil {
		opts = &PrefetchOptions{
			Workers:    4, // Default to 4 workers
			SkipErrors: true,
		}
	}

	if opts.Workers <= 0 {
		opts.Workers = 4
	}

	// Channel for distributing work
	pathChan := make(chan string, len(paths))
	for _, path := range paths {
		pathChan <- path
	}
	close(pathChan)

	// Errors channel
	errChan := make(chan error, len(paths))

	// Worker pool
	var wg sync.WaitGroup
	for i := 0; i < opts.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range pathChan {
				// Open and read file to populate cache
				file, err := c.OpenFile(path, 0, 0)
				if err != nil {
					errChan <- err
					if !opts.SkipErrors {
						return
					}
					continue
				}

				// Read entire file to ensure it's cached
				buf := make([]byte, 4096)
				for {
					_, err := file.Read(buf)
					if err != nil {
						break
					}
				}
				file.Close()
			}
		}()
	}

	// Wait for all workers
	wg.Wait()
	close(errChan)

	// Collect first error if any
	var firstError error
	for err := range errChan {
		if firstError == nil {
			firstError = err
		}
	}

	return firstError
}
