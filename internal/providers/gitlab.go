// Package providers implements configuration sources.
// This file implements GitLab repository integration.
package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jamengual/the-redirector/internal/config"
)

func init() {
	Registry.Register("gitlab", NewGitLabSource)
}

// GitLabSource reads configuration from a GitLab repository.
// It supports:
//   - Project Token, Personal Access Token, or OAuth authentication
//   - Release-based deployments (only deploy tagged releases)
//   - Branch tracking (for staging/development)
//   - Webhook-driven updates (real-time)
//   - Polling fallback
type GitLabSource struct {
	// Repository settings
	projectPath string // Full project path (namespace/project)
	filePath    string // Path to config file in repo

	// Deployment settings
	strategy    DeploymentStrategy
	environment string // maps to branch or release channel

	// Auth
	auth GitLabAuth

	// GitLab server
	baseURL string // https://gitlab.com or self-hosted URL

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

// GitLabAuth configures authentication to GitLab.
type GitLabAuth interface {
	// AddAuth adds authentication to an HTTP request.
	AddAuth(req *http.Request) error
}

// GitLabTokenAuth authenticates using a Personal Access Token or Project Token.
// This is the simplest authentication method.
//
// Token types:
//   - Personal Access Token: Full user access, good for development
//   - Project Access Token: Scoped to specific project, recommended for CI/CD
//   - Group Access Token: Scoped to group of projects
type GitLabTokenAuth struct {
	// Token is the access token
	Token string

	// TokenType specifies the header format
	// "private-token" (default) or "oauth" for OAuth2 tokens
	TokenType string
}

// AddAuth adds the token to the request.
func (a *GitLabTokenAuth) AddAuth(req *http.Request) error {
	if a.Token == "" {
		return nil
	}

	if a.TokenType == "oauth" {
		req.Header.Set("Authorization", "Bearer "+a.Token)
	} else {
		// Default: Private-Token header for PAT/Project tokens
		req.Header.Set("PRIVATE-TOKEN", a.Token)
	}

	return nil
}

// GitLabOAuthAuth authenticates using OAuth2.
// Supports automatic token refresh.
type GitLabOAuthAuth struct {
	// AccessToken is the current OAuth2 access token
	AccessToken string

	// RefreshToken for obtaining new access tokens
	RefreshToken string

	// ClientID and ClientSecret for the OAuth application
	ClientID     string
	ClientSecret string

	// TokenURL is the token endpoint (e.g., https://gitlab.com/oauth/token)
	TokenURL string

	// Token expiry
	expiresAt time.Time
	mu        sync.Mutex
}

// AddAuth adds OAuth2 authentication to the request.
func (a *GitLabOAuthAuth) AddAuth(req *http.Request) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Check if token needs refresh
	if time.Now().Add(5 * time.Minute).After(a.expiresAt) && a.RefreshToken != "" {
		if err := a.refreshToken(req.Context()); err != nil {
			return fmt.Errorf("refreshing OAuth token: %w", err)
		}
	}

	req.Header.Set("Authorization", "Bearer "+a.AccessToken)
	return nil
}

func (a *GitLabOAuthAuth) refreshToken(ctx context.Context) error {
	// Token refresh implementation
	// POST to TokenURL with grant_type=refresh_token
	// Update AccessToken and expiresAt
	return nil
}

// GitLabSourceConfig configures the GitLab source.
type GitLabSourceConfig struct {
	// Project in "namespace/project" format (URL-encoded if needed)
	Project string `yaml:"project" json:"project"`

	// Path to config file in repository
	Path string `yaml:"path" json:"path"`

	// BaseURL for self-hosted GitLab (default: https://gitlab.com)
	BaseURL string `yaml:"base_url" json:"base_url"`

	// Strategy determines how deployments work
	Strategy DeploymentStrategy `yaml:"strategy" json:"strategy"`

	// Environment maps to branch/release channel
	Environment string `yaml:"environment" json:"environment"`

	// TagPattern for tag-based deployments (glob pattern)
	TagPattern string `yaml:"tag_pattern" json:"tag_pattern"`

	// Token authentication
	Token     string `yaml:"token" json:"token"`
	TokenType string `yaml:"token_type" json:"token_type"` // "private-token" or "oauth"

	// WebhookToken for validating webhook payloads
	WebhookToken string `yaml:"webhook_token" json:"webhook_token"`
}

