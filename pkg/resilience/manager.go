package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Connector defines the operations a resilience Manager uses to interact
// with the underlying network service. Consumers inject an implementation
// that knows how to reach the actual service (SMB, FTP, NFS, etc.).
type Connector interface {
	// Connect attempts to establish a connection to the source.
	Connect(ctx context.Context, source *Source) error

	// HealthCheck verifies that the source is still reachable.
	HealthCheck(ctx context.Context, source *Source) error
}

// EventHandler is a callback invoked when an Event occurs.
type EventHandler func(event *Event)

// Manager coordinates resilience across multiple network sources. It
// monitors connection health, drives automatic recovery, and emits events
// for observability.
type Manager struct {
	sources  map[string]*Source
	mu       sync.RWMutex
	logger   Logger
	metrics  MetricsReporter
	connector Connector

	handlers []EventHandler
	handlerMu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewManager creates a new resilience Manager.
func NewManager(logger Logger, metrics MetricsReporter) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		sources: make(map[string]*Source),
		logger:  logger,
		metrics: metrics,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// SetConnector injects a Connector implementation used for connect and
// health-check operations on all managed sources.
func (m *Manager) SetConnector(c Connector) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connector = c
}

// OnEvent registers an EventHandler that is called whenever an event occurs.
func (m *Manager) OnEvent(handler EventHandler) {
	m.handlerMu.Lock()
	defer m.handlerMu.Unlock()
	m.handlers = append(m.handlers, handler)
}

// emit sends an event to all registered handlers.
func (m *Manager) emit(event *Event) {
	m.handlerMu.RLock()
	defer m.handlerMu.RUnlock()
	for _, h := range m.handlers {
		h(event)
	}
}

// AddSource registers a new source with the manager and optionally starts
// health monitoring if the manager's context is still active.
func (m *Manager) AddSource(source *Source) error {
	if source == nil {
		return fmt.Errorf("source must not be nil")
	}
	if source.ID == "" {
		return fmt.Errorf("source ID must not be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sources[source.ID]; exists {
		return fmt.Errorf("source %q already exists", source.ID)
	}

	m.sources[source.ID] = source
	m.logger.Info("source added", "id", source.ID, "endpoint", source.Endpoint)

	if m.metrics != nil {
		m.metrics.SetSourceHealth(source.ID, source.State.HealthMetric())
	}

	return nil
}

// RemoveSource unregisters a source from the manager.
func (m *Manager) RemoveSource(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sources[id]; !exists {
		return fmt.Errorf("source %q not found", id)
	}

	delete(m.sources, id)
	m.logger.Info("source removed", "id", id)
	return nil
}

// GetSourceStatus returns a snapshot of the source's current state. It is
// safe for concurrent use.
func (m *Manager) GetSourceStatus(id string) (*Source, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	src, exists := m.sources[id]
	if !exists {
		return nil, fmt.Errorf("source %q not found", id)
	}

	src.Lock()
	defer src.Unlock()

	// Return a shallow copy so the caller cannot mutate internal state.
	cp := *src
	return &cp, nil
}

// SourceCount returns the number of registered sources.
func (m *Manager) SourceCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sources)
}

// ForceReconnect resets a source's retry counter and initiates a
// reconnection attempt. Returns an error if no connector is set or the
// source does not exist.
func (m *Manager) ForceReconnect(ctx context.Context, id string) error {
	m.mu.RLock()
	src, exists := m.sources[id]
	connector := m.connector
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("source %q not found", id)
	}
	if connector == nil {
		return fmt.Errorf("no connector configured")
	}

	src.Lock()
	src.RetryAttempts = 0
	src.State = Reconnecting
	src.Unlock()

	m.emit(NewEvent(EventReconnecting, id, nil))
	m.logger.Info("force reconnect initiated", "id", id)

	if m.metrics != nil {
		m.metrics.SetSourceHealth(id, Reconnecting.HealthMetric())
	}

	err := connector.Connect(ctx, src)

	src.Lock()
	if err != nil {
		src.LastError = err
		src.State = Disconnected
		src.Unlock()

		m.emit(NewEvent(EventError, id, err))
		m.logger.Warn("force reconnect failed", "id", id, "error", err)

		if m.metrics != nil {
			m.metrics.SetSourceHealth(id, Disconnected.HealthMetric())
		}
		return fmt.Errorf("reconnect to %q failed: %w", id, err)
	}

	src.State = Connected
	src.LastConnected = time.Now()
	src.LastError = nil
	src.Unlock()

	m.emit(NewEvent(EventConnected, id, nil))
	m.logger.Info("source connected", "id", id)

	if m.metrics != nil {
		m.metrics.SetSourceHealth(id, Connected.HealthMetric())
	}
	return nil
}

