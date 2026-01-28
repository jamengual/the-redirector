package providers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jamengual/the-redirector/internal/config"
)

// validTestConfig is a minimal valid YAML config for testing Fetch methods.
const validTestConfig = `version: "1.0"
rules:
  - id: test-rule
    match:
      type: exact
      path: /test
    redirect:
      to: https://example.com
      status: 301
`

func TestNewGitHubSource(t *testing.T) {
	tests := []struct {
		name    string
		cfg     map[string]interface{}
		wantErr bool
		check   func(*testing.T, Source)
	}{
		{
			name: "valid minimal config",
			cfg: map[string]interface{}{
				"repository": "myorg/myrepo",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				gh, _ := s.(*GitHubSource)
				if gh.owner != "myorg" {
					t.Errorf("owner = %q, want %q", gh.owner, "myorg")
				}
				if gh.repo != "myrepo" {
					t.Errorf("repo = %q, want %q", gh.repo, "myrepo")
				}
				if gh.path != "config.yaml" {
					t.Errorf("path = %q, want %q", gh.path, "config.yaml")
				}
				if gh.strategy != StrategyRelease {
					t.Errorf("strategy = %q, want %q", gh.strategy, StrategyRelease)
				}
				if gh.environment != "production" {
					t.Errorf("environment = %q, want %q", gh.environment, "production")
				}
			},
		},
		{
			name: "with PAT token",
			cfg: map[string]interface{}{
				"repository": "myorg/myrepo",
				"token":      "ghp_xxxxxxxxxxxx",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				gh, _ := s.(*GitHubSource)
				if gh.auth == nil {
					t.Fatal("expected auth to be set")
				}
				patAuth, ok := gh.auth.(*GitHubPATAuth)
				if !ok {
					t.Fatal("expected GitHubPATAuth")
				}
				if patAuth.Token != "ghp_xxxxxxxxxxxx" {
					t.Errorf("token = %q, want %q", patAuth.Token, "ghp_xxxxxxxxxxxx")
				}
			},
		},
		{
			name: "with GitHub App config",
			cfg: map[string]interface{}{
				"repository": "myorg/myrepo",
				"app": map[string]interface{}{
					"app_id":          float64(12345),
					"installation_id": float64(67890),
				},
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				gh, _ := s.(*GitHubSource)
				if gh.auth == nil {
					t.Fatal("expected auth to be set")
				}
				appAuth, ok := gh.auth.(*GitHubAppAuth)
				if !ok {
					t.Fatal("expected GitHubAppAuth")
				}
				if appAuth.AppID != 12345 {
					t.Errorf("AppID = %d, want %d", appAuth.AppID, 12345)
				}
				if appAuth.InstallationID != 67890 {
					t.Errorf("InstallationID = %d, want %d", appAuth.InstallationID, 67890)
				}
			},
		},
		{
			name: "with strategy and environment",
			cfg: map[string]interface{}{
				"repository":  "myorg/myrepo",
				"strategy":    "branch",
				"environment": "staging",
				"path":        "redirects/config.yaml",
			},
			wantErr: false,
			check: func(t *testing.T, s Source) {
				gh, _ := s.(*GitHubSource)
				if gh.strategy != StrategyBranch {
					t.Errorf("strategy = %q, want %q", gh.strategy, StrategyBranch)
				}
				if gh.environment != "staging" {
					t.Errorf("environment = %q, want %q", gh.environment, "staging")
				}
				if gh.path != "redirects/config.yaml" {
					t.Errorf("path = %q, want %q", gh.path, "redirects/config.yaml")
				}
			},
		},
		{
			name:    "missing repository",
			cfg:     map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "empty repository",
			cfg: map[string]interface{}{
				"repository": "",
			},
			wantErr: true,
		},
		{
			name: "invalid repository format",
			cfg: map[string]interface{}{
				"repository": "noslash",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewGitHubSource(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewGitHubSource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && source != nil {
				tt.check(t, source)
			}
		})
	}
}

func TestGitHubPATAuth_GetToken(t *testing.T) {
	auth := &GitHubPATAuth{Token: "ghp_test_token"}
	token, err := auth.GetToken(context.Background())
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}
	if token != "ghp_test_token" {
		t.Errorf("GetToken() = %q, want %q", token, "ghp_test_token")
	}
}

