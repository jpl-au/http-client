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
