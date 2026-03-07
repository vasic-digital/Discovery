package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"digital.vasic.discovery/pkg/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReport_EmptyResults verifies report generation when there are no scan results.
func TestReport_EmptyResults(t *testing.T) {
	report := NewReport("192.168.1.0/24", []*scanner.Service{}, 500*time.Millisecond)

	require.NotNil(t, report)
	assert.Equal(t, 0, report.TotalFound)
	assert.Empty(t, report.Services)
	assert.Equal(t, "192.168.1.0/24", report.Network)
	assert.Equal(t, 500*time.Millisecond, report.Duration)
	assert.False(t, report.ScanTime.IsZero())

	// Summary should not contain "Services:" section.
	summary := report.Summary()
	assert.Contains(t, summary, "0 service(s)")
	assert.NotContains(t, summary, "Services:")

	// JSON should serialize cleanly with empty array.
	data, err := report.ToJSON()
	require.NoError(t, err)

	var parsed Report
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)
	assert.Equal(t, 0, parsed.TotalFound)
	assert.Empty(t, parsed.Services)
}

// TestReport_Serialization_JSON verifies full JSON serialization and deserialization
// round-trip preserves all fields.
func TestReport_Serialization_JSON(t *testing.T) {
	now := time.Now()
	services := []*scanner.Service{
		{
			Name:     "smb-10.0.0.1:445",
			Host:     "10.0.0.1",
			Port:     445,
			Protocol: "smb",
			Metadata: map[string]string{"port_type": "microsoft-ds", "os": "Linux"},
			FoundAt:  now,
		},
		{
			Name:     "ftp-10.0.0.2:21",
			Host:     "10.0.0.2",
			Port:     21,
			Protocol: "ftp",
			Metadata: map[string]string{"port_type": "ftp-data"},
			FoundAt:  now,
		},
	}

	original := NewReport("10.0.0.0/24", services, 3500*time.Millisecond)

	// Serialize to JSON.
	data, err := original.ToJSON()
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify it's valid, indented JSON.
	assert.True(t, json.Valid(data))
	assert.Contains(t, string(data), "\n")

	// Deserialize back.
	var restored Report
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	// Verify all top-level fields.
	assert.Equal(t, original.Network, restored.Network)
	assert.Equal(t, original.TotalFound, restored.TotalFound)
	assert.Equal(t, original.Duration, restored.Duration)
	assert.WithinDuration(t, original.ScanTime, restored.ScanTime, time.Second)

	// Verify services.
	require.Len(t, restored.Services, 2)
	for i, svc := range restored.Services {
		assert.Equal(t, original.Services[i].Name, svc.Name)
		assert.Equal(t, original.Services[i].Host, svc.Host)
		assert.Equal(t, original.Services[i].Port, svc.Port)
		assert.Equal(t, original.Services[i].Protocol, svc.Protocol)
	}

	// Re-serialize the restored report and compare.
	data2, err := restored.ToJSON()
	require.NoError(t, err)
	assert.JSONEq(t, string(data), string(data2))
}

// TestReport_MissingFields tests report behavior with nil or zero-value fields.
func TestReport_MissingFields(t *testing.T) {
	t.Run("nil_services", func(t *testing.T) {
		report := NewReport("", nil, 0)

		require.NotNil(t, report)
		assert.Equal(t, 0, report.TotalFound)
		assert.Nil(t, report.Services)
		assert.Empty(t, report.Network)
		assert.Equal(t, time.Duration(0), report.Duration)

		// Should still produce valid JSON.
		data, err := report.ToJSON()
		require.NoError(t, err)
		assert.True(t, json.Valid(data))
	})

	t.Run("zero_value_report", func(t *testing.T) {
		var report Report

		assert.Equal(t, 0, report.TotalFound)
		assert.Nil(t, report.Services)
		assert.True(t, report.ScanTime.IsZero())
		assert.Empty(t, report.Network)

		// ToJSON on zero-value report.
		data, err := report.ToJSON()
		require.NoError(t, err)
		assert.True(t, json.Valid(data))

		summary := report.Summary()
		assert.Contains(t, summary, "Discovery Report")
		assert.Contains(t, summary, "0 service(s)")
	})

	t.Run("services_with_nil_metadata", func(t *testing.T) {
		services := []*scanner.Service{
			{
				Name:     "minimal",
				Host:     "10.0.0.1",
				Port:     80,
				Protocol: "http",
				// Metadata is nil, FoundAt is zero.
			},
		}
		report := NewReport("10.0.0.0/24", services, 100*time.Millisecond)

		data, err := report.ToJSON()
		require.NoError(t, err)

		var restored Report
		err = json.Unmarshal(data, &restored)
		require.NoError(t, err)
		require.Len(t, restored.Services, 1)
		assert.Nil(t, restored.Services[0].Metadata)
	})
}

