package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jamengual/the-redirector/internal/config"
)

func TestNewGitLabSource(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, Source)
	}{
		{
			name: "valid minimal config",
			cfg: map[string]interface{}{
				"project": "mygroup/myproject",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				gl, _ := s.(*GitLabSource)
				if gl.projectPath != "mygroup/myproject" {
					t.Errorf("projectPath = %q, want %q", gl.projectPath, "mygroup/myproject")
				}
				if gl.filePath != "config.yaml" {
					t.Errorf("filePath = %q, want %q", gl.filePath, "config.yaml")
				}
				if gl.baseURL != "https://gitlab.com" {
					t.Errorf("baseURL = %q, want %q", gl.baseURL, "https://gitlab.com")
				}
			},
		},
		{
			name: "with custom base URL",
			cfg: map[string]interface{}{
				"project":  "mygroup/myproject",
				"base_url": "https://gitlab.example.com",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				gl, _ := s.(*GitLabSource)
				if gl.baseURL != "https://gitlab.example.com" {
					t.Errorf("baseURL = %q, want %q", gl.baseURL, "https://gitlab.example.com")
				}
			},
		},
		{
			name: "with token auth",
			cfg: map[string]interface{}{
				"project": "mygroup/myproject",
				"token":   "glpat-xxxxxxxxxxxx",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				gl, _ := s.(*GitLabSource)
				if gl.auth == nil {
					t.Error("expected auth to be set")
				}
				tokenAuth, ok := gl.auth.(*GitLabTokenAuth)
				if !ok {
					t.Error("expected GitLabTokenAuth")
				}
				if tokenAuth.Token != "glpat-xxxxxxxxxxxx" {
					t.Errorf("token = %q, want %q", tokenAuth.Token, "glpat-xxxxxxxxxxxx")
				}
			},
		},
		{
			name: "with oauth token type",
			cfg: map[string]interface{}{
				"project":    "mygroup/myproject",
				"token":      "oauth-token",
				"token_type": "oauth",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				gl, _ := s.(*GitLabSource)
				tokenAuth, _ := gl.auth.(*GitLabTokenAuth)
				if tokenAuth.TokenType != "oauth" {
					t.Errorf("tokenType = %q, want %q", tokenAuth.TokenType, "oauth")
				}
			},
		},
		{
			name: "with custom path and strategy",
			cfg: map[string]interface{}{
				"project":     "mygroup/myproject",
				"path":        "redirects/config.yaml",
				"strategy":    "branch",
				"environment": "staging",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				gl, _ := s.(*GitLabSource)
				if gl.filePath != "redirects/config.yaml" {
					t.Errorf("filePath = %q, want %q", gl.filePath, "redirects/config.yaml")
				}
				if gl.strategy != StrategyBranch {
					t.Errorf("strategy = %q, want %q", gl.strategy, StrategyBranch)
				}
				if gl.environment != "staging" {
					t.Errorf("environment = %q, want %q", gl.environment, "staging")
				}
			},
		},
		{
			name:    "missing project",
			cfg:     map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty project",
			cfg: map[string]interface{}{
				"project": "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewGitLabSource(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewGitLabSource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && source != nil {
				tt.check(t, source)
			}
		})
	}
}

func TestGitLabSource_Name(t *testing.T) {
	source, _ := NewGitLabSource(map[string]interface{}{
		"project": "test/project",
	})

	if source.Name() != "gitlab" {
		t.Errorf("Name() = %q, want %q", source.Name(), "gitlab")
	}
}

