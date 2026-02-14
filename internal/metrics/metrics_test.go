package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNew(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.RequestsTotal == nil {
		t.Error("RequestsTotal not initialized")
	}
	if m.RequestDuration == nil {
		t.Error("RequestDuration not initialized")
	}
	if m.ConfigReloadsTotal == nil {
		t.Error("ConfigReloadsTotal not initialized")
	}
	if m.ConfigInfo == nil {
		t.Error("ConfigInfo not initialized")
	}
	if m.UptimeSeconds == nil {
		t.Error("UptimeSeconds not initialized")
	}
	if m.RateLimitedTotal == nil {
		t.Error("RateLimitedTotal not initialized")
	}
}

func TestNewWithRuntimeMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewWithRuntimeMetrics(registry)

	if m == nil {
		t.Fatal("NewWithRuntimeMetrics returned nil")
	}
	if m.Goroutines == nil {
		t.Error("Goroutines metric not initialized")
	}
	if m.MemoryAlloc == nil {
		t.Error("MemoryAlloc metric not initialized")
	}

	// Verify standard collectors are registered by gathering metrics
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Look for go_* and process_* metrics from standard collectors
	hasGoMetric := false
	hasProcessMetric := false
	for _, mf := range families {
		if strings.HasPrefix(mf.GetName(), "go_") {
			hasGoMetric = true
		}
		if strings.HasPrefix(mf.GetName(), "process_") {
			hasProcessMetric = true
		}
	}

	if !hasGoMetric {
		t.Error("Expected go_* metrics from GoCollector")
	}
	if !hasProcessMetric {
		t.Error("Expected process_* metrics from ProcessCollector")
	}
}

func TestRecordRequest(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	// Should not panic
	m.RecordRequest("GET", 301, "rule-1", 0.001, 256)
	m.RecordRequest("POST", 404, "", 0.005, 0)
	m.RecordRequest("GET", 500, "rule-2", 0.1, 1024)
}

func TestRecordRuleMatch(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	// Should not panic
	m.RecordRuleMatch("rule-1", "exact")
	m.RecordRuleMatch("rule-2", "regex")
	m.RecordRuleMatch("rule-3", "prefix")
}

func TestRecordConfigReload(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	// Record successful reload
	m.RecordConfigReload(true, 100, 0.05)

	// Record failed reload
	m.RecordConfigReload(false, 0, 0.1)
}

func TestInFlightRequests(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	m.IncrementInFlight()
	m.IncrementInFlight()
	m.DecrementInFlight()
	// Should not panic when decrementing below zero (gauges allow this)
	m.DecrementInFlight()
	m.DecrementInFlight()
}

func TestRecordHostRejected(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	if m.HostRejectedTotal == nil {
		t.Fatal("HostRejectedTotal not initialized")
	}

	// Should not panic and should increment
	m.RecordHostRejected()
	m.RecordHostRejected()
}

func TestStatusToString(t *testing.T) {
	tests := []struct {
		status   int
		expected string
	}{
		{200, "2xx"},
		{201, "2xx"},
		{301, "3xx"},
		{302, "3xx"},
		{404, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{100, "unknown"},
		{0, "unknown"},
	}

	for _, tt := range tests {
		result := statusToString(tt.status)
		if result != tt.expected {
			t.Errorf("statusToString(%d) = %s, expected %s", tt.status, result, tt.expected)
		}
	}
}

func TestRuntimeMetrics(t *testing.T) {
	// Test goroutine count
	count := getGoroutineCount()
	if count < 1 {
		t.Errorf("Expected at least 1 goroutine, got %d", count)
	}

	// Test memory alloc
	alloc := getMemoryAlloc()
	if alloc == 0 {
		t.Error("Expected non-zero memory allocation")
	}
}

func TestRegisterBuildInfo(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	m.RegisterBuildInfo(registry, BuildInfo{
		Version:   "1.2.3",
		Commit:    "abc123",
		BuildTime: "2025-01-15T10:00:00Z",
		GoVersion: "go1.25",
	})

	if m.Info == nil {
		t.Fatal("Info gauge not set after RegisterBuildInfo")
	}

	// Verify the metric has value 1
	expected := `
		# HELP redirector_build_info Build information for the redirector
		# TYPE redirector_build_info gauge
		redirector_build_info{build_time="2025-01-15T10:00:00Z",commit="abc123",go_version="go1.25",version="1.2.3"} 1
	`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "redirector_build_info"); err != nil {
		t.Errorf("Build info metric mismatch: %v", err)
	}
}

func TestRegisterBuildInfoDefaultGoVersion(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	// When GoVersion is empty, should use runtime.Version()
	m.RegisterBuildInfo(registry, BuildInfo{
		Version: "dev",
	})

	if m.Info == nil {
		t.Fatal("Info gauge not set")
	}
}

func TestSetConfigInfo(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	m.SetConfigInfo("1.0", "sha256:abc123", "config.yaml")

	expected := `
		# HELP redirector_config_info Current configuration metadata
		# TYPE redirector_config_info gauge
		redirector_config_info{hash="sha256:abc123",source="config.yaml",version="1.0"} 1
	`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "redirector_config_info"); err != nil {
		t.Errorf("Config info metric mismatch: %v", err)
	}

	// Updating should reset old labels and set new ones
	m.SetConfigInfo("2.0", "sha256:def456", "new-config.yaml")

	expected2 := `
		# HELP redirector_config_info Current configuration metadata
		# TYPE redirector_config_info gauge
		redirector_config_info{hash="sha256:def456",source="new-config.yaml",version="2.0"} 1
	`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected2), "redirector_config_info"); err != nil {
		t.Errorf("Updated config info metric mismatch: %v", err)
	}
}

func TestUptimeSeconds(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	if m.UptimeSeconds == nil {
		t.Fatal("UptimeSeconds not initialized")
	}

	// Gather and verify it's a positive value
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	found := false
	for _, mf := range families {
		if mf.GetName() == "redirector_uptime_seconds" {
			found = true
			if len(mf.GetMetric()) == 0 {
				t.Error("Expected at least one metric value")
			} else {
				val := mf.GetMetric()[0].GetGauge().GetValue()
				if val < 0 {
					t.Errorf("Expected non-negative uptime, got %f", val)
				}
			}
		}
	}
	if !found {
		t.Error("redirector_uptime_seconds metric not found")
	}
}

func TestRecordRateLimited(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := New(registry)

	m.RecordRateLimited("global")
	m.RecordRateLimited("global")
	m.RecordRateLimited("per_ip")
	m.RecordRateLimited("path")

	expected := `
		# HELP redirector_rate_limited_total Total requests rejected by rate limiting
		# TYPE redirector_rate_limited_total counter
		redirector_rate_limited_total{scope="global"} 2
		redirector_rate_limited_total{scope="path"} 1
		redirector_rate_limited_total{scope="per_ip"} 1
	`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "redirector_rate_limited_total"); err != nil {
		t.Errorf("Rate limited metric mismatch: %v", err)
	}
}
