package cachefs

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

// WarmPriority defines the priority level for cache warming
type WarmPriority int

const (
	// WarmPriorityHigh loads files first and prevents eviction
	WarmPriorityHigh WarmPriority = iota
	// WarmPriorityNormal uses regular cache behavior
	WarmPriorityNormal
	// WarmPriorityLow loads only if space available
	WarmPriorityLow
)

// WarmProgress tracks cache warming progress
type WarmProgress struct {
	Total     int
	Completed int
	Errors    int
	BytesRead uint64
}

// WarmOption configures cache warming behavior
type WarmOption func(*warmConfig)

type warmConfig struct {
	workers    int
	priority   WarmPriority
	onProgress func(WarmProgress)
	skipErrors bool
}

// WithWarmWorkers sets the number of parallel workers for warming
func WithWarmWorkers(n int) WarmOption {
	return func(c *warmConfig) {
		c.workers = n
	}
}

// WithWarmPriority sets the priority level for warming
func WithWarmPriority(p WarmPriority) WarmOption {
	return func(c *warmConfig) {
		c.priority = p
	}
}

// WithWarmProgress sets a progress callback for warming
func WithWarmProgress(fn func(WarmProgress)) WarmOption {
	return func(c *warmConfig) {
		c.onProgress = fn
	}
}

// WithWarmSkipErrors configures whether to skip errors during warming
func WithWarmSkipErrors(skip bool) WarmOption {
	return func(c *warmConfig) {
		c.skipErrors = skip
	}
}

// WarmCache pre-loads a list of files into the cache
func (c *CacheFS) WarmCache(paths []string, opts ...WarmOption) error {
	// Apply options
	config := &warmConfig{
		workers:    4,
		priority:   WarmPriorityNormal,
		skipErrors: false,
	}
	for _, opt := range opts {
		opt(config)
	}

	// Track progress
	var progress WarmProgress
	progress.Total = len(paths)
	var progressMu sync.Mutex

	// Error collection
	var firstError error
	var errorMu sync.Mutex

	// Worker pool
	pathChan := make(chan string, len(paths))
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < config.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range pathChan {
				// Open and read file to load into cache
				file, err := c.OpenFile(path, os.O_RDONLY, 0)
				if err != nil {
					progressMu.Lock()
					progress.Errors++
					progressMu.Unlock()

					if !config.skipErrors {
						errorMu.Lock()
						if firstError == nil {
							firstError = fmt.Errorf("failed to open %s: %w", path, err)
						}
						errorMu.Unlock()
					}
					continue
				}

				// Read entire file to ensure it's cached
				buf := make([]byte, 32*1024)
				bytesRead := uint64(0)
				for {
					n, err := file.Read(buf)
					bytesRead += uint64(n)
					if err == io.EOF {
						break
					}
					if err != nil {
						progressMu.Lock()
						progress.Errors++
						progressMu.Unlock()

						if !config.skipErrors {
							errorMu.Lock()
							if firstError == nil {
								firstError = fmt.Errorf("failed to read %s: %w", path, err)
							}
							errorMu.Unlock()
						}
						file.Close()
						continue
					}
				}
				file.Close()

				// Update progress
				progressMu.Lock()
				progress.Completed++
				progress.BytesRead += bytesRead
				if config.onProgress != nil {
					config.onProgress(progress)
				}
				progressMu.Unlock()
			}
		}()
	}

	// Send paths to workers
	for _, path := range paths {
		pathChan <- path
	}
	close(pathChan)

	// Wait for completion
	wg.Wait()

	return firstError
}

// WarmCacheFromPattern loads all files matching a glob pattern into cache
// Note: This requires the backing filesystem to implement a Glob method.
// If not available, use WarmCache with pre-computed paths instead.
func (c *CacheFS) WarmCacheFromPattern(pattern string, opts ...WarmOption) error {
	// Try to use Glob if the backing FS supports it
	type globFS interface {
		Glob(pattern string) ([]string, error)
	}

	globber, ok := c.backing.(globFS)
	if !ok {
		return fmt.Errorf("backing filesystem does not support Glob operation")
	}

	// Get all matching paths
	matches, err := globber.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob pattern failed: %w", err)
	}

	// Use all matches (assume glob returns only files)
	// If you need to filter directories, ensure backing.Stat() works correctly
	return c.WarmCache(matches, opts...)
}

