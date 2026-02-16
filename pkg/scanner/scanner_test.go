package scanner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	require.NotNil(t, cfg)
	assert.Equal(t, 5*time.Second, cfg.Timeout)
	assert.Equal(t, 50, cfg.MaxConc)
	assert.Empty(t, cfg.Ports)
	assert.Empty(t, cfg.Network)
}

func TestDefaultConfig_IsModifiable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Network = "10.0.0.0/8"
	cfg.Ports = []int{80, 443}
	cfg.Timeout = 10 * time.Second
	cfg.MaxConc = 100

	assert.Equal(t, "10.0.0.0/8", cfg.Network)
	assert.Equal(t, []int{80, 443}, cfg.Ports)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
	assert.Equal(t, 100, cfg.MaxConc)
}

func TestService_Fields(t *testing.T) {
	now := time.Now()
	svc := &Service{
		Name:     "test-smb",
		Host:     "192.168.1.100",
		Port:     445,
		Protocol: "smb",
		Metadata: map[string]string{
			"version": "3.0",
			"os":      "Windows",
		},
		FoundAt: now,
	}

	assert.Equal(t, "test-smb", svc.Name)
	assert.Equal(t, "192.168.1.100", svc.Host)
	assert.Equal(t, 445, svc.Port)
	assert.Equal(t, "smb", svc.Protocol)
	assert.Equal(t, "3.0", svc.Metadata["version"])
	assert.Equal(t, "Windows", svc.Metadata["os"])
	assert.Equal(t, now, svc.FoundAt)
}

func TestService_NilMetadata(t *testing.T) {
	svc := &Service{
		Name:     "minimal",
		Host:     "10.0.0.1",
		Port:     80,
		Protocol: "http",
	}

	assert.Nil(t, svc.Metadata)
	assert.True(t, svc.FoundAt.IsZero())
}

func TestConfig_ZeroValue(t *testing.T) {
	var cfg Config

	assert.Empty(t, cfg.Network)
	assert.Zero(t, cfg.Timeout)
	assert.Nil(t, cfg.Ports)
	assert.Zero(t, cfg.MaxConc)
}
