package serviceregistry

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"
)

// UDPServer handles UDP registration requests
type UDPServer struct {
	registry *ConfigRegistry
	conn     *net.UDPConn
	logger   *slog.Logger
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewUDPServer creates a new UDP server for the given registry
func NewUDPServer(registry *ConfigRegistry) *UDPServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &UDPServer{
		registry: registry,
		logger:   registry.logger,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Listen starts listening for UDP registration packets
func (u *UDPServer) Listen(port int) error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP port %d: %w", port, err)
	}

	u.conn = conn
	u.logger.Info("UDP server listening", slog.Int("port", port))

	// Start handling packets
	go u.handlePackets()

	return nil
}

// Close closes the UDP connection
func (u *UDPServer) Close() error {
	u.cancel() // Cancel context to signal shutdown
	if u.conn != nil {
		return u.conn.Close()
	}
	return nil
}

// handlePackets processes incoming UDP registration packets
func (u *UDPServer) handlePackets() {
	buffer := make([]byte, 1024) // 1KB buffer should be enough for registration data

	for {
		select {
		case <-u.ctx.Done():
			return // Shutdown signal received
		default:
		}

		// Set read timeout to allow checking for shutdown
		u.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, clientAddr, err := u.conn.ReadFromUDP(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // Timeout, check for shutdown
			}
			if u.ctx.Err() != nil {
				return // Context cancelled, shutting down
			}
			u.logger.Error("error reading UDP packet", slog.String("error", err.Error()))
			continue
		}

		packet := string(buffer[:n])
		u.logger.Debug("received UDP packet",
			slog.String("from", clientAddr.String()),
			slog.String("data", packet))

		endpoint, err := parseUDPPacket(packet, clientAddr)
		if err != nil {
			u.logger.Error("failed to parse UDP packet",
				slog.String("from", clientAddr.String()),
				slog.String("packet", packet),
				slog.String("error", err.Error()))
			continue
		}

		// Register the endpoint
		u.registry.Register(endpoint)
	}
}

// parseUDPPacket parses a UDP registration packet
// Expected format: "REGISTER|host|name|label1:value1,label2:value2"
// Example: "REGISTER|192.168.1.100:80|kitchen-sensor|type:temperature,location:kitchen"
func parseUDPPacket(packet string, clientAddr *net.UDPAddr) (endpoint, error) {
	parts := strings.Split(packet, "|")
	if len(parts) < 3 {
		return endpoint{}, fmt.Errorf("invalid packet format, expected at least 3 parts separated by |")
	}

	if parts[0] != "REGISTER" {
		return endpoint{}, fmt.Errorf("invalid packet type, expected REGISTER, got %s", parts[0])
	}

	host := parts[1]
	name := parts[2]

	// If host is empty, use client address
	if host == "" {
		host = clientAddr.IP.String()
	}

	e := endpoint{
		host:     host,
		name:     name,
		labels:   map[string]string{},
		lastSeen: time.Now(),
	}

	// Use host as name if name is empty
	if e.name == "" {
		e.name = e.host
	}

	// Add device name and host as labels for better Prometheus identification
	if e.name != "" && e.name != e.host {
		e.labels["device_name"] = e.name
	}
	if e.host != "" {
		e.labels["instance"] = e.host
	}

	// Parse labels if provided
	if len(parts) > 3 && parts[3] != "" {
		labels := strings.Split(parts[3], ",")
		for _, labelPair := range labels {
			kv := strings.SplitN(labelPair, ":", 2)
			if len(kv) != 2 || len(kv[0]) == 0 {
				return endpoint{}, fmt.Errorf("invalid label format %q", labelPair)
			}

			key := kv[0]
			value := kv[1]

			if len(value) == 0 {
				continue
			}

			// Apply same label sanitization as HTTP endpoint
			if !startMatcher.MatchString(key) {
				key = "_" + key[1:]
			}
			key = illegalMatcher.ReplaceAllString(key, "_")

			e.labels[key] = value
		}
	}

	return e, nil
}