func TestGitHubPATAuth_GetToken_Empty(t *testing.T) {
	auth := &GitHubPATAuth{Token: ""}
	_, err := auth.GetToken(context.Background())
	if err == nil {
		t.Error("GetToken() expected error for empty token")
	}
}

func TestGitHubAppAuth_GetToken(t *testing.T) {
	// Generate test RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}

	expiresAt := time.Now().Add(1 * time.Hour)

	// Mock GitHub API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a POST to the correct path
		expectedPath := "/app/installations/67890/access_tokens"
		if r.URL.Path != expectedPath {
			t.Errorf("unexpected path: %s, want %s", r.URL.Path, expectedPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != "POST" {
			t.Errorf("unexpected method: %s, want POST", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Verify JWT in Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			t.Error("missing Authorization header")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Parse and validate JWT claims
		tokenStr := authHeader[len("Bearer "):]
		parsedToken, parseErr := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
			return &privateKey.PublicKey, nil
		})
		if parseErr != nil {
			t.Errorf("parsing JWT: %v", parseErr)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		claims, ok := parsedToken.Claims.(jwt.MapClaims)
		if !ok {
			t.Error("invalid JWT claims")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Verify issuer is the App ID
		iss, _ := claims.GetIssuer()
		if iss != "12345" {
			t.Errorf("JWT issuer = %q, want %q", iss, "12345")
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      "ghs_test_installation_token",
			"expires_at": expiresAt.Format(time.RFC3339),
		})
	}))
	defer server.Close()

	// Override the package-level base URL for testing
	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	auth := &GitHubAppAuth{
		AppID:          12345,
		InstallationID: 67890,
		PrivateKey:     privateKey,
	}

	token, getErr := auth.GetToken(context.Background())
	if getErr != nil {
		t.Fatalf("GetToken() error = %v", getErr)
	}
	if token != "ghs_test_installation_token" {
		t.Errorf("GetToken() = %q, want %q", token, "ghs_test_installation_token")
	}
}

func TestGitHubAppAuth_GetToken_Cached(t *testing.T) {
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"token":      fmt.Sprintf("ghs_token_%d", callCount),
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer server.Close()

	origBaseURL := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() { githubAPIBaseURL = origBaseURL }()

	privateKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	auth := &GitHubAppAuth{
		AppID:          12345,
		InstallationID: 67890,
		PrivateKey:     privateKey,
	}

	// First call should hit the server
	token1, err := auth.GetToken(context.Background())
	if err != nil {
		t.Fatalf("first GetToken() error = %v", err)
	}

	// Second call should return cached token
	token2, err := auth.GetToken(context.Background())
	if err != nil {
		t.Fatalf("second GetToken() error = %v", err)
	}

	if token1 != token2 {
		t.Errorf("expected cached token, got different tokens: %q vs %q", token1, token2)
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
}

func TestGitHubAppAuth_GetToken_NoPrivateKey(t *testing.T) {
	auth := &GitHubAppAuth{
		AppID:          12345,
		InstallationID: 67890,
	}

	_, err := auth.GetToken(context.Background())
	if err == nil {
		t.Error("expected error when no private key configured")
	}
}

func TestGitHubSource_Name(t *testing.T) {
	source, _ := NewGitHubSource(map[string]interface{}{
		"repository": "test/project",
	})

	if source.Name() != "github" {
		t.Errorf("Name() = %q, want %q", source.Name(), "github")
	}
}

func TestGitHubSource_SupportsWatch(t *testing.T) {
	source, _ := NewGitHubSource(map[string]interface{}{
		"repository": "test/project",
	})

	if !source.SupportsWatch() {
		t.Error("SupportsWatch() = false, want true")
	}
}

func TestGitHubSource_Registry(t *testing.T) {
	factory, ok := Registry.Get("github")
	if !ok {
		t.Fatal("github source not registered")
	}

	source, err := factory(map[string]interface{}{
		"repository": "test/project",
	})
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if source.Name() != "github" {
		t.Errorf("Name() = %q, want %q", source.Name(), "github")
	}
}

func TestGitHubSource_Validate(t *testing.T) {
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
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			source := &GitHubSource{
				owner:   "test",
				repo:    "project",
				baseURL: server.URL,
				client:  server.Client(),
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

func TestGitHubSource_Fetch_ReleaseStrategy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/test/project/releases":
			// Return releases
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"tag_name": "v1.0.0", "prerelease": false, "draft": false},
			})
		case r.URL.Path == "/repos/test/project/contents/config.yaml":
			// Return raw config content
			w.Header().Set("Content-Type", "application/vnd.github.raw")
			w.Write([]byte(validTestConfig))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	source := &GitHubSource{
		owner:       "test",
		repo:        "project",
		path:        "config.yaml",
		strategy:    StrategyRelease,
		environment: "production",
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
}

