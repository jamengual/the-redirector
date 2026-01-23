package tracing

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("Default config should be disabled")
	}
	if cfg.ServiceName != "the-redirector" {
		t.Errorf("Expected service name 'the-redirector', got %s", cfg.ServiceName)
	}
	if cfg.SamplingRate != 1.0 {
		t.Errorf("Expected sampling rate 1.0, got %f", cfg.SamplingRate)
	}
}

func TestNewProviderDisabled(t *testing.T) {
	ctx := context.Background()

	// Test with nil config (should create no-op provider)
	p, err := NewProvider(ctx, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p.IsEnabled() {
		t.Error("Provider with nil config should be disabled")
	}

	// Test with disabled config
	cfg := &Config{Enabled: false}
	p2, err := NewProvider(ctx, cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if p2.IsEnabled() {
		t.Error("Provider with disabled config should be disabled")
	}

	// Tracer should still work (no-op)
	tracer := p2.Tracer()
	if tracer == nil {
		t.Error("Tracer should not be nil even when disabled")
	}
}

func TestNewProviderEnabled(t *testing.T) {
	ctx := context.Background()

	cfg := &Config{
		Enabled:      true,
		Endpoint:     "localhost:4317",
		ServiceName:  "test-service",
		Environment:  "test",
		SamplingRate: 0.5,
		Insecure:     true,
	}

	p, err := NewProvider(ctx, cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer p.Shutdown(ctx)

	if !p.IsEnabled() {
		t.Error("Provider should be enabled")
	}

	tracer := p.Tracer()
	if tracer == nil {
		t.Error("Tracer should not be nil")
	}
}

func TestStartRequestSpan(t *testing.T) {
	ctx := context.Background()

	// Use disabled provider
	p, _ := NewProvider(ctx, nil)

	spanCtx, span := p.StartRequestSpan(ctx, "GET", "/test", "example.com")
	if span == nil {
		t.Error("Span should not be nil")
	}
	if spanCtx == nil {
		t.Error("Context should not be nil")
	}
	span.End()
}

func TestStartConfigReloadSpan(t *testing.T) {
	ctx := context.Background()

	p, _ := NewProvider(ctx, nil)

	spanCtx, span := p.StartConfigReloadSpan(ctx, "/etc/config.yaml")
	if span == nil {
		t.Error("Span should not be nil")
	}
	if spanCtx == nil {
		t.Error("Context should not be nil")
	}
	span.End()
}

func TestRecordRuleMatch(t *testing.T) {
	ctx := context.Background()
	p, _ := NewProvider(ctx, nil)

	_, span := p.StartRequestSpan(ctx, "GET", "/test", "example.com")
	defer span.End()

	// Should not panic
	RecordRuleMatch(span, "rule-1", "exact", "https://example.com", 301)
}

func TestRecordNoMatch(t *testing.T) {
	ctx := context.Background()
	p, _ := NewProvider(ctx, nil)

	_, span := p.StartRequestSpan(ctx, "GET", "/notfound", "example.com")
	defer span.End()

	// Should not panic
	RecordNoMatch(span)
}

func TestRecordConfigReload(t *testing.T) {
	ctx := context.Background()
	p, _ := NewProvider(ctx, nil)

	_, span := p.StartConfigReloadSpan(ctx, "/etc/config.yaml")
	defer span.End()

	// Should not panic
	RecordConfigReload(span, 5, 100, time.Millisecond*150)
}

func TestSpanFromContext(t *testing.T) {
	ctx := context.Background()
	p, _ := NewProvider(ctx, nil)

	spanCtx, span := p.StartRequestSpan(ctx, "GET", "/test", "example.com")
	defer span.End()

	extracted := SpanFromContext(spanCtx)
	if extracted == nil {
		t.Error("Should extract span from context")
	}

	// For no-op tracer, span context may not be valid but should not panic
	_ = extracted.SpanContext()
}

func TestProviderShutdown(t *testing.T) {
	ctx := context.Background()

	// Test shutdown with nil provider (disabled)
	p, _ := NewProvider(ctx, nil)
	err := p.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown should not error for disabled provider: %v", err)
	}
}

func TestSamplingRateEdgeCases(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		samplingRate float64
	}{
		{"zero", 0.0},
		{"negative", -1.0},
		{"one", 1.0},
		{"greater than one", 2.0},
		{"fractional", 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Enabled:      true,
				Endpoint:     "localhost:4317",
				SamplingRate: tt.samplingRate,
				Insecure:     true,
			}

			p, err := NewProvider(ctx, cfg)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			defer p.Shutdown(ctx)

			// Should create provider without panic
			if !p.IsEnabled() {
				t.Error("Provider should be enabled")
			}
		})
	}
}

func TestStartSpan(t *testing.T) {
	ctx := context.Background()
	p, _ := NewProvider(ctx, nil)

	spanCtx, span := p.StartSpan(ctx, "custom_operation",
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	if span == nil {
		t.Error("Span should not be nil")
	}
	if spanCtx == nil {
		t.Error("Context should not be nil")
	}
	span.End()
}
