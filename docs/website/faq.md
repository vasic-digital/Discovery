# FAQ

## Does the SMB scanner actually speak the SMB protocol?

No. The SMB scanner discovers services by attempting TCP connections to SMB ports (445 and 139). It does not implement SMB protocol negotiation. If a TCP connection succeeds, the host is reported as having an SMB-accessible port. This keeps the module dependency-free.

## How does the scanner handle unreachable hosts?

Unreachable hosts simply time out on the TCP dial. The timeout is configurable via `Config.Timeout` (default 5 seconds). Failed connections are silently skipped -- only successfully connected hosts appear in the results. Context cancellation is respected at every step.

## What is the connection state machine in the resilience package?

The resilience package defines four states: `Connected` (source is reachable), `Disconnected` (connection lost, not yet recovering), `Reconnecting` (recovery attempt in progress), and `Offline` (declared unreachable after exhausting retries). State transitions emit events that registered handlers can observe.

## How does the offline cache handle capacity limits?

The `OfflineCache` is bounded by `maxSize` (default 1000 entries). When the cache is full and a new entry is inserted, the oldest entry is evicted (FIFO). `ProcessCachedChanges(sourceID)` removes and returns all entries for a specific source, freeing space.

## Can I add scanners for protocols other than SMB?

Yes. Create a new package under `pkg/` and implement the `scanner.Scanner` interface: `Scan(ctx) ([]*Service, error)`, `ScanHost(ctx, host) ([]*Service, error)`, and `Protocol() string`. Set protocol-appropriate default ports in the constructor.
