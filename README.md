# digital.vasic.discovery

A Go module for network and service discovery. Provides interfaces and implementations for scanning networks to detect running services, with an initial focus on SMB/CIFS discovery.

## Installation

```bash
go get digital.vasic.discovery
```

## Packages

### `pkg/scanner` -- Core Interfaces

Defines the `Scanner` interface and shared types used by all protocol implementations.

```go
import "digital.vasic.discovery/pkg/scanner"

// Scanner is the common interface for all discovery implementations.
type Scanner interface {
    Scan(ctx context.Context) ([]*Service, error)
    ScanHost(ctx context.Context, host string) ([]*Service, error)
    Protocol() string
}
```

### `pkg/smb` -- SMB/CIFS Discovery

Discovers SMB services by probing TCP ports 445 and 139 across a network range.

```go
import "digital.vasic.discovery/pkg/smb"

cfg := &scanner.Config{
    Network: "192.168.1.0/24",
    Timeout: 3 * time.Second,
    MaxConc: 100,
}

s := smb.NewScanner(cfg)
services, err := s.Scan(context.Background())
```

Scan a single host:

```go
services, err := s.ScanHost(ctx, "192.168.1.50")
```

### `pkg/report` -- Report Generation

Generates structured reports from scan results.

```go
import "digital.vasic.discovery/pkg/report"

r := report.NewReport("192.168.1.0/24", services, scanDuration)

// JSON output
jsonBytes, err := r.ToJSON()

// Human-readable summary
fmt.Println(r.Summary())
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
)

func main() {
    cfg := &scanner.Config{
        Network: "192.168.1.0/24",
        Timeout: 2 * time.Second,
        MaxConc: 50,
    }

    s := smb.NewScanner(cfg)

    start := time.Now()
    services, err := s.Scan(context.Background())
    if err != nil {
        fmt.Printf("Scan error: %v\n", err)
        return
    }
    elapsed := time.Since(start)

    r := report.NewReport(cfg.Network, services, elapsed)
    fmt.Println(r.Summary())
}
```

## Development

```bash
# Run all tests
go test ./... -count=1

# Build
go build ./...

# Tidy dependencies
go mod tidy
```

## Requirements

- Go 1.24.0 or later

## License

See LICENSE file for details.
