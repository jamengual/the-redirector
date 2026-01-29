package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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
