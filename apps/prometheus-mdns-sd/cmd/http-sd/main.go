package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/R167/esphome-config/apps/prometheus-mdns-sd/internal/discovery"
	"github.com/R167/esphome-config/apps/prometheus-mdns-sd/internal/targets"
)

type Server struct {
	manager *targets.Manager
	client  *discovery.Client
	logger  *slog.Logger
	port    int
}

func main() {
	var (
		port  = flag.Int("port", 8080, "HTTP server port")
		debug = flag.Bool("debug", false, "Enable debug logging")
		addr  = flag.String("addr", "", "HTTP server address (empty for all interfaces)")
	)
	flag.Parse()

	// Set up logging
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	// Create target manager and discovery client
	manager := targets.NewManager(logger)
	client := discovery.NewClient(manager, logger)

	// Create and start server
	server := &Server{
		manager: manager,
		client:  client,
		logger:  logger,
		port:    *port,
	}

	if err := server.Run(*addr); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func (s *Server) Run(addr string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start mDNS discovery
	if err := s.client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start mDNS discovery: %w", err)
	}

	// Set up HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/targets", s.handleTargets)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/metrics", s.handleMetrics)

	// Create HTTP server
	listenAddr := fmt.Sprintf("%s:%d", addr, s.port)
	httpServer := &http.Server{
		Addr:    listenAddr,
		Handler: mux,
	}

	// Start HTTP server in background
	go func() {
		s.logger.Info("starting HTTP server", "addr", listenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server failed", "error", err)
			cancel()
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		s.logger.Info("context cancelled")
	case sig := <-sigChan:
		s.logger.Info("received signal", "signal", sig)
	}

	// Graceful shutdown
	s.logger.Info("shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		s.logger.Error("server shutdown failed", "error", err)
		return err
	}

	s.logger.Info("server stopped")
	return nil
}

// handleTargets serves the Prometheus HTTP service discovery endpoint
func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	targetGroups := s.manager.GetTargetGroups()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(targetGroups); err != nil {
		s.logger.Error("failed to encode target groups", "error", err)
		return
	}

	s.logger.Debug("served target groups", "count", len(targetGroups), "targets", s.manager.GetTargetCount())
}

// handleHealth provides a health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := map[string]interface{}{
		"status":  "healthy",
		"targets": s.manager.GetTargetCount(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(status)
}

// handleMetrics provides basic metrics about the service discovery
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	targets := s.manager.GetTargetCount()

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "# HELP prometheus_mdns_sd_targets_total Number of discovered targets\n")
	fmt.Fprintf(w, "# TYPE prometheus_mdns_sd_targets_total gauge\n")
	fmt.Fprintf(w, "prometheus_mdns_sd_targets_total %d\n", targets)
}