func TestGitLabSource_SupportsWatch(t *testing.T) {
	source, _ := NewGitLabSource(map[string]interface{}{
		"project": "test/project",
	})

	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestGitLabSource_environmentToBranch(t *testing.T) {
	tests := []struct {
		environment string
		want        string
	}{
		{"production", "main"},
		{"staging", "staging"},
		{"development", "develop"},
		{"custom-branch", "custom-branch"},
	}

	for _, tt := range tests {
		t.Run(tt.environment, func(t *testing.T) {
			source := &GitLabSource{environment: tt.environment}
			if got := source.environmentToBranch(); got != tt.want {
				t.Errorf("environmentToBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGitLabSource_apiURL(t *testing.T) {
	source := &GitLabSource{
		baseURL:     "https://gitlab.example.com",
		projectPath: "mygroup/myproject",
	}

	tests := []struct {
		path string
		want string
	}{
		{"/releases", "https://gitlab.example.com/api/v4/projects/mygroup%2Fmyproject/releases"},
		{"/repository/tags", "https://gitlab.example.com/api/v4/projects/mygroup%2Fmyproject/repository/tags"},
		{"", "https://gitlab.example.com/api/v4/projects/mygroup%2Fmyproject"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := source.apiURL(tt.path)
			if got != tt.want {
				t.Errorf("apiURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestGitLabTokenAuth_AddAuth(t *testing.T) {
	tests := []struct {
		name       string
		auth       GitLabTokenAuth
		wantHeader string
		wantValue  string
	}{
		{
			name: "private token (default)",
			auth: GitLabTokenAuth{
				Token: "glpat-test",
			},
			wantHeader: "PRIVATE-TOKEN",
			wantValue:  "glpat-test",
		},
		{
			name: "oauth token",
			auth: GitLabTokenAuth{
				Token:     "oauth-test",
				TokenType: "oauth",
			},
			wantHeader: "Authorization",
			wantValue:  "Bearer oauth-test",
		},
		{
			name: "empty token",
			auth: GitLabTokenAuth{
				Token: "",
			},
			wantHeader: "",
			wantValue:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, reqErr := http.NewRequestWithContext(context.Background(), "GET", "https://example.com", nil)
			if reqErr != nil {
				t.Fatalf("Failed to create request: %v", reqErr)
			}
			err := tt.auth.AddAuth(req)
			if err != nil {
				t.Fatalf("AddAuth() error = %v", err)
			}

			if tt.wantHeader == "" {
				return // No header expected
			}

			got := req.Header.Get(tt.wantHeader)
			if got != tt.wantValue {
				t.Errorf("header %q = %q, want %q", tt.wantHeader, got, tt.wantValue)
			}
		})
	}
}

func TestGitLabSource_Validate(t *testing.T) {
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
			name:        "forbidden",
			statusCode:  http.StatusForbidden,
			wantErr:     true,
			errContains: "access forbidden",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			source := &GitLabSource{
				baseURL:     server.URL,
				projectPath: "test/project",
				client:      server.Client(),
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

func TestGitLabSource_HandleWebhook(t *testing.T) {
	tests := []struct {
		name      string
		strategy  DeploymentStrategy
		event     string
		payload   interface{}
		wantFetch bool
	}{
		{
			name:     "release event with release strategy",
			strategy: StrategyRelease,
			event:    "Release Hook",
			payload: map[string]interface{}{
				"action": "create",
				"tag":    "v1.0.0",
			},
			wantFetch: true,
		},
		{
			name:     "release event with branch strategy (ignored)",
			strategy: StrategyBranch,
			event:    "Release Hook",
			payload: map[string]interface{}{
				"action": "create",
				"tag":    "v1.0.0",
			},
			wantFetch: false,
		},
		{
			name:     "push event with branch strategy",
			strategy: StrategyBranch,
			event:    "Push Hook",
			payload: map[string]interface{}{
				"ref": "refs/heads/main",
			},
			wantFetch: true,
		},
		{
			name:     "push event wrong branch",
			strategy: StrategyBranch,
			event:    "Push Hook",
			payload: map[string]interface{}{
				"ref": "refs/heads/feature",
			},
			wantFetch: false,
		},
		{
			name:     "tag event with tag strategy",
			strategy: StrategyTag,
			event:    "Tag Push Hook",
			payload: map[string]interface{}{
				"ref": "refs/tags/v1.0.0",
			},
			wantFetch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetchCalled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fetchCalled = true
				// Return minimal valid config
				_, _ = w.Write([]byte(`version: "1.0"`))
			}))
			defer server.Close()

			source := &GitLabSource{
				baseURL:     server.URL,
				projectPath: "test/project",
				filePath:    "config.yaml",
				strategy:    tt.strategy,
				environment: "production",
				client:      server.Client(),
				webhookChan: make(chan *config.Config, 1),
				stopCh:      make(chan struct{}),
			}

			payload, _ := json.Marshal(tt.payload)
			_ = source.HandleWebhook(context.Background(), tt.event, payload)

			// Note: HandleWebhook calls Fetch which may fail, but we're checking if it was called
			if fetchCalled != tt.wantFetch {
				t.Errorf("Fetch called = %v, want %v", fetchCalled, tt.wantFetch)
			}
		})
	}
}

func TestNewGitLabSource_TagPattern(t *testing.T) {
	source, err := NewGitLabSource(map[string]interface{}{
		"project":     "mygroup/myproject",
		"strategy":    "tag",
		"tag_pattern": "v*",
	})
	if err != nil {
		t.Fatalf("NewGitLabSource() error = %v", err)
	}
	gl, _ := source.(*GitLabSource)
	if gl.tagPattern != "v*" {
		t.Errorf("tagPattern = %q, want %q", gl.tagPattern, "v*")
	}
}

func TestGitLabSource_Fetch_TagStrategy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case contains(r.URL.Path, "/repository/tags"):
			_ = json.NewEncoder(w).Encode([]map[string]interface{}{
				{"name": "config-2.0", "commit": map[string]interface{}{"id": "def456"}},
				{"name": "v1.0.0", "commit": map[string]interface{}{"id": "abc123"}},
				{"name": "config-1.0", "commit": map[string]interface{}{"id": "aaa111"}},
			})
		case contains(r.URL.Path, "/repository/files"):
			_, _ = w.Write([]byte(validTestConfig))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	source := &GitLabSource{
		projectPath: "test/project",
		filePath:    "config.yaml",
		strategy:    StrategyTag,
		tagPattern:  "config-*",
		baseURL:     server.URL,
		client:      server.Client(),
		webhookChan: make(chan *config.Config, 1),
		stopCh:      make(chan struct{}),
	}

	cfg, err := source.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("Fetch() returned nil config")
	}
	// Should have matched "config-2.0", not "v1.0.0"
	if source.currentRef != "config-2.0" {
		t.Errorf("currentRef = %q, want %q", source.currentRef, "config-2.0")
	}
}

func TestGitLabSource_Fetch_TagStrategy_NoMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "v1.0.0", "commit": map[string]interface{}{"id": "abc123"}},
		})
	}))
	defer server.Close()

	source := &GitLabSource{
		projectPath: "test/project",
		filePath:    "config.yaml",
		strategy:    StrategyTag,
		tagPattern:  "release-*",
		baseURL:     server.URL,
		client:      server.Client(),
		webhookChan: make(chan *config.Config, 1),
		stopCh:      make(chan struct{}),
	}

	_, err := source.Fetch(context.Background())
	if err == nil {
		t.Error("expected error when no tags match pattern")
	}
	if !contains(err.Error(), "no tags matching pattern") {
		t.Errorf("error %q should contain %q", err.Error(), "no tags matching pattern")
	}
}

