package download

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"

	"github.com/jpl-au/http-client/options"
)

// checksum is the hash of a downloaded body and the value it must match.
type checksum struct {
	hash   hash.Hash
	want   []byte
	source string // Where want came from, for a server's checksum. Empty for SetChecksum.
}

// newChecksum returns the checksum set with SetChecksum, or nil when there is
// none. It returns an error when the expected value is not hex of the hash's
// size, so the request fails before it is sent.
func newChecksum(cfg options.ChecksumConfig) (*checksum, error) {
	if cfg.New == nil {
		return nil, nil
	}
	want, err := hex.DecodeString(cfg.Expected)
	if err != nil {
		return nil, fmt.Errorf("invalid checksum %q: %w", cfg.Expected, err)
	}
	h := cfg.New()
	if len(want) != h.Size() {
		return nil, fmt.Errorf("invalid checksum %q: it has %d bytes, the hash has %d", cfg.Expected, len(want), h.Size())
	}
	return &checksum{hash: h, want: want}, nil
}

// check returns an error wrapping ErrChecksumMismatch when the bytes hashed so
// far do not match.
func (c *checksum) check() error {
	got := c.hash.Sum(nil)
	switch {
	case bytes.Equal(got, c.want):
		return nil
	case c.source != "":
		return fmt.Errorf("%w %s: got %x, want %x", ErrChecksumMismatch, c.source, got, c.want)
	default:
		return fmt.Errorf("%w: got %x, want %x", ErrChecksumMismatch, got, c.want)
	}
}

// hashFile adds the whole file at path to the checksum.
func (c *checksum) hashFile(path string) (err error) {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to read file for checksum: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err := io.Copy(c.hash, file); err != nil {
		return fmt.Errorf("failed to read file for checksum: %w", err)
	}
	return nil
}

// hashPartialFile adds the first size bytes of the partial file for dest to
// the checksum, so a resumed download is checked as a whole file.
func (c *checksum) hashPartialFile(dest string, size int64) (err error) {
	file, err := os.Open(options.PartialPath(dest))
	if err != nil {
		return fmt.Errorf("failed to read partial file for checksum: %w", err)
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err := io.CopyN(c.hash, file, size); err != nil {
		return fmt.Errorf("failed to read partial file for checksum: %w", err)
	}
	return nil
}
