package storage

import (
	"context"
	"errors"
	"io"
)

var (
	// ErrNotFound is returned when the requested object does not exist.
	ErrNotFound = errors.New("storage: object not found")

	// ErrInvalidPath is returned when a path is empty, malformed, or attempts directory traversal.
	ErrInvalidPath = errors.New("storage: invalid path traversal")
)

// Storage defines the minimal abstraction for object storage.
type Storage interface {
	// Put writes data from the reader to the specified path.
	// Intermediate directories are created automatically. Writes are atomic.
	Put(ctx context.Context, path string, data io.Reader) error

	// Get retrieves a reader for the object at the specified path.
	// The caller is responsible for closing the returned ReadCloser.
	Get(ctx context.Context, path string) (io.ReadCloser, error)

	// Delete removes the object at the specified path.
	Delete(ctx context.Context, path string) error

	// Exists checks if an object exists at the specified path.
	Exists(ctx context.Context, path string) (bool, error)
}
