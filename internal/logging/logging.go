// Package logging provides structured logging with rotation and live stats.
package logging

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Config configures the logging system.
type Config struct {
	// Level is the minimum log level (debug, info, warn, error)
	Level string `yaml:"level" json:"level"`

	// Format is the log format (json, console)
	Format string `yaml:"format" json:"format"`

	// File configures file output with rotation
	File *FileConfig `yaml:"file" json:"file"`
}

// FileConfig configures file-based logging with rotation.
type FileConfig struct {
	// Path is the log file path
	Path string `yaml:"path" json:"path"`

	// MaxSizeMB is the maximum size in MB before rotation (default: 100)
	MaxSizeMB int `yaml:"max_size_mb" json:"max_size_mb"`

	// MaxBackups is the maximum number of old log files to retain (default: 5)
	MaxBackups int `yaml:"max_backups" json:"max_backups"`

	// MaxAgeDays is the maximum days to retain old log files (default: 30)
	MaxAgeDays int `yaml:"max_age_days" json:"max_age_days"`

	// Compress enables gzip compression of rotated files
	Compress bool `yaml:"compress" json:"compress"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Level:  "info",
		Format: "json",
		File: &FileConfig{
			MaxSizeMB:  100,
			MaxBackups: 5,
			MaxAgeDays: 30,
			Compress:   true,
		},
	}
}

// Setup configures the global logger based on the config.
func Setup(cfg Config) error {
	// Parse log level
	level, err := zerolog.ParseLevel(cfg.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Configure time format
	zerolog.TimeFieldFormat = time.RFC3339Nano

	// Build writers
	var writers []io.Writer

	// Console output
	if cfg.Format == "console" {
		writers = append(writers, zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05",
		})
	} else {
		writers = append(writers, os.Stderr)
	}

	// File output with rotation
	if cfg.File != nil && cfg.File.Path != "" {
		fileWriter := &lumberjack.Logger{
			Filename:   cfg.File.Path,
			MaxSize:    cfg.File.MaxSizeMB,
			MaxBackups: cfg.File.MaxBackups,
			MaxAge:     cfg.File.MaxAgeDays,
			Compress:   cfg.File.Compress,
		}
		writers = append(writers, fileWriter)
	}

	// Create multi-writer
	multi := io.MultiWriter(writers...)
	log.Logger = zerolog.New(multi).With().Timestamp().Logger()

	return nil
}

// RequestLogger creates a logger for request logging.
func RequestLogger() zerolog.Logger {
	return log.With().Str("component", "request").Logger()
}

// ConfigLogger creates a logger for config operations.
func ConfigLogger() zerolog.Logger {
	return log.With().Str("component", "config").Logger()
}
