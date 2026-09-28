package client

import (
	"fmt"
	netURL "net/url"
	"strings"
)

// normaliseURL ensures the URL has a valid scheme and format.
// If protocolScheme is provided, it overrides any existing scheme.
// If no scheme is present and protocolScheme is empty, defaults to https.
func normaliseURL(rawURL string, protocolScheme string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", ErrEmptyURL
	}

	// Clean up protocolScheme if provided
	if protocolScheme != "" {
		protocolScheme = strings.TrimSuffix(protocolScheme, "://")
	}

	// Add the scheme before parsing. net/url reads a URL without one as a
	// path, and reads a host with a port, such as localhost:8080, as a scheme.
	if !hasScheme(rawURL) {
		scheme := "https"
		if protocolScheme != "" {
			scheme = protocolScheme
		}
		rawURL = scheme + "://" + rawURL
	}

	parsed, err := netURL.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if protocolScheme != "" && parsed.Scheme != protocolScheme {
		// Scheme exists but protocolScheme override requested
		parsed.Scheme = protocolScheme
	}

	// Validate we have a host
	if parsed.Host == "" {
		return "", ErrMissingHost
	}

	return parsed.String(), nil
}

// hasScheme reports whether rawURL starts with a scheme followed by "://".
// Only the start counts: a URL in the path, query or fragment is not the
// scheme of rawURL.
func hasScheme(rawURL string) bool {
	scheme, _, found := strings.Cut(rawURL, "://")
	if !found || scheme == "" {
		return false
	}
	// A scheme is a letter followed by letters, digits, "+", "-" or ".".
	for i, c := range scheme {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z':
		case i > 0 && ('0' <= c && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return true
}
