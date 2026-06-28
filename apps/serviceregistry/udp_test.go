package serviceregistry

import (
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestParseUDPPacket(t *testing.T) {
	clientAddr := &net.UDPAddr{IP: net.ParseIP("192.168.1.50"), Port: 12345}

	testCases := []struct {
		name        string
		packet      string
		want        endpoint
		wantError   bool
		errorString string
	}{
		{
			name:   "basic registration",
			packet: "REGISTER|192.168.1.100:80|kitchen-sensor|type:temperature,location:kitchen",
			want: endpoint{
				host: "192.168.1.100:80",
				name: "kitchen-sensor",
				labels: map[string]string{
					"device_name": "kitchen-sensor",
					"instance":    "192.168.1.100:80",
					"type":        "temperature",
					"location":    "kitchen",
				},
			},
		},
		{
			name:   "registration without labels",
			packet: "REGISTER|192.168.1.101:80|bedroom-switch|",
			want: endpoint{
				host: "192.168.1.101:80",
				name: "bedroom-switch",
				labels: map[string]string{
					"device_name": "bedroom-switch",
					"instance":    "192.168.1.101:80",
				},
			},
		},
		{
			name:   "registration with empty host uses client IP",
			packet: "REGISTER||outdoor-sensor|type:humidity",
			want: endpoint{
				host: "192.168.1.50",
				name: "outdoor-sensor",
				labels: map[string]string{
					"device_name": "outdoor-sensor",
					"instance":    "192.168.1.50",
					"type":        "humidity",
				},
			},
		},
		{
			name:   "registration with empty name uses host",
			packet: "REGISTER|192.168.1.102:80||type:switch",
			want: endpoint{
				host: "192.168.1.102:80",
				name: "192.168.1.102:80",
				labels: map[string]string{
					"instance": "192.168.1.102:80",
					"type":     "switch",
				},
			},
		},
		{
			name:   "registration with label sanitization",
			packet: "REGISTER|192.168.1.103:80|test-device|1invalid:value,valid_label:test",
			want: endpoint{
				host: "192.168.1.103:80",
				name: "test-device",
				labels: map[string]string{
					"device_name": "test-device",
					"instance":    "192.168.1.103:80",
					"_invalid":    "value",
					"valid_label": "test",
				},
			},
		},
		{
			name:        "invalid packet format - too few parts",
			packet:      "REGISTER|192.168.1.100",
			wantError:   true,
			errorString: "invalid packet format",
		},
		{
			name:        "invalid packet type",
			packet:      "INVALID|192.168.1.100:80|test|",
			wantError:   true,
			errorString: "invalid packet type",
		},
		{
			name:        "invalid label format",
			packet:      "REGISTER|192.168.1.100:80|test|invalidlabel",
			wantError:   true,
			errorString: "invalid label format",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseUDPPacket(tc.packet, clientAddr)

			if tc.wantError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tc.errorString)
					return
				}
				if tc.errorString != "" && !contains(err.Error(), tc.errorString) {
					t.Errorf("expected error containing %q, got %q", tc.errorString, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Compare fields (ignoring lastSeen which will be different)
			if got.host != tc.want.host {
				t.Errorf("host = %v, want %v", got.host, tc.want.host)
			}
			if got.name != tc.want.name {
				t.Errorf("name = %v, want %v", got.name, tc.want.name)
			}
			if !reflect.DeepEqual(got.labels, tc.want.labels) {
				t.Errorf("labels = %v, want %v", got.labels, tc.want.labels)
			}
			if got.lastSeen.IsZero() {
				t.Error("lastSeen should not be zero")
			}
		})
	}
}

func TestUDPServerLifecycle(t *testing.T) {
	registry := NewConfigRegistry(5 * time.Minute)
	server := NewUDPServer(registry)

	// Start server on a random available port
	err := server.Listen(0) // 0 means OS will choose available port
	if err != nil {
		t.Fatalf("failed to start UDP server: %v", err)
	}
	defer server.Close()

	// Give server a moment to start
	time.Sleep(10 * time.Millisecond)

	// Get the actual port the server is listening on
	addr := server.conn.LocalAddr().(*net.UDPAddr)
	port := addr.Port

	// Test sending a registration packet
	serverAddr := fmt.Sprintf("localhost:%d", port)
	clientConn, err := net.Dial("udp", serverAddr)
	if err != nil {
		t.Fatalf("failed to connect to UDP server at %s: %v", serverAddr, err)
	}
	defer clientConn.Close()

	packet := "REGISTER|192.168.1.200:80|udp-test-device|type:test,protocol:udp"
	_, err = clientConn.Write([]byte(packet))
	if err != nil {
		t.Fatalf("failed to send UDP packet: %v", err)
	}

	// Give server time to process the packet
	time.Sleep(50 * time.Millisecond)

	// Check that the endpoint was registered
	config := registry.Config()
	if len(config) != 1 {
		t.Errorf("expected 1 registered endpoint, got %d", len(config))
		return
	}

	endpoint, exists := registry.GetEndpoint("udp-test-device")
	if !exists {
		t.Error("endpoint should be registered with name 'udp-test-device'")
		return
	}

	target := endpoint.Target()
	if target.Targets[0] != "192.168.1.200:80" {
		t.Errorf("expected target 192.168.1.200:80, got %s", target.Targets[0])
	}

	expectedLabels := map[string]string{
		"device_name": "udp-test-device",
		"instance":    "192.168.1.200:80",
		"type":        "test",
		"protocol":    "udp",
	}
	if !reflect.DeepEqual(target.Labels, expectedLabels) {
		t.Errorf("expected labels %v, got %v", expectedLabels, target.Labels)
	}
}

func TestNewUDPServer(t *testing.T) {
	registry := NewConfigRegistry(5 * time.Minute)
	server := NewUDPServer(registry)

	if server.registry != registry {
		t.Error("server should reference the provided registry")
	}

	if server.logger != registry.logger {
		t.Error("server should use the registry's logger")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 1; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