func TestGitHubSource_Fetch_BranchStrategy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/test/project/branches/main":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"commit": map[string]interface{}{
					"sha": "abc123",
				},
			})
		case r.URL.Path == "/repos/test/project/contents/config.yaml":
			w.Header().Set("Content-Type", "application/vnd.github.raw")
			w.Write([]byte(validTestConfig))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	source := &GitHubSource{
		owner:       "test",
		repo:        "project",
		path:        "config.yaml",
		strategy:    StrategyBranch,
		environment: "production",
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
}

func TestGitHubSource_getFileContent_AcceptHeader(t *testing.T) {
	// Verify that getFileContent sends "application/vnd.github.raw" Accept header,
	// not the JSON API header. This ensures we get raw file content instead of
	// a JSON envelope with base64-encoded content.
	var receivedAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/test/project/contents/config.yaml" {
			receivedAccept = r.Header.Get("Accept")
			w.Write([]byte(validTestConfig))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	source := &GitHubSource{
		owner:   "test",
		repo:    "project",
		path:    "config.yaml",
		baseURL: server.URL,
		client:  server.Client(),
	}

	content, err := source.getFileContent(context.Background(), "main")
	if err != nil {
		t.Fatalf("getFileContent() error = %v", err)
	}
	if string(content) != validTestConfig {
		t.Errorf("getFileContent() returned unexpected content")
	}
	if receivedAccept != "application/vnd.github.raw" {
		t.Errorf("Accept header = %q, want %q", receivedAccept, "application/vnd.github.raw")
	}
}

