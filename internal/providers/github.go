// Package providers implements configuration sources.
// This file implements GitHub repository integration using GitHub App authentication.
package providers

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jamengual/the-redirector/internal/config"
	"github.com/rs/zerolog/log"
)

func init() {
	Registry.Register("github", NewGitHubSource)
}

// GitHubSource reads configuration from a GitHub repository.
// It supports:
//   - GitHub App authentication (recommended for production)
//   - Release-based deployments (only deploy tagged releases)
//   - Branch tracking (for staging/development)
//   - Webhook-driven updates (real-time)
//   - Polling fallback
type GitHubSource struct {
	// Repository settings
	owner string
	repo  string
	path  string // Path to config file in repo

	// Deployment settings
	strategy    DeploymentStrategy
	environment string // maps to branch or release channel
	tagPattern  string // glob pattern for tag matching (e.g., "v*", "config-*")

	// Auth
	auth GitHubAuth

	// API base URL (defaults to https://api.github.com)
	baseURL string

	// State
	currentRef  string // Current commit SHA or tag
	currentETag string

	// Options
	options SourceOptions

	// HTTP client
	client *http.Client

	// Webhook handling
	webhookChan chan *config.Config
	mu          sync.RWMutex
	stopCh      chan struct{}
}

// DeploymentStrategy defines how config changes are deployed.
type DeploymentStrategy string

const (
	// StrategyRelease only deploys from GitHub Releases.
	// This is the recommended production strategy.
	// Config is deployed when a new release is published.
	StrategyRelease DeploymentStrategy = "release"

	// StrategyTag deploys from Git tags matching a pattern.
	// Example: v1.*, config-*
	StrategyTag DeploymentStrategy = "tag"

	// StrategyBranch tracks a specific branch (e.g., main, staging).
	// Deploys on every push to the branch.
	// Recommended for staging/development environments.
	StrategyBranch DeploymentStrategy = "branch"

	// StrategyCommit deploys a specific commit (immutable).
	// Useful for rollbacks or pinned deployments.
	StrategyCommit DeploymentStrategy = "commit"
)

// GitHubAuth configures authentication to GitHub.
type GitHubAuth interface {
	// GetToken returns a valid access token.
	// For GitHub Apps, this handles JWT generation and installation token exchange.
	GetToken(ctx context.Context) (string, error)
}

// GitHubPATAuth authenticates using a Personal Access Token.
type GitHubPATAuth struct {
	Token string
}

// GetToken returns the PAT token.
func (a *GitHubPATAuth) GetToken(ctx context.Context) (string, error) {
	if a.Token == "" {
		return "", fmt.Errorf("PAT token is empty")
	}
	return a.Token, nil
}

// GitHubAppAuth authenticates using a GitHub App.
// This is the recommended authentication method for production.
//
// Benefits over Personal Access Tokens:
//   - Fine-grained permissions (only what's needed)
//   - Automatic token rotation
//   - Audit trail of actions
//   - Organization-level control
//   - No dependency on individual user accounts
type GitHubAppAuth struct {
	// AppID is the GitHub App's ID
	AppID int64

	// InstallationID is the installation ID for the target org/repo
	InstallationID int64

	// PrivateKey is the App's private key (PEM format)
	PrivateKey *rsa.PrivateKey

	// Cached installation token
	token          string
	tokenExpiresAt time.Time
	mu             sync.Mutex
}

// GetToken returns a valid installation access token.
// Handles JWT creation and token refresh automatically.
func (a *GitHubAppAuth) GetToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return cached token if still valid (with 5 min buffer)
	if a.token != "" && time.Now().Add(5*time.Minute).Before(a.tokenExpiresAt) {
		return a.token, nil
	}

	if a.PrivateKey == nil {
		return "", fmt.Errorf("GitHub App private key is not configured")
	}

	// 1. Create JWT signed with App private key
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", a.AppID),
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
	}

	jwtToken := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signedJWT, err := jwtToken.SignedString(a.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("signing JWT: %w", err)
	}

	// 2. Exchange JWT for installation access token
	token, expiresAt, err := a.exchangeForInstallationToken(ctx, signedJWT)
	if err != nil {
		return "", err
	}

	a.token = token
	a.tokenExpiresAt = expiresAt

	return a.token, nil
}

