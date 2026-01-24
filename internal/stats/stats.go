// Package stats provides request statistics and live monitoring.
package stats

import (
	"sync"
	"sync/atomic"
	"time"
)

// RequestRecord represents a single request for live view.
type RequestRecord struct {
	Timestamp   time.Time `json:"timestamp"`
	Path        string    `json:"path"`
	Host        string    `json:"host"`
	Status      int       `json:"status"`
	RuleID      string    `json:"rule_id"`
	Destination string    `json:"destination"`
	LatencyUs   int64     `json:"latency_us"` // Microseconds
	ClientIP    string    `json:"client_ip"`
}

// RuleStats tracks statistics for a single rule.
type RuleStats struct {
	ID           string  `json:"id"`
	Hits         int64   `json:"hits"`
	TotalLatency int64   `json:"total_latency_us"` // Total latency in microseconds
	AvgLatency   float64 `json:"avg_latency_us"`   // Computed on read
}

// Collector collects and stores request statistics.
type Collector struct {
	// Ring buffer for live view
	ringBuffer []RequestRecord
	ringHead   int
	ringSize   int
	ringMu     sync.RWMutex

	// Counters
	totalRequests atomic.Int64
	totalErrors   atomic.Int64
	startTime     time.Time

	// Per-status counters
	statusCounts sync.Map // map[int]*atomic.Int64

	// Per-rule stats
	ruleStats sync.Map // map[string]*ruleStatsInternal

	// Latency histogram buckets (microseconds)
	latencyBuckets []int64 // [<100, <500, <1000, <5000, <10000, >=10000]
	latencyMu      sync.Mutex

	// Sampling
	samplingRate float64
	sampleCount  atomic.Int64

	// Enabled flag (disabled by default for performance)
	enabled atomic.Bool
}

type ruleStatsInternal struct {
	hits         atomic.Int64
	totalLatency atomic.Int64
}

