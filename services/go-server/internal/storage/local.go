package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage implements the Storage interface for the local filesystem.
type LocalStorage struct {
	root string
}

// NewLocalStorage creates a new LocalStorage rooted at the specified directory.
// The root directory will be created if it does not already exist.
func NewLocalStorage(root string) (*LocalStorage, error) {
	trimmed := strings.TrimSpace(root)
	if trimmed == "" {
		return nil, errors.New("storage: root directory cannot be empty")
	}

	cleanRoot := filepath.Clean(trimmed)
	absRoot, err := filepath.Abs(cleanRoot)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to resolve root path: %w", err)
	}

	if err := os.MkdirAll(absRoot, 0755); err != nil {
		return nil, fmt.Errorf("storage: failed to create root directory: %w", err)
	}

	return &LocalStorage{root: absRoot}, nil
}

// resolve sanitizes input path and validates against path traversal outside root.
func (s *LocalStorage) resolve(relPath string) (string, error) {
	trimmed := strings.TrimSpace(relPath)
	if trimmed == "" || strings.ContainsRune(trimmed, 0) {
		return "", ErrInvalidPath
	}

	// Reject absolute paths attempting to bypass the storage root
	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "\\") || strings.Contains(trimmed, ":") {
		return "", ErrInvalidPath
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrInvalidPath
	}

	target := filepath.Join(s.root, cleaned)

	// Ensure the resolved target is strictly contained within root
	rel, err := filepath.Rel(s.root, target)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", ErrInvalidPath
	}

	return target, nil
}

// Put writes data from the reader to the specified path atomically using a temporary file.
func (s *LocalStorage) Put(ctx context.Context, path string, data io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	target, err := s.resolve(path)
	if err != nil {
		return err
	}

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("storage: failed to create directory: %w", err)
	}

	// Create temp file in the same directory to ensure atomic os.Rename across filesystem boundaries
	pattern := fmt.Sprintf(".tmp.%s.*", filepath.Base(target))
	tmpFile, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("storage: failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup in case of failure before successful rename
	committed := false
	defer func() {
		if !committed {
			_ = tmpFile.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := io.Copy(tmpFile, data); err != nil {
		return fmt.Errorf("storage: write failed: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("storage: sync failed: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("storage: close failed: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("storage: atomic rename failed: %w", err)
	}

	committed = true
	return nil
}

// Get opens the object at path for reading. Caller is responsible for closing the returned ReadCloser.
func (s *LocalStorage) Get(ctx context.Context, path string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	target, err := s.resolve(path)
	if err != nil {
		return nil, err
	}

	fi, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if fi.IsDir() {
		return nil, ErrNotFound
	}

	f, err := os.Open(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return f, nil
}

// Delete removes the object at path. Returns ErrNotFound if the object does not exist.
func (s *LocalStorage) Delete(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	target, err := s.resolve(path)
	if err != nil {
		return err
	}

	err = os.Remove(target)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}

	return nil
}

// Exists checks if the object at path exists and is a regular file.
func (s *LocalStorage) Exists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	target, err := s.resolve(path)
	if err != nil {
		return false, err
	}

	fi, err := os.Stat(target)
	if err == nil {
		return !fi.IsDir(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// List returns all stored items whose keys match the given prefix.
func (s *LocalStorage) List(ctx context.Context, prefix string) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(prefix)
	if strings.ContainsRune(trimmed, 0) || filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "\\") || strings.Contains(trimmed, ":") {
		return nil, ErrInvalidPath
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return nil, ErrInvalidPath
	}

	// Normalize prefix to forward slashes for matching
	normalizedPrefix := filepath.ToSlash(trimmed)

	var items []Item
	err := filepath.WalkDir(s.root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".tmp.") {
			return nil
		}

		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}

		key := filepath.ToSlash(rel)
		if normalizedPrefix == "" || strings.HasPrefix(key, normalizedPrefix) {
			info, err := d.Info()
			if err != nil {
				return err
			}
			items = append(items, Item{
				Key:          key,
				Size:         info.Size(),
				LastModified: info.ModTime(),
			})
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if items == nil {
		items = []Item{}
	}
	return items, nil
}
