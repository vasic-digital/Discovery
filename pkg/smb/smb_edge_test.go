package smb

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"digital.vasic.discovery/pkg/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSMBScanner_InvalidHost verifies scanning an invalid hostname returns
// no results and no error (connection simply fails silently per-port).
func TestSMBScanner_InvalidHost(t *testing.T) {
	tests := []struct {
		name string
		host string
	}{
		{"non_routable_ip", "198.51.100.1"},
		{"invalid_hostname", "this-host-does-not-exist.invalid"},
		{"empty_host", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &scanner.Config{
				Timeout: 200 * time.Millisecond,
				Ports:   []int{445},
				MaxConc: 5,
			}
			s := NewScanner(cfg)

			services, err := s.ScanHost(context.Background(), tt.host)
			// ScanHost does not return errors for unreachable hosts; it just
			// skips ports that fail to connect.
			assert.NoError(t, err)
			assert.Empty(t, services)
		})
	}
}

// TestSMBScanner_EmptyConfig verifies scanner behavior with an empty/default
// configuration struct (no network, no ports initially).
func TestSMBScanner_EmptyConfig(t *testing.T) {
	t.Run("empty_struct_config", func(t *testing.T) {
		cfg := &scanner.Config{}
		s := NewScanner(cfg)

		require.NotNil(t, s)
		// Empty ports should get defaults.
		assert.Equal(t, []int{445, 139}, s.config.Ports)
		// Timeout defaults to zero (from Config struct).
		assert.Equal(t, time.Duration(0), s.config.Timeout)
		// MaxConc defaults to zero but Scan handles it (falls back to 50).
		assert.Equal(t, 0, s.config.MaxConc)

		// Scan with no network should return an error.
		services, err := s.Scan(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no network configured")
		assert.Nil(t, services)
	})

	t.Run("nil_config", func(t *testing.T) {
		s := NewScanner(nil)

		require.NotNil(t, s)
		assert.Equal(t, []int{445, 139}, s.config.Ports)
		assert.Equal(t, 5*time.Second, s.config.Timeout)
		assert.Equal(t, 50, s.config.MaxConc)
	})

	t.Run("zero_maxconc_scan_with_listener", func(t *testing.T) {
		// Start a local TCP listener.
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

		cfg := &scanner.Config{
			Network: "127.0.0.1/32",
			Timeout: 2 * time.Second,
			Ports:   []int{port},
			MaxConc: 0, // Zero — should fall back to 50 in Scan.
		}
		s := NewScanner(cfg)

		services, err := s.Scan(context.Background())
		require.NoError(t, err)
		require.Len(t, services, 1)
		assert.Equal(t, "127.0.0.1", services[0].Host)
	})
}

// TestSMBScanner_Protocol verifies the protocol string returned by the scanner.
func TestSMBScanner_Protocol(t *testing.T) {
	tests := []struct {
		name   string
		config *scanner.Config
	}{
		{"nil_config", nil},
		{"custom_config", &scanner.Config{Ports: []int{8445}}},
		{"default_config", scanner.DefaultConfig()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewScanner(tt.config)
			assert.Equal(t, "smb", s.Protocol())
		})
	}

	// Verify it satisfies the scanner.Scanner interface.
	var _ scanner.Scanner = NewScanner(nil)
}

// TestSMBScanner_ConcurrentScans verifies that 10 goroutines can scan
// simultaneously without data races or panics.
func TestSMBScanner_ConcurrentScans(t *testing.T) {
	// Start local TCP listeners to simulate SMB services.
	const listenerCount = 3
	listeners := make([]net.Listener, listenerCount)
	ports := make([]int, listenerCount)

	for i := 0; i < listenerCount; i++ {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		listeners[i] = listener
		ports[i] = listener.Addr().(*net.TCPAddr).Port

		go func(l net.Listener) {
			for {
				conn, err := l.Accept()
				if err != nil {
					return
				}
				conn.Close()
			}
		}(listener)
	}
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()

	const goroutines = 10
	var wg sync.WaitGroup
	results := make([][]*scanner.Service, goroutines)
	errors := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			cfg := &scanner.Config{
				Timeout: 2 * time.Second,
				Ports:   ports,
				MaxConc: 5,
			}
			s := NewScanner(cfg)

			svcs, err := s.ScanHost(context.Background(), "127.0.0.1")
			results[idx] = svcs
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// All goroutines should succeed with the same number of services.
	for i := 0; i < goroutines; i++ {
		assert.NoError(t, errors[i], "goroutine %d had an error", i)
		assert.Len(t, results[i], listenerCount,
			"goroutine %d found %d services, expected %d", i, len(results[i]), listenerCount)
	}

	// Also test concurrent Scan (with network) across goroutines.
	var wg2 sync.WaitGroup
	const scanGoroutines = 10

	for i := 0; i < scanGoroutines; i++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()

			cfg := &scanner.Config{
				Network: "127.0.0.1/32",
				Timeout: 2 * time.Second,
				Ports:   ports,
				MaxConc: 5,
			}
			s := NewScanner(cfg)

			svcs, err := s.Scan(context.Background())
			assert.NoError(t, err)
			assert.Len(t, svcs, listenerCount)
		}()
	}

	wg2.Wait()
}
