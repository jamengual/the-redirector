package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHTTPSource(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, Source)
	}{
		{
			name: "valid minimal config",
			cfg: map[string]interface{}{
				"url": "https://example.com/config.yaml",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				h, ok := s.(*HTTPSource)
				if !ok {
					t.Fatal("expected *HTTPSource")
				}
				if h.url != "https://example.com/config.yaml" {
					t.Errorf("url = %q, want %q", h.url, "https://example.com/config.yaml")
				}
			},
		},
		{
			name: "with bearer token",
			cfg: map[string]interface{}{
				"url":          "https://example.com/config.yaml",
				"bearer_token": "my-secret-token",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				h, _ := s.(*HTTPSource)
				if h.bearerToken != "my-secret-token" {
					t.Errorf("bearerToken = %q, want %q", h.bearerToken, "my-secret-token")
				}
			},
		},
		{
			name: "with basic auth",
			cfg: map[string]interface{}{
				"url": "https://example.com/config.yaml",
				"basic_auth": map[string]interface{}{
					"username": "admin",
					"password": "secret",
				},
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				h, _ := s.(*HTTPSource)
				if h.basicAuth == nil {
					t.Fatal("expected basicAuth to be set")
				}
				if h.basicAuth.Username != "admin" {
					t.Errorf("username = %q, want %q", h.basicAuth.Username, "admin")
				}
				if h.basicAuth.Password != "secret" {
					t.Errorf("password = %q, want %q", h.basicAuth.Password, "secret")
				}
			},
		},
		{
			name: "with custom headers",
			cfg: map[string]interface{}{
				"url": "https://example.com/config.yaml",
				"headers": map[string]interface{}{
					"X-Custom": "value",
				},
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				h, _ := s.(*HTTPSource)
				if h.headers["X-Custom"] != "value" {
					t.Errorf("headers[X-Custom] = %q, want %q", h.headers["X-Custom"], "value")
				}
			},
		},
		{
			name: "with timeout",
			cfg: map[string]interface{}{
				"url":     "https://example.com/config.yaml",
				"timeout": "10s",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				h, _ := s.(*HTTPSource)
				if h.timeout.Seconds() != 10 {
					t.Errorf("timeout = %v, want 10s", h.timeout)
				}
			},
		},
		{
			name:    "missing url",
			cfg:     map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty url",
			cfg: map[string]interface{}{
				"url": "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewHTTPSource(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewHTTPSource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && source != nil {
				tt.check(t, source)
			}
		})
	}
}

func TestHTTPSource_Name(t *testing.T) {
	source, _ := NewHTTPSource(map[string]interface{}{
		"url": "https://example.com/config.yaml",
	})
	if source.Name() != "http" {
		t.Errorf("Name() = %q, want %q", source.Name(), "http")
	}
}

func TestHTTPSource_SupportsWatch(t *testing.T) {
	source, _ := NewHTTPSource(map[string]interface{}{
		"url": "https://example.com/config.yaml",
	})
	if source.SupportsWatch() {
		t.Error("SupportsWatch() = true, want false")
	}
}

func TestHTTPSource_Registry(t *testing.T) {
	factory, ok := Registry.Get("http")
	if !ok {
		t.Fatal("http source not registered")
	}

	source, err := factory(map[string]interface{}{
		"url": "https://example.com/config.yaml",
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if source.Name() != "http" {
		t.Errorf("Name() = %q, want %q", source.Name(), "http")
	}
}

func TestHTTPSource_Validate(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		wantErr     bool
		errContains string
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			wantErr:    false,
		},
		{
			name:        "not found",
			statusCode:  http.StatusNotFound,
			wantErr:     true,
			errContains: "not found",
		},
		{
			name:        "unauthorized",
			statusCode:  http.StatusUnauthorized,
			wantErr:     true,
			errContains: "authentication failed",
		},
		{
			name:        "server error",
			statusCode:  http.StatusInternalServerError,
			wantErr:     true,
			errContains: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "HEAD" {
					t.Errorf("expected HEAD request, got %s", r.Method)
				}
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			source := &HTTPSource{
				url:    server.URL,
				client: server.Client(),
			}

			err := source.Validate(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errContains != "" && err != nil {
				if !contains(err.Error(), tt.errContains) {
					t.Errorf("error %q should contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestHTTPSource_Fetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET request, got %s", r.Method)
		}
		w.Header().Set("ETag", `"abc123"`)
		w.Write([]byte(validTestConfig))
	}))
	defer server.Close()

	source := &HTTPSource{
		url:     server.URL,
		client:  server.Client(),
		options: DefaultSourceOptions(),
	}

	cfg, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Fetch() returned nil config")
	}

	// Verify ETag was stored
	if source.lastETag != `"abc123"` {
		t.Errorf("lastETag = %q, want %q", source.lastETag, `"abc123"`)
	}
}

func TestHTTPSource_Fetch_WithBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer my-token")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(validTestConfig))
	}))
	defer server.Close()

	source := &HTTPSource{
		url:         server.URL,
		bearerToken: "my-token",
		client:      server.Client(),
		options:     DefaultSourceOptions(),
	}

	cfg, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Fetch() returned nil config")
	}
}

func TestHTTPSource_Fetch_WithBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(validTestConfig))
	}))
	defer server.Close()

	source := &HTTPSource{
		url: server.URL,
		basicAuth: &HTTPBasicAuth{
			Username: "admin",
			Password: "secret",
		},
		client:  server.Client(),
		options: DefaultSourceOptions(),
	}

	cfg, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Fetch() returned nil config")
	}
}

func TestHTTPSource_Fetch_WithCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "myvalue" {
			t.Errorf("X-Custom = %q, want %q", r.Header.Get("X-Custom"), "myvalue")
		}
		w.Write([]byte(validTestConfig))
	}))
	defer server.Close()

	source := &HTTPSource{
		url:     server.URL,
		headers: map[string]string{"X-Custom": "myvalue"},
		client:  server.Client(),
		options: DefaultSourceOptions(),
	}

	_, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestHTTPSource_Fetch_NotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"etag1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write([]byte(validTestConfig))
	}))
	defer server.Close()

	source := &HTTPSource{
		url:      server.URL,
		lastETag: `"etag1"`,
		client:   server.Client(),
		options:  DefaultSourceOptions(),
	}

	_, err := source.Fetch(context.Background())
	if err == nil {
		t.Error("expected error for not modified response")
	}
}

func TestHTTPSource_Close(t *testing.T) {
	source := &HTTPSource{}
	if err := source.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}