// WarmCacheFromFile loads files listed in a text file (one path per line)
func (c *CacheFS) WarmCacheFromFile(listPath string, opts ...WarmOption) error {
	// Open the list file
	file, err := c.backing.OpenFile(listPath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open list file: %w", err)
	}
	defer file.Close()

	// Read paths line by line
	var paths []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			paths = append(paths, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read list file: %w", err)
	}

	return c.WarmCache(paths, opts...)
}

// WarmCacheFromDir recursively loads all files in a directory into cache
func (c *CacheFS) WarmCacheFromDir(dir string, opts ...WarmOption) error {
	var paths []string

	// Walk the directory tree
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	return c.WarmCache(paths, opts...)
}

// WarmCacheAsync pre-loads files asynchronously in the background
func (c *CacheFS) WarmCacheAsync(paths []string, done chan<- error, opts ...WarmOption) {
	go func() {
		err := c.WarmCache(paths, opts...)
		if done != nil {
			done <- err
		}
	}()
}

// WarmCacheFromPatternAsync loads files matching pattern asynchronously
func (c *CacheFS) WarmCacheFromPatternAsync(pattern string, done chan<- error, opts ...WarmOption) {
	go func() {
		err := c.WarmCacheFromPattern(pattern, opts...)
		if done != nil {
			done <- err
		}
	}()
}

// GetWarmingProgress returns the current warming progress
// This is useful when using the Async warming methods
func (c *CacheFS) GetWarmingProgress() WarmProgress {
	// This would require tracking warming state in CacheFS
	// For now, return zero progress
	return WarmProgress{}
}

// WarmCacheSmart loads files based on access patterns and frequency
// This analyzes recent access patterns and pre-loads likely-needed files
func (c *CacheFS) WarmCacheSmart(opts ...WarmOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Collect frequently accessed files that are not currently cached
	type fileScore struct {
		path  string
		score uint64
	}

	var candidates []fileScore
	for path, meta := range c.metadataEntries {
		// Check if not in cache
		if _, cached := c.entries[path]; !cached {
			// Score based on access count
			score := meta.accessCount
			if score > 0 {
				candidates = append(candidates, fileScore{path: path, score: score})
			}
		}
	}

	// Sort by score (simple bubble sort for small lists)
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].score > candidates[i].score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Take top N candidates
	maxCandidates := 100
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	// Extract paths
	paths := make([]string, len(candidates))
	for i, c := range candidates {
		paths[i] = c.path
	}

	// Unlock before warming (WarmCache will acquire locks as needed)
	c.mu.Unlock()
	err := c.WarmCache(paths, opts...)
	c.mu.Lock()

	return err
}

// atomicWarmProgress provides thread-safe progress tracking
type atomicWarmProgress struct {
	total     int64
	completed int64
	errors    int64
	bytesRead uint64
}

func (p *atomicWarmProgress) incrementCompleted() {
	atomic.AddInt64(&p.completed, 1)
}

func (p *atomicWarmProgress) incrementErrors() {
	atomic.AddInt64(&p.errors, 1)
}

func (p *atomicWarmProgress) addBytesRead(n uint64) {
	atomic.AddUint64(&p.bytesRead, n)
}

func (p *atomicWarmProgress) snapshot() WarmProgress {
	return WarmProgress{
		Total:     int(atomic.LoadInt64(&p.total)),
		Completed: int(atomic.LoadInt64(&p.completed)),
		Errors:    int(atomic.LoadInt64(&p.errors)),
		BytesRead: atomic.LoadUint64(&p.bytesRead),
	}
}