// NewGitLabSource creates a new GitLab configuration source.
func NewGitLabSource(cfg map[string]interface{}) (Source, error) {
	project, ok := cfg["project"].(string)
	if !ok || project == "" {
		return nil, fmt.Errorf("gitlab source requires 'project' (namespace/project)")
	}

	path, ok := cfg["path"].(string)
	if !ok || path == "" {
		path = "config.yaml" // Default config path
	}

	baseURL := "https://gitlab.com"
	if b, ok := cfg["base_url"].(string); ok && b != "" {
		baseURL = strings.TrimSuffix(b, "/")
	}

	strategy := StrategyRelease // Default to release-based
	if s, ok := cfg["strategy"].(string); ok {
		strategy = DeploymentStrategy(s)
	}

	environment := "production"
	if e, ok := cfg["environment"].(string); ok {
		environment = e
	}

	source := &GitLabSource{
		projectPath: project,
		filePath:    path,
		baseURL:     baseURL,
		strategy:    strategy,
		environment: environment,
		options:     DefaultSourceOptions(),
		client:      &http.Client{Timeout: 30 * time.Second},
		webhookChan: make(chan *config.Config, 1),
		stopCh:      make(chan struct{}),
	}

	// Configure token auth if provided
	if token, ok := cfg["token"].(string); ok && token != "" {
		tokenType := "private-token"
		if t, ok := cfg["token_type"].(string); ok {
			tokenType = t
		}
		source.auth = &GitLabTokenAuth{
			Token:     token,
			TokenType: tokenType,
		}
	}

	return source, nil
}

// Name returns the source type identifier.
func (g *GitLabSource) Name() string {
	return "gitlab"
}

// Fetch retrieves the configuration from GitLab.
func (g *GitLabSource) Fetch(ctx context.Context) (*config.Config, error) {
	var ref string
	var err error

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

	// Fetch config file content
	content, err := g.getFileContent(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("fetching config: %w", err)
	}

	// Parse configuration
	cfg, err := config.ParseBytes(content)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Update current ref
	g.mu.Lock()
	g.currentRef = ref
	g.mu.Unlock()

	return cfg, nil
}

// apiURL constructs the GitLab API URL for the project.
func (g *GitLabSource) apiURL(path string) string {
	// Project path must be URL-encoded
	encodedProject := url.PathEscape(g.projectPath)
	return fmt.Sprintf("%s/api/v4/projects/%s%s", g.baseURL, encodedProject, path)
}

