// Package providers implements configuration sources.
// This file implements HTTP/HTTPS endpoint integration.
package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jamengual/the-redirector/internal/config"
)

func init() {
	Registry.Register("http", NewHTTPSource)
}

// HTTPSource reads configuration from an HTTP/HTTPS endpoint.
type HTTPSource struct {
	url     string
	headers map[string]string
	timeout time.Duration

	// Auth
	basicAuth   *HTTPBasicAuth
	bearerToken string

	// Options
	options SourceOptions

	// HTTP client
	client *http.Client

	// ETag caching
	lastETag string
}

// HTTPBasicAuth configures HTTP basic authentication.
type HTTPBasicAuth struct {
	Username string
	Password string
}

// HTTPSourceConfig configures the HTTP source.
type HTTPSourceConfig struct {
	URL         string            `yaml:"url" json:"url"`
	Headers     map[string]string `yaml:"headers" json:"headers"`
	Timeout     time.Duration     `yaml:"timeout" json:"timeout"`
	BasicAuth   *HTTPBasicAuth    `yaml:"basic_auth" json:"basic_auth"`
	BearerToken string            `yaml:"bearer_token" json:"bearer_token"`
}

// NewHTTPSource creates a new HTTP configuration source from a config map.
func NewHTTPSource(cfg map[string]interface{}) (Source, error) {
	url, ok := cfg["url"].(string)
	if !ok || url == "" {
		return nil, fmt.Errorf("http source requires 'url'")
	}

	timeout := 30 * time.Second
	if t, ok := cfg["timeout"].(string); ok {
		if d, err := time.ParseDuration(t); err == nil {
			timeout = d
		}
	}

	source := &HTTPSource{
		url:     url,
		timeout: timeout,
		options: DefaultSourceOptions(),
		client:  &http.Client{Timeout: timeout},
	}

	if headers, ok := cfg["headers"].(map[string]interface{}); ok {
		source.headers = make(map[string]string, len(headers))
		for k, v := range headers {
			if s, ok := v.(string); ok {
				source.headers[k] = s
			}
		}
	}

	if bearerToken, ok := cfg["bearer_token"].(string); ok && bearerToken != "" {
		source.bearerToken = bearerToken
	}

	if basicAuth, ok := cfg["basic_auth"].(map[string]interface{}); ok {
		username, _ := basicAuth["username"].(string)
		password, _ := basicAuth["password"].(string)
		if username != "" {
			source.basicAuth = &HTTPBasicAuth{
				Username: username,
				Password: password,
			}
		}
	}

	if interval, ok := cfg["poll_interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			source.options.PollInterval = d
		}
	}

	return source, nil
}

// Name returns the source type identifier.
func (h *HTTPSource) Name() string {
	return "http"
}

// Fetch retrieves the configuration from the HTTP endpoint.
func (h *HTTPSource) Fetch(ctx context.Context) (*config.Config, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", h.url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	h.addHeaders(req)

	if h.lastETag != "" {
		req.Header.Set("If-None-Match", h.lastETag)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching config from %s: %w", h.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return nil, fmt.Errorf("config not modified")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, h.url, string(body))
	}

	if etag := resp.Header.Get("ETag"); etag != "" {
		h.lastETag = etag
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	cfg, err := config.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

// addHeaders adds authentication and custom headers to the request.
func (h *HTTPSource) addHeaders(req *http.Request) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}

	if h.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.bearerToken)
	}

	if h.basicAuth != nil {
		req.SetBasicAuth(h.basicAuth.Username, h.basicAuth.Password)
	}
}

// Watch polls the HTTP endpoint for changes.
func (h *HTTPSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	updates := make(chan *config.Config, 1)

	go func() {
		defer close(updates)

		ticker := time.NewTicker(h.options.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cfg, err := h.Fetch(ctx)
				if err == nil {
					select {
					case updates <- cfg:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return updates, nil
}

// SupportsWatch returns false — HTTP uses polling, not real-time push.
func (h *HTTPSource) SupportsWatch() bool {
	return false
}

// Validate checks that the HTTP endpoint is accessible.
func (h *HTTPSource) Validate(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "HEAD", h.url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	h.addHeaders(req)

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("accessing %s: %w", h.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed for %s", h.url)
	}

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("endpoint not found: %s", h.url)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, h.url)
	}

	return nil
}

// Close releases any resources.
func (h *HTTPSource) Close() error {
	return nil
}
