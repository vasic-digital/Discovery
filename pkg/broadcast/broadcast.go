// SPDX-License-Identifier: MIT

// Package broadcast provides UDP multicast service announcement and discovery
// for Catalogizer API instances on a local network.
//
// The Announcer periodically broadcasts the API's presence via UDP multicast.
// The Listener receives these broadcasts and returns discovered services.
// Clients use the Listener to find API instances without knowing their addresses.
package broadcast

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// DefaultMulticastGroup is the multicast address for Catalogizer discovery.
const DefaultMulticastGroup = "239.42.42.42"

// DefaultPort is the default UDP port for discovery broadcasts.
const DefaultPort = 42069

// DefaultInterval is the default broadcast interval.
const DefaultInterval = 5 * time.Second

// DefaultTimeout is the default discovery listen timeout.
const DefaultTimeout = 5 * time.Second

// MessageType identifies the purpose of a broadcast message.
type MessageType string

const (
	// TypeAnnounce indicates an API instance announcing its presence.
	TypeAnnounce MessageType = "catalogizer-announce"
	// TypeDiscover indicates a client requesting API instances to identify.
	TypeDiscover MessageType = "catalogizer-discover"
)

// ServiceInfo describes a discovered Catalogizer API instance.
type ServiceInfo struct {
	Type         MessageType `json:"type"`
	Service      string      `json:"service"`
	Version      string      `json:"version"`
	Build        string      `json:"build,omitempty"`
	Host         string      `json:"host"`
	Port         int         `json:"port"`
	Protocol     string      `json:"protocol"`
	Name         string      `json:"name,omitempty"`
	InstanceID   string      `json:"id,omitempty"`
	Capabilities []string    `json:"capabilities,omitempty"`
	Database     string      `json:"database,omitempty"`
	StorageRoots int         `json:"storage_roots,omitempty"`
	Uptime       int64       `json:"uptime_seconds,omitempty"`
}

// Config holds configuration for the announcer and listener.
type Config struct {
	MulticastGroup string
	Port           int
	Interval       time.Duration
	Timeout        time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MulticastGroup: DefaultMulticastGroup,
		Port:           DefaultPort,
		Interval:       DefaultInterval,
		Timeout:        DefaultTimeout,
	}
}

// Announcer periodically broadcasts service presence via UDP multicast.
type Announcer struct {
	config  Config
	info    ServiceInfo
	conn    *net.UDPConn
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

// NewAnnouncer creates an Announcer that will broadcast the given service info.
func NewAnnouncer(info ServiceInfo, cfg Config) *Announcer {
	if cfg.MulticastGroup == "" {
		cfg.MulticastGroup = DefaultMulticastGroup
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Interval == 0 {
		cfg.Interval = DefaultInterval
	}
	info.Type = TypeAnnounce
	return &Announcer{
		config: cfg,
		info:   info,
		stopCh: make(chan struct{}),
	}
}

// Start begins broadcasting service announcements. It is safe to call
// multiple times; only the first call has effect.
func (a *Announcer) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running {
		return nil
	}

	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", a.config.MulticastGroup, a.config.Port))
	if err != nil {
		return fmt.Errorf("resolve multicast addr: %w", err)
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("dial multicast: %w", err)
	}
	a.conn = conn
	a.running = true

	go a.loop()
	return nil
}

// Stop halts the broadcast loop and closes the connection.
func (a *Announcer) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		return
	}
	a.running = false
	close(a.stopCh)
	if a.conn != nil {
		a.conn.Close()
	}
}

// UpdateInfo atomically updates the service info for future broadcasts.
func (a *Announcer) UpdateInfo(info ServiceInfo) {
	a.mu.Lock()
	info.Type = TypeAnnounce
	a.info = info
	a.mu.Unlock()
}

func (a *Announcer) loop() {
	ticker := time.NewTicker(a.config.Interval)
	defer ticker.Stop()

	// Send initial announcement immediately
	a.send()

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			a.send()
		}
	}
}

func (a *Announcer) send() {
	a.mu.Lock()
	info := a.info
	conn := a.conn
	a.mu.Unlock()

	if conn == nil {
		return
	}

	data, err := json.Marshal(info)
	if err != nil {
		log.Printf("[broadcast] marshal error: %v", err)
		return
	}

	if _, err := conn.Write(data); err != nil {
		// Log at debug level; transient network errors are expected
		log.Printf("[broadcast] send error: %v", err)
	}
}

// Listener discovers Catalogizer API instances by listening for multicast
// announcements on the local network.
type Listener struct {
	config Config
}

// NewListener creates a Listener with the given configuration.
func NewListener(cfg Config) *Listener {
	if cfg.MulticastGroup == "" {
		cfg.MulticastGroup = DefaultMulticastGroup
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	return &Listener{config: cfg}
}

// Discover listens for service announcements until the context is cancelled
// or the configured timeout expires. Returns all unique services discovered.
func (l *Listener) Discover(ctx context.Context) ([]ServiceInfo, error) {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", l.config.MulticastGroup, l.config.Port))
	if err != nil {
		return nil, fmt.Errorf("resolve multicast addr: %w", err)
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("listen multicast: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(l.config.Timeout)
	seen := make(map[string]ServiceInfo)
	buf := make([]byte, 4096)

	for {
		select {
		case <-ctx.Done():
			return toSlice(seen), ctx.Err()
		default:
		}

		// Use short read deadlines (200ms) so we can check context frequently
		readDeadline := time.Now().Add(200 * time.Millisecond)
		if readDeadline.After(deadline) {
			readDeadline = deadline
		}
		if err := conn.SetReadDeadline(readDeadline); err != nil {
			return toSlice(seen), fmt.Errorf("set read deadline: %w", err)
		}

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Check if overall timeout expired
				if time.Now().After(deadline) {
					break
				}
				continue // short deadline expired, loop to check context
			}
			return toSlice(seen), fmt.Errorf("read: %w", err)
		}

		var info ServiceInfo
		if err := json.Unmarshal(buf[:n], &info); err != nil {
			continue
		}
		if info.Type != TypeAnnounce {
			continue
		}

		key := fmt.Sprintf("%s:%d", info.Host, info.Port)
		seen[key] = info
	}

	return toSlice(seen), nil
}

// DiscoverOne listens until at least one service is found or timeout.
func (l *Listener) DiscoverOne(ctx context.Context) (*ServiceInfo, error) {
	services, err := l.Discover(ctx)
	if err != nil && len(services) == 0 {
		return nil, err
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no services discovered")
	}
	return &services[0], nil
}

func toSlice(m map[string]ServiceInfo) []ServiceInfo {
	result := make([]ServiceInfo, 0, len(m))
	for _, v := range m {
		result = append(result, v)
	}
	return result
}