func TestGitHubSource_HandleWebhook(t *testing.T) {
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
			event:    "release",
			payload: map[string]interface{}{
				"action": "published",
				"release": map[string]interface{}{
					"tag_name":   "v1.0.0",
					"prerelease": false,
					"draft":      false,
				},
			},
			wantFetch: true,
		},
		{
			name:     "release event with branch strategy (ignored)",
			strategy: StrategyBranch,
			event:    "release",
			payload: map[string]interface{}{
				"action": "published",
				"release": map[string]interface{}{
					"tag_name":   "v1.0.0",
					"prerelease": false,
					"draft":      false,
				},
			},
			wantFetch: false,
		},
		{
			name:     "push event with branch strategy",
			strategy: StrategyBranch,
			event:    "push",
			payload: map[string]interface{}{
				"ref": "refs/heads/main",
			},
			wantFetch: true,
		},
		{
			name:     "push event wrong branch",
			strategy: StrategyBranch,
			event:    "push",
			payload: map[string]interface{}{
				"ref": "refs/heads/feature-x",
			},
			wantFetch: false,
		},
		{
			name:     "create tag event with tag strategy",
			strategy: StrategyTag,
			event:    "create",
			payload: map[string]interface{}{
				"ref_type": "tag",
				"ref":      "v1.0.0",
			},
			wantFetch: true,
		},
		{
			name:     "create branch event with tag strategy (ignored)",
			strategy: StrategyTag,
			event:    "create",
			payload: map[string]interface{}{
				"ref_type": "branch",
				"ref":      "feature-x",
			},
			wantFetch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetchCalled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fetchCalled = true
				switch {
				case contains(r.URL.Path, "releases"):
					json.NewEncoder(w).Encode([]map[string]interface{}{
						{"tag_name": "v1.0.0", "prerelease": false, "draft": false},
					})
				case contains(r.URL.Path, "branches"):
					json.NewEncoder(w).Encode(map[string]interface{}{
						"commit": map[string]interface{}{"sha": "abc123"},
					})
				case contains(r.URL.Path, "tags"):
					json.NewEncoder(w).Encode([]map[string]interface{}{
						{"name": "v1.0.0", "commit": map[string]interface{}{"sha": "abc123"}},
					})
				case contains(r.URL.Path, "contents"):
					w.Write([]byte(validTestConfig))
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()

			source := &GitHubSource{
				owner:       "test",
				repo:        "project",
				path:        "config.yaml",
				strategy:    tt.strategy,
				environment: "production",
				baseURL:     server.URL,
				client:      server.Client(),
				webhookChan: make(chan *config.Config, 1),
				stopCh:      make(chan struct{}),
			}

			payload, _ := json.Marshal(tt.payload)
			_ = source.HandleWebhook(context.Background(), tt.event, payload)

			if fetchCalled != tt.wantFetch {
				t.Errorf("Fetch called = %v, want %v", fetchCalled, tt.wantFetch)
			}
		})
	}
}

func TestGitHubSource_addAuthHeader(t *testing.T) {
	tests := []struct {
		name       string
		auth       GitHubAuth
		wantBearer string
	}{
		{
			name:       "no auth",
			auth:       nil,
			wantBearer: "",
		},
		{
			name:       "PAT auth",
			auth:       &GitHubPATAuth{Token: "ghp_test"},
			wantBearer: "Bearer ghp_test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &GitHubSource{auth: tt.auth}
			req, reqErr := http.NewRequestWithContext(context.Background(), "GET", "https://example.com", nil)
			if reqErr != nil {
				t.Fatalf("creating request: %v", reqErr)
			}

			err := source.addAuthHeader(context.Background(), req)
			if err != nil {
				t.Fatalf("addAuthHeader() error = %v", err)
			}

			// Verify standard headers are always set
			if got := req.Header.Get("Accept"); got != "application/vnd.github+json" {
				t.Errorf("Accept = %q, want %q", got, "application/vnd.github+json")
			}
			if got := req.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
				t.Errorf("X-GitHub-Api-Version = %q, want %q", got, "2022-11-28")
			}

			// Verify auth header
			got := req.Header.Get("Authorization")
			if got != tt.wantBearer {
				t.Errorf("Authorization = %q, want %q", got, tt.wantBearer)
			}
		})
	}
}

func TestNewGitHubSource_TagPattern(t *testing.T) {
	source, err := NewGitHubSource(map[string]interface{}{
		"repository":  "myorg/myrepo",
		"strategy":    "tag",
		"tag_pattern": "v*",
	})
	if err != nil {
		t.Fatalf("NewGitHubSource() error = %v", err)
	}
	gh := source.(*GitHubSource)
	if gh.tagPattern != "v*" {
		t.Errorf("tagPattern = %q, want %q", gh.tagPattern, "v*")
	}
}