// apiBaseURL is the GitHub API base URL. Overridable for testing.
var githubAPIBaseURL = "https://api.github.com"

// exchangeForInstallationToken exchanges a JWT for an installation access token.
func (a *GitHubAppAuth) exchangeForInstallationToken(ctx context.Context, signedJWT string) (string, time.Time, error) {
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", githubAPIBaseURL, a.InstallationID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("creating token request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+signedJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("exchanging JWT for token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("GitHub token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", time.Time{}, fmt.Errorf("decoding token response: %w", err)
	}

	return tokenResp.Token, tokenResp.ExpiresAt, nil
}

// GitHubSourceConfig configures the GitHub source.
type GitHubSourceConfig struct {
	// Repository in "owner/repo" format
	Repository string `yaml:"repository" json:"repository"`

	// Path to config file in repository
	Path string `yaml:"path" json:"path"`

	// Strategy determines how deployments work
	Strategy DeploymentStrategy `yaml:"strategy" json:"strategy"`

	// Environment maps to branch/release channel
	// Examples: "production" -> main branch or stable releases
	//           "staging" -> staging branch or pre-releases
	Environment string `yaml:"environment" json:"environment"`

	// TagPattern for tag-based deployments (glob pattern)
	// Example: "v*", "config-*"
	TagPattern string `yaml:"tag_pattern" json:"tag_pattern"`

	// GitHubApp authentication settings
	App *GitHubAppConfig `yaml:"app" json:"app"`

	// WebhookSecret for validating webhook payloads
	WebhookSecret string `yaml:"webhook_secret" json:"webhook_secret"`
}

// GitHubAppConfig configures GitHub App authentication.
type GitHubAppConfig struct {
	// AppID is the GitHub App ID
	AppID int64 `yaml:"app_id" json:"app_id"`

	// InstallationID for the target repository
	InstallationID int64 `yaml:"installation_id" json:"installation_id"`

	// PrivateKeyPath is path to the App's private key file
	PrivateKeyPath string `yaml:"private_key_path" json:"private_key_path"`

	// PrivateKey is the PEM-encoded private key (alternative to path)
	// Use environment variable: ${GITHUB_APP_PRIVATE_KEY}
	PrivateKey string `yaml:"private_key" json:"private_key"`
}

// NewGitHubSource creates a new GitHub configuration source.
func NewGitHubSource(cfg map[string]interface{}) (Source, error) {
	repository, ok := cfg["repository"].(string)
	if !ok || repository == "" {
		return nil, fmt.Errorf("github source requires 'repository' (owner/repo)")
	}

	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository format, expected 'owner/repo'")
	}

	path, ok := cfg["path"].(string)
	if !ok || path == "" {
		path = "config.yaml" // Default config path
	}

	strategy := StrategyRelease // Default to release-based
	if s, ok := cfg["strategy"].(string); ok {
		strategy = DeploymentStrategy(s)
	}

	environment := "production"
	if e, ok := cfg["environment"].(string); ok {
		environment = e
	}

	baseURL := "https://api.github.com"
	if u, ok := cfg["base_url"].(string); ok && u != "" {
		baseURL = u
	}

	tagPattern := ""
	if tp, ok := cfg["tag_pattern"].(string); ok {
		tagPattern = tp
	}

	source := &GitHubSource{
		owner:       parts[0],
		repo:        parts[1],
		path:        path,
		strategy:    strategy,
		environment: environment,
		tagPattern:  tagPattern,
		baseURL:     baseURL,
		options:     DefaultSourceOptions(),
		client:      &http.Client{Timeout: 30 * time.Second},
		webhookChan: make(chan *config.Config, 1),
		stopCh:      make(chan struct{}),
	}

	// Configure authentication
	authType := "none"
	if appCfg, ok := cfg["app"].(map[string]interface{}); ok {
		auth, err := configureGitHubAppAuth(appCfg)
		if err != nil {
			return nil, fmt.Errorf("configuring GitHub App auth: %w", err)
		}
		source.auth = auth
		authType = "github-app"
	} else if token, ok := cfg["token"].(string); ok && token != "" {
		source.auth = &GitHubPATAuth{Token: token}
		authType = "pat"
	}

	log.Debug().
		Str("owner", source.owner).
		Str("repo", source.repo).
		Str("path", source.path).
		Str("strategy", string(source.strategy)).
		Str("environment", source.environment).
		Str("auth", authType).
		Str("base_url", source.baseURL).
		Msg("GitHub: source configured")

	return source, nil
}

