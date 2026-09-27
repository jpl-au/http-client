package client

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"hash"
	"net/http"
	"strings"
)

// digestAlgorithms are the RFC 9530 algorithms the client checks, strongest
// first. Other algorithms are ignored.
var digestAlgorithms = []struct {
	name    string
	newHash func() hash.Hash
}{
	{"sha-512", sha512.New},
	{"sha-256", sha256.New},
}

// serverDigest returns the checksum a server sent for r in a Repr-Digest or
// Content-Digest header, or nil when there is none to check. A 200 response
// holds the whole representation, so either header applies. A resumed download
// is checked as a whole file against Repr-Digest. Any other range response is
// checked against Content-Digest, which covers only the bytes it holds.
func serverDigest(r *http.Response, resuming bool) *checksum {
	var headers []string
	switch {
	case r.StatusCode == http.StatusOK:
		headers = []string{"Repr-Digest", "Content-Digest"}
	case r.StatusCode == http.StatusPartialContent && resuming:
		headers = []string{"Repr-Digest"}
	case r.StatusCode == http.StatusPartialContent:
		headers = []string{"Content-Digest"}
	default:
		return nil
	}

	for _, name := range headers {
		digests := parseDigests(r.Header.Values(name))
		for _, algorithm := range digestAlgorithms {
			h := algorithm.newHash()
			if want, ok := digests[algorithm.name]; ok && len(want) == h.Size() {
				return &checksum{hash: h, want: want, source: "the " + name + " header (" + algorithm.name + ")"}
			}
		}
	}
	return nil
}

// parseDigests returns the digests in the values of an RFC 9530 digest header,
// by algorithm. Each member has the form name=:base64:, and may be followed by
// parameters after a semicolon. A member that does not have this form is
// ignored.
func parseDigests(values []string) map[string][]byte {
	digests := make(map[string][]byte)
	for _, value := range values {
		for member := range strings.SplitSeq(value, ",") {
			name, item, ok := strings.Cut(strings.TrimSpace(member), "=")
			if !ok {
				continue
			}
			item, _, _ = strings.Cut(item, ";")
			encoded, ok := strings.CutPrefix(strings.TrimSpace(item), ":")
			if !ok {
				continue
			}
			encoded, ok = strings.CutSuffix(encoded, ":")
			if !ok {
				continue
			}
			sum, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue
			}
			digests[strings.ToLower(name)] = sum
		}
	}
	return digests
}
