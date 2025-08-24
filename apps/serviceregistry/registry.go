package serviceregistry

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

func NewConfigRegistry(ttl time.Duration) *ConfigRegistry {
	return &ConfigRegistry{
		Ttl:    ttl,
		logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
}

func NewConfigRegistryWithPersistence(ttl time.Duration, persistenceFile string) *ConfigRegistry {
	r := &ConfigRegistry{
		Ttl:             ttl,
		logger:          slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		persistenceFile: persistenceFile,
	}
	r.loadFromFile()
	return r
}

type ConfigRegistry struct {
	m sync.RWMutex

	Ttl             time.Duration
	Registry        map[string]Endpoint
	logger          *slog.Logger
	persistenceFile string
}

type Target struct {
	Targets []string          `json:"targets"`
	Labels  map[string]string `json:"labels,omitempty"`
}

type Endpoint interface {
	// Name returns the name of the endpoint.
	Name() string
	// Target returns the target configuration for the endpoint.
	Target() Target
	// LastUpdated returns the last time the endpoint was seen.
	// If this returns the zero value, the endpoint is considered permanently valid.
	LastUpdated() time.Time
}

type endpoint struct {
	host     string            `json:"host"`
	name     string            `json:"name"`
	lastSeen time.Time         `json:"last_seen"`
	labels   map[string]string `json:"labels"`
}

func (e endpoint) Target() Target {
	return Target{
		Targets: []string{e.host},
		Labels:  e.labels,
	}
}

func (e endpoint) Name() string {
	return e.name
}

func (e endpoint) LastUpdated() time.Time {
	return e.lastSeen
}

// Register adds an entry to the registry.
func (r *ConfigRegistry) Register(e Endpoint) {
	r.m.Lock()
	defer r.m.Unlock()

	if r.Registry == nil {
		r.Registry = make(map[string]Endpoint)
	}
	
	_, wasRegistered := r.Registry[e.Name()]
	r.Registry[e.Name()] = e
	
	if wasRegistered {
		r.logger.Info("endpoint re-registered", 
			slog.String("name", e.Name()),
			slog.String("target", e.Target().Targets[0]),
			slog.Any("labels", e.Target().Labels),
			slog.Time("last_update", e.LastUpdated()))
	} else {
		r.logger.Info("endpoint registered", 
			slog.String("name", e.Name()),
			slog.String("target", e.Target().Targets[0]),
			slog.Any("labels", e.Target().Labels),
			slog.Time("last_update", e.LastUpdated()))
	}
	
	// Save to file after registration
	go r.saveToFile()
}

// Deregister removes an entry from the registry.
func (r *ConfigRegistry) Deregister(e Endpoint) {
	r.m.Lock()
	defer r.m.Unlock()

	delete(r.Registry, e.Name())
}

// Cleaner runs a background process that cleans up stale entries.
// This is a blocking call and should be run as a goroutine. To stop the cleaner,
// cancel the context.
func (r *ConfigRegistry) Cleaner(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.ExpireEntries()
		}
	}
}

// ExpireEntries removes entries that have not been seen within the TTL window.
func (r *ConfigRegistry) ExpireEntries() {
	r.m.Lock()
	defer r.m.Unlock()
	
	expired := []string{}
	for k, e := range r.Registry {
		if !e.LastUpdated().IsZero() && time.Since(e.LastUpdated()) > r.Ttl {
			expired = append(expired, k)
			delete(r.Registry, k)
		}
	}
	
	if len(expired) > 0 {
		r.logger.Info("expired endpoints", 
			slog.Int("count", len(expired)),
			slog.Any("endpoints", expired),
			slog.Duration("ttl", r.Ttl))
	}
}

// Config returns the current configuration for all registered endpoints.
func (r *ConfigRegistry) Config() []Target {
	r.m.RLock()
	defer r.m.RUnlock()

	cfgs := []Target{}
	for _, e := range r.Registry {
		if !e.LastUpdated().IsZero() && time.Since(e.LastUpdated()) > r.Ttl {
			continue
		}
		cfgs = append(cfgs, e.Target())
	}
	return cfgs
}

// loadFromFile loads the registry state from the persistence file
func (r *ConfigRegistry) loadFromFile() {
	if r.persistenceFile == "" {
		return
	}

	data, err := os.ReadFile(r.persistenceFile)
	if err != nil {
		if !os.IsNotExist(err) {
			r.logger.Error("failed to read persistence file", slog.String("file", r.persistenceFile), slog.String("error", err.Error()))
		}
		return
	}

	var endpoints map[string]endpoint
	if err := json.Unmarshal(data, &endpoints); err != nil {
		r.logger.Error("failed to unmarshal persistence file", slog.String("file", r.persistenceFile), slog.String("error", err.Error()))
		return
	}

	r.Registry = make(map[string]Endpoint)
	for k, e := range endpoints {
		r.Registry[k] = e
	}
	r.logger.Info("loaded registry from file", slog.String("file", r.persistenceFile), slog.Int("count", len(endpoints)))
}

// saveToFile saves the current registry state to the persistence file
func (r *ConfigRegistry) saveToFile() {
	if r.persistenceFile == "" {
		return
	}

	r.m.RLock()
	endpoints := make(map[string]endpoint)
	for k, e := range r.Registry {
		if ep, ok := e.(endpoint); ok {
			endpoints[k] = ep
		}
	}
	r.m.RUnlock()

	data, err := json.MarshalIndent(endpoints, "", "  ")
	if err != nil {
		r.logger.Error("failed to marshal registry", slog.String("error", err.Error()))
		return
	}

	if err := os.WriteFile(r.persistenceFile, data, 0644); err != nil {
		r.logger.Error("failed to write persistence file", slog.String("file", r.persistenceFile), slog.String("error", err.Error()))
		return
	}

	r.logger.Debug("saved registry to file", slog.String("file", r.persistenceFile), slog.Int("count", len(endpoints)))
}
