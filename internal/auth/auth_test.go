package auth

import (
	"testing"
	"time"

	"github.com/valyala/fasthttp"
)

func TestPrincipalHasPermission(t *testing.T) {
	tests := []struct {
		name        string
		permissions []Permission
		check       Permission
		want        bool
	}{
		{
			name:        "admin has all permissions",
			permissions: []Permission{PermissionAdmin},
			check:       PermissionWrite,
			want:        true,
		},
		{
			name:        "exact permission match",
			permissions: []Permission{PermissionRead},
			check:       PermissionRead,
			want:        true,
		},
		{
			name:        "write includes read",
			permissions: []Permission{PermissionWrite},
			check:       PermissionRead,
			want:        true,
		},
		{
			name:        "write includes reload",
			permissions: []Permission{PermissionWrite},
			check:       PermissionReload,
			want:        true,
		},
		{
			name:        "read does not include write",
			permissions: []Permission{PermissionRead},
			check:       PermissionWrite,
			want:        false,
		},
		{
			name:        "stats:write includes stats:read",
			permissions: []Permission{PermissionStatsWrite},
			check:       PermissionStatsRead,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Principal{Permissions: tt.permissions}
			if got := p.HasPermission(tt.check); got != tt.want {
				t.Errorf("HasPermission(%v) = %v, want %v", tt.check, got, tt.want)
			}
		})
	}
}

func TestAPIKeyAuthenticator(t *testing.T) {
	entries := []APIKeyEntry{
		{Name: "test-key", Key: "secret123", Permissions: []Permission{PermissionRead}},
		{Name: "admin-key", Key: "admin456", Permissions: []Permission{PermissionAdmin}},
	}

	auth := NewAPIKeyAuthenticator(entries)

	tests := []struct {
		name      string
		header    string
		headerVal string
		wantErr   error
		wantID    string
	}{
		{
			name:      "valid X-API-Key header",
			header:    "X-API-Key",
			headerVal: "secret123",
			wantErr:   nil,
			wantID:    "test-key",
		},
		{
			name:      "valid ApiKey auth header",
			header:    "Authorization",
			headerVal: "ApiKey admin456",
			wantErr:   nil,
			wantID:    "admin-key",
		},
		{
			name:      "invalid key",
			header:    "X-API-Key",
			headerVal: "wrongkey",
			wantErr:   ErrInvalidAPIKey,
		},
		{
			name:    "missing auth",
			wantErr: ErrMissingAuth,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			if tt.header != "" {
				ctx.Request.Header.Set(tt.header, tt.headerVal)
			}

			principal, err := auth.Authenticate(ctx)
			if err != tt.wantErr {
				t.Errorf("Authenticate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr == nil && principal.ID != tt.wantID {
				t.Errorf("Authenticate() principal.ID = %v, want %v", principal.ID, tt.wantID)
			}
		})
	}
}

func TestJWTAuthenticator(t *testing.T) {
	secret := "test-secret-key-for-jwt-signing"

	config := &JWTConfig{
		Enabled: true,
		Secret:  secret,
		// No issuer validation for basic test
	}

	auth, err := NewJWTAuthenticator(config)
	if err != nil {
		t.Fatalf("NewJWTAuthenticator() error = %v", err)
	}

	// Generate a valid token
	validToken, err := GenerateToken(secret, "test-user", []Permission{PermissionRead, PermissionReload}, time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	// Generate an expired token
	expiredToken, err := GenerateToken(secret, "expired-user", []Permission{PermissionRead}, -time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	tests := []struct {
		name    string
		token   string
		wantErr bool
		wantID  string
	}{
		{
			name:   "valid token",
			token:  validToken,
			wantID: "test-user",
		},
		{
			name:    "expired token",
			token:   expiredToken,
			wantErr: true,
		},
		{
			name:    "invalid token",
			token:   "invalid.token.here",
			wantErr: true,
		},
		{
			name:    "missing token",
			token:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			if tt.token != "" {
				ctx.Request.Header.Set("Authorization", "Bearer "+tt.token)
			}

			principal, err := auth.Authenticate(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Authenticate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && principal.ID != tt.wantID {
				t.Errorf("Authenticate() principal.ID = %v, want %v", principal.ID, tt.wantID)
			}
		})
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid bearer", "Bearer token123", "token123"},
		{"bearer lowercase", "bearer token123", "token123"},
		{"no bearer prefix", "token123", ""},
		{"empty", "", ""},
		{"basic auth", "Basic dXNlcjpwYXNz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			if tt.header != "" {
				ctx.Request.Header.Set("Authorization", tt.header)
			}
			if got := ExtractBearerToken(ctx); got != tt.want {
				t.Errorf("ExtractBearerToken() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractAPIKey(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		headerVal string
		queryKey  string
		want      string
	}{
		{"X-API-Key header", "X-API-Key", "key123", "", "key123"},
		{"ApiKey auth header", "Authorization", "ApiKey key456", "", "key456"},
		{"query param", "", "", "key789", "key789"},
		{"header takes precedence", "X-API-Key", "key123", "key789", "key123"},
		{"no key", "", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &fasthttp.RequestCtx{}
			if tt.header != "" {
				ctx.Request.Header.Set(tt.header, tt.headerVal)
			}
			if tt.queryKey != "" {
				ctx.QueryArgs().Set("api_key", tt.queryKey)
			}
			if got := ExtractAPIKey(ctx); got != tt.want {
				t.Errorf("ExtractAPIKey() = %v, want %v", got, tt.want)
			}
		})
	}
}
