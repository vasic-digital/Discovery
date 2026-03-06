# Examples

## Multi-Protocol Discovery with Report

Scan a network and generate a JSON report:

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
        Network: "10.0.0.0/24",
        Timeout: 2 * time.Second,
        MaxConc: 100,
    }

    s := smb.NewScanner(cfg)

    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()

    start := time.Now()
    services, _ := s.Scan(ctx)

    r := report.NewReport("10.0.0.0/24", services, time.Since(start))

    // JSON output
    jsonBytes, _ := r.ToJSON()
    fmt.Println(string(jsonBytes))

    // Human-readable summary
    fmt.Println(r.Summary())
}
```

## Offline Cache with Change Replay

Cache changes while a source is unavailable and replay them on reconnection:

```go
import "digital.vasic.discovery/pkg/resilience"

cache := resilience.NewOfflineCache(500, myLogger)

// Source goes offline -- cache changes
cache.EnableOfflineMode()
cache.CacheChange("file-created:/data/movie.mkv", "nas-1", map[string]string{
    "path": "/data/movie.mkv",
    "size": "4294967296",
})
cache.CacheChange("file-modified:/data/index.db", "nas-1", map[string]string{
    "path": "/data/index.db",
})

fmt.Printf("Cached entries: %d\n", cache.Size()) // 2

// Source comes back online -- replay changes
cache.DisableOfflineMode()
entries := cache.ProcessCachedChanges("nas-1")
for _, entry := range entries {
    fmt.Printf("Replay: %s (cached at %s)\n", entry.Key, entry.CachedAt)
}
fmt.Printf("Remaining entries: %d\n", cache.Size()) // 0
```

## Resilience Manager with Health Checks

Monitor source health and handle state transitions:

```go
import (
    "context"
    "digital.vasic.discovery/pkg/resilience"
)

// Implement the Connector interface
type smbConnector struct{}

func (c *smbConnector) Connect(ctx context.Context, src *resilience.Source) error {
    // TCP dial to src.Endpoint
    return nil
}

func (c *smbConnector) HealthCheck(ctx context.Context, src *resilience.Source) error {
    // Verify the source is still reachable
    return nil
}

mgr := resilience.NewManager(myLogger, nil)
mgr.SetConnector(&smbConnector{})

source := resilience.DefaultSource("nas-1", "NAS", "192.168.1.50:445")
mgr.AddSource(source)

// Run health check
err := mgr.CheckHealth(context.Background(), "nas-1")
if err != nil {
    // Source is down -- start recovery
    mgr.RecoverSource(context.Background(), "nas-1")
}
```
