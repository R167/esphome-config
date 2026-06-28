package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/R167/esphome-config/apps/serviceregistry"
)

type Config struct {
	HTTPPort        int
	UDPPort         int
	TTL             time.Duration
	PersistenceFile string
	EnableUDP       bool
}

func main() {
	var config Config

	// Parse command line flags
	flag.IntVar(&config.HTTPPort, "http-port", 8080, "HTTP server port")
	flag.IntVar(&config.UDPPort, "udp-port", 8081, "UDP server port")
	flag.DurationVar(&config.TTL, "ttl", 10*time.Minute, "Time-to-live for registered endpoints")
	flag.StringVar(&config.PersistenceFile, "persistence", "", "File path for registry persistence (optional)")
	flag.BoolVar(&config.EnableUDP, "enable-udp", false, "Enable UDP registration server")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Create registry
	var registry *serviceregistry.ConfigRegistry
	if config.PersistenceFile != "" {
		registry = serviceregistry.NewConfigRegistryWithPersistence(config.TTL, config.PersistenceFile)
		logger.Info("registry created with persistence",
			slog.String("file", config.PersistenceFile),
			slog.Duration("ttl", config.TTL))
	} else {
		registry = serviceregistry.NewConfigRegistry(config.TTL)
		logger.Info("registry created", slog.Duration("ttl", config.TTL))
	}

	// Start cleanup goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go registry.Cleaner(ctx)

	// Start UDP server if enabled
	var udpServer *serviceregistry.UDPServer
	if config.EnableUDP {
		udpServer = serviceregistry.NewUDPServer(registry)
		if err := udpServer.Listen(config.UDPPort); err != nil {
			logger.Error("failed to start UDP server", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer udpServer.Close()
		logger.Info("UDP registration enabled", slog.Int("port", config.UDPPort))
	}

	// Create HTTP server
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.HTTPPort),
		Handler: registry.Mux(),
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("shutting down server")
		cancel() // Cancel cleanup goroutine

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("server shutdown error", slog.String("error", err.Error()))
		}
	}()

	logger.Info("HTTP server starting", slog.Int("port", config.HTTPPort))
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		logger.Error("HTTP server error", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("server stopped")
}
