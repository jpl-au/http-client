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

	// Try parsing the URL as-is first
	parsed, err := netURL.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}

	// Handle URLs without a scheme (net/url parses "example.com" as path, not host)
	if parsed.Scheme == "" {
		// No scheme present - add one and reparse
		scheme := "https"
		if protocolScheme != "" {
			scheme = protocolScheme
		}
		rawURL = scheme + "://" + rawURL
		parsed, err = netURL.Parse(rawURL)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidURL, err)
		}
	} else if protocolScheme != "" && parsed.Scheme != protocolScheme {
		// Scheme exists but protocolScheme override requested
		parsed.Scheme = protocolScheme
	}

	// Validate we have a host
	if parsed.Host == "" {
		return "", ErrMissingHost
	}

	return parsed.String(), nil
}