// CheckHealth runs a health check on a single source using the configured
// Connector. It updates the source's state and emits appropriate events.
func (m *Manager) CheckHealth(ctx context.Context, id string) error {
	m.mu.RLock()
	src, exists := m.sources[id]
	connector := m.connector
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("source %q not found", id)
	}
	if connector == nil {
		return fmt.Errorf("no connector configured")
	}

	err := connector.HealthCheck(ctx, src)

	src.Lock()
	defer src.Unlock()

	if err != nil {
		src.LastError = err
		if src.State == Connected {
			src.State = Disconnected
			m.emit(NewEvent(EventDisconnected, id, err))
			m.logger.Warn("source disconnected", "id", id, "error", err)
		}
		if m.metrics != nil {
			m.metrics.SetSourceHealth(id, src.State.HealthMetric())
		}
		return fmt.Errorf("health check for %q failed: %w", id, err)
	}

	if src.State != Connected {
		src.State = Connected
		src.LastConnected = time.Now()
		src.LastError = nil
		m.emit(NewEvent(EventConnected, id, nil))
		m.logger.Info("source recovered", "id", id)
	}

	m.emit(NewEvent(EventHealthCheck, id, nil))

	if m.metrics != nil {
		m.metrics.SetSourceHealth(id, Connected.HealthMetric())
	}
	return nil
}

// RecoverSource attempts to reconnect a disconnected source with
// exponential backoff up to MaxRetryAttempts. It blocks until the source
// is connected, all retries are exhausted, or the context is cancelled.
func (m *Manager) RecoverSource(ctx context.Context, id string) error {
	m.mu.RLock()
	src, exists := m.sources[id]
	connector := m.connector
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("source %q not found", id)
	}
	if connector == nil {
		return fmt.Errorf("no connector configured")
	}

	src.Lock()
	maxRetries := src.MaxRetryAttempts
	baseDelay := src.RetryDelay
	src.State = Reconnecting
	src.RetryAttempts = 0
	src.Unlock()

	m.emit(NewEvent(EventReconnecting, id, nil))
	m.logger.Info("starting recovery", "id", id, "max_retries", maxRetries)

	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			src.Lock()
			src.State = Offline
			src.Unlock()
			m.emit(NewEvent(EventOffline, id, ctx.Err()))
			if m.metrics != nil {
				m.metrics.SetSourceHealth(id, Offline.HealthMetric())
			}
			return ctx.Err()
		default:
		}

		err := connector.Connect(ctx, src)

		src.Lock()
		src.RetryAttempts = attempt + 1

		if err == nil {
			src.State = Connected
			src.LastConnected = time.Now()
			src.LastError = nil
			src.Unlock()

			m.emit(NewEvent(EventConnected, id, nil))
			m.logger.Info("source recovered", "id", id, "attempts", attempt+1)
			if m.metrics != nil {
				m.metrics.SetSourceHealth(id, Connected.HealthMetric())
			}
			return nil
		}

		src.LastError = err
		src.Unlock()

		m.emit(NewEvent(EventError, id, err))
		m.logger.Warn("recovery attempt failed", "id", id, "attempt", attempt+1, "error", err)

		// Exponential backoff: baseDelay * 2^attempt.
		delay := baseDelay * time.Duration(1<<uint(attempt))
		select {
		case <-ctx.Done():
			src.Lock()
			src.State = Offline
			src.Unlock()
			m.emit(NewEvent(EventOffline, id, ctx.Err()))
			if m.metrics != nil {
				m.metrics.SetSourceHealth(id, Offline.HealthMetric())
			}
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	// Exhausted retries.
	src.Lock()
	src.State = Offline
	src.Unlock()

	m.emit(NewEvent(EventOffline, id, src.LastError))
	m.logger.Error("source declared offline", "id", id, "attempts", maxRetries)

	if m.metrics != nil {
		m.metrics.SetSourceHealth(id, Offline.HealthMetric())
	}
	return fmt.Errorf("source %q declared offline after %d attempts", id, maxRetries)
}

// Stop cancels the manager's internal context and waits for any background
// goroutines to finish.
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
	m.logger.Info("manager stopped")
}
