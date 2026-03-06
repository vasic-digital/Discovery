# Lesson 3: Connection Resilience and Offline Caching

## Learning Objectives

- Manage connection state with the resilience Manager
- Implement the Connector interface for health checks and reconnection
- Use OfflineCache to queue and replay changes during outages

## Key Concepts

- **Connection State Machine**: Four states (`Connected`, `Disconnected`, `Reconnecting`, `Offline`) with health metrics (1.0, 0.5, 0.5, 0.0). The Manager transitions sources between states based on Connector results.
- **Connector Interface**: `Connect(ctx, source) error` and `HealthCheck(ctx, source) error` are implemented by consumers for their specific protocol. The Manager calls these during recovery and health monitoring.
- **Event System**: State changes emit events (`EventConnected`, `EventDisconnected`, `EventReconnecting`, `EventOffline`, `EventError`, `EventHealthCheck`) to registered handlers for observability.
- **Exponential Backoff**: `RecoverSource` retries with `baseDelay * 2^attempt` up to `MaxRetryAttempts`. After exhausting retries, the source is declared Offline.
- **Offline Cache**: Bounded FIFO cache that stores changes while a source is unavailable. `ProcessCachedChanges(sourceID)` extracts and removes all entries for a source, enabling replay after reconnection.

## Code Walkthrough

### Source: `pkg/resilience/manager.go`

`RecoverSource` implements the retry loop with exponential backoff:

```go
for attempt := 0; attempt < maxRetries; attempt++ {
    err := connector.Connect(ctx, src)
    if err == nil {
        src.State = Connected
        return nil
    }
    delay := baseDelay * time.Duration(1<<uint(attempt))
    // wait or cancel...
}
src.State = Offline
```

`CheckHealth` runs a single health check and transitions the source to Disconnected on failure or back to Connected on success.

### Source: `pkg/resilience/cache.go`

The `OfflineCache` stores `CacheEntry` values keyed by source ID. When full, the oldest entry is evicted on insert. `ProcessCachedChanges` filters entries by source, removes them from the cache, and returns them in insertion order.

## Practice Exercise

1. Create a Manager with a mock Connector that fails the first 3 connect attempts, then succeeds. Call `RecoverSource` and verify the source transitions through Reconnecting to Connected.
2. Register an event handler and verify it receives `EventReconnecting`, `EventError` (for each failure), and `EventConnected` events in order.
3. Create an OfflineCache with `maxSize=3`. Insert 4 entries and verify the first entry was evicted. Call `ProcessCachedChanges` and verify all remaining entries for the source are returned and removed.