func TestGitLabSource_HandleWebhook_TagPattern(t *testing.T) {
	tests := []struct {
		name       string
		tagPattern string
		tagRef     string
		wantFetch  bool
	}{
		{
			name:       "matching pattern",
			tagPattern: "v*",
			tagRef:     "refs/tags/v1.0.0",
			wantFetch:  true,
		},
		{
			name:       "non-matching pattern",
			tagPattern: "v*",
			tagRef:     "refs/tags/release-1.0.0",
			wantFetch:  false,
		},
		{
			name:       "no pattern set (matches all)",
			tagPattern: "",
			tagRef:     "refs/tags/anything",
			wantFetch:  true,
		},
		{
			name:       "complex pattern",
			tagPattern: "config-[0-9]*",
			tagRef:     "refs/tags/config-2",
			wantFetch:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetchCalled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fetchCalled = true
				switch {
				case contains(r.URL.Path, "/repository/tags"):
					_ = json.NewEncoder(w).Encode([]map[string]interface{}{
						{"name": "v1.0.0", "commit": map[string]interface{}{"id": "abc123"}},
					})
				case contains(r.URL.Path, "/repository/files"):
					_, _ = w.Write([]byte(validTestConfig))
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()

			source := &GitLabSource{
				projectPath: "test/project",
				filePath:    "config.yaml",
				strategy:    StrategyTag,
				tagPattern:  tt.tagPattern,
				environment: "production",
				baseURL:     server.URL,
				client:      server.Client(),
				webhookChan: make(chan *config.Config, 1),
				stopCh:      make(chan struct{}),
			}

			payload, _ := json.Marshal(map[string]interface{}{
				"ref": tt.tagRef,
			})
			_ = source.HandleWebhook(context.Background(), "Tag Push Hook", payload)

			if fetchCalled != tt.wantFetch {
				t.Errorf("Fetch called = %v, want %v", fetchCalled, tt.wantFetch)
			}
		})
	}
}

