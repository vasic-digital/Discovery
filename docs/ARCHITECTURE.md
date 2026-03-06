# Discovery Architecture

## Purpose

`digital.vasic.discovery` is a standalone Go module for network and service discovery.
It provides a `Scanner` interface, a concrete SMB/CIFS implementation that probes TCP
ports across CIDR ranges, a resilience framework for managing connection health with
automatic recovery and offline caching, and a report generator for scan results.

## Package Overview

| Package | Responsibility |
|---------|---------------|
| `pkg/scanner` | Core interfaces and types: `Scanner`, `Service`, `Config` |
| `pkg/smb` | SMB/CIFS service discovery via concurrent TCP port scanning of ports 445 and 139 |
| `pkg/resilience` | Connection state machine, health checking, exponential-backoff recovery, offline caching, and event emission |
| `pkg/report` | Structured scan report generation with JSON serialization and human-readable summaries |

## Design Patterns

| Package | Pattern | Rationale |
|---------|---------|-----------|
| `pkg/scanner` | **Strategy (interface)** | `Scanner` interface allows plugging in protocol-specific scanners (SMB, FTP, NFS, etc.) without changing consumers |
| `pkg/smb` | **Fan-Out / Semaphore** | Concurrent goroutines scan hosts in parallel, bounded by a semaphore channel (`MaxConc`) for resource control |
| `pkg/resilience` | **State Machine** | `ConnectionState` (Connected, Disconnected, Reconnecting, Offline) drives transitions and event emission |
| `pkg/resilience` | **Observer** | `EventHandler` callbacks notify consumers of state changes without coupling to specific logging or metrics systems |
| `pkg/resilience` | **Strategy (injected)** | `Connector` interface is injected into `Manager` so connect/health-check logic is swappable per protocol |
| `pkg/resilience` (cache) | **Bounded Buffer / LRU Eviction** | `OfflineCache` stores changes while sources are unavailable; oldest entries are evicted when full |
| `pkg/report` | **Value Object** | `Report` aggregates scan results into an immutable snapshot with formatting methods |

## Dependency Diagram

```
  +----------+         +----------+
  |  report  +-------->|  scanner |
  +----------+         +----+-----+
                             ^
                             |  implements
                        +----+-----+
                        |   smb    |
                        +----------+

  +----------------------------------+
  |           resilience             |
  |                                  |
  |  +---------+   +----------+     |
  |  | Manager |-->| Connector|     |  (Connector is injected)
  |  +----+----+   +----------+     |
  |       |                         |
  |  +----+-------+  +-----------+ |
  |  |   Source    |  | OfflineCache| |
  |  +------------+  +-----------+ |
  |       |                         |
  |  +----+-----+                   |
  |  |  Event   |                   |
  |  +----------+                   |
  +----------------------------------+

  scanner and resilience are independent.
  report depends on scanner for the Service type.
  smb implements the scanner.Scanner interface.
```

## Key Interfaces

```go
// pkg/scanner -- implemented by protocol-specific scanners:
type Scanner interface {
    Scan(ctx context.Context) ([]*Service, error)
    ScanHost(ctx context.Context, host string) ([]*Service, error)
    Protocol() string
}

// pkg/resilience -- injected into Manager for connect/health-check:
type Connector interface {
    Connect(ctx context.Context, source *Source) error
    HealthCheck(ctx context.Context, source *Source) error
}

// pkg/resilience -- structured logging (inject zap, slog, etc.):
type Logger interface {
    Info(msg string, keysAndValues ...interface{})
    Warn(msg string, keysAndValues ...interface{})
    Error(msg string, keysAndValues ...interface{})
    Debug(msg string, keysAndValues ...interface{})
}

// pkg/resilience -- metrics reporting (inject Prometheus, StatsD, etc.):
type MetricsReporter interface {
    SetSourceHealth(sourceID string, value float64)
}

// pkg/resilience -- event callback:
type EventHandler func(event *Event)
```

### Connection State Machine

```
  Disconnected -----> Reconnecting -----> Connected
       ^                   |                  |
       |                   |                  |
       +-------------------+      (health check fails)
                                          |
  Offline <---- (retries exhausted) ------+
```

## Usage Example

```go
package main

import (
    "context"
    "fmt"
    "time"

    "digital.vasic.discovery/pkg/report"
    "digital.vasic.discovery/pkg/scanner"
    "digital.vasic.discovery/pkg/smb"
    "digital.vasic.discovery/pkg/resilience"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // 1. Scan the network for SMB services.
    cfg := &scanner.Config{
        Network: "192.168.1.0/24",
        Timeout: 2 * time.Second,
        MaxConc: 50,
    }
    s := smb.NewScanner(cfg)

    start := time.Now()
    services, err := s.Scan(ctx)
    if err != nil {
        panic(err)
    }

    // 2. Generate a report.
    r := report.NewReport(cfg.Network, services, time.Since(start))
    fmt.Println(r.Summary())

    // 3. Track a discovered source with resilience.
    mgr := resilience.NewManager(myLogger, myMetrics)
    mgr.SetConnector(mySMBConnector)

    for _, svc := range services {
        src := resilience.DefaultSource(svc.Name, svc.Name, svc.Host)
        mgr.AddSource(src)
    }

    mgr.OnEvent(func(e *resilience.Event) {
        fmt.Printf("[%s] source=%s type=%s\n", e.Timestamp, e.SourceID, e.Type)
    })

    // Health check and automatic recovery are driven by the caller.
    _ = mgr.CheckHealth(ctx, services[0].Name)
}
```