// TestReport_LargeResultSet tests report with 10000 service entries.
func TestReport_LargeResultSet(t *testing.T) {
	const count = 10000
	services := make([]*scanner.Service, count)
	now := time.Now()

	for i := 0; i < count; i++ {
		services[i] = &scanner.Service{
			Name:     fmt.Sprintf("svc-%d", i),
			Host:     fmt.Sprintf("10.%d.%d.%d", (i>>16)&0xFF, (i>>8)&0xFF, i&0xFF),
			Port:     445,
			Protocol: "smb",
			Metadata: map[string]string{"index": fmt.Sprintf("%d", i)},
			FoundAt:  now,
		}
	}

	report := NewReport("10.0.0.0/8", services, 30*time.Second)

	require.NotNil(t, report)
	assert.Equal(t, count, report.TotalFound)
	assert.Len(t, report.Services, count)

	// JSON serialization should work for large sets.
	data, err := report.ToJSON()
	require.NoError(t, err)
	assert.True(t, json.Valid(data))

	// Verify round-trip preserves count.
	var restored Report
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)
	assert.Equal(t, count, restored.TotalFound)
	assert.Len(t, restored.Services, count)

	// Summary should report correct count.
	summary := report.Summary()
	assert.Contains(t, summary, fmt.Sprintf("%d service(s)", count))
}

// TestReport_SpecialCharacters tests hostnames and paths with special characters.
func TestReport_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		network  string
		svcName  string
		host     string
		protocol string
		metadata map[string]string
	}{
		{
			name:     "unicode_hostname",
			network:  "192.168.1.0/24",
			svcName:  "smb-serveur-\u00e9tranger:445",
			host:     "serveur-\u00e9tranger.local",
			protocol: "smb",
			metadata: map[string]string{"desc": "Acc\u00e8s r\u00e9seau"},
		},
		{
			name:     "special_chars_in_path",
			network:  "10.0.0.0/8",
			svcName:  "smb-host:445",
			host:     "nas-server.local",
			protocol: "smb",
			metadata: map[string]string{"path": "/media/My Videos & Photos (2024)/file [1].mp4"},
		},
		{
			name:     "json_escape_chars",
			network:  "172.16.0.0/16",
			svcName:  "svc-with-\"quotes\"",
			host:     "host\\with\\backslash",
			protocol: "smb",
			metadata: map[string]string{"note": "line1\nline2\ttab"},
		},
		{
			name:     "empty_strings",
			network:  "",
			svcName:  "",
			host:     "",
			protocol: "",
			metadata: map[string]string{"": ""},
		},
		{
			name:     "very_long_hostname",
			network:  "10.0.0.0/8",
			svcName:  strings.Repeat("a", 500),
			host:     strings.Repeat("b", 255),
			protocol: "smb",
			metadata: map[string]string{"key": strings.Repeat("v", 1000)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			services := []*scanner.Service{
				{
					Name:     tt.svcName,
					Host:     tt.host,
					Port:     445,
					Protocol: tt.protocol,
					Metadata: tt.metadata,
					FoundAt:  time.Now(),
				},
			}

			report := NewReport(tt.network, services, 1*time.Second)
			require.NotNil(t, report)
			assert.Equal(t, 1, report.TotalFound)

			// JSON round-trip should preserve special characters.
			data, err := report.ToJSON()
			require.NoError(t, err)
			assert.True(t, json.Valid(data))

			var restored Report
			err = json.Unmarshal(data, &restored)
			require.NoError(t, err)
			require.Len(t, restored.Services, 1)
			assert.Equal(t, tt.svcName, restored.Services[0].Name)
			assert.Equal(t, tt.host, restored.Services[0].Host)
			assert.Equal(t, tt.protocol, restored.Services[0].Protocol)

			// Summary should not panic.
			summary := report.Summary()
			assert.Contains(t, summary, "Discovery Report")
		})
	}
}
