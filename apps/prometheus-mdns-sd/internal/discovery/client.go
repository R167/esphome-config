package discovery

import (
	"context"
	"log"
	"log/slog"
	"strings"
	"time"

	"github.com/R167/esphome-config/apps/prometheus-mdns-sd/internal/targets"
	"github.com/hashicorp/mdns"
)

const (
	// ServiceType is the mDNS service type we're looking for
	ServiceType = "_prometheus-http._tcp"

	// DefaultCleanupInterval is how often we check for expired targets
	DefaultCleanupInterval = 30 * time.Second
)

// Client handles mDNS service discovery for Prometheus targets
type Client struct {
	manager         *targets.Manager
	logger          *slog.Logger
	cleanupInterval time.Duration
	mdnsLogger      *log.Logger
}

// NewClient creates a new mDNS discovery client
func NewClient(manager *targets.Manager, logger *slog.Logger) *Client {
	// Create a log.Logger that writes to our slog.Logger
	mdnsWriter := &slogWriter{logger: logger}
	mdnsLogger := log.New(mdnsWriter, "", 0) // No prefix/timestamp since slog handles it

	return &Client{
		manager:         manager,
		logger:          logger,
		cleanupInterval: DefaultCleanupInterval,
		mdnsLogger:      mdnsLogger,
	}
}

// SetCleanupInterval sets how often expired targets are cleaned up
func (c *Client) SetCleanupInterval(interval time.Duration) {
	c.cleanupInterval = interval
}

// slogWriter wraps slog.Logger to implement io.Writer for log.Logger
type slogWriter struct {
	logger *slog.Logger
}

func (w *slogWriter) Write(p []byte) (n int, err error) {
	msg := strings.TrimSpace(string(p))
	if msg == "" {
		return len(p), nil
	}

	// Log mdns messages at debug level with structured logging
	w.logger.Debug("mdns", "message", msg)
	return len(p), nil
}

// Start begins continuous mDNS discovery.
// It performs an initial lookup and then starts a background goroutine
// to handle ongoing announcements and cleanup expired targets.
func (c *Client) Start(ctx context.Context) error {
	c.logger.Info("starting mDNS discovery", "service_type", ServiceType)

	// Perform initial discovery
	if err := c.performLookup(ctx); err != nil {
		c.logger.Error("initial mDNS lookup failed", "error", err)
		return err
	}

	// Start background tasks
	go c.continuousDiscovery(ctx)
	go c.cleanupWorker(ctx)

	return nil
}

// performLookup does a one-time mDNS lookup for the service type
func (c *Client) performLookup(ctx context.Context) error {
	entriesCh := make(chan *mdns.ServiceEntry, 10)

	// Process entries in background
	go func() {
		for entry := range entriesCh {
			c.logger.Debug("discovered service entry",
				"name", entry.Name,
				"host", entry.Host,
				"port", entry.Port,
				"addr_v4", entry.AddrV4,
				"addr_v6", entry.AddrV6,
				"info", entry.Info,
				"txt", entry.Info)

			// Only process entries that match our target service type
			if !strings.Contains(entry.Name, ServiceType) {
				c.logger.Debug("skipping non-prometheus service", "name", entry.Name, "expected_service", ServiceType)
				continue
			}

			c.manager.AddEntry(entry)
		}
	}()

	// Perform lookup
	params := &mdns.QueryParam{
		Service: ServiceType,
		Domain:  "local",
		Timeout: 3 * time.Second,
		Entries: entriesCh,
		Logger:  c.mdnsLogger,
	}

	err := mdns.Query(params)
	close(entriesCh)

	if err != nil {
		return err
	}

	c.logger.Info("completed initial mDNS lookup", "targets", c.manager.GetTargetCount())
	return nil
}

// continuousDiscovery handles ongoing mDNS announcements.
// It periodically performs lookups to catch new services and updates.
func (c *Client) continuousDiscovery(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second) // Lookup every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("stopping continuous mDNS discovery")
			return
		case <-ticker.C:
			if err := c.performLookup(ctx); err != nil {
				c.logger.Error("periodic mDNS lookup failed", "error", err)
			}
		}
	}
}

// cleanupWorker periodically removes expired targets based on their TTL
func (c *Client) cleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("stopping cleanup worker")
			return
		case <-ticker.C:
			removed := c.manager.RemoveExpired()
			if removed > 0 {
				c.logger.Debug("cleaned up expired targets", "removed", removed, "remaining", c.manager.GetTargetCount())
			}
		}
	}
}

// GetTargetCount returns the current number of discovered targets
func (c *Client) GetTargetCount() int {
	return c.manager.GetTargetCount()
}
