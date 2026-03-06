# Course: Network Discovery and Service Resilience in Go

## Module Overview

This course covers the `digital.vasic.discovery` module, teaching network service discovery via port scanning, scan report generation, and connection resilience with state machines, offline caching, and automatic recovery. The focus is on building robust, concurrent discovery systems.

## Prerequisites

- Intermediate Go knowledge (interfaces, goroutines, channels, `net` package)
- Basic networking concepts (TCP, CIDR, ports)
- Go 1.24+ installed

## Lessons

| # | Title | Duration |
|---|-------|----------|
| 1 | Scanner Interface and SMB Discovery | 40 min |
| 2 | Scan Reports and CIDR Expansion | 30 min |
| 3 | Connection Resilience and Offline Caching | 45 min |

## Source Files

- `pkg/scanner/` -- Core scanner interface, Service, and Config types
- `pkg/smb/` -- SMB/CIFS discovery via TCP port scanning
- `pkg/report/` -- Scan report generation
- `pkg/resilience/` -- Connection state machine, Manager, Connector, OfflineCache
