// Package redirect provides public types for the redirector.
package redirect

// StatusPermanent represents HTTP 301 Moved Permanently.
const StatusPermanent = 301

// StatusFound represents HTTP 302 Found (Temporary Redirect).
const StatusFound = 302

// StatusTemporaryRedirect represents HTTP 307 Temporary Redirect.
// Unlike 302, this preserves the request method.
const StatusTemporaryRedirect = 307

// StatusPermanentRedirect represents HTTP 308 Permanent Redirect.
// Unlike 301, this preserves the request method.
const StatusPermanentRedirect = 308

// Result represents the outcome of a redirect lookup.
type Result struct {
	// Matched indicates whether a rule was found.
	Matched bool

	// RuleID is the ID of the matched rule.
	RuleID string

	// Destination is the redirect target URL.
	Destination string

	// StatusCode is the HTTP status code for the redirect.
	StatusCode int

	// Headers are additional headers to include in the response.
	Headers map[string]string
}
