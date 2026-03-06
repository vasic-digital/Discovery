package resilience

import (
	"fmt"
	"sync"
	"time"
)

// CacheEntry represents a single cached item stored while a source is
// unavailable. Entries are timestamped so consumers can decide how stale
// data is acceptable.
type CacheEntry struct {
	Key       string      `json:"key"`
	Value     interface{} `json:"value"`
	SourceID  string      `json:"source_id"`
	CachedAt  time.Time   `json:"cached_at"`
}

// OfflineCache stores metadata entries when network sources are unavailable.
// It is bounded by maxSize; when full, the oldest entry is evicted on insert.
type OfflineCache struct {
	entries  []*CacheEntry
	maxSize  int
	offline  bool
	logger   Logger
	mu       sync.RWMutex
}

// NewOfflineCache creates a new OfflineCache with the given maximum size.
// If maxSize is <= 0, a default of 1000 is used.
func NewOfflineCache(maxSize int, logger Logger) *OfflineCache {
	if maxSize <= 0 {
		maxSize = 1000
	}
	return &OfflineCache{
		entries: make([]*CacheEntry, 0),
		maxSize: maxSize,
		logger:  logger,
	}
}

// CacheChange stores a change entry in the offline cache. If the cache is
// full the oldest entry is evicted. Returns an error if key or sourceID
// are empty.
func (c *OfflineCache) CacheChange(key, sourceID string, value interface{}) error {
	if key == "" {
		return fmt.Errorf("cache key must not be empty")
	}
	if sourceID == "" {
		return fmt.Errorf("source ID must not be empty")
	}

	entry := &CacheEntry{
		Key:      key,
		Value:    value,
		SourceID: sourceID,
		CachedAt: time.Now(),
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxSize {
		// Evict the oldest entry.
		c.entries = c.entries[1:]
		c.logger.Debug("cache eviction", "evicted_oldest", true, "max_size", c.maxSize)
	}

	c.entries = append(c.entries, entry)
	c.logger.Debug("change cached", "key", key, "source_id", sourceID)
	return nil
}

// ProcessCachedChanges removes and returns all cached entries for the
// given sourceID, in insertion order. This is typically called when a
// source comes back online so queued changes can be replayed.
func (c *OfflineCache) ProcessCachedChanges(sourceID string) []*CacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	var matched []*CacheEntry
	remaining := make([]*CacheEntry, 0, len(c.entries))

	for _, e := range c.entries {
		if e.SourceID == sourceID {
			matched = append(matched, e)
		} else {
			remaining = append(remaining, e)
		}
	}

	c.entries = remaining

	if len(matched) > 0 {
		c.logger.Info("processed cached changes",
			"source_id", sourceID,
			"count", len(matched),
		)
	}

	return matched
}

// EnableOfflineMode sets the cache to offline mode.
func (c *OfflineCache) EnableOfflineMode() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offline = true
	c.logger.Info("offline mode enabled")
}

// DisableOfflineMode sets the cache back to online mode.
func (c *OfflineCache) DisableOfflineMode() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offline = false
	c.logger.Info("offline mode disabled")
}

// IsOffline reports whether the cache is in offline mode.
func (c *OfflineCache) IsOffline() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.offline
}

// Size returns the current number of entries in the cache.
func (c *OfflineCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// MaxSize returns the configured maximum cache size.
func (c *OfflineCache) MaxSize() int {
	return c.maxSize
}

// Clear removes all entries from the cache.
func (c *OfflineCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make([]*CacheEntry, 0)
	c.logger.Info("cache cleared")
}

// Entries returns a copy of all current cache entries.
func (c *OfflineCache) Entries() []*CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cp := make([]*CacheEntry, len(c.entries))
	copy(cp, c.entries)
	return cp
}

// EntriesForSource returns a copy of all cache entries for the given
// sourceID, without removing them.
func (c *OfflineCache) EntriesForSource(sourceID string) []*CacheEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*CacheEntry
	for _, e := range c.entries {
		if e.SourceID == sourceID {
			result = append(result, e)
		}
	}
	return result
}
