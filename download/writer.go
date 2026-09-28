package download

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/jpl-au/http-client/options"
)

// fileWriter stages an ordinary download beside its destination.
type fileWriter struct {
	*os.File
	path       string
	terminal   bool
	cleanupErr error
}

// Publish closes the staging file and makes it the destination.
func (w *fileWriter) Publish() error {
	if w.terminal {
		return fs.ErrClosed
	}
	w.terminal = true
	if err := w.File.Close(); err != nil {
		w.cleanupErr = errors.Join(err, os.Remove(w.Name()))
		return w.cleanupErr
	}
	if err := os.Rename(w.Name(), w.path); err != nil {
		w.cleanupErr = os.Remove(w.Name())
		return errors.Join(err, w.cleanupErr)
	}
	return nil
}

// Discard closes and removes unpublished staging data.
func (w *fileWriter) Discard() error {
	if w.terminal {
		return fs.ErrClosed
	}
	return w.Close()
}

// Close discards an unpublished staging file and repeats its cleanup result.
func (w *fileWriter) Close() error {
	if !w.terminal {
		w.terminal = true
		w.cleanupErr = errors.Join(w.File.Close(), os.Remove(w.Name()))
	}
	return w.cleanupErr
}

// partialWriter keeps valid interrupted data for a later resume.
type partialWriter struct {
	*os.File
	path       string
	offset     int64 // Size before this response.
	terminal   bool
	cleanupErr error
}

// Discard rolls back only the bytes this response added.
func (w *partialWriter) Discard() error {
	if w.terminal {
		return fs.ErrClosed
	}
	w.terminal = true
	err := w.Truncate(w.offset)
	w.cleanupErr = w.File.Close()
	return errors.Join(err, w.cleanupErr)
}

// Remove closes and deletes the entire invalid partial file.
func (w *partialWriter) Remove() error {
	if w.terminal {
		return fs.ErrClosed
	}
	w.terminal = true
	w.cleanupErr = errors.Join(w.File.Close(), os.Remove(w.Name()))
	return w.cleanupErr
}

// Publish closes the complete partial file and makes it the destination.
func (w *partialWriter) Publish() error {
	if w.terminal {
		return fs.ErrClosed
	}
	w.terminal = true
	permErr := matchDestinationPermissions(w.File, w.path)
	w.cleanupErr = w.File.Close()
	if permErr != nil || w.cleanupErr != nil {
		return errors.Join(permErr, w.cleanupErr)
	}
	return os.Rename(w.Name(), w.path)
}

// Close keeps an unpublished partial file and repeats its cleanup result.
func (w *partialWriter) Close() error {
	if !w.terminal {
		w.terminal = true
		w.cleanupErr = w.File.Close()
	}
	return w.cleanupErr
}

// matchDestinationPermissions keeps an existing destination's access mode.
// Without a destination, an imported private partial file stays private.
func matchDestinationPermissions(file *os.File, dest string) error {
	info, err := os.Stat(dest)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return file.Chmod(info.Mode().Perm())
}

// createLike creates a file with the destination's permissions, or the usual
// creation permissions when the destination does not exist.
func createLike(name, dest string) (*os.File, error) {
	perm := fs.FileMode(0o666)
	info, err := os.Stat(dest)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	exists := err == nil
	if exists {
		perm = info.Mode().Perm()
	}
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return nil, err
	}
	if exists {
		if err := file.Chmod(perm); err != nil {
			return nil, errors.Join(err, file.Close(), os.Remove(name))
		}
	}
	return file, nil
}

// newWriter opens the response destination using the transfer's continuation
// decision, which is execution state rather than option configuration.
func newWriter(cfg options.ResponseWriter, resume, continuation bool) (io.WriteCloser, error) {
	switch cfg.Type {
	case options.WriteToFile:
		if cfg.FilePath == "" {
			return nil, options.ErrMissingFilePath
		}
		if resume {
			partial := options.PartialPath(cfg.FilePath)
			var file *os.File
			var offset int64
			var err error
			if continuation {
				file, err = os.OpenFile(partial, os.O_WRONLY|os.O_APPEND, 0)
				if err == nil {
					err = matchDestinationPermissions(file, cfg.FilePath)
					if err == nil {
						offset, err = file.Seek(0, io.SeekEnd)
					}
					if err != nil {
						err = errors.Join(err, file.Close())
					}
				}
			} else {
				if err := os.Remove(partial); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return nil, fmt.Errorf("failed to remove partial file: %w", err)
				}
				file, err = createLike(partial, cfg.FilePath)
			}
			if err != nil {
				return nil, fmt.Errorf("failed to open partial file: %w", err)
			}
			return &partialWriter{File: file, path: cfg.FilePath, offset: offset}, nil
		}
		file, err := createLike(cfg.FilePath+"."+rand.Text()+".part", cfg.FilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create file: %w", err)
		}
		return &fileWriter{File: file, path: cfg.FilePath}, nil
	case options.WriteToBuffer:
		if cfg.FilePath != "" {
			return nil, options.ErrUnexpectedFilePath
		}
		return &options.WriteCloserBuffer{Buffer: &bytes.Buffer{}}, nil
	default:
		return nil, options.ErrInvalidWriterType
	}
}
