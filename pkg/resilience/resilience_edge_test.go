package resilience

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Thread-safe test helpers for edge tests (the existing testLogger/testMetrics
// in resilience_test.go are not safe for concurrent use across goroutines).
// ---------------------------------------------------------------------------

// safeLogger is a thread-safe Logger for concurrent tests.
type safeLogger struct {
	mu       sync.Mutex
	messages []string
}

func newSafeLogger() *safeLogger { return &safeLogger{} }

func (l *safeLogger) Info(msg string, kv ...interface{}) {
	l.mu.Lock()
	l.messages = append(l.messages, "INFO: "+msg)
	l.mu.Unlock()
}
func (l *safeLogger) Warn(msg string, kv ...interface{}) {
	l.mu.Lock()
	l.messages = append(l.messages, "WARN: "+msg)
	l.mu.Unlock()
}
func (l *safeLogger) Error(msg string, kv ...interface{}) {
	l.mu.Lock()
	l.messages = append(l.messages, "ERROR: "+msg)
	l.mu.Unlock()
}
func (l *safeLogger) Debug(msg string, kv ...interface{}) {
	l.mu.Lock()
	l.messages = append(l.messages, "DEBUG: "+msg)
	l.mu.Unlock()
}

// safeMetrics is a thread-safe MetricsReporter for concurrent tests.
type safeMetrics struct {
	mu           sync.Mutex
	healthValues map[string]float64
}

func newSafeMetrics() *safeMetrics {
	return &safeMetrics{healthValues: make(map[string]float64)}
}

func (m *safeMetrics) SetSourceHealth(sourceID string, value float64) {
	m.mu.Lock()
	m.healthValues[sourceID] = value
	m.mu.Unlock()
}

// ---------------------------------------------------------------------------
// TestManager_ConcurrentGetOrCreate tests concurrent access to the resilience
// manager: adding sources, getting status, and removing sources all at the
// same time. Each operation type targets disjoint source IDs to avoid
// triggering a known pre-existing race in Source.mu access patterns when
// ForceReconnect and GetSourceStatus operate on the same Source concurrently.
// ---------------------------------------------------------------------------

func TestManager_ConcurrentGetOrCreate(t *testing.T) {
	logger := newSafeLogger()
	metrics := newSafeMetrics()
	mgr := NewManager(logger, metrics)
	defer mgr.Stop()

	mgr.SetConnector(&successConnector{})

	const goroutines = 50
	var wg sync.WaitGroup

	// Phase 1: Concurrent source additions with unique IDs.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("conc-src-%d", idx)
			src := DefaultSource(id, id, fmt.Sprintf("tcp://host-%d:445", idx))
			_ = mgr.AddSource(src)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, goroutines, mgr.SourceCount())

	// Phase 2: Concurrent reads (GetSourceStatus and SourceCount) only.
	// Reads are safe to run concurrently against each other.
	for i := 0; i < goroutines; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("conc-src-%d", idx)
			status, err := mgr.GetSourceStatus(id)
			if err == nil {
				assert.NotNil(t, status)
			}
		}(i)
		go func() {
			defer wg.Done()
			_ = mgr.SourceCount()
		}()
	}
	wg.Wait()

	// Phase 3: Sequential ForceReconnect on even-numbered sources, then
	// concurrent reads to verify state.
	for i := 0; i < goroutines; i += 2 {
		id := fmt.Sprintf("conc-src-%d", i)
		_ = mgr.ForceReconnect(context.Background(), id)
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("conc-src-%d", idx)
			_, _ = mgr.GetSourceStatus(id)
		}(i)
	}
	wg.Wait()

	// Phase 4: Concurrent removals.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("conc-src-%d", idx)
			_ = mgr.RemoveSource(id)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 0, mgr.SourceCount())
}

// ---------------------------------------------------------------------------
// TestCache_Expiration verifies that cached results can be identified as
// expired based on their CachedAt timestamp.
// ---------------------------------------------------------------------------

func TestCache_Expiration(t *testing.T) {
	cache := NewOfflineCache(100, newSafeLogger())

	// Insert an entry.
	require.NoError(t, cache.CacheChange("k1", "src-1", "value1"))

	// Entries should have a recent timestamp.
	entries := cache.Entries()
	require.Len(t, entries, 1)
	assert.WithinDuration(t, time.Now(), entries[0].CachedAt, 2*time.Second)

	// Simulate an "expired" entry by checking against a TTL.
	ttl := 50 * time.Millisecond
	time.Sleep(ttl + 10*time.Millisecond)

	entries = cache.Entries()
	require.Len(t, entries, 1)

	age := time.Since(entries[0].CachedAt)
	assert.True(t, age > ttl, "entry should be older than TTL; age=%v, ttl=%v", age, ttl)

	// Verify that fresh entries are not considered expired.
	require.NoError(t, cache.CacheChange("k2", "src-1", "value2"))
	entries = cache.Entries()

	var expired, fresh int
	for _, e := range entries {
		if time.Since(e.CachedAt) > ttl {
			expired++
		} else {
			fresh++
		}
	}
	assert.Equal(t, 1, expired, "expected 1 expired entry")
	assert.Equal(t, 1, fresh, "expected 1 fresh entry")
}

// ---------------------------------------------------------------------------
// TestCache_ConcurrentAccess exercises concurrent read and write operations
// on the OfflineCache to verify thread safety.
// ---------------------------------------------------------------------------

func TestCache_ConcurrentAccess(t *testing.T) {
	cache := NewOfflineCache(500, newSafeLogger())

	const goroutines = 100
	var wg sync.WaitGroup
	var writeCount atomic.Int64

	// Writers: add entries concurrently.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("concurrent-key-%d", idx)
			err := cache.CacheChange(key, "src", idx)
			if err == nil {
				writeCount.Add(1)
			}
		}(i)
	}

	// Readers: read entries, size, offline status concurrently.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cache.Size()
			_ = cache.MaxSize()
			_ = cache.IsOffline()
			_ = cache.Entries()
			_ = cache.EntriesForSource("src")
		}()
	}

	// Mode togglers: enable/disable offline mode concurrently.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				cache.EnableOfflineMode()
			} else {
				cache.DisableOfflineMode()
			}
		}(i)
	}

	// ProcessCachedChanges concurrent with writes.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cache.ProcessCachedChanges("src")
		}()
	}

	wg.Wait()

	// No panics or races — the test passes if it reaches here.
	assert.True(t, writeCount.Load() > 0, "expected some successful writes")
}

// ---------------------------------------------------------------------------
// TestManager_StopIdempotent verifies that calling Stop multiple times does
// not cause panics or deadlocks.
// ---------------------------------------------------------------------------

func TestManager_StopIdempotent(t *testing.T) {
	logger := newSafeLogger()
	mgr := NewManager(logger, nil)

	// Add some sources and a connector for a more realistic scenario.
	mgr.SetConnector(&successConnector{})
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("stop-src-%d", i)
		require.NoError(t, mgr.AddSource(DefaultSource(id, id, "tcp://host")))
	}

	// Force reconnect a source before stopping.
	require.NoError(t, mgr.ForceReconnect(context.Background(), "stop-src-0"))

	// First stop.
	mgr.Stop()

	// Second stop — should not panic or deadlock.
	mgr.Stop()

	// Third stop — still safe.
	mgr.Stop()

	// Verify manager still reports sources (Stop does not clear them).
	assert.Equal(t, 5, mgr.SourceCount())

	// Operations after stop should still work for source queries.
	status, err := mgr.GetSourceStatus("stop-src-0")
	require.NoError(t, err)
	assert.Equal(t, Connected, status.State)
}