func configureGitHubAppAuth(cfg map[string]interface{}) (*GitHubAppAuth, error) {
	appID, ok := cfg["app_id"].(int64)
	if !ok {
		if f, floatOk := cfg["app_id"].(float64); floatOk {
			appID = int64(f)
		}
	}
	if appID == 0 {
		return nil, fmt.Errorf("app_id is required")
	}

	installationID, ok := cfg["installation_id"].(int64)
	if !ok {
		if f, ok := cfg["installation_id"].(float64); ok {
			installationID = int64(f)
		}
	}
	if installationID == 0 {
		return nil, fmt.Errorf("installation_id is required")
	}

	// Load private key from file path or inline PEM string
	var pemData []byte
	if keyPath, ok := cfg["private_key_path"].(string); ok && keyPath != "" {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("reading private key file: %w", err)
		}
		pemData = data
	} else if keyStr, ok := cfg["private_key"].(string); ok && keyStr != "" {
		pemData = []byte(keyStr)
	}

	var privateKey *rsa.PrivateKey
	if len(pemData) > 0 {
		key, err := parseRSAPrivateKey(pemData)
		if err != nil {
			return nil, fmt.Errorf("parsing private key: %w", err)
		}
		privateKey = key
	}

	return &GitHubAppAuth{
		AppID:          appID,
		InstallationID: installationID,
		PrivateKey:     privateKey,
	}, nil
}

// parseRSAPrivateKey parses a PEM-encoded RSA private key.
// Supports both PKCS1 and PKCS8 formats.
func parseRSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in private key data")
	}

	// Try PKCS1 first (RSA PRIVATE KEY)
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}

	// Try PKCS8 (PRIVATE KEY)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key (tried PKCS1 and PKCS8): %w", err)
	}

	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}

	return rsaKey, nil
}

// Name returns the source type identifier.
func (g *GitHubSource) Name() string {
	return "github"
}

