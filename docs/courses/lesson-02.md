# Lesson 2: Scan Reports and CIDR Expansion

## Learning Objectives

- Generate structured scan reports from discovery results
- Produce JSON and human-readable summaries
- Group discovered services by protocol

## Key Concepts

- **Report Structure**: `report.Report` contains scan time, duration, network CIDR, services list, and total count. It is created from raw scan results via `NewReport()`.
- **JSON Serialization**: `ToJSON()` produces indented JSON suitable for storage or API responses. All fields have JSON struct tags.
- **Summary Format**: `Summary()` generates a text report with network, scan time, duration, total count, and services grouped by protocol with host:port details.

## Code Walkthrough

### Source: `pkg/report/report.go`

`NewReport` captures the current timestamp and wraps the services:

```go
func NewReport(network string, services []*scanner.Service, duration time.Duration) *Report {
    return &Report{
        ScanTime:   time.Now(),
        Duration:   duration,
        Network:    network,
        Services:   services,
        TotalFound: len(services),
    }
}
```

`Summary()` groups services by protocol using a `map[string][]*scanner.Service` and formats each group with indentation.

### CIDR Expansion (in `pkg/smb/smb.go`)

`expandCIDR` parses the CIDR notation, iterates from network address to broadcast, and strips the first (network) and last (broadcast) addresses for networks larger than `/31`.

## Practice Exercise

1. Create a report from mock scan results containing 3 SMB services on different hosts. Call `Summary()` and verify the text output includes all hosts grouped under `[smb]`.
2. Serialize the same report to JSON with `ToJSON()`. Parse the JSON back and verify `total_found` equals 3.
3. Test `expandCIDR` with `/30` (expect 2 hosts), `/31` (expect 2 hosts), and `/24` (expect 254 hosts).
