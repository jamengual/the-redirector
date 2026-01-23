package auth

import (
	"crypto/subtle"

	"github.com/valyala/fasthttp"
)

// APIKeyAuthenticator validates API keys.
type APIKeyAuthenticator struct {
	keys map[string]*APIKeyEntry // key value -> entry
}

// NewAPIKeyAuthenticator creates a new API key authenticator.
func NewAPIKeyAuthenticator(entries []APIKeyEntry) *APIKeyAuthenticator {
	keys := make(map[string]*APIKeyEntry, len(entries))
	for i := range entries {
		entry := &entries[i]
		keys[entry.Key] = entry
	}
	return &APIKeyAuthenticator{keys: keys}
}

// Name returns the authenticator name.
func (a *APIKeyAuthenticator) Name() string {
	return "apikey"
}

// Authenticate validates the API key and returns a principal.
func (a *APIKeyAuthenticator) Authenticate(ctx *fasthttp.RequestCtx) (*Principal, error) {
	key := ExtractAPIKey(ctx)
	if key == "" {
		return nil, ErrMissingAuth
	}

	// Use constant-time comparison to prevent timing attacks
	for storedKey, entry := range a.keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(storedKey)) == 1 {
			return &Principal{
				ID:          entry.Name,
				Type:        "apikey",
				Permissions: entry.Permissions,
			}, nil
		}
	}

	return nil, ErrInvalidAPIKey
}

// ValidateKey checks if a key is valid without creating a principal.
func (a *APIKeyAuthenticator) ValidateKey(key string) bool {
	for storedKey := range a.keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(storedKey)) == 1 {
			return true
		}
	}
	return false
}
