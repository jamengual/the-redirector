package auth

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/valyala/fasthttp"
)

// JWTAuthenticator validates JWT tokens.
type JWTAuthenticator struct {
	config    *JWTConfig
	secretKey []byte
	publicKey any // *rsa.PublicKey or *ecdsa.PublicKey
}

// JWTClaims represents the expected JWT claims.
type JWTClaims struct {
	jwt.RegisteredClaims
	Permissions []string `json:"permissions,omitempty"`
	Scope       string   `json:"scope,omitempty"` // Space-separated permissions
}

// NewJWTAuthenticator creates a new JWT authenticator.
func NewJWTAuthenticator(config *JWTConfig) (*JWTAuthenticator, error) {
	auth := &JWTAuthenticator{config: config}

	// Set up signing key
	if config.Secret != "" {
		auth.secretKey = []byte(config.Secret)
	}

	if config.PublicKey != "" {
		key, err := parsePublicKey(config.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("parsing public key: %w", err)
		}
		auth.publicKey = key
	}

	if auth.secretKey == nil && auth.publicKey == nil {
		return nil, errors.New("JWT authenticator requires either secret or public_key")
	}

	return auth, nil
}

// Name returns the authenticator name.
func (a *JWTAuthenticator) Name() string {
	return "jwt"
}

// Authenticate validates the JWT and returns a principal.
func (a *JWTAuthenticator) Authenticate(ctx *fasthttp.RequestCtx) (*Principal, error) {
	tokenString := ExtractBearerToken(ctx)
	if tokenString == "" {
		return nil, ErrMissingAuth
	}

	// Parse and validate token
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		return a.getSigningKey(token)
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return nil, ErrInvalidToken
	}

	// Validate issuer
	if a.config.Issuer != "" {
		issuer, issuerErr := claims.GetIssuer()
		if issuerErr != nil || issuer != a.config.Issuer {
			return nil, fmt.Errorf("%w: invalid issuer", ErrInvalidToken)
		}
	}

	// Validate audience
	if a.config.Audience != "" {
		audiences, audErr := claims.GetAudience()
		if audErr != nil {
			return nil, fmt.Errorf("%w: invalid audience", ErrInvalidToken)
		}
		found := false
		for _, aud := range audiences {
			if aud == a.config.Audience {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: audience mismatch", ErrInvalidToken)
		}
	}

	// Extract permissions from claims
	permissions := a.extractPermissions(claims)

	subject, err := claims.GetSubject()
	if err != nil {
		subject = "" // Default to empty if subject claim is missing
	}

	return &Principal{
		ID:          subject,
		Type:        "jwt",
		Permissions: permissions,
	}, nil
}

// getSigningKey returns the appropriate signing key based on the token algorithm.
func (a *JWTAuthenticator) getSigningKey(token *jwt.Token) (any, error) {
	switch token.Method.(type) {
	case *jwt.SigningMethodHMAC:
		if a.secretKey == nil {
			return nil, errors.New("HMAC secret not configured")
		}
		return a.secretKey, nil

	case *jwt.SigningMethodRSA:
		if a.publicKey == nil {
			return nil, errors.New("RSA public key not configured")
		}
		if _, ok := a.publicKey.(*rsa.PublicKey); !ok {
			return nil, errors.New("public key is not RSA")
		}
		return a.publicKey, nil

	case *jwt.SigningMethodECDSA:
		if a.publicKey == nil {
			return nil, errors.New("ECDSA public key not configured")
		}
		if _, ok := a.publicKey.(*ecdsa.PublicKey); !ok {
			return nil, errors.New("public key is not ECDSA")
		}
		return a.publicKey, nil

	default:
		return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
	}
}

// extractPermissions extracts permissions from JWT claims.
func (a *JWTAuthenticator) extractPermissions(claims *JWTClaims) []Permission {
	var permissions []Permission

	// Check "permissions" claim (array)
	for _, p := range claims.Permissions {
		permissions = append(permissions, Permission(p))
	}

	// Check "scope" claim (space-separated string, OAuth2 style)
	if claims.Scope != "" {
		for _, s := range splitScope(claims.Scope) {
			permissions = append(permissions, Permission(s))
		}
	}

	// Default to read permission if no permissions specified
	if len(permissions) == 0 {
		permissions = []Permission{PermissionRead}
	}

	return permissions
}

// splitScope splits a space-separated scope string.
func splitScope(scope string) []string {
	var scopes []string
	current := ""
	for _, c := range scope {
		if c == ' ' {
			if current != "" {
				scopes = append(scopes, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		scopes = append(scopes, current)
	}
	return scopes
}

// parsePublicKey parses a PEM-encoded public key.
func parsePublicKey(pemData string) (any, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, errors.New("failed to parse PEM block")
	}

	switch block.Type {
	case "PUBLIC KEY":
		return x509.ParsePKIXPublicKey(block.Bytes)
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported key type: %s", block.Type)
	}
}

// GenerateToken creates a JWT token (useful for testing).
func GenerateToken(secret string, subject string, permissions []Permission, expiry time.Duration) (string, error) {
	claims := JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		Permissions: make([]string, len(permissions)),
	}

	for i, p := range permissions {
		claims.Permissions[i] = string(p)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}
