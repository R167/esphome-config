package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/R167/esphome-config/apps/prometheus-mdns-sd/internal/discovery"
	"github.com/R167/esphome-config/apps/prometheus-mdns-sd/internal/targets"
)

type FileWriter struct {
	manager    *targets.Manager
	client     *discovery.Client
	logger     *slog.Logger
	outputPath string
	interval   time.Duration
}

func main() {
	var (
		outputPath = flag.String("output", "prometheus_targets.json", "Output file path for target groups")
		interval   = flag.Duration("interval", 30*time.Second, "Write interval for target file updates")
		debug      = flag.Bool("debug", false, "Enable debug logging")
	)
	flag.Parse()

	// Set up logging
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	// Validate output path
	if *outputPath == "" {
		logger.Error("output path cannot be empty")
		os.Exit(1)
	}

	// Create target manager and discovery client
	manager := targets.NewManager(logger)
	client := discovery.NewClient(manager, logger)

	// Create file writer
	writer := &FileWriter{
		manager:    manager,
		client:     client,
		logger:     logger,
		outputPath: *outputPath,
		interval:   *interval,
	}

	if err := writer.Run(); err != nil {
		logger.Error("file writer failed", "error", err)
		os.Exit(1)
	}
}

func (fw *FileWriter) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start mDNS discovery
	if err := fw.client.Start(ctx); err != nil {
		return fmt.Errorf("failed to start mDNS discovery: %w", err)
	}

	// Ensure output directory exists
	if err := fw.ensureOutputDir(); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Write initial empty file
	if err := fw.writeTargets(); err != nil {
		fw.logger.Error("failed to write initial target file", "error", err)
		return err
	}

	// Start file writing worker
	go fw.writeWorker(ctx)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-ctx.Done():
		fw.logger.Info("context cancelled")
	case sig := <-sigChan:
		fw.logger.Info("received signal", "signal", sig)
	}

	// Write final target file before shutting down
	if err := fw.writeTargets(); err != nil {
		fw.logger.Error("failed to write final target file", "error", err)
		return err
	}

	fw.logger.Info("file writer stopped")
	return nil
}

// ensureOutputDir creates the output directory if it doesn't exist
func (fw *FileWriter) ensureOutputDir() error {
	dir := filepath.Dir(fw.outputPath)
	if dir == "." {
		return nil // Current directory, no need to create
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	fw.logger.Debug("ensured output directory exists", "dir", dir)
	return nil
}

// writeWorker periodically writes target groups to the file
func (fw *FileWriter) writeWorker(ctx context.Context) {
	ticker := time.NewTicker(fw.interval)
	defer ticker.Stop()

	lastTargetCount := -1

	for {
		select {
		case <-ctx.Done():
			fw.logger.Info("stopping file write worker")
			return
		case <-ticker.C:
			currentCount := fw.manager.GetTargetCount()

			// Only write if target count changed to reduce I/O
			if currentCount != lastTargetCount {
				if err := fw.writeTargets(); err != nil {
					fw.logger.Error("failed to write targets to file", "error", err)
				} else {
					fw.logger.Debug("wrote targets to file",
						"path", fw.outputPath,
						"target_groups", len(fw.manager.GetTargetGroups()),
						"targets", currentCount)
				}
				lastTargetCount = currentCount
			}
		}
	}
}

// writeTargets writes the current target groups to the output file
func (fw *FileWriter) writeTargets() error {
	targetGroups := fw.manager.GetTargetGroups()

	// Create temporary file for atomic write
	tempPath := fw.outputPath + ".tmp"

	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file %s: %w", tempPath, err)
	}
	defer func() {
		tempFile.Close()
		// Clean up temp file on error
		if _, err := os.Stat(tempPath); err == nil {
			os.Remove(tempPath)
		}
	}()

	// Write JSON with indentation for readability
	encoder := json.NewEncoder(tempFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(targetGroups); err != nil {
		return fmt.Errorf("failed to encode target groups: %w", err)
	}

	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	tempFile.Close()

	// Atomic rename
	if err := os.Rename(tempPath, fw.outputPath); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", fw.outputPath, err)
	}

	fw.logger.Info("updated target file",
		"path", fw.outputPath,
		"target_groups", len(targetGroups),
		"targets", fw.manager.GetTargetCount())

	return nil
}
