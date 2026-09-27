// Package validator finds the strong validator of a response, which proves
// that two range responses come from the same version of a resource.
package validator

import (
	"net/http"
	"time"
)

// Strong returns the value to send as If-Range for data that came from a
// response with header from, or "" when it has no strong validator
// (RFC 9110, section 13.1.5).
//
// A strong ETag is used when present. A date is used only when the response
// has no ETag at all, and only when it is strong: the response's Date must be
// at least one second after its Last-Modified (RFC 9110, section 8.8.2.2).
// A header that does not parse gives no validator.
func Strong(from http.Header) string {
	if etag := from.Get("ETag"); etag != "" {
		if isStrongETag(etag) {
			return etag
		}
		return ""
	}

	modified, err := http.ParseTime(from.Get("Last-Modified"))
	if err != nil {
		return ""
	}
	date, err := http.ParseTime(from.Get("Date"))
	if err != nil {
		return ""
	}
	if date.Sub(modified) < time.Second {
		return ""
	}
	return from.Get("Last-Modified")
}

// isStrongETag reports whether v is a strong entity tag: a quoted string of
// etagc characters (RFC 9110, section 8.8.3).
func isStrongETag(v string) bool {
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return false
	}
	for i := 1; i < len(v)-1; i++ {
		c := v[i]
		if c != 0x21 && (c < 0x23 || c > 0x7e) && c < 0x80 {
			return false
		}
	}
	return true
}