func TestGitHubSource_Fetch_TagStrategy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/test/project/tags":
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"name": "config-2.0", "commit": map[string]interface{}{"sha": "def456"}},
				{"name": "v1.0.0", "commit": map[string]interface{}{"sha": "abc123"}},
				{"name": "config-1.0", "commit": map[string]interface{}{"sha": "aaa111"}},
			})
		case r.URL.Path == "/repos/test/project/contents/config.yaml":
			w.Write([]byte(validTestConfig))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	source := &GitHubSource{
		owner:       "test",
		repo:        "project",
		path:        "config.yaml",
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
	// Should have matched "config-2.0" (first matching tag), not "v1.0.0"
	if source.currentRef != "config-2.0" {
		t.Errorf("currentRef = %q, want %q", source.currentRef, "config-2.0")
	}
}

func TestGitHubSource_Fetch_TagStrategy_NoPattern(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/repos/test/project/tags":
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"name": "v2.0.0", "commit": map[string]interface{}{"sha": "def456"}},
				{"name": "v1.0.0", "commit": map[string]interface{}{"sha": "abc123"}},
			})
		case r.URL.Path == "/repos/test/project/contents/config.yaml":
			w.Write([]byte(validTestConfig))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	source := &GitHubSource{
		owner:       "test",
		repo:        "project",
		path:        "config.yaml",
		strategy:    StrategyTag,
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
	// Without pattern, should return latest (first) tag
	if source.currentRef != "v2.0.0" {
		t.Errorf("currentRef = %q, want %q", source.currentRef, "v2.0.0")
	}
}

func TestGitHubSource_Fetch_TagStrategy_NoMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"name": "v1.0.0", "commit": map[string]interface{}{"sha": "abc123"}},
		})
	}))
	defer server.Close()

	source := &GitHubSource{
		owner:       "test",
		repo:        "project",
		path:        "config.yaml",
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

func TestGitHubSource_HandleWebhook_TagPattern(t *testing.T) {
	tests := []struct {
		name       string
		tagPattern string
		tagRef     string
		wantFetch  bool
	}{
		{
			name:       "matching pattern",
			tagPattern: "v*",
			tagRef:     "v1.0.0",
			wantFetch:  true,
		},
		{
			name:       "non-matching pattern",
			tagPattern: "v*",
			tagRef:     "release-1.0.0",
			wantFetch:  false,
		},
		{
			name:       "no pattern set (matches all)",
			tagPattern: "",
			tagRef:     "anything",
			wantFetch:  true,
		},
		{
			name:       "complex pattern",
			tagPattern: "config-[0-9]*",
			tagRef:     "config-2",
			wantFetch:  true,
		},
		{
			name:       "complex pattern no match",
			tagPattern: "config-[0-9]*",
			tagRef:     "config-beta",
			wantFetch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetchCalled := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fetchCalled = true
				switch {
				case contains(r.URL.Path, "tags"):
					json.NewEncoder(w).Encode([]map[string]interface{}{
						{"name": tt.tagRef, "commit": map[string]interface{}{"sha": "abc123"}},
					})
				case contains(r.URL.Path, "contents"):
					w.Write([]byte(validTestConfig))
				default:
					w.WriteHeader(http.StatusOK)
				}
			}))
			defer server.Close()

			source := &GitHubSource{
				owner:       "test",
				repo:        "project",
				path:        "config.yaml",
				strategy:    StrategyTag,
				tagPattern:  tt.tagPattern,
				environment: "production",
				baseURL:     server.URL,
				client:      server.Client(),
				webhookChan: make(chan *config.Config, 1),
				stopCh:      make(chan struct{}),
			}

			payload, _ := json.Marshal(map[string]interface{}{
				"ref_type": "tag",
				"ref":      tt.tagRef,
			})
			_ = source.HandleWebhook(context.Background(), "create", payload)

			if fetchCalled != tt.wantFetch {
				t.Errorf("Fetch called = %v, want %v", fetchCalled, tt.wantFetch)
			}
		})
	}
}

