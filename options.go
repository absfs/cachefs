package cachefs

import "time"

// Option is a function that configures a CacheFS
type Option func(*CacheFS)

// WithWriteMode sets the write mode for the cache
func WithWriteMode(mode WriteMode) Option {
	return func(c *CacheFS) {
		c.writeMode = mode
	}
}

// WithEvictionPolicy sets the eviction policy for the cache
func WithEvictionPolicy(policy EvictionPolicy) Option {
	return func(c *CacheFS) {
		c.evictionPolicy = policy
	}
}

// WithMaxBytes sets the maximum cache size in bytes
func WithMaxBytes(bytes uint64) Option {
	return func(c *CacheFS) {
		c.maxBytes = bytes
	}
}

// WithMaxEntries sets the maximum number of cache entries
func WithMaxEntries(entries uint64) Option {
	return func(c *CacheFS) {
		c.maxEntries = entries
	}
}

// WithTTL sets the time-to-live for cache entries
func WithTTL(ttl time.Duration) Option {
	return func(c *CacheFS) {
		c.ttl = ttl
	}
}

// WithFlushInterval sets the interval for flushing dirty entries in write-back mode
func WithFlushInterval(interval time.Duration) Option {
	return func(c *CacheFS) {
		c.flushInterval = interval
	}
}

// WithFlushOnClose enables flushing dirty entries when files are closed
func WithFlushOnClose(enable bool) Option {
	return func(c *CacheFS) {
		c.flushOnClose = enable
	}
}

// WithMetadataCache enables separate metadata caching
func WithMetadataCache(enable bool) Option {
	return func(c *CacheFS) {
		c.metadataCache = enable
	}
}

// WithMetadataMaxEntries sets the maximum number of metadata cache entries
func WithMetadataMaxEntries(entries uint64) Option {
	return func(c *CacheFS) {
		c.metadataMaxEntries = entries
	}
}
