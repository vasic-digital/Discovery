package resilience

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// RecoverSource: cover context cancellation during the backoff sleep
// (the second ctx.Done() case at lines 321-329 in manager.go).
//
// The existing test TestManager_RecoverSource_ContextCancelled uses a
// short timeout which may cancel either before the Connect call or
// during the backoff sleep. This test is designed to precisely trigger
// the cancellation during the backoff sleep by using a slow connector
// that fails on the first attempt, then a longer backoff delay.
// ---------------------------------------------------------------------------

// slowFailConnector fails on every Connect and takes a configurable delay
// to simulate slow connections.
type slowFailConnector struct {
	err error
}

func (c *slowFailConnector) Connect(_ context.Context, _ *Source) error {
	return c.err
}

func (c *slowFailConnector) HealthCheck(_ context.Context, _ *Source) error {
	return nil
}

// TestRecoverSource_ContextCancelledDuringBackoff verifies that RecoverSource
// properly handles context cancellation while waiting in the exponential
// backoff sleep (the second ctx.Done select case).
func TestRecoverSource_ContextCancelledDuringBackoff(t *testing.T) {
	metrics := newTestMetrics()
	mgr := NewManager(newTestLogger(), metrics)
	defer mgr.Stop()

	// Use a connector that always fails immediately.
	mgr.SetConnector(&slowFailConnector{err: fmt.Errorf("connection refused")})

	src := DefaultSource("backoff-cancel", "BackoffCancel", "tcp://host")
	src.MaxRetryAttempts = 100 // High enough that we won't exhaust retries
	src.RetryDelay = 2 * time.Second // Long enough that context cancels during sleep
	require.NoError(t, mgr.AddSource(src))

	// Create a context that cancels after the first failure + partial backoff.
	// The first connect attempt fails immediately, then we enter the
	// backoff sleep of 2s. We cancel after 200ms, which is during the sleep.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := mgr.RecoverSource(ctx, "backoff-cancel")
	assert.Error(t, err)

	status, _ := mgr.GetSourceStatus("backoff-cancel")
	assert.Equal(t, Offline, status.State)
	assert.Equal(t, OfflineHealth, metrics.healthValues["backoff-cancel"])
}

// TestRecoverSource_ContextCancelledBeforeFirstAttempt verifies that
// RecoverSource handles context cancellation before the first connect
// attempt (the first ctx.Done select case in the loop).
func TestRecoverSource_ContextCancelledBeforeFirstAttempt(t *testing.T) {
	metrics := newTestMetrics()
	mgr := NewManager(newTestLogger(), metrics)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	src := DefaultSource("pre-cancel", "PreCancel", "tcp://host")
	src.MaxRetryAttempts = 5
	src.RetryDelay = 1 * time.Millisecond
	require.NoError(t, mgr.AddSource(src))

	// Cancel context before calling RecoverSource.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := mgr.RecoverSource(ctx, "pre-cancel")
	assert.Error(t, err)

	status, _ := mgr.GetSourceStatus("pre-cancel")
	assert.Equal(t, Offline, status.State)
	assert.Equal(t, OfflineHealth, metrics.healthValues["pre-cancel"])
}

// ---------------------------------------------------------------------------
// Additional coverage for AddSource with nil metrics
// ---------------------------------------------------------------------------

// TestManager_AddSource_NilMetrics verifies that AddSource works correctly
// when no MetricsReporter is configured (the m.metrics != nil branch).
func TestManager_AddSource_NilMetrics(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil) // nil metrics
	defer mgr.Stop()

	src := DefaultSource("no-metrics", "NoMetrics", "tcp://host")
	err := mgr.AddSource(src)
	assert.NoError(t, err)
	assert.Equal(t, 1, mgr.SourceCount())
}

// ---------------------------------------------------------------------------
// CheckHealth: cover failure on already-disconnected source
// (src.State != Connected means it does NOT emit EventDisconnected)
// ---------------------------------------------------------------------------

// TestManager_CheckHealth_FailureOnAlreadyDisconnected verifies that a
// health check failure on an already-disconnected source does NOT change
// the state to Disconnected again (covers the inner if src.State == Connected
// branch being false).
func TestManager_CheckHealth_FailureOnAlreadyDisconnected(t *testing.T) {
	metrics := newTestMetrics()
	mgr := NewManager(newTestLogger(), metrics)
	defer mgr.Stop()
	mgr.SetConnector(&failConnector{err: fmt.Errorf("unreachable")})

	src := DefaultSource("disc-health", "DiscHealth", "tcp://host")
	// Source starts as Disconnected (default from DefaultSource).
	require.NoError(t, mgr.AddSource(src))

	err := mgr.CheckHealth(context.Background(), "disc-health")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "health check")

	status, _ := mgr.GetSourceStatus("disc-health")
	// State should remain Disconnected (not change to something else).
	assert.Equal(t, Disconnected, status.State)
}

