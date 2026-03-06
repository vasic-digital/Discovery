// Package resilience provides generic network service resilience primitives
// including connection state management, offline caching, and health checking.
// It uses only stdlib types and exposes Logger and MetricsReporter interfaces
// so consumers can inject their own implementations (e.g., zap, prometheus).
package resilience

import (
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Logger & MetricsReporter interfaces
// ---------------------------------------------------------------------------

// Logger defines a structured logging interface. Implementations might wrap
// zap, slog, logrus, or any other structured logger.
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
}

// MetricsReporter defines a metrics reporting interface. Implementations
// might push to Prometheus, StatsD, or any other metrics backend.
type MetricsReporter interface {
	// SetSourceHealth sets the health gauge for a given source.
	// value should be one of the Health* constants (1.0, 0.5, 0.0).
	SetSourceHealth(sourceID string, value float64)
}

// ---------------------------------------------------------------------------
// Connection state machine
// ---------------------------------------------------------------------------

// ConnectionState represents the current state of a connection to a
// network service source.
type ConnectionState int

const (
	// Connected means the source is reachable and healthy.
	Connected ConnectionState = iota
	// Disconnected means the connection was lost but recovery has not started.
	Disconnected
	// Reconnecting means a reconnection attempt is in progress.
	Reconnecting
	// Offline means the source has been declared unreachable after
	// exhausting retry attempts.
	Offline
)

// String returns the human-readable name of the connection state.
func (s ConnectionState) String() string {
	switch s {
	case Connected:
		return "connected"
	case Disconnected:
		return "disconnected"
	case Reconnecting:
		return "reconnecting"
	case Offline:
		return "offline"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ---------------------------------------------------------------------------
// Health metric constants
// ---------------------------------------------------------------------------

const (
	// Healthy indicates the source is fully operational.
	Healthy = 1.0
	// Degraded indicates the source is partially operational or reconnecting.
	Degraded = 0.5
	// OfflineHealth indicates the source is unreachable.
	OfflineHealth = 0.0
)

// HealthMetric returns the health metric value for the connection state.
func (s ConnectionState) HealthMetric() float64 {
	switch s {
	case Connected:
		return Healthy
	case Disconnected, Reconnecting:
		return Degraded
	case Offline:
		return OfflineHealth
	default:
		return OfflineHealth
	}
}

// ---------------------------------------------------------------------------
// Source
// ---------------------------------------------------------------------------

// Source represents a network service endpoint whose availability is managed
// by the resilience framework.
type Source struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Endpoint          string          `json:"endpoint"`
	State             ConnectionState `json:"state"`
	LastConnected     time.Time       `json:"last_connected"`
	LastError         error           `json:"-"`
	RetryAttempts     int             `json:"retry_attempts"`
	MaxRetryAttempts  int             `json:"max_retry_attempts"`
	RetryDelay        time.Duration   `json:"retry_delay"`
	ConnectionTimeout time.Duration   `json:"connection_timeout"`
	HealthCheckInterval time.Duration `json:"health_check_interval"`
	IsEnabled         bool            `json:"is_enabled"`

	mu sync.Mutex
}

// Lock acquires the source's mutex.
func (s *Source) Lock() { s.mu.Lock() }

// Unlock releases the source's mutex.
func (s *Source) Unlock() { s.mu.Unlock() }

// DefaultSource returns a Source initialised with sensible defaults.
func DefaultSource(id, name, endpoint string) *Source {
	return &Source{
		ID:                  id,
		Name:                name,
		Endpoint:            endpoint,
		State:               Disconnected,
		MaxRetryAttempts:    5,
		RetryDelay:          5 * time.Second,
		ConnectionTimeout:   10 * time.Second,
		HealthCheckInterval: 30 * time.Second,
		IsEnabled:           true,
	}
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

// EventType identifies the kind of event emitted by the resilience manager.
type EventType int

const (
	// EventConnected is emitted when a source transitions to Connected.
	EventConnected EventType = iota
	// EventDisconnected is emitted when a source transitions to Disconnected.
	EventDisconnected
	// EventReconnecting is emitted when a reconnection attempt starts.
	EventReconnecting
	// EventOffline is emitted when a source is declared Offline.
	EventOffline
	// EventError is emitted when an error occurs during connection or health check.
	EventError
	// EventHealthCheck is emitted after a periodic health check completes.
	EventHealthCheck
)

// String returns the human-readable name of the event type.
func (e EventType) String() string {
	switch e {
	case EventConnected:
		return "connected"
	case EventDisconnected:
		return "disconnected"
	case EventReconnecting:
		return "reconnecting"
	case EventOffline:
		return "offline"
	case EventError:
		return "error"
	case EventHealthCheck:
		return "health_check"
	default:
		return fmt.Sprintf("unknown(%d)", int(e))
	}
}

// Event represents a state change or notable occurrence for a source.
type Event struct {
	Type      EventType              `json:"type"`
	SourceID  string                 `json:"source_id"`
	Error     error                  `json:"error,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

// NewEvent creates a new Event with the current timestamp.
func NewEvent(eventType EventType, sourceID string, err error) *Event {
	return &Event{
		Type:      eventType,
		SourceID:  sourceID,
		Error:     err,
		Timestamp: time.Now(),
	}
}