// Fetch retrieves the configuration from GitHub.
func (g *GitHubSource) Fetch(ctx context.Context) (*config.Config, error) {
	var ref string
	var err error

	log.Debug().
		Str("owner", g.owner).
		Str("repo", g.repo).
		Str("path", g.path).
		Str("strategy", string(g.strategy)).
		Str("environment", g.environment).
		Msg("GitHub: starting fetch")

	switch g.strategy {
	case StrategyRelease:
		ref, err = g.getLatestRelease(ctx)
	case StrategyTag:
		ref, err = g.getLatestTag(ctx)
	case StrategyBranch:
		ref, err = g.getBranchRef(ctx)
	case StrategyCommit:
		ref = g.currentRef // Use pinned commit
	default:
		return nil, fmt.Errorf("unknown deployment strategy: %s", g.strategy)
	}

	if err != nil {
		return nil, fmt.Errorf("getting ref: %w", err)
	}

	log.Debug().Str("ref", ref).Msg("GitHub: resolved ref")

	// Fetch config file content
	content, err := g.getFileContent(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("fetching config: %w", err)
	}

	log.Debug().Int("bytes", len(content)).Msg("GitHub: fetched file content")

	// Parse configuration
	cfg, err := config.ParseBytes(content)
	if err != nil {
		log.Debug().Str("content_preview", string(content[:min(len(content), 200)])).Msg("GitHub: content that failed to parse")
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Update current ref
	g.mu.Lock()
	g.currentRef = ref
	g.mu.Unlock()

	log.Debug().Str("ref", ref).Int("rules", len(cfg.Rules)).Msg("GitHub: fetch complete")

	return cfg, nil
}

// getLatestRelease finds the latest release for the environment.
func (g *GitHubSource) getLatestRelease(ctx context.Context) (string, error) {
	// GET /repos/{owner}/{repo}/releases
	// Filter by:
	//   - production: latest non-prerelease
	//   - staging: latest prerelease OR latest overall
	//   - specific: match release name pattern

	url := fmt.Sprintf("%s/repos/%s/%s/releases", g.baseURL, g.owner, g.repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	if err = g.addAuthHeader(ctx, req); err != nil {
		return "", err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var releases []struct {
		TagName    string `json:"tag_name"`
		Prerelease bool   `json:"prerelease"`
		Draft      bool   `json:"draft"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("decoding releases: %w", err)
	}

	for _, release := range releases {
		if release.Draft {
			continue
		}

		// Production: skip prereleases
		if g.environment == "production" && release.Prerelease {
			continue
		}

		// Staging: prefer prereleases
		if g.environment == "staging" && !release.Prerelease {
			continue
		}

		return release.TagName, nil
	}

	return "", fmt.Errorf("no suitable release found for environment %s", g.environment)
}

// getLatestTag finds the latest tag matching the pattern.
func (g *GitHubSource) getLatestTag(ctx context.Context) (string, error) {
	// GET /repos/{owner}/{repo}/tags
	// Filter by tag pattern

	url := fmt.Sprintf("%s/repos/%s/%s/tags", g.baseURL, g.owner, g.repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	if err = g.addAuthHeader(ctx, req); err != nil {
		return "", err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tags []struct {
		Name   string `json:"name"`
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "", fmt.Errorf("decoding tags: %w", err)
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("no tags found")
	}

	// Filter by tag pattern if configured
	if g.tagPattern != "" {
		for _, tag := range tags {
			if matched, _ := path.Match(g.tagPattern, tag.Name); matched {
				return tag.Name, nil
			}
		}
		return "", fmt.Errorf("no tags matching pattern %q found", g.tagPattern)
	}

	// Return the latest tag (GitHub returns sorted by date)
	return tags[0].Name, nil
}

// getBranchRef gets the current commit SHA for a branch.
func (g *GitHubSource) getBranchRef(ctx context.Context) (string, error) {
	branch := g.environmentToBranch()

	url := fmt.Sprintf("%s/repos/%s/%s/branches/%s",
		g.baseURL, g.owner, g.repo, branch)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	if err = g.addAuthHeader(ctx, req); err != nil {
		return "", err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var branchInfo struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&branchInfo); err != nil {
		return "", fmt.Errorf("decoding branch: %w", err)
	}

	return branchInfo.Commit.SHA, nil
}

// environmentToBranch maps environment names to branches.
func (g *GitHubSource) environmentToBranch() string {
	switch g.environment {
	case "production":
		return "main"
	case "staging":
		return "staging"
	case "development":
		return "develop"
	default:
		return g.environment // Use as-is if not a known environment
	}
}

// getFileContent fetches the config file at a specific ref.
func (g *GitHubSource) getFileContent(ctx context.Context, ref string) ([]byte, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
		g.baseURL, g.owner, g.repo, g.path, ref)

	log.Debug().Str("url", url).Str("ref", ref).Msg("GitHub: fetching file content")

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Request raw content
	req.Header.Set("Accept", "application/vnd.github.raw")

	if err = g.addAuthHeader(ctx, req); err != nil {
		return nil, err
	}

	// Use ETag for caching
	g.mu.RLock()
	if g.currentETag != "" {
		req.Header.Set("If-None-Match", g.currentETag)
	}
	g.mu.RUnlock()

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Not modified
	if resp.StatusCode == http.StatusNotModified {
		return nil, fmt.Errorf("config not modified")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	// Update ETag
	if etag := resp.Header.Get("ETag"); etag != "" {
		g.mu.Lock()
		g.currentETag = etag
		g.mu.Unlock()
	}

	// Read content
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	return content, nil
}

// addAuthHeader adds authentication to the request.
func (g *GitHubSource) addAuthHeader(ctx context.Context, req *http.Request) error {
	// Only set Accept if the caller hasn't already set it (e.g., getFileContent
	// sets application/vnd.github.raw to get raw file content).
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	if g.auth != nil {
		token, err := g.auth.GetToken(ctx)
		if err != nil {
			return fmt.Errorf("getting auth token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	return nil
}

// Watch returns a channel for config updates.
// Supports both webhook-driven and polling-based updates.
func (g *GitHubSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
	updates := make(chan *config.Config, 1)

	go func() {
		defer close(updates)

		// Poll for changes as fallback
		ticker := time.NewTicker(g.options.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-g.stopCh:
				return

			// Webhook-triggered update
			case cfg := <-g.webhookChan:
				updates <- cfg

			// Polling fallback
			case <-ticker.C:
				cfg, err := g.Fetch(ctx)
				if err == nil {
					updates <- cfg
				}
			}
		}
	}()

	return updates, nil
}

// HandleWebhook processes incoming GitHub webhook events.
// This should be called by the webhook HTTP handler.
func (g *GitHubSource) HandleWebhook(ctx context.Context, event string, payload []byte) error {
	switch event {
	case "release":
		if g.strategy == StrategyRelease {
			return g.handleReleaseEvent(ctx, payload)
		}
	case "push":
		if g.strategy == StrategyBranch {
			return g.handlePushEvent(ctx, payload)
		}
	case "create":
		if g.strategy == StrategyTag {
			return g.handleTagEvent(ctx, payload)
		}
	}

	return nil // Ignore irrelevant events
}

func (g *GitHubSource) handleReleaseEvent(ctx context.Context, payload []byte) error {
	var event struct {
		Action  string `json:"action"`
		Release struct {
			TagName    string `json:"tag_name"`
			Prerelease bool   `json:"prerelease"`
			Draft      bool   `json:"draft"`
		} `json:"release"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}

	// Only process published releases
	if event.Action != "published" {
		return nil
	}

	// Skip drafts
	if event.Release.Draft {
		return nil
	}

	// Check environment match
	if g.environment == "production" && event.Release.Prerelease {
		return nil
	}

	// Fetch and push new config
	cfg, err := g.Fetch(ctx)
	if err != nil {
		return err
	}

	select {
	case g.webhookChan <- cfg:
	default:
		// Channel full, skip
	}

	return nil
}

func (g *GitHubSource) handlePushEvent(ctx context.Context, payload []byte) error {
	var event struct {
		Ref string `json:"ref"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}

	expectedRef := "refs/heads/" + g.environmentToBranch()
	if event.Ref != expectedRef {
		return nil // Not our branch
	}

	cfg, err := g.Fetch(ctx)
	if err != nil {
		return err
	}

	select {
	case g.webhookChan <- cfg:
	default:
	}

	return nil
}

func (g *GitHubSource) handleTagEvent(ctx context.Context, payload []byte) error {
	var event struct {
		RefType string `json:"ref_type"`
		Ref     string `json:"ref"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}

	if event.RefType != "tag" {
		return nil
	}

	// Skip tags that don't match the configured pattern
	if g.tagPattern != "" {
		if matched, _ := path.Match(g.tagPattern, event.Ref); !matched {
			return nil
		}
	}

	cfg, err := g.Fetch(ctx)
	if err != nil {
		return err
	}

	select {
	case g.webhookChan <- cfg:
	default:
	}

	return nil
}

// SupportsWatch returns true - GitHub supports webhooks.
func (g *GitHubSource) SupportsWatch() bool {
	return true
}

// Validate checks GitHub API access.
func (g *GitHubSource) Validate(ctx context.Context) error {
	// Try to access the repository
	url := fmt.Sprintf("%s/repos/%s/%s", g.baseURL, g.owner, g.repo)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	if err = g.addAuthHeader(ctx, req); err != nil {
		return err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("accessing GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("repository %s/%s not found or not accessible", g.owner, g.repo)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed - check GitHub App credentials")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	return nil
}

// Close releases resources.
func (g *GitHubSource) Close() error {
	close(g.stopCh)
	return nil
}
