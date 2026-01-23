package auth

import (
	"net"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"
)

// Middleware provides authentication middleware for fasthttp.
type Middleware struct {
	config         *Config
	authenticators []Authenticator
	allowedIPs     []*net.IPNet
}

// NewMiddleware creates a new authentication middleware.
func NewMiddleware(config *Config) (*Middleware, error) {
	m := &Middleware{
		config: config,
	}

	if !config.Enabled {
		return m, nil
	}

	// Set up API key authenticator
	if len(config.APIKeys) > 0 {
		m.authenticators = append(m.authenticators, NewAPIKeyAuthenticator(config.APIKeys))
	}

	// Set up JWT authenticator
	if config.JWT != nil && config.JWT.Enabled {
		jwtAuth, err := NewJWTAuthenticator(config.JWT)
		if err != nil {
			return nil, err
		}
		m.authenticators = append(m.authenticators, jwtAuth)
	}

	// Parse IP allowlist (SECURITY WARNING: IPs in this list bypass ALL authentication)
	if len(config.AllowIPs) > 0 {
		log.Warn().
			Int("count", len(config.AllowIPs)).
			Msg("SECURITY: IP allowlist configured - these IPs will bypass ALL authentication")
	}

	for _, ipStr := range config.AllowIPs {
		// Handle CIDR notation
		if strings.Contains(ipStr, "/") {
			_, ipNet, err := net.ParseCIDR(ipStr)
			if err != nil {
				log.Warn().Str("ip", ipStr).Err(err).Msg("Invalid CIDR in allow_ips")
				continue
			}
			m.allowedIPs = append(m.allowedIPs, ipNet)
			log.Warn().Str("cidr", ipStr).Msg("SECURITY: Added CIDR to auth bypass allowlist")
		} else {
			// Single IP - convert to /32 or /128
			ip := net.ParseIP(ipStr)
			if ip == nil {
				log.Warn().Str("ip", ipStr).Msg("Invalid IP in allow_ips")
				continue
			}
			var mask net.IPMask
			if ip.To4() != nil {
				mask = net.CIDRMask(32, 32)
			} else {
				mask = net.CIDRMask(128, 128)
			}
			m.allowedIPs = append(m.allowedIPs, &net.IPNet{IP: ip, Mask: mask})
			log.Warn().Str("ip", ipStr).Msg("SECURITY: Added IP to auth bypass allowlist")
		}
	}

	return m, nil
}

// Wrap wraps a handler with authentication.
func (m *Middleware) Wrap(handler fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		if !m.config.Enabled {
			handler(ctx)
			return
		}

		// Check IP allowlist first
		if m.isIPAllowed(ctx) {
			handler(ctx)
			return
		}

		// Try each authenticator
		principal, err := m.authenticate(ctx)
		if err != nil {
			m.sendUnauthorized(ctx, err)
			return
		}

		// Store principal in context for handlers to use
		ctx.SetUserValue("principal", principal)

		handler(ctx)
	}
}

// RequirePermission wraps a handler with permission check.
func (m *Middleware) RequirePermission(handler fasthttp.RequestHandler, perm Permission) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		if !m.config.Enabled {
			handler(ctx)
			return
		}

		// Check IP allowlist first (bypass permission check)
		if m.isIPAllowed(ctx) {
			handler(ctx)
			return
		}

		// Get principal from context
		principal, ok := ctx.UserValue("principal").(*Principal)
		if !ok {
			// Try to authenticate if not already done
			var err error
			principal, err = m.authenticate(ctx)
			if err != nil {
				m.sendUnauthorized(ctx, err)
				return
			}
		}

		// Check permission
		if !principal.HasPermission(perm) {
			m.sendForbidden(ctx, perm)
			return
		}

		handler(ctx)
	}
}

// authenticate tries all authenticators in order.
func (m *Middleware) authenticate(ctx *fasthttp.RequestCtx) (*Principal, error) {
	if len(m.authenticators) == 0 {
		return nil, ErrMissingAuth
	}

	var lastErr error
	for _, auth := range m.authenticators {
		principal, err := auth.Authenticate(ctx)
		if err == nil {
			log.Debug().
				Str("principal", principal.ID).
				Str("type", principal.Type).
				Msg("Authentication successful")
			return principal, nil
		}

		// Skip "missing auth" errors to try next authenticator
		if err != ErrMissingAuth {
			lastErr = err
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrMissingAuth
}

// isIPAllowed checks if the client IP is in the allowlist.
func (m *Middleware) isIPAllowed(ctx *fasthttp.RequestCtx) bool {
	if len(m.allowedIPs) == 0 {
		return false
	}

	clientIP := ctx.RemoteIP()
	for _, ipNet := range m.allowedIPs {
		if ipNet.Contains(clientIP) {
			log.Debug().
				Str("ip", clientIP.String()).
				Msg("IP allowlisted, bypassing auth")
			return true
		}
	}

	return false
}

// sendUnauthorized sends a 401 response.
func (m *Middleware) sendUnauthorized(ctx *fasthttp.RequestCtx, err error) {
	ctx.SetStatusCode(fasthttp.StatusUnauthorized)
	ctx.SetContentType("application/json")

	// Set WWW-Authenticate header
	schemes := []string{}
	for _, auth := range m.authenticators {
		switch auth.Name() {
		case "apikey":
			schemes = append(schemes, `ApiKey realm="management"`)
		case "jwt":
			schemes = append(schemes, `Bearer realm="management"`)
		}
	}
	if len(schemes) > 0 {
		ctx.Response.Header.Set("WWW-Authenticate", strings.Join(schemes, ", "))
	}

	log.Debug().Err(err).Str("path", string(ctx.Path())).Msg("Authentication failed")
	ctx.SetBodyString(`{"error":"unauthorized","message":"` + err.Error() + `"}`)
}

// sendForbidden sends a 403 response.
func (m *Middleware) sendForbidden(ctx *fasthttp.RequestCtx, perm Permission) {
	ctx.SetStatusCode(fasthttp.StatusForbidden)
	ctx.SetContentType("application/json")

	log.Debug().
		Str("permission", string(perm)).
		Str("path", string(ctx.Path())).
		Msg("Permission denied")
	ctx.SetBodyString(`{"error":"forbidden","message":"permission denied: ` + string(perm) + `"}`)
}

// IsEnabled returns whether authentication is enabled.
func (m *Middleware) IsEnabled() bool {
	return m.config.Enabled
}
