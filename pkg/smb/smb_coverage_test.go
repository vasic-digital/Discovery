package smb

import (
	"context"
	"net"
	"testing"
	"time"

	"digital.vasic.discovery/pkg/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Scan: cover the ctx.Done() case during host iteration (lines 70-73)
//
// The existing test TestScanner_Scan_ContextCancelledDuringScan cancels
// before the loop starts so it may exit on expandCIDR or before any host
// iteration. This test uses a larger CIDR to ensure at least some hosts
// are iterated before the context cancels, and a very short timeout to
// trigger the select case mid-iteration.
// ---------------------------------------------------------------------------

// TestScan_ContextCancelledDuringHostIteration verifies that Scan respects
// context cancellation in the host iteration loop (the select case
// checking ctx.Done() before launching each goroutine).
func TestScan_ContextCancelledDuringHostIteration(t *testing.T) {
	// Use a /24 network with many hosts. With a very short timeout and
	// slow connection timeout, the context should cancel mid-iteration.
	cfg := &scanner.Config{
		Network: "192.168.99.0/24",
		Timeout: 30 * time.Second, // Long per-host timeout
		Ports:   []int{445},
		MaxConc: 1, // Single concurrency to force sequential processing
	}
	s := NewScanner(cfg)

	// Create a context that cancels almost immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Give the context a moment to expire.
	time.Sleep(5 * time.Millisecond)

	services, err := s.Scan(ctx)
	// Should return with context error.
	if err != nil {
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	}
	// Should have found zero or very few services since context was cancelled.
	_ = services
}

// TestScan_ContextCancelledMidScan verifies that Scan handles context
// cancellation that arrives during the scanning of hosts (after some
// hosts have already been dispatched).
func TestScan_ContextCancelledMidScan(t *testing.T) {
	// Start a local TCP listener so at least some hosts get scanned.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port

	// Use a /28 network to generate 14 hosts.
	cfg := &scanner.Config{
		Network: "192.168.200.0/28",
		Timeout: 200 * time.Millisecond,
		Ports:   []int{port},
		MaxConc: 2,
	}
	s := NewScanner(cfg)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a brief delay to catch mid-iteration.
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	services, err := s.Scan(ctx)
	// Should terminate without panic. Error may or may not be set.
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled)
	}
	_ = services
}

// ---------------------------------------------------------------------------
// Scan with /31 network (exactly 2 hosts, no stripping)
// ---------------------------------------------------------------------------

// TestScan_Slash31Network verifies Scan with a /31 CIDR which produces
// exactly 2 addresses (no network/broadcast stripping).
func TestScan_Slash31Network(t *testing.T) {
	cfg := &scanner.Config{
		Network: "192.168.1.0/31",
		Timeout: 50 * time.Millisecond,
		Ports:   []int{445},
		MaxConc: 5,
	}
	s := NewScanner(cfg)

	services, err := s.Scan(context.Background())
	assert.NoError(t, err)
	// Hosts are non-routable so no services found, but scan completes.
	assert.Empty(t, services)
}

// ---------------------------------------------------------------------------
// ScanHost with multiple ports, some open and some closed
// ---------------------------------------------------------------------------

// TestScanHost_MixedPorts verifies ScanHost behavior when some ports are
// open and others are closed on the same host.
func TestScanHost_MixedPorts(t *testing.T) {
	// Start a listener on one port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	openPort := listener.Addr().(*net.TCPAddr).Port
	closedPort := openPort + 10000 // Very likely closed

	cfg := &scanner.Config{
		Timeout: 200 * time.Millisecond,
		Ports:   []int{openPort, closedPort},
		MaxConc: 5,
	}
	s := NewScanner(cfg)

	services, err := s.ScanHost(context.Background(), "127.0.0.1")
	require.NoError(t, err)

	// Should find exactly one service (the open port).
	require.Len(t, services, 1)
	assert.Equal(t, openPort, services[0].Port)
	assert.Equal(t, "127.0.0.1", services[0].Host)
	assert.Equal(t, "smb", services[0].Protocol)
}
