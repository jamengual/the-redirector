package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// TracerName is the instrumentation name for the tracer.
	TracerName = "github.com/jamengual/the-redirector"
)

// Config holds tracing configuration.
type Config struct {
	Enabled      bool    `yaml:"enabled"`
	Endpoint     string  `yaml:"endpoint"`       // OTLP endpoint (e.g., "localhost:4317")
	ServiceName  string  `yaml:"service_name"`   // Service name (default: "the-redirector")
	Environment  string  `yaml:"environment"`    // Deployment environment
	SamplingRate float64 `yaml:"sampling_rate"`  // Sampling rate (0.0-1.0, default: 1.0)
	Insecure     bool    `yaml:"insecure"`       // Use insecure connection (no TLS)
}

// DefaultConfig returns default tracing configuration.
func DefaultConfig() *Config {
	return &Config{
		Enabled:      false,
		ServiceName:  "the-redirector",
		SamplingRate: 1.0,
		Insecure:     true,
	}
}

// Provider manages the OpenTelemetry tracer provider.
type Provider struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	config   *Config
}

// NewProvider creates a new tracing provider.
func NewProvider(ctx context.Context, cfg *Config) (*Provider, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if !cfg.Enabled {
		// Return a no-op provider
		return &Provider{
			tracer: otel.Tracer(TracerName),
			config: cfg,
		}, nil
	}

	// Build exporter options
	opts := []otlptracegrpc.Option{}
	if cfg.Endpoint != "" {
		opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Endpoint))
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	// Create OTLP exporter
	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating OTLP exporter: %w", err)
	}

	// Build resource with service information
	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "the-redirector"
	}

	// Create resource with service attributes (avoid Merge to prevent schema conflicts)
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion("1.0.0"),
		attribute.String("environment", cfg.Environment),
	)

	// Configure sampler
	var sampler sdktrace.Sampler
	if cfg.SamplingRate <= 0 {
		sampler = sdktrace.NeverSample()
	} else if cfg.SamplingRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(cfg.SamplingRate)
	}

	// Create tracer provider
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global tracer provider and propagator
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(autoprop.NewTextMapPropagator())

	return &Provider{
		provider: provider,
		tracer:   provider.Tracer(TracerName),
		config:   cfg,
	}, nil
}

// Tracer returns the tracer instance.
func (p *Provider) Tracer() trace.Tracer {
	return p.tracer
}

// IsEnabled returns whether tracing is enabled.
func (p *Provider) IsEnabled() bool {
	return p.config != nil && p.config.Enabled
}

// Shutdown gracefully shuts down the tracer provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.provider == nil {
		return nil
	}
	return p.provider.Shutdown(ctx)
}

// Span attributes for common operations.
var (
	AttrRuleID       = attribute.Key("redirector.rule.id")
	AttrMatchType    = attribute.Key("redirector.match.type")
	AttrPath         = attribute.Key("redirector.path")
	AttrHost         = attribute.Key("redirector.host")
	AttrDestination  = attribute.Key("redirector.destination")
	AttrStatusCode   = attribute.Key("redirector.status_code")
	AttrClientIP     = attribute.Key("redirector.client.ip")
	AttrMethod       = attribute.Key("http.method")
	AttrConfigSource = attribute.Key("redirector.config.source")
	AttrVersion      = attribute.Key("redirector.config.version")
	AttrRulesCount   = attribute.Key("redirector.config.rules_count")
)

// StartSpan starts a new span with the given name and options.
func (p *Provider) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return p.tracer.Start(ctx, name, opts...)
}

// StartRequestSpan starts a span for an HTTP request.
func (p *Provider) StartRequestSpan(ctx context.Context, method, path, host string) (context.Context, trace.Span) {
	return p.tracer.Start(ctx, "handle_request",
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			AttrMethod.String(method),
			AttrPath.String(path),
			AttrHost.String(host),
		),
	)
}

// RecordRuleMatch records a rule match on the span.
func RecordRuleMatch(span trace.Span, ruleID, matchType, destination string, statusCode int) {
	span.SetAttributes(
		AttrRuleID.String(ruleID),
		AttrMatchType.String(matchType),
		AttrDestination.String(destination),
		AttrStatusCode.Int(statusCode),
	)
}

// RecordNoMatch records that no rule matched.
func RecordNoMatch(span trace.Span) {
	span.SetAttributes(
		AttrStatusCode.Int(404),
		attribute.Bool("redirector.matched", false),
	)
}

// StartConfigReloadSpan starts a span for config reload.
func (p *Provider) StartConfigReloadSpan(ctx context.Context, source string) (context.Context, trace.Span) {
	return p.tracer.Start(ctx, "config_reload",
		trace.WithAttributes(
			AttrConfigSource.String(source),
		),
	)
}

// RecordConfigReload records config reload completion.
func RecordConfigReload(span trace.Span, version int, rulesCount int, duration time.Duration) {
	span.SetAttributes(
		AttrVersion.Int(version),
		AttrRulesCount.Int(rulesCount),
		attribute.Int64("redirector.config.reload_duration_ms", duration.Milliseconds()),
	)
}

// SpanFromContext extracts the current span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}
