package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewSyncerMetrics(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewSyncerMetrics(registry)

	if m == nil {
		t.Fatal("NewSyncerMetrics returned nil")
	}
	if m.SyncTotal == nil {
		t.Error("SyncTotal not initialized")
	}
	if m.FetchTotal == nil {
		t.Error("FetchTotal not initialized")
	}
	if m.PushTotal == nil {
		t.Error("PushTotal not initialized")
	}
	if m.LastSyncTimestamp == nil {
		t.Error("LastSyncTimestamp not initialized")
	}
	if m.RulesFetched == nil {
		t.Error("RulesFetched not initialized")
	}
	if m.LintErrors == nil {
		t.Error("LintErrors not initialized")
	}

	// Verify standard collectors are registered
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	hasGoMetric := false
	for _, mf := range families {
		if strings.HasPrefix(mf.GetName(), "go_") {
			hasGoMetric = true
			break
		}
	}
	if !hasGoMetric {
		t.Error("Expected go_* metrics from GoCollector")
	}
}

func TestRecordSync(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewSyncerMetrics(registry)

	m.RecordSync(true, 1.5)
	m.RecordSync(false, 0.5)
	m.RecordSync(true, 2.0)

	expected := `
		# HELP redirector_sync_sync_total Total number of sync operations
		# TYPE redirector_sync_sync_total counter
		redirector_sync_sync_total{status="failure"} 1
		redirector_sync_sync_total{status="success"} 2
	`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "redirector_sync_sync_total"); err != nil {
		t.Errorf("Sync total metric mismatch: %v", err)
	}

	// Last sync should be success (value=1)
	expectedSuccess := `
		# HELP redirector_sync_last_sync_success Whether the last sync was successful (1=success, 0=failure)
		# TYPE redirector_sync_last_sync_success gauge
		redirector_sync_last_sync_success 1
	`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expectedSuccess), "redirector_sync_last_sync_success"); err != nil {
		t.Errorf("Last sync success metric mismatch: %v", err)
	}
}

func TestRecordFetch(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewSyncerMetrics(registry)

	m.RecordFetch("github-primary", true, 0.5)
	m.RecordFetch("github-primary", false, 1.0)
	m.RecordFetch("s3-backup", true, 0.3)

	expected := `
		# HELP redirector_sync_fetch_total Total number of source fetch operations
		# TYPE redirector_sync_fetch_total counter
		redirector_sync_fetch_total{source="github-primary",status="failure"} 1
		redirector_sync_fetch_total{source="github-primary",status="success"} 1
		redirector_sync_fetch_total{source="s3-backup",status="success"} 1
	`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "redirector_sync_fetch_total"); err != nil {
		t.Errorf("Fetch total metric mismatch: %v", err)
	}
}

func TestRecordPush(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewSyncerMetrics(registry)

	m.RecordPush("redirector-1", true, 0.1)
	m.RecordPush("redirector-2", false, 5.0)

	expected := `
		# HELP redirector_sync_push_total Total number of config push operations to targets
		# TYPE redirector_sync_push_total counter
		redirector_sync_push_total{status="success",target="redirector-1"} 1
		redirector_sync_push_total{status="failure",target="redirector-2"} 1
	`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "redirector_sync_push_total"); err != nil {
		t.Errorf("Push total metric mismatch: %v", err)
	}
}

func TestRecordLintError(t *testing.T) {
	registry := prometheus.NewRegistry()
	m := NewSyncerMetrics(registry)

	m.RecordLintError("github-primary")
	m.RecordLintError("github-primary")
	m.RecordLintError("s3-backup")

	expected := `
		# HELP redirector_sync_lint_errors_total Total config lint errors encountered during sync
		# TYPE redirector_sync_lint_errors_total counter
		redirector_sync_lint_errors_total{source="github-primary"} 2
		redirector_sync_lint_errors_total{source="s3-backup"} 1
	`
	if err := testutil.GatherAndCompare(registry, strings.NewReader(expected), "redirector_sync_lint_errors_total"); err != nil {
		t.Errorf("Lint errors metric mismatch: %v", err)
	}
}
