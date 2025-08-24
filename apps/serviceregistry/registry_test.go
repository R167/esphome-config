package serviceregistry

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestNewConfigRegistry(t *testing.T) {
	ttl := 5 * time.Minute
	registry := NewConfigRegistry(ttl)

	if registry.Ttl != ttl {
		t.Errorf("expected TTL %v, got %v", ttl, registry.Ttl)
	}

	if registry.logger == nil {
		t.Error("expected logger to be initialized")
	}

	if registry.Registry != nil {
		t.Error("expected Registry to be nil initially")
	}
}

func TestEndpointInterface(t *testing.T) {
	testCases := []struct {
		name       string
		endpoint   endpoint
		wantName   string
		wantHost   string
		wantLabels map[string]string
	}{
		{
			name: "basic endpoint",
			endpoint: endpoint{
				host:     "192.168.1.100:80",
				name:     "test-device",
				labels:   map[string]string{"type": "sensor"},
				lastSeen: time.Now(),
			},
			wantName:   "test-device",
			wantHost:   "192.168.1.100:80",
			wantLabels: map[string]string{"type": "sensor"},
		},
		{
			name: "endpoint with empty name defaults to host",
			endpoint: endpoint{
				host:     "192.168.1.101:80",
				name:     "",
				labels:   map[string]string{},
				lastSeen: time.Now(),
			},
			wantName:   "",
			wantHost:   "192.168.1.101:80",
			wantLabels: map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.endpoint.Name(); got != tc.wantName {
				t.Errorf("Name() = %v, want %v", got, tc.wantName)
			}

			target := tc.endpoint.Target()
			if len(target.Targets) != 1 || target.Targets[0] != tc.wantHost {
				t.Errorf("Target().Targets = %v, want [%v]", target.Targets, tc.wantHost)
			}

			if !reflect.DeepEqual(target.Labels, tc.wantLabels) {
				t.Errorf("Target().Labels = %v, want %v", target.Labels, tc.wantLabels)
			}

			if tc.endpoint.LastUpdated().IsZero() {
				t.Error("LastUpdated() should not be zero")
			}
		})
	}
}

func TestRegister(t *testing.T) {
	registry := NewConfigRegistry(5 * time.Minute)

	ep := endpoint{
		host:     "192.168.1.100:80",
		name:     "test-device",
		labels:   map[string]string{"type": "sensor"},
		lastSeen: time.Now(),
	}

	// First registration
	registry.Register(ep)

	if len(registry.Registry) != 1 {
		t.Errorf("expected 1 endpoint, got %d", len(registry.Registry))
	}

	if _, exists := registry.Registry[ep.Name()]; !exists {
		t.Error("endpoint should be registered")
	}

	// Re-registration should update existing
	ep.lastSeen = time.Now().Add(1 * time.Minute)
	registry.Register(ep)

	if len(registry.Registry) != 1 {
		t.Errorf("expected 1 endpoint after re-registration, got %d", len(registry.Registry))
	}
}

func TestExpireEntries(t *testing.T) {
	registry := NewConfigRegistry(1 * time.Minute)

	// Add fresh endpoint
	freshEP := endpoint{
		host:     "192.168.1.100:80",
		name:     "fresh-device",
		labels:   map[string]string{"type": "sensor"},
		lastSeen: time.Now(),
	}
	registry.Register(freshEP)

	// Add expired endpoint
	expiredEP := endpoint{
		host:     "192.168.1.101:80",
		name:     "expired-device",
		labels:   map[string]string{"type": "sensor"},
		lastSeen: time.Now().Add(-2 * time.Minute), // 2 minutes ago, beyond TTL
	}
	registry.Register(expiredEP)

	// Manually set the expired endpoint to simulate time passage
	registry.Registry[expiredEP.Name()] = expiredEP

	if len(registry.Registry) != 2 {
		t.Errorf("expected 2 endpoints before expiration, got %d", len(registry.Registry))
	}

	// Expire entries
	registry.ExpireEntries()

	if len(registry.Registry) != 1 {
		t.Errorf("expected 1 endpoint after expiration, got %d", len(registry.Registry))
	}

	if _, exists := registry.Registry[freshEP.Name()]; !exists {
		t.Error("fresh endpoint should still exist")
	}

	if _, exists := registry.Registry[expiredEP.Name()]; exists {
		t.Error("expired endpoint should be removed")
	}
}

