package targets

import (
	"log/slog"
	"net"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/mdns"
)

func TestManager_AddEntry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	tests := []struct {
		name         string
		entries      []*mdns.ServiceEntry
		expectedKeys []string
		expectCount  int
	}{
		{
			name: "single IPv4 entry",
			entries: []*mdns.ServiceEntry{
				{
					Name:       "test",
					Port:       9100,
					AddrV4:     net.ParseIP("192.168.1.10"),
					InfoFields: []string{"path=/metrics", "label:job=node"},
				},
			},
			expectedKeys: []string{"192.168.1.10:9100:/metrics"},
			expectCount:  1,
		},
		{
			name: "IPv4 and IPv6 same service - prefer IPv4",
			entries: []*mdns.ServiceEntry{
				{
					Name:       "test",
					Port:       9100,
					AddrV4:     net.ParseIP("192.168.1.10"),
					AddrV6:     net.ParseIP("2001:db8::1"),
					InfoFields: []string{"path=/metrics"},
				},
			},
			expectedKeys: []string{"192.168.1.10:9100:/metrics"},
			expectCount:  1,
		},
		{
			name: "NO_SCRAPE entry ignored",
			entries: []*mdns.ServiceEntry{
				{
					Name:       "test",
					Port:       9100,
					AddrV4:     net.ParseIP("192.168.1.10"),
					InfoFields: []string{"NO_SCRAPE=true", "path=/metrics"},
				},
			},
			expectedKeys: []string{},
			expectCount:  0,
		},
		{
			name: "label merging for same endpoint",
			entries: []*mdns.ServiceEntry{
				{
					Name:       "test1",
					Port:       9100,
					AddrV4:     net.ParseIP("192.168.1.10"),
					InfoFields: []string{"path=/metrics", "label:env=staging"},
				},
				{
					Name:       "test2",
					Port:       9100,
					AddrV4:     net.ParseIP("192.168.1.10"),
					InfoFields: []string{"path=/metrics", "label:job=node", "label:env=prod"},
				},
			},
			expectedKeys: []string{"192.168.1.10:9100:/metrics"},
			expectCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(logger)

			for _, entry := range tt.entries {
				manager.AddEntry(entry)
			}

			count := manager.GetTargetCount()
			if count != tt.expectCount {
				t.Errorf("expected %d targets, got %d", tt.expectCount, count)
			}

			manager.mu.RLock()
			var keys []string
			for k := range manager.targets {
				keys = append(keys, k)
			}
			manager.mu.RUnlock()

			if len(tt.expectedKeys) == 0 && len(keys) == 0 {
				// Both empty, test passes
			} else if !reflect.DeepEqual(keys, tt.expectedKeys) {
				t.Errorf("expected keys %v, got %v", tt.expectedKeys, keys)
			}
		})
	}
}

func TestManager_GetPreferredIP(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	manager := NewManager(logger)

	tests := []struct {
		name     string
		entry    *mdns.ServiceEntry
		expected net.IP
	}{
		{
			name: "IPv4 only",
			entry: &mdns.ServiceEntry{
				AddrV4: net.ParseIP("192.168.1.10"),
			},
			expected: net.ParseIP("192.168.1.10"),
		},
		{
			name: "IPv6 only",
			entry: &mdns.ServiceEntry{
				AddrV6: net.ParseIP("2001:db8::1"),
			},
			expected: net.ParseIP("2001:db8::1"),
		},
		{
			name: "both IPv4 and IPv6 - prefer IPv4",
			entry: &mdns.ServiceEntry{
				AddrV4: net.ParseIP("192.168.1.10"),
				AddrV6: net.ParseIP("2001:db8::1"),
			},
			expected: net.ParseIP("192.168.1.10"),
		},
		{
			name:     "neither address",
			entry:    &mdns.ServiceEntry{},
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.getPreferredIP(tt.entry)
			if !result.Equal(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestManager_RemoveExpired(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	manager := NewManager(logger)

	// Add targets with different expiration times
	now := time.Now()
	manager.mu.Lock()
	manager.targets["expired1"] = &Target{
		Host:     "192.168.1.10",
		Port:     9100,
		ExpireAt: now.Add(-1 * time.Minute), // already expired
	}
	manager.targets["expired2"] = &Target{
		Host:     "192.168.1.11",
		Port:     9100,
		ExpireAt: now.Add(-30 * time.Second), // already expired
	}
	manager.targets["valid"] = &Target{
		Host:     "192.168.1.12",
		Port:     9100,
		ExpireAt: now.Add(5 * time.Minute), // still valid
	}
	manager.mu.Unlock()

	removed := manager.RemoveExpired()
	if removed != 2 {
		t.Errorf("expected 2 expired targets removed, got %d", removed)
	}

	count := manager.GetTargetCount()
	if count != 1 {
		t.Errorf("expected 1 remaining target, got %d", count)
	}
}

func TestManager_GetTargetGroups(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	manager := NewManager(logger)

	// Add some targets
	now := time.Now()
	manager.mu.Lock()
	manager.targets["192.168.1.10:9100:/metrics"] = &Target{
		Host:     "192.168.1.10",
		Port:     9100,
		Path:     "/metrics",
		Labels:   map[string]string{"job": "node", "env": "prod"},
		Meta:     map[string]string{"metrics_path": "/metrics", "zone": "us-east"},
		ExpireAt: now.Add(5 * time.Minute),
	}
	manager.targets["192.168.1.11:9200:/stats"] = &Target{
		Host:     "192.168.1.11",
		Port:     9200,
		Path:     "/stats",
		Labels:   map[string]string{"job": "elasticsearch"},
		Meta:     map[string]string{"metrics_path": "/stats"},
		ExpireAt: now.Add(5 * time.Minute),
	}
	manager.mu.Unlock()

	groups := manager.GetTargetGroups()

	if len(groups) != 2 {
		t.Fatalf("expected 2 target groups, got %d", len(groups))
	}

	// Verify first group
	group1 := groups[0]
	expectedTargets1 := []string{"192.168.1.10:9100"}
	if !reflect.DeepEqual(group1.Targets, expectedTargets1) {
		t.Errorf("group1 targets: expected %v, got %v", expectedTargets1, group1.Targets)
	}

	// Check that meta labels have __meta_ prefix
	if group1.Labels["__meta_metrics_path"] != "/metrics" {
		t.Errorf("expected __meta_metrics_path=/metrics, got %v", group1.Labels["__meta_metrics_path"])
	}
	if group1.Labels["job"] != "node" {
		t.Errorf("expected job=node, got %v", group1.Labels["job"])
	}
}

func TestTarget_Key(t *testing.T) {
	target := &Target{
		Host: "192.168.1.10",
		Port: 9100,
		Path: "/metrics",
	}

	expected := "192.168.1.10:9100:/metrics"
	if target.Key() != expected {
		t.Errorf("expected key %s, got %s", expected, target.Key())
	}
}
