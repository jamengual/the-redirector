package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
				gl := s.(*GitLabSource)
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
				gl := s.(*GitLabSource)
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
				gl := s.(*GitLabSource)
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
				gl := s.(*GitLabSource)
				tokenAuth := gl.auth.(*GitLabTokenAuth)
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
				gl := s.(*GitLabSource)
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
			req, _ := http.NewRequest("GET", "https://example.com", nil)
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
		name       string
		statusCode int
		wantErr    bool
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
		name     string
		strategy DeploymentStrategy
		event    string
		payload  interface{}
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
				w.Write([]byte(`version: "1.0"`))
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
