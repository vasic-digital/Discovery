// SPDX-License-Identifier: MIT

package broadcast

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, DefaultMulticastGroup, cfg.MulticastGroup)
	assert.Equal(t, DefaultPort, cfg.Port)
	assert.Equal(t, DefaultInterval, cfg.Interval)
	assert.Equal(t, DefaultTimeout, cfg.Timeout)
}

func TestNewAnnouncer_Defaults(t *testing.T) {
	info := ServiceInfo{Service: "test", Host: "1.2.3.4", Port: 8080}
	a := NewAnnouncer(info, Config{})
	assert.Equal(t, DefaultMulticastGroup, a.config.MulticastGroup)
	assert.Equal(t, DefaultPort, a.config.Port)
	assert.Equal(t, DefaultInterval, a.config.Interval)
	assert.Equal(t, TypeAnnounce, a.info.Type)
}

func TestNewAnnouncer_CustomConfig(t *testing.T) {
	cfg := Config{
		MulticastGroup: "239.1.2.3",
		Port:           9999,
		Interval:       10 * time.Second,
	}
	info := ServiceInfo{Service: "test"}
	a := NewAnnouncer(info, cfg)
	assert.Equal(t, "239.1.2.3", a.config.MulticastGroup)
	assert.Equal(t, 9999, a.config.Port)
	assert.Equal(t, 10*time.Second, a.config.Interval)
}

func TestAnnouncer_StartStop(t *testing.T) {
	info := ServiceInfo{
		Service:  "catalogizer-api",
		Version:  "1.0.0",
		Host:     "127.0.0.1",
		Port:     8080,
		Protocol: "http",
	}
	a := NewAnnouncer(info, DefaultConfig())

	err := a.Start()
	require.NoError(t, err)
	assert.True(t, a.running)

	// Double start should be no-op
	err = a.Start()
	require.NoError(t, err)

	a.Stop()
	assert.False(t, a.running)

	// Double stop should be safe
	a.Stop()
}

func TestAnnouncer_UpdateInfo(t *testing.T) {
	info := ServiceInfo{Service: "v1", Host: "1.2.3.4", Port: 8080}
	a := NewAnnouncer(info, DefaultConfig())

	newInfo := ServiceInfo{Service: "v2", Host: "5.6.7.8", Port: 9090}
	a.UpdateInfo(newInfo)

	assert.Equal(t, "v2", a.info.Service)
	assert.Equal(t, "5.6.7.8", a.info.Host)
	assert.Equal(t, TypeAnnounce, a.info.Type)
}

func TestNewListener_Defaults(t *testing.T) {
	l := NewListener(Config{})
	assert.Equal(t, DefaultMulticastGroup, l.config.MulticastGroup)
	assert.Equal(t, DefaultPort, l.config.Port)
	assert.Equal(t, DefaultTimeout, l.config.Timeout)
}

func TestServiceInfo_JSON(t *testing.T) {
	info := ServiceInfo{
		Type:         TypeAnnounce,
		Service:      "catalogizer-api",
		Version:      "1.0.0",
		Build:        "42",
		Host:         "192.168.0.1",
		Port:         8080,
		Protocol:     "http",
		Name:         "My Catalogizer",
		InstanceID:   "abc-123",
		Capabilities: []string{"catalog", "media"},
		Database:     "sqlite",
		StorageRoots: 3,
		Uptime:       3600,
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var decoded ServiceInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, info.Type, decoded.Type)
	assert.Equal(t, info.Service, decoded.Service)
	assert.Equal(t, info.Host, decoded.Host)
	assert.Equal(t, info.Port, decoded.Port)
	assert.Equal(t, info.Capabilities, decoded.Capabilities)
	assert.Equal(t, info.Uptime, decoded.Uptime)
}

func TestAnnouncer_SendsToMulticast(t *testing.T) {
	// Use a unique port to avoid conflicts
	port := 42070

	info := ServiceInfo{
		Service:  "catalogizer-api",
		Version:  "test",
		Host:     "127.0.0.1",
		Port:     8080,
		Protocol: "http",
	}

	cfg := Config{
		MulticastGroup: DefaultMulticastGroup,
		Port:           port,
		Interval:       100 * time.Millisecond,
	}

	// Start listening before announcer
	addr, err := net.ResolveUDPAddr("udp4", "239.42.42.42:42070")
	require.NoError(t, err)

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		t.Skipf("multicast not available: %v", err)
	}
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))

	// Start announcer
	a := NewAnnouncer(info, cfg)
	err = a.Start()
	require.NoError(t, err)
	defer a.Stop()

	// Read a broadcast message
	buf := make([]byte, 4096)
	n, _, err := conn.ReadFromUDP(buf)
	require.NoError(t, err)
	assert.Greater(t, n, 0)

	var received ServiceInfo
	err = json.Unmarshal(buf[:n], &received)
	require.NoError(t, err)
	assert.Equal(t, TypeAnnounce, received.Type)
	assert.Equal(t, "catalogizer-api", received.Service)
	assert.Equal(t, 8080, received.Port)
}

func TestListener_Discover_Timeout(t *testing.T) {
	// Use a port where no announcer is running
	cfg := Config{
		MulticastGroup: DefaultMulticastGroup,
		Port:           42071,
		Timeout:        500 * time.Millisecond,
	}
	l := NewListener(cfg)

	ctx := context.Background()
	services, err := l.Discover(ctx)
	// Should return empty, no error (timeout is normal)
	assert.NoError(t, err)
	assert.Empty(t, services)
}

func TestListener_DiscoverOne_NoServices(t *testing.T) {
	cfg := Config{
		MulticastGroup: DefaultMulticastGroup,
		Port:           42072,
		Timeout:        500 * time.Millisecond,
	}
	l := NewListener(cfg)

	_, err := l.DiscoverOne(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no services discovered")
}

func TestListener_Discover_ContextCancel(t *testing.T) {
	cfg := Config{
		MulticastGroup: DefaultMulticastGroup,
		Port:           42073,
		Timeout:        10 * time.Second, // long timeout
	}
	l := NewListener(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	services, err := l.Discover(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, services)
}

func TestToSlice(t *testing.T) {
	m := map[string]ServiceInfo{
		"a": {Host: "1.1.1.1", Port: 80},
		"b": {Host: "2.2.2.2", Port: 90},
	}
	s := toSlice(m)
	assert.Len(t, s, 2)
}

func TestToSlice_Empty(t *testing.T) {
	s := toSlice(map[string]ServiceInfo{})
	assert.Empty(t, s)
	assert.NotNil(t, s)
}

func TestMessageType_Constants(t *testing.T) {
	assert.Equal(t, MessageType("catalogizer-announce"), TypeAnnounce)
	assert.Equal(t, MessageType("catalogizer-discover"), TypeDiscover)
}
