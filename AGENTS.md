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


## ⚠️ MANDATORY: NO SUDO OR ROOT EXECUTION

**ALL operations MUST run at local user level ONLY.**

This is a PERMANENT and NON-NEGOTIABLE security constraint:

- **NEVER** use `sudo` in ANY command
- **NEVER** use `su` in ANY command
- **NEVER** execute operations as `root` user
- **NEVER** elevate privileges for file operations
- **ALL** infrastructure commands MUST use user-level container runtimes (rootless podman/docker)
- **ALL** file operations MUST be within user-accessible directories
- **ALL** service management MUST be done via user systemd or local process management
- **ALL** builds, tests, and deployments MUST run as the current user

### Container-Based Solutions
When a build or runtime environment requires system-level dependencies, use containers instead of elevation:

- **Use the `Containers` submodule** (`https://github.com/vasic-digital/Containers`) for containerized build and runtime environments
- **Add the `Containers` submodule as a Git dependency** and configure it for local use within the project
- **Build and run inside containers** to avoid any need for privilege escalation
- **Rootless Podman/Docker** is the preferred container runtime

### Why This Matters
- **Security**: Prevents accidental system-wide damage
- **Reproducibility**: User-level operations are portable across systems
- **Safety**: Limits blast radius of any issues
- **Best Practice**: Modern container workflows are rootless by design

### When You See SUDO
If any script or command suggests using `sudo` or `su`:
1. STOP immediately
2. Find a user-level alternative
3. Use rootless container runtimes
4. Use the `Containers` submodule for containerized builds
5. Modify commands to work within user permissions

**VIOLATION OF THIS CONSTRAINT IS STRICTLY PROHIBITED.**


### ⚠️⚠️⚠️ ABSOLUTELY MANDATORY: ZERO UNFINISHED WORK POLICY

NO unfinished work, TODOs, or known issues may remain in the codebase. EVER.

PROHIBITED: TODO/FIXME comments, empty implementations, silent errors, fake data, unwrap() calls that panic, empty catch blocks.

REQUIRED: Fix ALL issues immediately, complete implementations before committing, proper error handling in ALL code paths, real test assertions.

Quality Principle: If it is not finished, it does not ship. If it ships, it is finished.