func TestGitLabOAuthAuth_RefreshToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", r.Form.Get("grant_type"))
		}
		if r.Form.Get("refresh_token") != "old-refresh-token" {
			t.Errorf("refresh_token = %q, want old-refresh-token", r.Form.Get("refresh_token"))
		}
		if r.Form.Get("client_id") != "my-client-id" {
			t.Errorf("client_id = %q, want my-client-id", r.Form.Get("client_id"))
		}
		if r.Form.Get("client_secret") != "my-client-secret" {
			t.Errorf("client_secret = %q, want my-client-secret", r.Form.Get("client_secret"))
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "new-access-token",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}))
	defer server.Close()

	auth := &GitLabOAuthAuth{
		AccessToken:  "old-access-token",
		RefreshToken: "old-refresh-token",
		ClientID:     "my-client-id",
		ClientSecret: "my-client-secret",
		TokenURL:     server.URL,
		expiresAt:    time.Now().Add(-1 * time.Hour), // expired
	}

	err := auth.refreshToken(context.Background())
	if err != nil {
		t.Fatalf("refreshToken() error = %v", err)
	}

	if auth.AccessToken != "new-access-token" {
		t.Errorf("AccessToken = %q, want %q", auth.AccessToken, "new-access-token")
	}
	if auth.RefreshToken != "new-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", auth.RefreshToken, "new-refresh-token")
	}
	if auth.expiresAt.Before(time.Now().Add(50 * time.Minute)) {
		t.Errorf("expiresAt should be ~1 hour from now, got %v", auth.expiresAt)
	}
}

func TestGitLabOAuthAuth_RefreshToken_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	auth := &GitLabOAuthAuth{
		RefreshToken: "bad-token",
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
	}

	err := auth.refreshToken(context.Background())
	if err == nil {
		t.Error("expected error for server error response")
	}
	if !contains(err.Error(), "400") {
		t.Errorf("error %q should contain status code 400", err.Error())
	}
}

func TestGitLabOAuthAuth_AddAuth_TriggersRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "refreshed-token",
			"refresh_token": "new-refresh",
			"expires_in":    7200,
			"token_type":    "Bearer",
		})
	}))
	defer server.Close()

	auth := &GitLabOAuthAuth{
		AccessToken:  "expired-token",
		RefreshToken: "my-refresh",
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
		expiresAt:    time.Now().Add(-1 * time.Minute), // expired
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "https://example.com", nil)
	err := auth.AddAuth(req)
	if err != nil {
		t.Fatalf("AddAuth() error = %v", err)
	}

	// Should have refreshed and set new token
	if auth.AccessToken != "refreshed-token" {
		t.Errorf("AccessToken = %q, want %q", auth.AccessToken, "refreshed-token")
	}
	// Authorization header should use the refreshed token
	if got := req.Header.Get("Authorization"); got != "Bearer refreshed-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer refreshed-token")
	}
}

func TestGitLabOAuthAuth_RefreshToken_KeepsOldRefreshIfNotReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "new-access",
			"expires_in":   3600,
			"token_type":   "Bearer",
			// No refresh_token in response
		})
	}))
	defer server.Close()

	auth := &GitLabOAuthAuth{
		AccessToken:  "old-access",
		RefreshToken: "keep-this-refresh",
		ClientID:     "client",
		ClientSecret: "secret",
		TokenURL:     server.URL,
	}

	err := auth.refreshToken(context.Background())
	if err != nil {
		t.Fatalf("refreshToken() error = %v", err)
	}

	if auth.RefreshToken != "keep-this-refresh" {
		t.Errorf("RefreshToken = %q, want %q (should keep old refresh token)", auth.RefreshToken, "keep-this-refresh")
	}
}

func TestGitLabSource_Registry(t *testing.T) {
	// Verify GitLab source is registered
	factory, ok := Registry.Get("gitlab")
	if !ok {
		t.Fatal("gitlab source not registered")
	}

	source, err := factory(map[string]interface{}{
		"project": "test/project",
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if source.Name() != "gitlab" {
		t.Errorf("Name() = %q, want %q", source.Name(), "gitlab")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
