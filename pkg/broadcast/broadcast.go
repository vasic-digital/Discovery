// SPDX-License-Identifier: MIT

// Package broadcast provides UDP multicast service announcement and discovery
// for any service instance on a local network. The package is a
// general-purpose building block — it holds no project-specific
// defaults and can be reused by any HTTP, gRPC, or TCP service that
// wants to advertise itself to peers.
//
// The Announcer periodically broadcasts the service's presence via UDP
// multicast. The Listener receives these broadcasts and returns
// discovered services. Clients use the Listener to find service
// instances without knowing their addresses.
//
// The wire-protocol message namespace is configurable via Config so
// projects that share a LAN can isolate their own announcements from
// other broadcasters using the same multicast group. See
// Config.MessageNamespace for details.
package broadcast

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// DefaultMulticastGroup is the default multicast address for service
// discovery broadcasts.
const DefaultMulticastGroup = "239.42.42.42"

// DefaultPort is the default UDP port for discovery broadcasts.
const DefaultPort = 42069

// DefaultInterval is the default broadcast interval.
const DefaultInterval = 5 * time.Second

// DefaultTimeout is the default discovery listen timeout.
const DefaultTimeout = 5 * time.Second

// DefaultMessageNamespace is the default prefix applied to the wire
// type strings. Projects that share a LAN should set their own
// namespace via Config.MessageNamespace so their announcements do
// not collide with other broadcasters on 239.42.42.42.
const DefaultMessageNamespace = "vasic"

// MessageType identifies the purpose of a broadcast message. The
// concrete string is built from MessageNamespace + "-announce" or
// "-discover" at Announcer / Listener construction time so each
// project can pin a unique wire identifier.
type MessageType string

// messageTypeFor returns the namespaced wire value for kind.
func messageTypeFor(namespace, kind string) MessageType {
	if namespace == "" {
		namespace = DefaultMessageNamespace
	}
	return MessageType(namespace + "-" + kind)
}

const (
	// TypeAnnounce is the DEFAULT announce wire value, used when the
	// caller did not set Config.MessageNamespace. It exists only for
	// backward compatibility — new code should read the type from the
	// running Announcer / Listener which honours the config.
	TypeAnnounce MessageType = "vasic-announce"
	// TypeDiscover is the DEFAULT discovery wire value; same caveat
	// as TypeAnnounce.
	TypeDiscover MessageType = "vasic-discover"
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
	// MessageNamespace disambiguates wire messages so projects that
	// share a LAN do not cross-subscribe to each other's broadcasts.
	// The announce / discover MessageType values are derived as
	// "<namespace>-announce" / "<namespace>-discover". Empty falls
	// back to DefaultMessageNamespace.
	MessageNamespace string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MulticastGroup:   DefaultMulticastGroup,
		Port:             DefaultPort,
		Interval:         DefaultInterval,
		Timeout:          DefaultTimeout,
		MessageNamespace: DefaultMessageNamespace,
	}
}

