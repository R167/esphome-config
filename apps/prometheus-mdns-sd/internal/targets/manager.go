package targets

import (
	"fmt"
	"log/slog"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/R167/esphome-config/apps/prometheus-mdns-sd/internal/txt"
	"github.com/R167/esphome-config/apps/prometheus-mdns-sd/pkg/prometheus"
	"github.com/hashicorp/mdns"
)

// Target represents a discovered Prometheus target with metadata
type Target struct {
	Host     string            // IP address (IPv4 preferred over IPv6)
	Port     int               // port number
	Path     string            // metrics path (from TXT path= field)
	Labels   map[string]string // user labels (from TXT label:KEY=VALUE)
	Meta     map[string]string // meta labels (from TXT meta:KEY=VALUE, become __meta_KEY)
	ExpireAt time.Time         // when this target should be removed (based on TTL)
}

// Key returns a unique key for this target used for deduplication.
// Format: "host:port:path"
func (t *Target) Key() string {
	return fmt.Sprintf("%s:%d:%s", t.Host, t.Port, t.Path)
}

// Manager handles target discovery, deduplication, and TTL management
type Manager struct {
	mu      sync.RWMutex
	targets map[string]*Target // keyed by Target.Key()
	logger  *slog.Logger
}

// NewManager creates a new target manager
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		targets: make(map[string]*Target),
		logger:  logger,
	}
}

// AddEntry processes an mDNS service entry and updates targets.
// It handles IPv4/IPv6 deduplication (preferring IPv4) and label merging.
func (m *Manager) AddEntry(entry *mdns.ServiceEntry) {
	if entry == nil {
		return
	}

	// Parse TXT records
	parsed := txt.Parse(entry.InfoFields)
	parsed.LogSkipped(m.logger, entry.Name)

	// Skip if NO_SCRAPE is set
	if parsed.NoScrape {
		m.logger.Debug("skipping target with NO_SCRAPE=true", "service", entry.Name)
		return
	}

	// Determine preferred IP (IPv4 over IPv6)
	ip := m.getPreferredIP(entry)
	if ip == nil {
		m.logger.Debug("no valid IP address found", "service", entry.Name)
		return
	}

	// Create target (use default TTL of 5 minutes since mdns doesn't expose TTL)
	target := &Target{
		Host:     ip.String(),
		Port:     entry.Port,
		Path:     parsed.Path,
		Labels:   make(map[string]string),
		Meta:     make(map[string]string),
		ExpireAt: time.Now().Add(5 * time.Minute),
	}

	// Copy labels (deep copy to avoid mutation)
	for k, v := range parsed.Labels {
		target.Labels[k] = v
	}
	for k, v := range parsed.Meta {
		target.Meta[k] = v
	}

	// Add metrics path as meta label if present
	if target.Path != "" {
		target.Meta["metrics_path"] = target.Path
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := target.Key()
	existing, exists := m.targets[key]

	if exists {
		// Merge labels (last-write-wins)
		for k, v := range target.Labels {
			existing.Labels[k] = v
		}
		for k, v := range target.Meta {
			existing.Meta[k] = v
		}
		// Update TTL to the later expiration
		if target.ExpireAt.After(existing.ExpireAt) {
			existing.ExpireAt = target.ExpireAt
		}
		m.logger.Debug("merged labels for existing target", "key", key)
	} else {
		m.targets[key] = target
		m.logger.Debug("added new target", "key", key, "host", target.Host, "port", target.Port)
	}
}

// getPreferredIP selects IPv4 over IPv6 if both are available
func (m *Manager) getPreferredIP(entry *mdns.ServiceEntry) net.IP {
	var ipv4, ipv6 net.IP

	// Check the primary IP
	if entry.AddrV4 != nil {
		ipv4 = entry.AddrV4
	}
	if entry.AddrV6 != nil {
		ipv6 = entry.AddrV6
	}

	// Prefer IPv4 if available
	if ipv4 != nil {
		return ipv4
	}
	return ipv6
}

// RemoveExpired removes targets that have exceeded their TTL
func (m *Manager) RemoveExpired() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	removed := 0

	for key, target := range m.targets {
		if now.After(target.ExpireAt) {
			delete(m.targets, key)
			removed++
			m.logger.Debug("removed expired target", "key", key)
		}
	}

	return removed
}

// GetTargetGroups returns current targets as Prometheus target groups.
// Each unique (host, port) combination becomes a separate target group.
func (m *Manager) GetTargetGroups() prometheus.TargetGroupList {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.targets) == 0 {
		return prometheus.TargetGroupList{}
	}

	// Group targets by (host:port) since Prometheus expects one target group per unique endpoint
	groups := make(map[string]*prometheus.TargetGroup)

	for _, target := range m.targets {
		hostPort := fmt.Sprintf("%s:%d", target.Host, target.Port)

		group, exists := groups[hostPort]
		if !exists {
			group = &prometheus.TargetGroup{
				Targets: []string{hostPort},
				Labels:  make(map[string]string),
			}
			groups[hostPort] = group
		}

		// Merge all labels and meta labels
		for k, v := range target.Labels {
			group.Labels[k] = v
		}
		for k, v := range target.Meta {
			// Meta labels get __meta_ prefix
			group.Labels["__meta_"+k] = v
		}
	}

	// Convert to sorted slice for consistent output
	var result prometheus.TargetGroupList
	var keys []string
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		result = append(result, *groups[key])
	}

	return result
}

// GetTargetCount returns the current number of active targets
func (m *Manager) GetTargetCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.targets)
}
