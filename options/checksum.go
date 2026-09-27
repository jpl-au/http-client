package options

import "hash"

// ChecksumConfig is the checksum a downloaded body must match.
type ChecksumConfig struct {
	// New creates the hash, such as sha256.New. Nil means no checksum.
	New func() hash.Hash

	// Expected is the checksum as hex, in upper or lower case.
	Expected string
}

// SetChecksum sets the checksum a downloaded body must match. newHash creates
// the hash, such as sha256.New, and expected is the checksum as hex, as tools
// like sha256sum print it. A body that does not match fails the request with
// an error that wraps client.ErrChecksumMismatch.
//
// The checksum covers the body as it is saved, after decompression. For a
// resumed download it covers the whole file, and is checked when the file is
// complete. A file download that does not match leaves the destination as it
// was, and a resumed download that does not match removes its partial file,
// so the next resume starts again. Error responses, such as a 404, are not
// checked. A checksum that is not valid hex of the hash's size fails the
// request before it is sent.
//
// SetChecksum(nil, "") removes a checksum.
//
// Example usage:
//
//	opt := options.New().
//	    SetFileOutput("/path/file.zip").
//	    SetChecksum(sha256.New, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
func (opt *Option) SetChecksum(newHash func() hash.Hash, expected string) *Option {
	opt.mu.Lock()
	opt.Checksum = ChecksumConfig{New: newHash, Expected: expected}
	opt.explicit |= settingChecksum
	opt.mu.Unlock()
	return opt
}

// EnableDigestCheck checks the checksum a server sends with a response, in a
// Repr-Digest or Content-Digest header (RFC 9530). This is the default. A
// response that does not match fails the request with an error that wraps
// client.ErrChecksumMismatch.
//
// Only the sha-256 and sha-512 algorithms are checked, and sha-512 is preferred
// when a header has both. A header with no known algorithm is ignored. The
// checksum covers the bytes the server sent, so a response that net/http
// decompresses itself, such as gzip, is not checked. Error responses are not
// checked. A resumed download is checked against Repr-Digest, which covers the
// whole file, when the file is complete. Any other range response is checked
// against Content-Digest, which covers only the bytes it holds.
func (opt *Option) EnableDigestCheck() *Option {
	opt.mu.Lock()
	opt.SkipDigestCheck = false
	opt.explicit |= settingDigestCheck
	opt.mu.Unlock()
	return opt
}

// DisableDigestCheck stops checking the checksum a server sends with a
// response. Use it for a server that sends wrong checksums.
func (opt *Option) DisableDigestCheck() *Option {
	opt.mu.Lock()
	opt.SkipDigestCheck = true
	opt.explicit |= settingDigestCheck
	opt.mu.Unlock()
	return opt
}