// Announcer periodically broadcasts service presence via UDP multicast.
type Announcer struct {
	config      Config
	announceTyp MessageType
	info        ServiceInfo
	conn        *net.UDPConn
	mu          sync.Mutex
	running     bool
	stopCh      chan struct{}
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
	if cfg.MessageNamespace == "" {
		cfg.MessageNamespace = DefaultMessageNamespace
	}
	announceTyp := messageTypeFor(cfg.MessageNamespace, "announce")
	info.Type = announceTyp
	return &Announcer{
		config:      cfg,
		announceTyp: announceTyp,
		info:        info,
		stopCh:      make(chan struct{}),
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
	info.Type = a.announceTyp
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

// Listener discovers service instances by listening for multicast
// announcements on the local network. The wire message namespace is
// drawn from Config.MessageNamespace so any project can isolate its
// own announcements from other broadcasters sharing the multicast
// group.
type Listener struct {
	config      Config
	announceTyp MessageType
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
	if cfg.MessageNamespace == "" {
		cfg.MessageNamespace = DefaultMessageNamespace
	}
	return &Listener{
		config:      cfg,
		announceTyp: messageTypeFor(cfg.MessageNamespace, "announce"),
	}
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
		if info.Type != l.announceTyp {
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

// DefaultResponderPort is the default UDP port for the discovery responder.
// Clients send a discovery request to this port via broadcast; the responder
// replies with the service info directly to the sender.
const DefaultResponderPort = 19820

// Responder listens on a UDP port for discovery requests and replies with
// the current service info. Unlike the Announcer (which proactively multicasts),
// the Responder is reactive: it only sends service info when a client asks.
//
// Client protocol:
//
//	1. Client sends a UDP broadcast to port 19820 with body
//	   "<NAMESPACE>_DISCOVER" (uppercase of the configured
//	   MessageNamespace). The default namespace is "vasic" so the
//	   default probe body is "VASIC_DISCOVER"; projects that override
//	   the namespace override the probe body accordingly.
//	2. Responder replies with JSON-encoded ServiceInfo to the sender's address
type Responder struct {
	info        ServiceInfo
	port        int
	probeBody   string
	announceTyp MessageType
	conn        *net.UDPConn
	mu          sync.Mutex
	running     bool
	stopCh      chan struct{}
}

// NewResponder creates a Responder using the default message
// namespace. Port 0 falls back to DefaultResponderPort. New code
// should prefer NewResponderWithConfig so the wire namespace is
// explicit.
func NewResponder(info ServiceInfo, port int) *Responder {
	return NewResponderWithConfig(info, Config{
		Port:             port,
		MessageNamespace: DefaultMessageNamespace,
	})
}

// NewResponderWithConfig creates a Responder from a full Config so
// callers can pin the UDP port and the wire message namespace. An
// empty cfg.Port resolves to DefaultResponderPort; an empty
// cfg.MessageNamespace resolves to DefaultMessageNamespace.
func NewResponderWithConfig(info ServiceInfo, cfg Config) *Responder {
	port := cfg.Port
	if port == 0 {
		port = DefaultResponderPort
	}
	ns := cfg.MessageNamespace
	if ns == "" {
		ns = DefaultMessageNamespace
	}
	announceTyp := messageTypeFor(ns, "announce")
	info.Type = announceTyp
	return &Responder{
		info:        info,
		port:        port,
		probeBody:   strings.ToUpper(ns) + "_DISCOVER",
		announceTyp: announceTyp,
		stopCh:      make(chan struct{}),
	}
}

// Start begins listening for discovery requests. Safe to call multiple times.
func (r *Responder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return nil
	}

	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", r.port))
	if err != nil {
		return fmt.Errorf("resolve responder addr: %w", err)
	}

	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("listen responder: %w", err)
	}
	r.conn = conn
	r.running = true

	go r.loop()
	return nil
}

// Stop halts the responder and closes the connection.
func (r *Responder) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	r.running = false
	close(r.stopCh)
	if r.conn != nil {
		r.conn.Close()
	}
}

// UpdateInfo atomically updates the service info for future responses.
func (r *Responder) UpdateInfo(info ServiceInfo) {
	r.mu.Lock()
	info.Type = r.announceTyp
	r.info = info
	r.mu.Unlock()
}

func (r *Responder) loop() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-r.stopCh:
			return
		default:
		}

		// Short read deadline so we can check stopCh periodically
		if err := r.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)); err != nil {
			return
		}

		n, remoteAddr, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			// Connection closed or fatal error
			return
		}

		msg := string(buf[:n])
		if msg != r.probeBody {
			continue
		}

		r.mu.Lock()
		info := r.info
		r.mu.Unlock()

		data, err := json.Marshal(info)
		if err != nil {
			log.Printf("[broadcast] responder marshal error: %v", err)
			continue
		}

		if _, err := r.conn.WriteToUDP(data, remoteAddr); err != nil {
			log.Printf("[broadcast] responder reply error: %v", err)
		}
	}
}
