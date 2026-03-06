package resilience

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test connector implementations
// ---------------------------------------------------------------------------

// successConnector always succeeds.
type successConnector struct{}

func (c *successConnector) Connect(_ context.Context, _ *Source) error      { return nil }
func (c *successConnector) HealthCheck(_ context.Context, _ *Source) error  { return nil }

// failConnector always fails with the configured error.
type failConnector struct {
	err error
}

func (c *failConnector) Connect(_ context.Context, _ *Source) error     { return c.err }
func (c *failConnector) HealthCheck(_ context.Context, _ *Source) error { return c.err }

// countingConnector counts invocations and fails N times before succeeding.
type countingConnector struct {
	mu            sync.Mutex
	connectCalls  int
	healthCalls   int
	failUntil     int // Connect fails until connectCalls > failUntil
}

func (c *countingConnector) Connect(_ context.Context, _ *Source) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectCalls++
	if c.connectCalls <= c.failUntil {
		return fmt.Errorf("connect attempt %d failed", c.connectCalls)
	}
	return nil
}

func (c *countingConnector) HealthCheck(_ context.Context, _ *Source) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.healthCalls++
	return nil
}

// ---------------------------------------------------------------------------
// NewManager tests
// ---------------------------------------------------------------------------

func TestNewManager(t *testing.T) {
	logger := newTestLogger()
	metrics := newTestMetrics()
	mgr := NewManager(logger, metrics)
	defer mgr.Stop()

	require.NotNil(t, mgr)
	assert.Equal(t, 0, mgr.SourceCount())
}

func TestNewManager_NilMetrics(t *testing.T) {
	logger := newTestLogger()
	mgr := NewManager(logger, nil)
	defer mgr.Stop()

	require.NotNil(t, mgr)
}

// ---------------------------------------------------------------------------
// AddSource tests
// ---------------------------------------------------------------------------

func TestManager_AddSource(t *testing.T) {
	logger := newTestLogger()
	metrics := newTestMetrics()
	mgr := NewManager(logger, metrics)
	defer mgr.Stop()

	src := DefaultSource("s1", "Source 1", "tcp://host1:445")
	err := mgr.AddSource(src)

	assert.NoError(t, err)
	assert.Equal(t, 1, mgr.SourceCount())
	assert.Equal(t, Degraded, metrics.healthValues["s1"]) // Disconnected -> 0.5
}

