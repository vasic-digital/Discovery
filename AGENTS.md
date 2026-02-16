# AGENTS.md

Guidelines for AI agents working with this repository.

## Repository Purpose

This is the `digital.vasic.discovery` Go module -- a standalone library for network and service discovery. It is designed to be imported by other projects (e.g., Catalogizer's catalog-api) to discover services on local networks.

## Key Files

| File | Purpose |
|---|---|
| `pkg/scanner/scanner.go` | Core `Scanner` interface and `Service`/`Config` types |
| `pkg/smb/smb.go` | SMB/CIFS port scanner implementation |
| `pkg/report/report.go` | Scan report generation (JSON + text) |
| `go.mod` | Module definition: `digital.vasic.discovery` |

## How to Verify Changes

After any modification, always run:

```bash
go mod tidy && go build ./... && go test ./... -count=1
```

All tests must pass. There are no external service dependencies for testing; SMB tests use local TCP listeners.

## Adding New Scanner Implementations

When adding support for a new protocol:

1. Create `pkg/<protocol>/<protocol>.go` implementing `scanner.Scanner`.
2. Create `pkg/<protocol>/<protocol>_test.go` with comprehensive tests.
3. Default ports should be set in the constructor when the config has none.
4. The `Protocol()` method must return a lowercase string identifier.
5. All scan methods must honor `context.Context` cancellation.
6. Use the concurrency pattern from `pkg/smb/smb.go` (semaphore + WaitGroup).

## Do Not

- Add external dependencies beyond `stretchr/testify` without explicit approval.
- Modify the `scanner.Scanner` interface without updating all implementations.
- Skip context cancellation checks in scan loops.
- Create GitHub Actions workflow files.
