// Package scanner provides network and service discovery interfaces.
package scanner

import (
	"context"
	"time"
)

// Service represents a discovered network service.
type Service struct {
	Name     string            `json:"name"`
	Host     string            `json:"host"`
	Port     int               `json:"port"`
	Protocol string            `json:"protocol"`
	Metadata map[string]string `json:"metadata,omitempty"`
	FoundAt  time.Time         `json:"found_at"`
}

// Scanner defines the interface for service discovery.
type Scanner interface {
	// Scan discovers services across the configured network.
	Scan(ctx context.Context) ([]*Service, error)

	// ScanHost discovers services on a specific host.
	ScanHost(ctx context.Context, host string) ([]*Service, error)

	// Protocol returns the protocol identifier for this scanner.
	Protocol() string
}

// Config holds scanner configuration.
type Config struct {
	Network string        // CIDR network to scan (e.g., "192.168.1.0/24")
	Timeout time.Duration // Per-host scan timeout
	Ports   []int         // Ports to scan
	MaxConc int           // Maximum concurrent scans
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Timeout: 5 * time.Second,
		MaxConc: 50,
	}
}