func TestManager_AddSource_NilSource(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	err := mgr.AddSource(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must not be nil")
}

func TestManager_AddSource_EmptyID(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	src := &Source{Name: "test"}
	err := mgr.AddSource(src)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ID must not be empty")
}

func TestManager_AddSource_Duplicate(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	src := DefaultSource("dup", "Dup", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.AddSource(DefaultSource("dup", "Dup2", "tcp://host2"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestManager_AddMultipleSources(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("s-%d", i)
		require.NoError(t, mgr.AddSource(DefaultSource(id, id, "tcp://host")))
	}
	assert.Equal(t, 5, mgr.SourceCount())
}

// ---------------------------------------------------------------------------
// RemoveSource tests
// ---------------------------------------------------------------------------

func TestManager_RemoveSource(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	require.NoError(t, mgr.AddSource(DefaultSource("r1", "Remove", "tcp://h")))
	assert.Equal(t, 1, mgr.SourceCount())

	err := mgr.RemoveSource("r1")
	assert.NoError(t, err)
	assert.Equal(t, 0, mgr.SourceCount())
}

func TestManager_RemoveSource_NotFound(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	err := mgr.RemoveSource("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// GetSourceStatus tests
// ---------------------------------------------------------------------------

func TestManager_GetSourceStatus(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	src := DefaultSource("gs1", "Get Status", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	status, err := mgr.GetSourceStatus("gs1")
	require.NoError(t, err)
	require.NotNil(t, status)

	assert.Equal(t, "gs1", status.ID)
	assert.Equal(t, Disconnected, status.State)
}

func TestManager_GetSourceStatus_ReturnsSnapshot(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	src := DefaultSource("snap", "Snap", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	status, err := mgr.GetSourceStatus("snap")
	require.NoError(t, err)

	// Mutating the returned snapshot should not affect the internal source.
	status.State = Offline

	status2, err := mgr.GetSourceStatus("snap")
	require.NoError(t, err)
	assert.Equal(t, Disconnected, status2.State)
}

func TestManager_GetSourceStatus_NotFound(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	_, err := mgr.GetSourceStatus("nope")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// SetConnector tests
// ---------------------------------------------------------------------------

func TestManager_SetConnector(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	mgr.SetConnector(&successConnector{})
	// No panic, connector is set.
}

// ---------------------------------------------------------------------------
// ForceReconnect tests
// ---------------------------------------------------------------------------

func TestManager_ForceReconnect_Success(t *testing.T) {
	logger := newTestLogger()
	metrics := newTestMetrics()
	mgr := NewManager(logger, metrics)
	defer mgr.Stop()

	mgr.SetConnector(&successConnector{})

	src := DefaultSource("fr1", "Force", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.ForceReconnect(context.Background(), "fr1")
	assert.NoError(t, err)

	status, _ := mgr.GetSourceStatus("fr1")
	assert.Equal(t, Connected, status.State)
	assert.False(t, status.LastConnected.IsZero())
	assert.Equal(t, Healthy, metrics.healthValues["fr1"])
}

func TestManager_ForceReconnect_Failure(t *testing.T) {
	logger := newTestLogger()
	metrics := newTestMetrics()
	mgr := NewManager(logger, metrics)
	defer mgr.Stop()

	mgr.SetConnector(&failConnector{err: fmt.Errorf("refused")})

	src := DefaultSource("fr2", "FailRecon", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.ForceReconnect(context.Background(), "fr2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "refused")

	status, _ := mgr.GetSourceStatus("fr2")
	assert.Equal(t, Disconnected, status.State)
	assert.Equal(t, Degraded, metrics.healthValues["fr2"])
}

func TestManager_ForceReconnect_NoConnector(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	src := DefaultSource("nc1", "NoConn", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.ForceReconnect(context.Background(), "nc1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no connector configured")
}

func TestManager_ForceReconnect_NotFound(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	err := mgr.ForceReconnect(context.Background(), "ghost")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_ForceReconnect_ResetsRetryAttempts(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	src := DefaultSource("rr1", "ResetRetry", "tcp://host")
	src.RetryAttempts = 3
	require.NoError(t, mgr.AddSource(src))

	err := mgr.ForceReconnect(context.Background(), "rr1")
	assert.NoError(t, err)

	status, _ := mgr.GetSourceStatus("rr1")
	assert.Equal(t, 0, status.RetryAttempts)
}

// ---------------------------------------------------------------------------
// CheckHealth tests
// ---------------------------------------------------------------------------

func TestManager_CheckHealth_Success(t *testing.T) {
	metrics := newTestMetrics()
	mgr := NewManager(newTestLogger(), metrics)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	src := DefaultSource("ch1", "HealthOK", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.CheckHealth(context.Background(), "ch1")
	assert.NoError(t, err)

	status, _ := mgr.GetSourceStatus("ch1")
	assert.Equal(t, Connected, status.State)
	assert.Equal(t, Healthy, metrics.healthValues["ch1"])
}

func TestManager_CheckHealth_Failure_DisconnectsConnectedSource(t *testing.T) {
	metrics := newTestMetrics()
	mgr := NewManager(newTestLogger(), metrics)
	defer mgr.Stop()

	conn := &successConnector{}
	mgr.SetConnector(conn)

	src := DefaultSource("ch2", "HealthFail", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	// First connect the source.
	require.NoError(t, mgr.ForceReconnect(context.Background(), "ch2"))
	status, _ := mgr.GetSourceStatus("ch2")
	assert.Equal(t, Connected, status.State)

	// Now switch to a failing connector and check health.
	mgr.SetConnector(&failConnector{err: fmt.Errorf("timeout")})

	err := mgr.CheckHealth(context.Background(), "ch2")
	assert.Error(t, err)

	status, _ = mgr.GetSourceStatus("ch2")
	assert.Equal(t, Disconnected, status.State)
	assert.Equal(t, Degraded, metrics.healthValues["ch2"])
}

func TestManager_CheckHealth_RecoversPreviouslyDisconnected(t *testing.T) {
	mgr := NewManager(newTestLogger(), newTestMetrics())
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	src := DefaultSource("ch3", "Recover", "tcp://host")
	src.State = Disconnected
	require.NoError(t, mgr.AddSource(src))

	err := mgr.CheckHealth(context.Background(), "ch3")
	assert.NoError(t, err)

	status, _ := mgr.GetSourceStatus("ch3")
	assert.Equal(t, Connected, status.State)
	assert.False(t, status.LastConnected.IsZero())
}

func TestManager_CheckHealth_NotFound(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	err := mgr.CheckHealth(context.Background(), "missing")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_CheckHealth_NoConnector(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	src := DefaultSource("ch4", "NoConn", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.CheckHealth(context.Background(), "ch4")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no connector configured")
}

// ---------------------------------------------------------------------------
// RecoverSource tests
// ---------------------------------------------------------------------------

func TestManager_RecoverSource_ImmediateSuccess(t *testing.T) {
	metrics := newTestMetrics()
	mgr := NewManager(newTestLogger(), metrics)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	src := DefaultSource("rec1", "RecoverOK", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.RecoverSource(context.Background(), "rec1")
	assert.NoError(t, err)

	status, _ := mgr.GetSourceStatus("rec1")
	assert.Equal(t, Connected, status.State)
	assert.Equal(t, 1, status.RetryAttempts)
	assert.Equal(t, Healthy, metrics.healthValues["rec1"])
}

func TestManager_RecoverSource_SucceedsAfterRetries(t *testing.T) {
	mgr := NewManager(newTestLogger(), newTestMetrics())
	defer mgr.Stop()

	conn := &countingConnector{failUntil: 2}
	mgr.SetConnector(conn)

	src := DefaultSource("rec2", "RetryRecover", "tcp://host")
	src.MaxRetryAttempts = 5
	src.RetryDelay = 1 * time.Millisecond // fast for testing
	require.NoError(t, mgr.AddSource(src))

	err := mgr.RecoverSource(context.Background(), "rec2")
	assert.NoError(t, err)

	conn.mu.Lock()
	assert.Equal(t, 3, conn.connectCalls) // failed 2, succeeded on 3rd
	conn.mu.Unlock()

	status, _ := mgr.GetSourceStatus("rec2")
	assert.Equal(t, Connected, status.State)
	assert.Equal(t, 3, status.RetryAttempts)
}

func TestManager_RecoverSource_ExhaustsRetries(t *testing.T) {
	metrics := newTestMetrics()
	mgr := NewManager(newTestLogger(), metrics)
	defer mgr.Stop()
	mgr.SetConnector(&failConnector{err: fmt.Errorf("always fails")})

	src := DefaultSource("rec3", "ExhaustRetries", "tcp://host")
	src.MaxRetryAttempts = 3
	src.RetryDelay = 1 * time.Millisecond
	require.NoError(t, mgr.AddSource(src))

	err := mgr.RecoverSource(context.Background(), "rec3")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "declared offline")

	status, _ := mgr.GetSourceStatus("rec3")
	assert.Equal(t, Offline, status.State)
	assert.Equal(t, 3, status.RetryAttempts)
	assert.Equal(t, OfflineHealth, metrics.healthValues["rec3"])
}

func TestManager_RecoverSource_ContextCancelled(t *testing.T) {
	mgr := NewManager(newTestLogger(), newTestMetrics())
	defer mgr.Stop()
	mgr.SetConnector(&failConnector{err: fmt.Errorf("fail")})

	src := DefaultSource("rec4", "CancelRecover", "tcp://host")
	src.MaxRetryAttempts = 100
	src.RetryDelay = 1 * time.Millisecond
	require.NoError(t, mgr.AddSource(src))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := mgr.RecoverSource(ctx, "rec4")
	assert.Error(t, err)

	status, _ := mgr.GetSourceStatus("rec4")
	assert.Equal(t, Offline, status.State)
}

func TestManager_RecoverSource_NotFound(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	err := mgr.RecoverSource(context.Background(), "nope")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestManager_RecoverSource_NoConnector(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	src := DefaultSource("rec5", "NoConn", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.RecoverSource(context.Background(), "rec5")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no connector configured")
}

// ---------------------------------------------------------------------------
// Event handler tests
// ---------------------------------------------------------------------------

func TestManager_OnEvent_ReceivesEvents(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	var events []*Event
	var mu sync.Mutex
	mgr.OnEvent(func(evt *Event) {
		mu.Lock()
		events = append(events, evt)
		mu.Unlock()
	})

	src := DefaultSource("ev1", "Events", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	require.NoError(t, mgr.ForceReconnect(context.Background(), "ev1"))

	mu.Lock()
	defer mu.Unlock()

	// Should have received Reconnecting + Connected events.
	require.GreaterOrEqual(t, len(events), 2)

	types := make([]EventType, len(events))
	for i, e := range events {
		types[i] = e.Type
	}
	assert.Contains(t, types, EventReconnecting)
	assert.Contains(t, types, EventConnected)
}

func TestManager_OnEvent_MultipleHandlers(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	var count1, count2 int
	var mu sync.Mutex

	mgr.OnEvent(func(_ *Event) {
		mu.Lock()
		count1++
		mu.Unlock()
	})
	mgr.OnEvent(func(_ *Event) {
		mu.Lock()
		count2++
		mu.Unlock()
	})

	src := DefaultSource("mh1", "MultiHandler", "tcp://host")
	require.NoError(t, mgr.AddSource(src))
	require.NoError(t, mgr.ForceReconnect(context.Background(), "mh1"))

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, count1, count2)
	assert.Greater(t, count1, 0)
}

// ---------------------------------------------------------------------------
// Stop tests
// ---------------------------------------------------------------------------

func TestManager_Stop(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)

	// Should not panic when called.
	mgr.Stop()
}

func TestManager_Stop_Idempotent(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	mgr.Stop()
	// Second stop should not panic.
	mgr.Stop()
}

// ---------------------------------------------------------------------------
// Concurrent access tests
// ---------------------------------------------------------------------------

func TestManager_ConcurrentAddAndStatus(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("conc-%d", idx)
			_ = mgr.AddSource(DefaultSource(id, id, "tcp://host"))
			_, _ = mgr.GetSourceStatus(id)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, 20, mgr.SourceCount())
}
