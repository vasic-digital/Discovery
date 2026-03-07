package scanner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockScanner is a minimal Scanner implementation used to verify interface
// compliance and timeout behavior.
type mockScanner struct {
	protocol string
	scanFn   func(ctx context.Context) ([]*Service, error)
}

func (m *mockScanner) Scan(ctx context.Context) ([]*Service, error) {
	if m.scanFn != nil {
		return m.scanFn(ctx)
	}
	return nil, nil
}

func (m *mockScanner) ScanHost(ctx context.Context, host string) ([]*Service, error) {
	return m.Scan(ctx)
}

func (m *mockScanner) Protocol() string {
	return m.protocol
}

// TestScanner_Interface_Compliance verifies that the mockScanner satisfies
// the Scanner interface at compile time and at runtime.
func TestScanner_Interface_Compliance(t *testing.T) {
	// Compile-time check: mockScanner must satisfy Scanner.
	var _ Scanner = (*mockScanner)(nil)

	// Runtime check: create an instance and call all methods.
	s := &mockScanner{protocol: "test"}

	assert.Equal(t, "test", s.Protocol())

	services, err := s.Scan(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, services)

	services, err = s.ScanHost(context.Background(), "127.0.0.1")
	assert.NoError(t, err)
	assert.Nil(t, services)
}

// TestScanner_Timeout_Behavior verifies that a scanner implementation
// respects context timeout/cancellation.
func TestScanner_Timeout_Behavior(t *testing.T) {
	t.Run("context_cancelled_before_scan", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately.

		s := &mockScanner{
			protocol: "test",
			scanFn: func(ctx context.Context) ([]*Service, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
					return []*Service{{Name: "should-not-reach"}}, nil
				}
			},
		}

		services, err := s.Scan(ctx)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, services)
	})

	t.Run("context_deadline_exceeded", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		// Give the context time to expire.
		time.Sleep(5 * time.Millisecond)

		s := &mockScanner{
			protocol: "test",
			scanFn: func(ctx context.Context) ([]*Service, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				default:
					return []*Service{{Name: "should-not-reach"}}, nil
				}
			},
		}

		services, err := s.Scan(ctx)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, services)
	})

	t.Run("long_running_scan_with_timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		s := &mockScanner{
			protocol: "slow",
			scanFn: func(ctx context.Context) ([]*Service, error) {
				select {
				case <-time.After(5 * time.Second):
					return []*Service{{Name: "slow-result"}}, nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			},
		}

		start := time.Now()
		services, err := s.Scan(ctx)
		elapsed := time.Since(start)

		assert.Error(t, err)
		assert.Nil(t, services)
		// Should have finished in roughly the timeout period, not 5 seconds.
		assert.Less(t, elapsed, 1*time.Second)
	})

	t.Run("scan_completes_within_timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		expectedServices := []*Service{
			{Name: "fast-svc", Host: "127.0.0.1", Port: 445, Protocol: "test"},
		}

		s := &mockScanner{
			protocol: "test",
			scanFn: func(ctx context.Context) ([]*Service, error) {
				return expectedServices, nil
			},
		}

		services, err := s.Scan(ctx)
		assert.NoError(t, err)
		assert.Len(t, services, 1)
		assert.Equal(t, "fast-svc", services[0].Name)
	})
}
