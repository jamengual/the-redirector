// Package metrics provides Prometheus metrics for the redirector.
// This file defines metrics specific to the redirector-sync service.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SyncerMetrics holds Prometheus metrics for the redirector-sync service.
type SyncerMetrics struct {
	// Sync lifecycle
	SyncTotal         *prometheus.CounterVec
	SyncDuration      *prometheus.HistogramVec
	LastSyncTimestamp prometheus.Gauge
	LastSyncSuccess   prometheus.Gauge

	// Source fetch metrics
	FetchTotal    *prometheus.CounterVec
	FetchDuration *prometheus.HistogramVec

	// Target push metrics
	PushTotal    *prometheus.CounterVec
	PushDuration *prometheus.HistogramVec

	// Config metrics
	RulesFetched prometheus.Gauge
	LintErrors   *prometheus.CounterVec
}

// NewSyncerMetrics creates and registers syncer-specific Prometheus metrics.
func NewSyncerMetrics(registry prometheus.Registerer) *SyncerMetrics {
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}

	m := &SyncerMetrics{
		SyncTotal: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "redirector_sync",
				Name:      "sync_total",
				Help:      "Total number of sync operations",
			},
			[]string{"status"}, // success, failure
		),

		SyncDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "redirector_sync",
				Name:      "sync_duration_seconds",
				Help:      "Duration of sync operations in seconds",
				Buckets:   []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60},
			},
			[]string{"status"},
		),

		LastSyncTimestamp: promauto.With(registry).NewGauge(
			prometheus.GaugeOpts{
				Namespace: "redirector_sync",
				Name:      "last_sync_timestamp_seconds",
				Help:      "Unix timestamp of the last sync attempt",
			},
		),

		LastSyncSuccess: promauto.With(registry).NewGauge(
			prometheus.GaugeOpts{
				Namespace: "redirector_sync",
				Name:      "last_sync_success",
				Help:      "Whether the last sync was successful (1=success, 0=failure)",
			},
		),

		FetchTotal: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "redirector_sync",
				Name:      "fetch_total",
				Help:      "Total number of source fetch operations",
			},
			[]string{"source", "status"},
		),

		FetchDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "redirector_sync",
				Name:      "fetch_duration_seconds",
				Help:      "Duration of source fetch operations in seconds",
				Buckets:   []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30},
			},
			[]string{"source"},
		),

		PushTotal: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "redirector_sync",
				Name:      "push_total",
				Help:      "Total number of config push operations to targets",
			},
			[]string{"target", "status"},
		),

		PushDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "redirector_sync",
				Name:      "push_duration_seconds",
				Help:      "Duration of config push operations in seconds",
				Buckets:   []float64{.05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"target"},
		),

		RulesFetched: promauto.With(registry).NewGauge(
			prometheus.GaugeOpts{
				Namespace: "redirector_sync",
				Name:      "rules_fetched",
				Help:      "Number of rules from the last successful fetch",
			},
		),

		LintErrors: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "redirector_sync",
				Name:      "lint_errors_total",
				Help:      "Total config lint errors encountered during sync",
			},
			[]string{"source"},
		),
	}

	// Register standard process collectors
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

// RecordSync records a completed sync operation.
func (m *SyncerMetrics) RecordSync(success bool, durationSeconds float64) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.SyncTotal.WithLabelValues(status).Inc()
	m.SyncDuration.WithLabelValues(status).Observe(durationSeconds)
	m.LastSyncTimestamp.SetToCurrentTime()
	if success {
		m.LastSyncSuccess.Set(1)
	} else {
		m.LastSyncSuccess.Set(0)
	}
}

// RecordFetch records a source fetch operation.
func (m *SyncerMetrics) RecordFetch(source string, success bool, durationSeconds float64) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.FetchTotal.WithLabelValues(source, status).Inc()
	m.FetchDuration.WithLabelValues(source).Observe(durationSeconds)
}

// RecordPush records a config push to a target.
func (m *SyncerMetrics) RecordPush(target string, success bool, durationSeconds float64) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.PushTotal.WithLabelValues(target, status).Inc()
	m.PushDuration.WithLabelValues(target).Observe(durationSeconds)
}

// RecordLintError records a lint error for a source.
func (m *SyncerMetrics) RecordLintError(source string) {
	m.LintErrors.WithLabelValues(source).Inc()
}
