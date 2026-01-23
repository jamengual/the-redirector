package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/jamengual/the-redirector/internal/config"
	"github.com/jamengual/the-redirector/internal/server"
	"github.com/jamengual/the-redirector/internal/watcher"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	// Check for subcommands first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "sync":
			runSync(os.Args[2:])
			return
		case "version":
			fmt.Printf("The Redirector %s (built %s)\n", version, buildTime)
			return
		case "help", "-h", "--help":
			printHelp()
			return
		}
	}

	// Default: run server
	runServer()
}

func printHelp() {
	fmt.Print(`The Redirector - High-performance URL redirect service

Usage:
  redirector [flags]           Start the redirect server
  redirector sync [flags]      Trigger immediate config reload
  redirector version           Show version information
  redirector help              Show this help

Server flags:
  -config string         Path to configuration file or directory (default "config.yaml")
  -log-level string      Log level: debug, info, warn, error (default "info")
  -watch                 Watch configuration for changes (default true)
  -watch-debounce dur    Debounce duration for reload (default 500ms)

Sync flags:
  -url string            Management API URL (default "http://localhost:8081")
  -timeout duration      Request timeout (default 10s)

Examples:
  redirector -config /etc/redirector/config.yaml
  redirector -config ./config/ -watch=true
  redirector sync -url http://redirector:8081
`)
}

func runSync(args []string) {
	syncFlags := flag.NewFlagSet("sync", flag.ExitOnError)
	url := syncFlags.String("url", "http://localhost:8081", "Management API URL")
	timeout := syncFlags.Duration("timeout", 10*time.Second, "Request timeout")
	syncFlags.Parse(args)

	client := &http.Client{Timeout: *timeout}

	endpoint := *url + "/api/v1/reload"
	req, err := http.NewRequest("POST", endpoint, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating request: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Triggering config reload at %s...\n", endpoint)

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Reload failed (HTTP %d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	fmt.Printf("Success: %s\n", string(body))
}

func runServer() {
	// Parse command line flags
	configPath := flag.String("config", "config.yaml", "Path to configuration file or directory")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	watchConfig := flag.Bool("watch", true, "Watch configuration for changes and hot-reload")
	watchDebounce := flag.Duration("watch-debounce", 500*time.Millisecond, "Debounce duration for config reload")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("The Redirector %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	// Configure logging
	level, err := zerolog.ParseLevel(*logLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().
		Str("version", version).
		Str("config", *configPath).
		Bool("watch", *watchConfig).
		Msg("Starting The Redirector")

	// Load configuration (supports both files and directories)
	cfg, err := config.LoadDirectory(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load configuration")
	}

	log.Info().
		Int("rules", len(cfg.Rules)).
		Msg("Configuration loaded")

	// Create and start server
	srv, err := server.New(cfg, *configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create server")
	}

	// Handle graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Start file watcher for hot-reload
	var configWatcher *watcher.Watcher
	if *watchConfig {
		configWatcher, err = watcher.New(watcher.Config{
			Paths:    []string{*configPath},
			Debounce: *watchDebounce,
			ReloadFunc: func(path string) error {
				return srv.ReloadConfig(path)
			},
		})
		if err != nil {
			log.Warn().Err(err).Msg("Failed to create config watcher, hot-reload disabled")
		} else {
			if err := configWatcher.Start(ctx); err != nil {
				log.Warn().Err(err).Msg("Failed to start config watcher")
			}
			defer configWatcher.Stop()
		}
	}

	// Handle signals
	go func() {
		for {
			select {
			case sig := <-sigChan:
				switch sig {
				case syscall.SIGHUP:
					// SIGHUP triggers manual config reload
					log.Info().Msg("Received SIGHUP, reloading configuration")
					if err := srv.ReloadConfig(*configPath); err != nil {
						log.Error().Err(err).Msg("Failed to reload configuration")
					}
				case syscall.SIGINT, syscall.SIGTERM:
					log.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start server
	if err := srv.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("Server error")
	}

	log.Info().Msg("Server stopped gracefully")
}

