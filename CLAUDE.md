# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`digital.vasic.discovery` is a standalone Go module for network and service discovery. It provides interfaces and implementations for scanning networks to find services, with an initial focus on SMB/CIFS share discovery via TCP port probing.

## Commands

```bash
# Run all tests
go test ./... -count=1

# Run tests with verbose output
go test -v ./... -count=1

# Run a single package's tests
go test -v ./pkg/scanner/ -count=1
go test -v ./pkg/smb/ -count=1
go test -v ./pkg/report/ -count=1

# Run a specific test
go test -v -run TestNewScanner_NilConfig ./pkg/smb/

# Build all packages
go build ./...

# Tidy dependencies
go mod tidy
```

## Architecture

The module is organized into three packages under `pkg/`:

- **`pkg/scanner`** -- Core interfaces and types. Defines `Scanner` (interface), `Service` (discovered service), and `Config` (scanner configuration). All scanner implementations must satisfy the `Scanner` interface.

- **`pkg/smb`** -- SMB/CIFS discovery scanner. Implements `scanner.Scanner` by attempting TCP connections to SMB ports (445, 139) across a CIDR network range. Uses concurrent goroutines with a semaphore for controlled parallelism. Includes CIDR expansion utilities.

- **`pkg/report`** -- Report generation. Takes scan results and produces structured reports with JSON serialization and human-readable summaries. Groups discovered services by protocol.

### Adding a New Protocol Scanner

1. Create a new package under `pkg/` (e.g., `pkg/ftp/`).
2. Implement the `scanner.Scanner` interface: `Scan()`, `ScanHost()`, `Protocol()`.
3. Set protocol-appropriate default ports in the constructor.
4. Add tests covering: nil config, custom config, unreachable hosts, live listeners, context cancellation.

## Conventions

- **Go**: Constructor injection via `NewXxx(cfg)`, table-driven tests, `*_test.go` beside source files.
- **Error handling**: Wrap errors with `fmt.Errorf` and `%w` for unwrapping.
- **Concurrency**: Use semaphore channels for limiting goroutines; respect `context.Context` cancellation.
- **Testing**: Use `github.com/stretchr/testify` for assertions. Spin up local TCP listeners for integration-style tests.

## Constraints

- No external dependencies beyond `stretchr/testify` for testing.
- No SMB protocol-level libraries; discovery is pure TCP port scanning.
- Context cancellation must be respected in all scan operations.


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