// Config configures the stats collector.
type Config struct {
	// Enabled enables stats collection (disabled by default for max perf)
	Enabled bool `yaml:"enabled" json:"enabled"`

	// BufferSize is the ring buffer size for live view (default: 1000)
	BufferSize int `yaml:"buffer_size" json:"buffer_size"`

	// SamplingRate is the fraction of requests to record (1.0 = all, 0.01 = 1%)
	// Automatically reduces under high load
	SamplingRate float64 `yaml:"sampling_rate" json:"sampling_rate"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:      false, // Disabled by default for performance
		BufferSize:   1000,
		SamplingRate: 1.0,
	}
}

// NewCollector creates a new stats collector.
func NewCollector(cfg Config) *Collector {
	bufferSize := cfg.BufferSize
	if bufferSize <= 0 {
		bufferSize = 1000
	}

	samplingRate := cfg.SamplingRate
	if samplingRate <= 0 || samplingRate > 1 {
		samplingRate = 1.0
	}

	c := &Collector{
		ringBuffer:     make([]RequestRecord, bufferSize),
		ringSize:       bufferSize,
		startTime:      time.Now(),
		latencyBuckets: make([]int64, 6),
		samplingRate:   samplingRate,
	}

	c.enabled.Store(cfg.Enabled)

	return c
}

// Record records a request. Thread-safe.
func (c *Collector) Record(rec RequestRecord) {
	if !c.enabled.Load() {
		return
	}

	// Always count totals
	c.totalRequests.Add(1)

	if rec.Status >= 400 {
		c.totalErrors.Add(1)
	}

	// Update status counter
	c.incrementStatus(rec.Status)

	// Update rule stats
	c.updateRuleStats(rec.RuleID, rec.LatencyUs)

	// Update latency histogram
	c.updateLatencyHistogram(rec.LatencyUs)

	// Sampling for ring buffer
	count := c.sampleCount.Add(1)
	if c.samplingRate < 1.0 {
		// Simple sampling: only record every 1/rate requests
		interval := int64(1.0 / c.samplingRate)
		if count%interval != 0 {
			return
		}
	}

	// Add to ring buffer
	c.ringMu.Lock()
	c.ringBuffer[c.ringHead] = rec
	c.ringHead = (c.ringHead + 1) % c.ringSize
	c.ringMu.Unlock()
}

func (c *Collector) incrementStatus(status int) {
	val, _ := c.statusCounts.LoadOrStore(status, &atomic.Int64{})
	if counter, ok := val.(*atomic.Int64); ok {
		counter.Add(1)
	}
}

func (c *Collector) updateRuleStats(ruleID string, latencyUs int64) {
	if ruleID == "" {
		return
	}

	val, _ := c.ruleStats.LoadOrStore(ruleID, &ruleStatsInternal{})
	if stats, ok := val.(*ruleStatsInternal); ok {
		stats.hits.Add(1)
		stats.totalLatency.Add(latencyUs)
	}
}

func (c *Collector) updateLatencyHistogram(latencyUs int64) {
	c.latencyMu.Lock()
	defer c.latencyMu.Unlock()

	switch {
	case latencyUs < 100:
		c.latencyBuckets[0]++
	case latencyUs < 500:
		c.latencyBuckets[1]++
	case latencyUs < 1000:
		c.latencyBuckets[2]++
	case latencyUs < 5000:
		c.latencyBuckets[3]++
	case latencyUs < 10000:
		c.latencyBuckets[4]++
	default:
		c.latencyBuckets[5]++
	}
}

// Summary returns overall statistics.
type Summary struct {
	UptimeSeconds  float64          `json:"uptime_seconds"`
	TotalRequests  int64            `json:"total_requests"`
	TotalErrors    int64            `json:"total_errors"`
	RequestsPerSec float64          `json:"requests_per_second"`
	ErrorRate      float64          `json:"error_rate"`
	StatusCounts   map[int]int64    `json:"status_counts"`
	LatencyBuckets map[string]int64 `json:"latency_buckets"`
	TopRules       []RuleStats      `json:"top_rules"`
}

// GetSummary returns the current statistics summary.
func (c *Collector) GetSummary() Summary {
	uptime := time.Since(c.startTime).Seconds()
	total := c.totalRequests.Load()
	errors := c.totalErrors.Load()

	var rps, errorRate float64
	if uptime > 0 {
		rps = float64(total) / uptime
	}
	if total > 0 {
		errorRate = float64(errors) / float64(total)
	}

	// Collect status counts
	statusCounts := make(map[int]int64)
	c.statusCounts.Range(func(key, value interface{}) bool {
		if k, ok := key.(int); ok {
			if v, ok := value.(*atomic.Int64); ok {
				statusCounts[k] = v.Load()
			}
		}
		return true
	})

	// Collect latency buckets
	c.latencyMu.Lock()
	latencyBuckets := map[string]int64{
		"<100us": c.latencyBuckets[0],
		"<500us": c.latencyBuckets[1],
		"<1ms":   c.latencyBuckets[2],
		"<5ms":   c.latencyBuckets[3],
		"<10ms":  c.latencyBuckets[4],
		">=10ms": c.latencyBuckets[5],
	}
	c.latencyMu.Unlock()

	// Collect top rules
	var topRules []RuleStats
	c.ruleStats.Range(func(key, value interface{}) bool {
		stats, ok := value.(*ruleStatsInternal)
		if !ok {
			return true
		}
		ruleID, ok := key.(string)
		if !ok {
			return true
		}
		hits := stats.hits.Load()
		totalLat := stats.totalLatency.Load()

		var avgLat float64
		if hits > 0 {
			avgLat = float64(totalLat) / float64(hits)
		}

		topRules = append(topRules, RuleStats{
			ID:           ruleID,
			Hits:         hits,
			TotalLatency: totalLat,
			AvgLatency:   avgLat,
		})
		return true
	})

	// Sort by hits (simple bubble sort for small lists)
	for i := 0; i < len(topRules); i++ {
		for j := i + 1; j < len(topRules); j++ {
			if topRules[j].Hits > topRules[i].Hits {
				topRules[i], topRules[j] = topRules[j], topRules[i]
			}
		}
	}

	// Limit to top 10
	if len(topRules) > 10 {
		topRules = topRules[:10]
	}

	return Summary{
		UptimeSeconds:  uptime,
		TotalRequests:  total,
		TotalErrors:    errors,
		RequestsPerSec: rps,
		ErrorRate:      errorRate,
		StatusCounts:   statusCounts,
		LatencyBuckets: latencyBuckets,
		TopRules:       topRules,
	}
}

// GetRecentRequests returns the most recent requests from the ring buffer.
func (c *Collector) GetRecentRequests(limit int) []RequestRecord {
	if !c.enabled.Load() {
		return nil
	}

	c.ringMu.RLock()
	defer c.ringMu.RUnlock()

	if limit <= 0 || limit > c.ringSize {
		limit = c.ringSize
	}

	result := make([]RequestRecord, 0, limit)

	// Read from ring buffer in reverse order (newest first)
	for i := 0; i < limit; i++ {
		idx := (c.ringHead - 1 - i + c.ringSize) % c.ringSize
		rec := c.ringBuffer[idx]

		// Skip empty records
		if rec.Timestamp.IsZero() {
			break
		}

		result = append(result, rec)
	}

	return result
}

// GetRuleStats returns stats for a specific rule.
func (c *Collector) GetRuleStats(ruleID string) (RuleStats, bool) {
	val, ok := c.ruleStats.Load(ruleID)
	if !ok {
		return RuleStats{}, false
	}

	stats, ok := val.(*ruleStatsInternal)
	if !ok {
		return RuleStats{}, false
	}
	hits := stats.hits.Load()
	totalLat := stats.totalLatency.Load()

	var avgLat float64
	if hits > 0 {
		avgLat = float64(totalLat) / float64(hits)
	}

	return RuleStats{
		ID:           ruleID,
		Hits:         hits,
		TotalLatency: totalLat,
		AvgLatency:   avgLat,
	}, true
}

// Enable enables stats collection.
func (c *Collector) Enable() {
	c.enabled.Store(true)
}

// Disable disables stats collection.
func (c *Collector) Disable() {
	c.enabled.Store(false)
}

// IsEnabled returns whether stats collection is enabled.
func (c *Collector) IsEnabled() bool {
	return c.enabled.Load()
}

// Reset clears all statistics.
func (c *Collector) Reset() {
	c.totalRequests.Store(0)
	c.totalErrors.Store(0)
	c.startTime = time.Now()

	c.statusCounts = sync.Map{}
	c.ruleStats = sync.Map{}

	c.latencyMu.Lock()
	c.latencyBuckets = make([]int64, 6)
	c.latencyMu.Unlock()

	c.ringMu.Lock()
	c.ringBuffer = make([]RequestRecord, c.ringSize)
	c.ringHead = 0
	c.ringMu.Unlock()
}
