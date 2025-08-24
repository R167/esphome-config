package serviceregistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	startMatcher   = regexp.MustCompile(`^[a-zA-Z_]`)
	illegalMatcher = regexp.MustCompile(`[^a-zA-Z0-9_]`)
)

type httpError int

func (e httpError) Code() int {
	if e < 100 || e > 599 {
		return http.StatusInternalServerError
	}
	return int(e)
}

func (e httpError) Error() string {
	msg := http.StatusText(int(e))
	if msg == "" {
		return fmt.Sprintf("unknown status code %d", e)
	}
	return msg
}

func (r *ConfigRegistry) registerHandler(w http.ResponseWriter, req *http.Request) {
	e, err := parseEndpoint(req.URL.Query())
	if err != nil {
		jsonError(w, err)
		return
	}

	r.Register(e)

	// Send a response back to the client
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"success":true}`)); err != nil {
		// Connection likely closed, log at debug level
		slog.Debug("failed to write response", slog.String("error", err.Error()))
	}
}

func parseEndpoint(q url.Values) (endpoint, error) {
	e := endpoint{
		host:     q.Get("host"),
		name:     q.Get("name"),
		labels:   map[string]string{},
		lastSeen: time.Now(),
	}

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

	for _, v := range q["label"] {
		kv := strings.SplitN(v, ":", 2)
		if len(kv) != 2 || len(kv[0]) == 0 {
			return endpoint{}, fmt.Errorf("%w: invalid label %q", httpError(http.StatusBadRequest), v)
		}
		name := kv[0]
		value := kv[1]
		if len(value) == 0 {
			continue
		}
		if !startMatcher.MatchString(name) {
			// If the label doesn't start with a letter or underscore, replace it with an underscore.
			// This must be handled separately from the illegalMatcher, because the first character
			// must be a letter or underscore, but subsequent characters can be numbers.
			name = "_" + name[1:]
		}
		name = illegalMatcher.ReplaceAllString(name, "_")
		e.labels[name] = value
	}
	return e, nil
}

func (r *ConfigRegistry) Mux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /register", r.registerHandler)
	mux.HandleFunc("GET /config", renderJSON(r.Config))
	mux.HandleFunc("GET /health", r.healthHandler)
	mux.HandleFunc("GET /metrics", renderJSON(r.Metrics))
	return mux
}

func renderJSON[T any](f func() T) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp, err := json.Marshal(f())
		if err != nil {
			jsonError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(resp); err != nil {
			// Connection likely closed, log at debug level
			slog.Debug("failed to write JSON response", slog.String("error", err.Error()))
		}
	}
}

func (r *ConfigRegistry) healthHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	r.m.RLock()
	registrySize := len(r.Registry)
	r.m.RUnlock()
	
	if _, err := fmt.Fprintf(w, `{"status":"healthy","registry_size":%d}`, registrySize); err != nil {
		slog.Debug("failed to write health response", slog.String("error", err.Error()))
	}
}

func jsonError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	var httpErr httpError
	errors.As(err, &httpErr)
	w.WriteHeader(httpErr.Code())
	if _, writeErr := fmt.Fprintf(w, `{"success":false,"error":"%q"}`, err); writeErr != nil {
		slog.Debug("failed to write error response", slog.String("error", writeErr.Error()))
	}
}
