// Package smb provides SMB/CIFS share discovery via TCP port scanning.
package smb

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"digital.vasic.discovery/pkg/scanner"
)

// ShareInfo represents a discovered SMB share.
type ShareInfo struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	ShareName  string `json:"share_name"`
	ShareType  string `json:"share_type"`
	Accessible bool   `json:"accessible"`
}

// Scanner discovers SMB shares on the network.
type Scanner struct {
	config *scanner.Config
}

// NewScanner creates a new SMB scanner with the given configuration.
// If cfg is nil, DefaultConfig is used. If no ports are specified,
// the standard SMB ports (445, 139) are used.
func NewScanner(cfg *scanner.Config) *Scanner {
	if cfg == nil {
		cfg = scanner.DefaultConfig()
	}
	if len(cfg.Ports) == 0 {
		cfg.Ports = []int{445, 139}
	}
	return &Scanner{config: cfg}
}

// Protocol returns the protocol identifier for the SMB scanner.
func (s *Scanner) Protocol() string { return "smb" }

// Scan discovers SMB services across the configured network by attempting
// TCP connections to SMB ports on each host in the CIDR range.
func (s *Scanner) Scan(ctx context.Context) ([]*scanner.Service, error) {
	if s.config.Network == "" {
		return nil, fmt.Errorf("no network configured for scan")
	}

	hosts, err := expandCIDR(s.config.Network)
	if err != nil {
		return nil, fmt.Errorf("invalid network CIDR %q: %w", s.config.Network, err)
	}

	var (
		mu       sync.Mutex
		services []*scanner.Service
		wg       sync.WaitGroup
	)

	// Semaphore for concurrency control.
	maxConc := s.config.MaxConc
	if maxConc <= 0 {
		maxConc = 50
	}
	sem := make(chan struct{}, maxConc)

	for _, host := range hosts {
		select {
		case <-ctx.Done():
			return services, ctx.Err()
		default:
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(h string) {
			defer wg.Done()
			defer func() { <-sem }()

			found, err := s.ScanHost(ctx, h)
			if err != nil || len(found) == 0 {
				return
			}

			mu.Lock()
			services = append(services, found...)
			mu.Unlock()
		}(host)
	}

	wg.Wait()
	return services, nil
}

// ScanHost discovers SMB services on a specific host by attempting
// TCP connections to the configured SMB ports.
//
// Empty host strings are rejected early: `net.JoinHostPort("", port)`
// produces `:port` which resolves to localhost, which would accidentally
// discover any local SMB daemon and break the "no host → no results"
// contract that callers rely on.
func (s *Scanner) ScanHost(ctx context.Context, host string) ([]*scanner.Service, error) {
	if host == "" {
		return nil, nil
	}

	var services []*scanner.Service

	for _, port := range s.config.Ports {
		select {
		case <-ctx.Done():
			return services, ctx.Err()
		default:
		}

		addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

		dialer := &net.Dialer{
			Timeout: s.config.Timeout,
		}

		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			continue
		}
		conn.Close()

		svc := &scanner.Service{
			Name:     fmt.Sprintf("smb-%s:%d", host, port),
			Host:     host,
			Port:     port,
			Protocol: "smb",
			Metadata: map[string]string{
				"port_type": portDescription(port),
			},
			FoundAt: time.Now(),
		}
		services = append(services, svc)
	}

	return services, nil
}

// portDescription returns a human-readable description for known SMB ports.
func portDescription(port int) string {
	switch port {
	case 445:
		return "microsoft-ds"
	case 139:
		return "netbios-ssn"
	default:
		return "unknown"
	}
}

// expandCIDR takes a CIDR string and returns all usable host IPs.
// For example, "192.168.1.0/30" yields [192.168.1.1, 192.168.1.2].
func expandCIDR(cidr string) ([]string, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	var hosts []string
	for addr := cloneIP(ip.Mask(ipNet.Mask)); ipNet.Contains(addr); incrementIP(addr) {
		hosts = append(hosts, addr.String())
	}

	// Remove network address (first) and broadcast address (last) for
	// networks larger than /31.
	if len(hosts) > 2 {
		hosts = hosts[1 : len(hosts)-1]
	}

	return hosts, nil
}

// cloneIP creates a copy of a net.IP so mutations don't affect the original.
func cloneIP(ip net.IP) net.IP {
	dup := make(net.IP, len(ip))
	copy(dup, ip)
	return dup
}

// incrementIP increments an IP address in place by one.
func incrementIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			break
		}
	}
}