func TestParseRSAPrivateKey_PKCS1(t *testing.T) {
	// Generate a test key and encode as PKCS1 PEM
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	parsed, err := parseRSAPrivateKey(pemBytes)
	if err != nil {
		t.Fatalf("parseRSAPrivateKey() error = %v", err)
	}
	if parsed == nil {
		t.Fatal("expected non-nil key")
	}
	if parsed.N.Cmp(privateKey.N) != 0 {
		t.Error("parsed key does not match original")
	}
}

func TestParseRSAPrivateKey_PKCS8(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}

	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshaling PKCS8: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: pkcs8Bytes,
	})

	parsed, err := parseRSAPrivateKey(pemBytes)
	if err != nil {
		t.Fatalf("parseRSAPrivateKey() error = %v", err)
	}
	if parsed == nil {
		t.Fatal("expected non-nil key")
	}
	if parsed.N.Cmp(privateKey.N) != 0 {
		t.Error("parsed key does not match original")
	}
}

func TestParseRSAPrivateKey_InvalidPEM(t *testing.T) {
	_, err := parseRSAPrivateKey([]byte("not a PEM block"))
	if err == nil {
		t.Error("expected error for invalid PEM")
	}
	if !contains(err.Error(), "no PEM block") {
		t.Errorf("error %q should contain 'no PEM block'", err.Error())
	}
}

func TestConfigureGitHubAppAuth_InlinePEM(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	cfg := map[string]interface{}{
		"app_id":          float64(12345),
		"installation_id": float64(67890),
		"private_key":     string(pemBytes),
	}

	auth, err := configureGitHubAppAuth(cfg)
	if err != nil {
		t.Fatalf("configureGitHubAppAuth() error = %v", err)
	}
	if auth.AppID != 12345 {
		t.Errorf("AppID = %d, want 12345", auth.AppID)
	}
	if auth.InstallationID != 67890 {
		t.Errorf("InstallationID = %d, want 67890", auth.InstallationID)
	}
	if auth.PrivateKey == nil {
		t.Fatal("expected PrivateKey to be set")
	}
	if auth.PrivateKey.N.Cmp(privateKey.N) != 0 {
		t.Error("parsed key does not match original")
	}
}

func TestConfigureGitHubAppAuth_KeyFile(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	// Write PEM to temp file
	tmpFile, err := os.CreateTemp("", "gh-app-key-*.pem")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	tmpFile.Write(pemBytes)
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cfg := map[string]interface{}{
		"app_id":           float64(12345),
		"installation_id":  float64(67890),
		"private_key_path": tmpFile.Name(),
	}

	auth, err := configureGitHubAppAuth(cfg)
	if err != nil {
		t.Fatalf("configureGitHubAppAuth() error = %v", err)
	}
	if auth.PrivateKey == nil {
		t.Fatal("expected PrivateKey to be set")
	}
	if auth.PrivateKey.N.Cmp(privateKey.N) != 0 {
		t.Error("parsed key does not match original")
	}
}

func TestConfigureGitHubAppAuth_MissingAppID(t *testing.T) {
	_, err := configureGitHubAppAuth(map[string]interface{}{
		"installation_id": float64(67890),
	})
	if err == nil {
		t.Error("expected error for missing app_id")
	}
}

func TestConfigureGitHubAppAuth_MissingInstallationID(t *testing.T) {
	_, err := configureGitHubAppAuth(map[string]interface{}{
		"app_id": float64(12345),
	})
	if err == nil {
		t.Error("expected error for missing installation_id")
	}
}

func TestGitHubSource_environmentToBranch(t *testing.T) {
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
			source := &GitHubSource{environment: tt.environment}
			if got := source.environmentToBranch(); got != tt.want {
				t.Errorf("environmentToBranch() = %q, want %q", got, tt.want)
			}
		})
	}
}
