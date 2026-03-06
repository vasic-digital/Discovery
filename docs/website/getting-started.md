# Getting Started

## Installation

```bash
go get digital.vasic.discovery
```

## Scanning a Network for SMB Shares

Discover SMB services on a local network:

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
        Timeout: 3 * time.Second,
        Ports:   []int{445, 139},
        MaxConc: 50,
    }

    s := smb.NewScanner(cfg)

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    start := time.Now()
    services, err := s.Scan(ctx)
    if err != nil {
        fmt.Printf("Scan error: %v\n", err)
        return
    }

    r := report.NewReport("192.168.1.0/24", services, time.Since(start))
    fmt.Println(r.Summary())
}
```

## Scanning a Single Host

Check a specific host for SMB availability:

```go
s := smb.NewScanner(nil) // uses default config

services, err := s.ScanHost(context.Background(), "192.168.1.100")
if err != nil {
    log.Fatal(err)
}

for _, svc := range services {
    fmt.Printf("Found: %s:%d (%s)\n", svc.Host, svc.Port, svc.Metadata["port_type"])
}
```

## Managing Connection Resilience

Use the resilience manager to monitor and recover sources:

```go
import "digital.vasic.discovery/pkg/resilience"

mgr := resilience.NewManager(myLogger, myMetrics)

source := resilience.DefaultSource("nas-1", "NAS Primary", "192.168.1.50:445")
mgr.AddSource(source)

// Register event handler for state changes
mgr.OnEvent(func(event *resilience.Event) {
    fmt.Printf("[%s] source %s: %s\n",
        event.Timestamp.Format(time.RFC3339),
        event.SourceID,
        event.Type)
})

// Attempt recovery with exponential backoff
err := mgr.RecoverSource(context.Background(), "nas-1")
```
