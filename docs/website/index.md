# Discovery Module

`digital.vasic.discovery` is a standalone Go module for network and service discovery. It provides interfaces and implementations for scanning networks to find services, with SMB/CIFS share discovery via TCP port probing, structured scan reports, and a resilience framework for managing connection state, offline caching, and automatic recovery with exponential backoff.

## Key Features

- **Scanner interface** -- Generic `Scanner` interface for implementing protocol-specific discovery (SMB, FTP, NFS, etc.)
- **SMB discovery** -- Discovers SMB/CIFS shares by TCP port scanning (445, 139) across CIDR network ranges with concurrent goroutines
- **Scan reports** -- Structured reports with JSON serialization, human-readable summaries, and grouping by protocol
- **Resilience manager** -- Connection state machine (Connected, Disconnected, Reconnecting, Offline) with event-driven health monitoring
- **Offline cache** -- Bounded cache for queuing changes while a source is unavailable, with automatic replay on reconnection
- **Automatic recovery** -- Exponential backoff retry with configurable max attempts and delay

## Package Overview

| Package | Purpose |
|---------|---------|
| `pkg/scanner` | Core scanner interface, Service type, and Config |
| `pkg/smb` | SMB/CIFS discovery via TCP port scanning |
| `pkg/report` | Scan report generation with JSON and text output |
| `pkg/resilience` | Connection state management, offline cache, and recovery |

## Installation

```bash
go get digital.vasic.discovery
```

Requires Go 1.24 or later. Only external dependency is `testify` for tests.