func TestConfig(t *testing.T) {
	registry := NewConfigRegistry(5 * time.Minute)

	// Add some endpoints
	endpoints := []endpoint{
		{
			host:     "192.168.1.100:80",
			name:     "device1",
			labels:   map[string]string{"type": "sensor", "location": "kitchen"},
			lastSeen: time.Now(),
		},
		{
			host:     "192.168.1.101:80",
			name:     "device2",
			labels:   map[string]string{"type": "switch", "location": "bedroom"},
			lastSeen: time.Now(),
		},
	}

	for _, ep := range endpoints {
		registry.Register(ep)
	}

	config := registry.Config()

	if len(config) != 2 {
		t.Errorf("expected 2 targets in config, got %d", len(config))
	}

	// Verify config structure
	for i, target := range config {
		if len(target.Targets) != 1 {
			t.Errorf("target %d should have 1 target, got %d", i, len(target.Targets))
		}

		expectedHost := endpoints[i].host
		if target.Targets[0] != expectedHost {
			t.Errorf("target %d should have host %s, got %s", i, expectedHost, target.Targets[0])
		}

		if target.Labels == nil {
			t.Errorf("target %d should have labels", i)
		}
	}
}

func TestPersistence(t *testing.T) {
	// Create a temporary file for persistence
	tmpDir := t.TempDir()
	persistenceFile := filepath.Join(tmpDir, "registry.json")

	// Create registry with persistence
	registry := NewConfigRegistryWithPersistence(5*time.Minute, persistenceFile)

	// Add some endpoints
	ep1 := endpoint{
		host:     "192.168.1.100:80",
		name:     "device1",
		labels:   map[string]string{"type": "sensor"},
		lastSeen: time.Now(),
	}
	ep2 := endpoint{
		host:     "192.168.1.101:80",
		name:     "device2",
		labels:   map[string]string{"type": "switch"},
		lastSeen: time.Now(),
	}

	registry.Register(ep1)
	registry.Register(ep2)

	// Wait a bit for async save to complete
	time.Sleep(100 * time.Millisecond)

	// Check that file was created
	if _, err := os.Stat(persistenceFile); os.IsNotExist(err) {
		t.Error("persistence file should be created")
	}

	// Create new registry and load from file
	newRegistry := NewConfigRegistryWithPersistence(5*time.Minute, persistenceFile)

	if len(newRegistry.Registry) != 2 {
		t.Errorf("expected 2 endpoints loaded from file, got %d", len(newRegistry.Registry))
	}

	if _, exists := newRegistry.Registry["device1"]; !exists {
		t.Error("device1 should be loaded from file")
	}

	if _, exists := newRegistry.Registry["device2"]; !exists {
		t.Error("device2 should be loaded from file")
	}
}

func TestPersistenceNoFile(t *testing.T) {
	// Test registry without persistence file
	registry := NewConfigRegistry(5 * time.Minute)

	// Should not panic when trying to save
	registry.saveToFile()

	// Should not panic when trying to load
	registry.loadFromFile()
}

func TestDeregister(t *testing.T) {
	registry := NewConfigRegistry(5 * time.Minute)

	ep := endpoint{
		host:     "192.168.1.100:80",
		name:     "test-device",
		labels:   map[string]string{"type": "sensor"},
		lastSeen: time.Now(),
	}

	// Register first
	registry.Register(ep)

	if len(registry.Registry) != 1 {
		t.Errorf("expected 1 endpoint after registration, got %d", len(registry.Registry))
	}

	// Deregister
	registry.Deregister(ep)

	if len(registry.Registry) != 0 {
		t.Errorf("expected 0 endpoints after deregistration, got %d", len(registry.Registry))
	}
}
