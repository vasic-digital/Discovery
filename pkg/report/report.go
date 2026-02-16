// Package report provides discovery scan report generation.
package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"digital.vasic.discovery/pkg/scanner"
)

// Report represents the results of a discovery scan.
type Report struct {
	ScanTime   time.Time          `json:"scan_time"`
	Duration   time.Duration      `json:"duration"`
	Network    string             `json:"network"`
	Services   []*scanner.Service `json:"services"`
	TotalFound int                `json:"total_found"`
}

// NewReport creates a new Report from scan results.
func NewReport(network string, services []*scanner.Service, duration time.Duration) *Report {
	return &Report{
		ScanTime:   time.Now(),
		Duration:   duration,
		Network:    network,
		Services:   services,
		TotalFound: len(services),
	}
}

// ToJSON serializes the report to indented JSON bytes.
func (r *Report) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Summary returns a human-readable summary of the scan results.
func (r *Report) Summary() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Discovery Report\n"))
	sb.WriteString(fmt.Sprintf("================\n"))
	sb.WriteString(fmt.Sprintf("Network:    %s\n", r.Network))
	sb.WriteString(fmt.Sprintf("Scan time:  %s\n", r.ScanTime.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Duration:   %s\n", r.Duration.Round(time.Millisecond)))
	sb.WriteString(fmt.Sprintf("Found:      %d service(s)\n", r.TotalFound))

	if r.TotalFound > 0 {
		sb.WriteString(fmt.Sprintf("\nServices:\n"))

		// Group services by protocol.
		byProtocol := make(map[string][]*scanner.Service)
		for _, svc := range r.Services {
			byProtocol[svc.Protocol] = append(byProtocol[svc.Protocol], svc)
		}

		for proto, svcs := range byProtocol {
			sb.WriteString(fmt.Sprintf("  [%s] %d service(s)\n", proto, len(svcs)))
			for _, svc := range svcs {
				sb.WriteString(fmt.Sprintf("    - %s:%d (%s)\n", svc.Host, svc.Port, svc.Name))
			}
		}
	}

	return sb.String()
}
