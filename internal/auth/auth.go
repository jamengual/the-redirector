// Package auth provides authentication for the management API.
package auth

import (
	"errors"
	"strings"

	"github.com/valyala/fasthttp"
)

// Common errors
var (
	ErrUnauthorized     = errors.New("unauthorized")
	ErrInvalidToken     = errors.New("invalid token")
	ErrTokenExpired     = errors.New("token expired")
	ErrMissingAuth      = errors.New("missing authorization")
	ErrInvalidAPIKey    = errors.New("invalid API key")
	ErrPermissionDenied = errors.New("permission denied")
)

// Permission represents an API permission.
type Permission string

const (
	PermissionRead       Permission = "read"        // Read config, stats
	PermissionWrite      Permission = "write"       // Modify config
	PermissionReload     Permission = "reload"      // Trigger reload
	PermissionAdmin      Permission = "admin"       // Full access
	PermissionStatsRead  Permission = "stats:read"  // Read stats only
	PermissionStatsWrite Permission = "stats:write" // Enable/disable/reset stats
)

// Principal represents an authenticated entity.
type Principal struct {
	ID          string       // Unique identifier (API key name or JWT subject)
	Type        string       // "apikey" or "jwt"
	Permissions []Permission // Granted permissions
}

// HasPermission checks if the principal has the given permission.
func (p *Principal) HasPermission(perm Permission) bool {
	for _, granted := range p.Permissions {
		if granted == PermissionAdmin || granted == perm {
			return true
		}
		// Check hierarchical permissions
		if granted == PermissionWrite && (perm == PermissionRead || perm == PermissionReload) {
			return true
		}
		if granted == PermissionStatsWrite && perm == PermissionStatsRead {
			return true
		}
	}
	return false
}

// Authenticator validates credentials and returns a principal.
type Authenticator interface {
	// Authenticate validates the request and returns a principal.
	// Returns ErrUnauthorized if authentication fails.
	Authenticate(ctx *fasthttp.RequestCtx) (*Principal, error)

	// Name returns the authenticator name (for logging).
	Name() string
}

// Config holds authentication configuration.
type Config struct {
	Enabled  bool          `yaml:"enabled"`
	APIKeys  []APIKeyEntry `yaml:"api_keys"`
	JWT      *JWTConfig    `yaml:"jwt"`
	AllowIPs []string      `yaml:"allow_ips"` // IP allowlist (bypass auth)
}

// APIKeyEntry represents a single API key configuration.
type APIKeyEntry struct {
	Name        string       `yaml:"name"`        // Descriptive name
	Key         string       `yaml:"key"`         // The API key value (use env var)
	Permissions []Permission `yaml:"permissions"` // Granted permissions
}

// JWTConfig holds JWT authentication configuration.
type JWTConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Secret    string `yaml:"secret"`     // HMAC secret (use env var)
	PublicKey string `yaml:"public_key"` // RSA/ECDSA public key (PEM)
	Issuer    string `yaml:"issuer"`     // Expected issuer claim
	Audience  string `yaml:"audience"`   // Expected audience claim
}

// ExtractBearerToken extracts the token from Authorization header.
func ExtractBearerToken(ctx *fasthttp.RequestCtx) string {
	auth := string(ctx.Request.Header.Peek("Authorization"))
	if auth == "" {
		return ""
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}

// ExtractAPIKey extracts the API key from various sources.
// Checks: X-API-Key header, Authorization header (ApiKey scheme), query param.
func ExtractAPIKey(ctx *fasthttp.RequestCtx) string {
	// Check X-API-Key header first
	if key := string(ctx.Request.Header.Peek("X-API-Key")); key != "" {
		return key
	}

	// Check Authorization header with "ApiKey" scheme
	auth := string(ctx.Request.Header.Peek("Authorization"))
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "ApiKey") {
			return strings.TrimSpace(parts[1])
		}
	}

	// Check query parameter (least preferred, not recommended)
	if key := string(ctx.QueryArgs().Peek("api_key")); key != "" {
		return key
	}

	return ""
}