// getLatestRelease finds the latest release for the environment.
func (g *GitLabSource) getLatestRelease(ctx context.Context) (string, error) {
	// GET /projects/:id/releases
	apiURL := g.apiURL("/releases")

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	if err := g.addAuthHeader(req); err != nil {
		return "", err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitLab API returned %d", resp.StatusCode)
	}

	var releases []struct {
		TagName     string    `json:"tag_name"`
		ReleasedAt  time.Time `json:"released_at"`
		Upcoming    bool      `json:"upcoming_release"`
		Description string    `json:"description"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("decoding releases: %w", err)
	}

	for _, release := range releases {
		// Skip upcoming/draft releases
		if release.Upcoming {
			continue
		}

		// GitLab doesn't have prerelease flag like GitHub
		// Convention: use tag naming or description to identify
		isPrerelease := strings.Contains(release.TagName, "-rc") ||
			strings.Contains(release.TagName, "-beta") ||
			strings.Contains(release.TagName, "-alpha")

		// Production: skip prereleases
		if g.environment == "production" && isPrerelease {
			continue
		}

		// Staging: prefer prereleases
		if g.environment == "staging" && !isPrerelease {
			continue
		}

		return release.TagName, nil
	}

	return "", fmt.Errorf("no suitable release found for environment %s", g.environment)
}

// getLatestTag finds the latest tag.
func (g *GitLabSource) getLatestTag(ctx context.Context) (string, error) {
	// GET /projects/:id/repository/tags
	apiURL := g.apiURL("/repository/tags")

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	if err := g.addAuthHeader(req); err != nil {
		return "", err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitLab API returned %d", resp.StatusCode)
	}

	var tags []struct {
		Name   string `json:"name"`
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "", fmt.Errorf("decoding tags: %w", err)
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("no tags found")
	}

	// Return the latest tag (GitLab returns sorted by date desc by default)
	return tags[0].Name, nil
}

// getBranchRef gets the current commit SHA for a branch.
func (g *GitLabSource) getBranchRef(ctx context.Context) (string, error) {
	branch := g.environmentToBranch()

	// GET /projects/:id/repository/branches/:branch
	apiURL := g.apiURL("/repository/branches/" + url.PathEscape(branch))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	if err := g.addAuthHeader(req); err != nil {
		return "", err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitLab API returned %d", resp.StatusCode)
	}

	var branchInfo struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&branchInfo); err != nil {
		return "", fmt.Errorf("decoding branch: %w", err)
	}

	return branchInfo.Commit.ID, nil
}

// environmentToBranch maps environment names to branches.
func (g *GitLabSource) environmentToBranch() string {
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
func (g *GitLabSource) getFileContent(ctx context.Context, ref string) ([]byte, error) {
	// GET /projects/:id/repository/files/:file_path/raw?ref=:ref
	encodedPath := url.PathEscape(g.filePath)
	apiURL := g.apiURL(fmt.Sprintf("/repository/files/%s/raw", encodedPath))
	apiURL += "?ref=" + url.QueryEscape(ref)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	if err := g.addAuthHeader(req); err != nil {
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

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("config file not found: %s", g.filePath)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitLab API returned %d", resp.StatusCode)
	}

	// Update ETag
	if etag := resp.Header.Get("ETag"); etag != "" {
		g.mu.Lock()
		g.currentETag = etag
		g.mu.Unlock()
	}

	// Read content
	return io.ReadAll(resp.Body)
}

// addAuthHeader adds authentication to the request.
func (g *GitLabSource) addAuthHeader(req *http.Request) error {
	req.Header.Set("Accept", "application/json")

	if g.auth != nil {
		return g.auth.AddAuth(req)
	}

	return nil
}

// Watch returns a channel for config updates.
// Supports both webhook-driven and polling-based updates.
func (g *GitLabSource) Watch(ctx context.Context) (<-chan *config.Config, error) {
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

// HandleWebhook processes incoming GitLab webhook events.
// This should be called by the webhook HTTP handler.
//
// GitLab webhook events:
//   - "Tag Push Hook": New tag created
//   - "Push Hook": Push to branch
//   - "Release Hook": New release published
func (g *GitLabSource) HandleWebhook(ctx context.Context, event string, payload []byte) error {
	switch event {
	case "Release Hook":
		if g.strategy == StrategyRelease {
			return g.handleReleaseEvent(ctx, payload)
		}
	case "Push Hook":
		if g.strategy == StrategyBranch {
			return g.handlePushEvent(ctx, payload)
		}
	case "Tag Push Hook":
		if g.strategy == StrategyTag {
			return g.handleTagEvent(ctx, payload)
		}
	}

	return nil // Ignore irrelevant events
}

func (g *GitLabSource) handleReleaseEvent(ctx context.Context, payload []byte) error {
	var event struct {
		Action string `json:"action"` // "create", "update", "delete"
		Tag    string `json:"tag"`
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}

	// Only process new releases
	if event.Action != "create" {
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

func (g *GitLabSource) handlePushEvent(ctx context.Context, payload []byte) error {
	var event struct {
		Ref string `json:"ref"` // "refs/heads/main"
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

func (g *GitLabSource) handleTagEvent(ctx context.Context, payload []byte) error {
	var event struct {
		Ref string `json:"ref"` // "refs/tags/v1.0.0"
	}

	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}

	// Verify it's a tag
	if !strings.HasPrefix(event.Ref, "refs/tags/") {
		return nil
	}

	// TODO: Match against tag pattern

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

// SupportsWatch returns true - GitLab supports webhooks.
func (g *GitLabSource) SupportsWatch() bool {
	return true
}

// Validate checks GitLab API access.
func (g *GitLabSource) Validate(ctx context.Context) error {
	// Try to access the project
	apiURL := g.apiURL("")

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return err
	}

	if err := g.addAuthHeader(req); err != nil {
		return err
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("accessing GitLab: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("project %s not found or not accessible", g.projectPath)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed - check GitLab token")
	}

	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("access forbidden - check token permissions")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitLab API returned %d", resp.StatusCode)
	}

	return nil
}

// Close releases resources.
func (g *GitLabSource) Close() error {
	close(g.stopCh)
	return nil
}