// ---------------------------------------------------------------------------
// CheckHealth: cover already-connected source passing health check
// (the src.State != Connected branch being false — no state transition)
// ---------------------------------------------------------------------------

// TestManager_CheckHealth_AlreadyConnectedAndHealthy verifies that a
// health check on an already-Connected source does not re-emit
// EventConnected (covers the src.State != Connected check being false).
func TestManager_CheckHealth_AlreadyConnectedAndHealthy(t *testing.T) {
	metrics := newTestMetrics()
	mgr := NewManager(newTestLogger(), metrics)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	src := DefaultSource("conn-health", "ConnHealth", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	// First, connect the source.
	require.NoError(t, mgr.ForceReconnect(context.Background(), "conn-health"))

	// Now check health — should pass without state transition.
	var events []*Event
	mgr.OnEvent(func(evt *Event) {
		events = append(events, evt)
	})

	err := mgr.CheckHealth(context.Background(), "conn-health")
	assert.NoError(t, err)

	// Should have emitted EventHealthCheck but NOT EventConnected again.
	foundHealthCheck := false
	for _, evt := range events {
		if evt.Type == EventHealthCheck {
			foundHealthCheck = true
		}
		assert.NotEqual(t, EventConnected, evt.Type,
			"should not emit EventConnected for already-connected source")
	}
	assert.True(t, foundHealthCheck, "should emit EventHealthCheck")
}

// ---------------------------------------------------------------------------
// ForceReconnect: cover nil metrics branches
// ---------------------------------------------------------------------------

// TestManager_ForceReconnect_NilMetrics_Success verifies that
// ForceReconnect works correctly when MetricsReporter is nil.
func TestManager_ForceReconnect_NilMetrics_Success(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	src := DefaultSource("nil-met-ok", "NilMetOK", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.ForceReconnect(context.Background(), "nil-met-ok")
	assert.NoError(t, err)

	status, _ := mgr.GetSourceStatus("nil-met-ok")
	assert.Equal(t, Connected, status.State)
}

// TestManager_ForceReconnect_NilMetrics_Failure verifies that
// ForceReconnect handles failure correctly when MetricsReporter is nil.
func TestManager_ForceReconnect_NilMetrics_Failure(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil)
	defer mgr.Stop()
	mgr.SetConnector(&failConnector{err: fmt.Errorf("refused")})

	src := DefaultSource("nil-met-fail", "NilMetFail", "tcp://host")
	require.NoError(t, mgr.AddSource(src))

	err := mgr.ForceReconnect(context.Background(), "nil-met-fail")
	assert.Error(t, err)

	status, _ := mgr.GetSourceStatus("nil-met-fail")
	assert.Equal(t, Disconnected, status.State)
}

// ---------------------------------------------------------------------------
// RecoverSource: cover nil metrics in all branches
// ---------------------------------------------------------------------------

// TestRecoverSource_NilMetrics_ExhaustsRetries covers the nil metrics
// path for the exhausted-retries outcome.
func TestRecoverSource_NilMetrics_ExhaustsRetries(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil) // nil metrics
	defer mgr.Stop()
	mgr.SetConnector(&failConnector{err: fmt.Errorf("always fails")})

	src := DefaultSource("nil-met-exhaust", "NilMetExhaust", "tcp://host")
	src.MaxRetryAttempts = 2
	src.RetryDelay = 1 * time.Millisecond
	require.NoError(t, mgr.AddSource(src))

	err := mgr.RecoverSource(context.Background(), "nil-met-exhaust")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "declared offline")
}

// TestRecoverSource_NilMetrics_Success covers the nil metrics path
// for successful recovery.
func TestRecoverSource_NilMetrics_Success(t *testing.T) {
	mgr := NewManager(newTestLogger(), nil) // nil metrics
	defer mgr.Stop()
	mgr.SetConnector(&successConnector{})

	src := DefaultSource("nil-met-success", "NilMetSuccess", "tcp://host")
	src.MaxRetryAttempts = 3
	src.RetryDelay = 1 * time.Millisecond
	require.NoError(t, mgr.AddSource(src))

	err := mgr.RecoverSource(context.Background(), "nil-met-success")
	assert.NoError(t, err)

	status, _ := mgr.GetSourceStatus("nil-met-success")
	assert.Equal(t, Connected, status.State)
}
