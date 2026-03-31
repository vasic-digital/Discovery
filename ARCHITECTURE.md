# Architecture -- Discovery

## Purpose

Go module for network and service discovery. Provides interfaces and implementations for scanning networks to detect running services, with an initial focus on SMB/CIFS discovery via TCP port probing. Designed to be extended with additional protocol scanners.

## Structure

```
pkg/
  scanner/   Core interfaces and types: Scanner interface, Service, Config
  smb/       SMB/CIFS discovery scanner (TCP ports 445, 139) with CIDR range expansion
  report/    Report generation with JSON serialization and human-readable summaries
```

## Key Components

- **`scanner.Scanner`** -- Interface: Scan(ctx) for full network scan, ScanHost(ctx, host) for single host, Protocol() for identifier
- **`scanner.Config`** -- Network CIDR, timeout, max concurrency
- **`scanner.Service`** -- Discovered service with host, port, protocol, and response time
- **`smb.Scanner`** -- SMB discovery: expands CIDR to individual IPs, probes TCP 445 and 139 with concurrent goroutines controlled by a semaphore channel
- **`report.Report`** -- Aggregated scan results with JSON output, summary generation, and per-protocol grouping

## Data Flow

```
smb.Scanner.Scan(ctx) -> expand CIDR to IP list
    |
    for each IP (concurrent, semaphore-limited):
        dial TCP 445 -> success? record Service
        dial TCP 139 -> success? record Service
    |
    collect results -> report.NewReport(network, services, duration)
    |
    report.ToJSON() or report.Summary()
```

## Dependencies

- `github.com/stretchr/testify` -- Test assertions (only dependency)

## Testing Strategy

Table-driven tests with `testify`. Tests spin up local TCP listeners on ephemeral ports for integration-style testing without requiring actual SMB services. Tests cover nil config handling, custom config, unreachable hosts, live listeners, and context cancellation.
