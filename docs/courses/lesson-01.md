# Lesson 1: Scanner Interface and SMB Discovery

## Learning Objectives

- Understand the Scanner interface and its three methods
- Implement concurrent network scanning with semaphore-based parallelism
- Expand CIDR ranges into individual host IPs

## Key Concepts

- **Scanner Interface**: Defines `Scan(ctx) ([]*Service, error)` for full network scans, `ScanHost(ctx, host) ([]*Service, error)` for single-host scans, and `Protocol() string` for protocol identification.
- **Semaphore Concurrency**: The SMB scanner uses a buffered channel as a semaphore to limit concurrent goroutines to `Config.MaxConc` (default 50). Each goroutine acquires a slot before scanning and releases it when done.
- **CIDR Expansion**: `expandCIDR("192.168.1.0/24")` returns all usable host IPs (excluding network and broadcast addresses) by iterating through the IP range with `incrementIP`.

## Code Walkthrough

### Source: `pkg/scanner/scanner.go`

The core types: `Service` (discovered service with host, port, protocol, metadata, timestamp), `Scanner` (interface), and `Config` (network CIDR, timeout, ports, max concurrency).

### Source: `pkg/smb/smb.go`

The `Scan` method expands the CIDR, then launches a goroutine per host with semaphore control:

```go
sem := make(chan struct{}, maxConc)
for _, host := range hosts {
    wg.Add(1)
    sem <- struct{}{}
    go func(h string) {
        defer wg.Done()
        defer func() { <-sem }()
        found, err := s.ScanHost(ctx, h)
        // ...
    }(host)
}
```

`ScanHost` iterates configured ports, attempts a TCP dial with timeout, and records services for successful connections.

## Practice Exercise

1. Create an SMB scanner with default config and scan a `/30` network (4 IPs, 2 usable). Verify only reachable hosts appear in results.
2. Start a local TCP listener on port 445 and scan `127.0.0.1`. Verify the scanner discovers it with protocol "smb" and port_type "microsoft-ds".
3. Test context cancellation: start a scan of a large `/16` network with a 1-second timeout context. Verify the scan returns early without blocking.
