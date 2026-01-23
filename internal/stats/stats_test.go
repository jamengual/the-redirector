package stats

import (
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		BufferSize:   100,
		SamplingRate: 1.0,
	}

	c := NewCollector(cfg)

	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	if !c.IsEnabled() {
		t.Error("Collector should be enabled")
	}
	if c.ringSize != 100 {
		t.Errorf("Expected buffer size 100, got %d", c.ringSize)
	}
}

func TestCollector_Record(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		BufferSize:   10,
		SamplingRate: 1.0,
	}

	c := NewCollector(cfg)

	// Record a request
	c.Record(RequestRecord{
		Timestamp:   time.Now(),
		Path:        "/test",
		Host:        "example.com",
		Status:      301,
		RuleID:      "test-rule",
		Destination: "https://example.org/",
		LatencyUs:   500,
		ClientIP:    "127.0.0.1",
	})

	summary := c.GetSummary()

	if summary.TotalRequests != 1 {
		t.Errorf("Expected 1 request, got %d", summary.TotalRequests)
	}

	if summary.TotalErrors != 0 {
		t.Errorf("Expected 0 errors, got %d", summary.TotalErrors)
	}
}

func TestCollector_RecordErrors(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		BufferSize:   10,
		SamplingRate: 1.0,
	}

	c := NewCollector(cfg)

	// Record an error
	c.Record(RequestRecord{
		Timestamp: time.Now(),
		Path:      "/error",
		Status:    404,
		LatencyUs: 100,
	})

	summary := c.GetSummary()

	if summary.TotalErrors != 1 {
		t.Errorf("Expected 1 error, got %d", summary.TotalErrors)
	}
}

func TestCollector_RingBuffer(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		BufferSize:   5,
		SamplingRate: 1.0,
	}

	c := NewCollector(cfg)

	// Record more requests than buffer size
	for i := 0; i < 10; i++ {
		c.Record(RequestRecord{
			Timestamp: time.Now(),
			Path:      "/test",
			Status:    301,
			LatencyUs: int64(i * 100),
		})
	}

	// Should only get 5 recent requests (buffer size)
	recent := c.GetRecentRequests(10)
	if len(recent) != 5 {
		t.Errorf("Expected 5 recent requests, got %d", len(recent))
	}
}

func TestCollector_Disabled(t *testing.T) {
	cfg := Config{
		Enabled:      false,
		BufferSize:   10,
		SamplingRate: 1.0,
	}

	c := NewCollector(cfg)

	c.Record(RequestRecord{
		Timestamp: time.Now(),
		Path:      "/test",
		Status:    301,
	})

	summary := c.GetSummary()

	if summary.TotalRequests != 0 {
		t.Errorf("Disabled collector should not record, got %d requests", summary.TotalRequests)
	}
}

func TestCollector_EnableDisable(t *testing.T) {
	cfg := Config{
		Enabled:      false,
		BufferSize:   10,
		SamplingRate: 1.0,
	}

	c := NewCollector(cfg)

	if c.IsEnabled() {
		t.Error("Collector should start disabled")
	}

	c.Enable()
	if !c.IsEnabled() {
		t.Error("Collector should be enabled after Enable()")
	}

	c.Disable()
	if c.IsEnabled() {
		t.Error("Collector should be disabled after Disable()")
	}
}

func TestCollector_GetRuleStats(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		BufferSize:   10,
		SamplingRate: 1.0,
	}

	c := NewCollector(cfg)

	// Record requests for a rule
	c.Record(RequestRecord{RuleID: "rule-1", Status: 301, LatencyUs: 100})
	c.Record(RequestRecord{RuleID: "rule-1", Status: 301, LatencyUs: 200})
	c.Record(RequestRecord{RuleID: "rule-2", Status: 302, LatencyUs: 150})

	stats1, found := c.GetRuleStats("rule-1")
	if !found {
		t.Fatal("rule-1 stats not found")
	}
	if stats1.Hits != 2 {
		t.Errorf("Expected 2 hits, got %d", stats1.Hits)
	}
	if stats1.AvgLatency != 150 {
		t.Errorf("Expected avg latency 150, got %f", stats1.AvgLatency)
	}

	_, found = c.GetRuleStats("nonexistent")
	if found {
		t.Error("Should not find nonexistent rule")
	}
}

func TestCollector_Reset(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		BufferSize:   10,
		SamplingRate: 1.0,
	}

	c := NewCollector(cfg)

	c.Record(RequestRecord{Status: 301, LatencyUs: 100})
	c.Record(RequestRecord{Status: 404, LatencyUs: 100})

	c.Reset()

	summary := c.GetSummary()
	if summary.TotalRequests != 0 {
		t.Errorf("Expected 0 requests after reset, got %d", summary.TotalRequests)
	}
	if summary.TotalErrors != 0 {
		t.Errorf("Expected 0 errors after reset, got %d", summary.TotalErrors)
	}
}

func TestCollector_LatencyBuckets(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		BufferSize:   10,
		SamplingRate: 1.0,
	}

	c := NewCollector(cfg)

	// Record requests in different latency buckets
	c.Record(RequestRecord{Status: 301, LatencyUs: 50})     // <100us
	c.Record(RequestRecord{Status: 301, LatencyUs: 250})    // <500us
	c.Record(RequestRecord{Status: 301, LatencyUs: 750})    // <1ms
	c.Record(RequestRecord{Status: 301, LatencyUs: 3000})   // <5ms
	c.Record(RequestRecord{Status: 301, LatencyUs: 7500})   // <10ms
	c.Record(RequestRecord{Status: 301, LatencyUs: 15000})  // >=10ms

	summary := c.GetSummary()

	if summary.LatencyBuckets["<100us"] != 1 {
		t.Errorf("Expected 1 in <100us bucket, got %d", summary.LatencyBuckets["<100us"])
	}
	if summary.LatencyBuckets["<500us"] != 1 {
		t.Errorf("Expected 1 in <500us bucket, got %d", summary.LatencyBuckets["<500us"])
	}
	if summary.LatencyBuckets[">=10ms"] != 1 {
		t.Errorf("Expected 1 in >=10ms bucket, got %d", summary.LatencyBuckets[">=10ms"])
	}
}
