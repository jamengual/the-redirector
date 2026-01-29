// Package metrics provides Prometheus metrics for the redirector.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the redirector.
type Metrics struct {
	// Request metrics
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	RequestsInFlight prometheus.Gauge

	// Response metrics
	ResponseSize *prometheus.HistogramVec

	// Config metrics
	ConfigReloadsTotal   *prometheus.CounterVec
	ConfigRulesCount     prometheus.Gauge
	ConfigLastReloadTime prometheus.Gauge
	ConfigLoadDuration   prometheus.Histogram

	// System metrics
	Goroutines  prometheus.GaugeFunc
	MemoryAlloc prometheus.GaugeFunc

	// Rule metrics
	RuleMatchesTotal *prometheus.CounterVec

	// Host rejection metrics
	HostRejectedTotal prometheus.Counter
}

// New creates and registers all metrics.
func New(registry prometheus.Registerer) *Metrics {
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}

	m := &Metrics{
		// Request metrics
		RequestsTotal: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "redirector",
				Name:      "requests_total",
				Help:      "Total number of HTTP requests processed",
			},
			[]string{"method", "status", "rule_id"},
		),

		RequestDuration: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "redirector",
				Name:      "request_duration_seconds",
				Help:      "HTTP request duration in seconds",
				Buckets:   []float64{.00005, .0001, .00025, .0005, .001, .0025, .005, .01, .025, .05, .1},
			},
			[]string{"method", "status", "rule_id"},
		),

		RequestsInFlight: promauto.With(registry).NewGauge(
			prometheus.GaugeOpts{
				Namespace: "redirector",
				Name:      "requests_in_flight",
				Help:      "Current number of requests being processed",
			},
		),

		// Response metrics
		ResponseSize: promauto.With(registry).NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: "redirector",
				Name:      "response_size_bytes",
				Help:      "HTTP response size in bytes",
				Buckets:   []float64{100, 500, 1000, 5000, 10000},
			},
			[]string{"status"},
		),

		// Config metrics
		ConfigReloadsTotal: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "redirector",
				Name:      "config_reloads_total",
				Help:      "Total number of configuration reloads",
			},
			[]string{"status"}, // success, failure
		),

		ConfigRulesCount: promauto.With(registry).NewGauge(
			prometheus.GaugeOpts{
				Namespace: "redirector",
				Name:      "config_rules_count",
				Help:      "Current number of redirect rules loaded",
			},
		),

		ConfigLastReloadTime: promauto.With(registry).NewGauge(
			prometheus.GaugeOpts{
				Namespace: "redirector",
				Name:      "config_last_reload_timestamp_seconds",
				Help:      "Unix timestamp of the last successful config reload",
			},
		),

		ConfigLoadDuration: promauto.With(registry).NewHistogram(
			prometheus.HistogramOpts{
				Namespace: "redirector",
				Name:      "config_load_duration_seconds",
				Help:      "Time taken to load and parse configuration",
				Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
			},
		),

		// Rule metrics
		RuleMatchesTotal: promauto.With(registry).NewCounterVec(
			prometheus.CounterOpts{
				Namespace: "redirector",
				Name:      "rule_matches_total",
				Help:      "Total number of times each rule was matched",
			},
			[]string{"rule_id", "match_type"},
		),

		// Host rejection metrics
		HostRejectedTotal: promauto.With(registry).NewCounter(
			prometheus.CounterOpts{
				Namespace: "redirector",
				Name:      "host_rejected_total",
				Help:      "Total requests rejected due to unknown Host header",
			},
		),
	}

	return m
}

// NewWithRuntimeMetrics creates metrics including Go runtime metrics.
func NewWithRuntimeMetrics(registry prometheus.Registerer) *Metrics {
	m := New(registry)

	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}

	// Register Go runtime metrics
	m.Goroutines = promauto.With(registry).NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: "redirector",
			Name:      "goroutines",
			Help:      "Current number of goroutines",
		},
		func() float64 {
			return float64(getGoroutineCount())
		},
	)

	m.MemoryAlloc = promauto.With(registry).NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: "redirector",
			Name:      "memory_alloc_bytes",
			Help:      "Current memory allocation in bytes",
		},
		func() float64 {
			return float64(getMemoryAlloc())
		},
	)

	return m
}

// RecordRequest records metrics for a completed request.
func (m *Metrics) RecordRequest(method string, status int, ruleID string, durationSeconds float64, responseBytes int) {
	statusStr := statusToString(status)
	if ruleID == "" {
		ruleID = "none"
	}

	m.RequestsTotal.WithLabelValues(method, statusStr, ruleID).Inc()
	m.RequestDuration.WithLabelValues(method, statusStr, ruleID).Observe(durationSeconds)
	m.ResponseSize.WithLabelValues(statusStr).Observe(float64(responseBytes))
}

// RecordHostRejected increments the counter for rejected unknown hosts.
func (m *Metrics) RecordHostRejected() {
	m.HostRejectedTotal.Inc()
}

// RecordRuleMatch records a rule match.
func (m *Metrics) RecordRuleMatch(ruleID, matchType string) {
	m.RuleMatchesTotal.WithLabelValues(ruleID, matchType).Inc()
}

// RecordConfigReload records a config reload attempt.
func (m *Metrics) RecordConfigReload(success bool, rulesCount int, durationSeconds float64) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.ConfigReloadsTotal.WithLabelValues(status).Inc()
	m.ConfigLoadDuration.Observe(durationSeconds)

	if success {
		m.ConfigRulesCount.Set(float64(rulesCount))
		m.ConfigLastReloadTime.SetToCurrentTime()
	}
}

// IncrementInFlight increments the in-flight request counter.
func (m *Metrics) IncrementInFlight() {
	m.RequestsInFlight.Inc()
}

// DecrementInFlight decrements the in-flight request counter.
func (m *Metrics) DecrementInFlight() {
	m.RequestsInFlight.Dec()
}

func statusToString(status int) string {
	switch {
	case status >= 200 && status < 300:
		return "2xx"
	case status >= 300 && status < 400:
		return "3xx"
	case status >= 400 && status < 500:
		return "4xx"
	case status >= 500:
		return "5xx"
	default:
		return "unknown"
	}
}
