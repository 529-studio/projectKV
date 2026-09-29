package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLocalStorage_InterfaceCompliance(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	var _ Storage = store
}

func TestLocalStorage_NewInvalidRoot(t *testing.T) {
	_, err := NewLocalStorage("")
	if err == nil {
		t.Fatal("expected error for empty root, got nil")
	}

	_, err = NewLocalStorage("   ")
	if err == nil {
		t.Fatal("expected error for whitespace root, got nil")
	}
}

func TestLocalStorage_PutAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	ctx := context.Background()
	testPath := "proofs/frame1.jpg"
	content := []byte("JPEG_IMAGE_DATA_SAMPLE")

	// Put
	if err := store.Put(ctx, testPath, bytes.NewReader(content)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Exists
	exists, err := store.Exists(ctx, testPath)
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected file to exist, but Exists returned false")
	}

	// Get
	rc, err := store.Get(ctx, testPath)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()

	readData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	if !bytes.Equal(readData, content) {
		t.Fatalf("content mismatch: expected %q, got %q", content, readData)
	}
}

func TestLocalStorage_Subdirectories(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	ctx := context.Background()
	nestedPath := "2026-09-29/cam_abc/1727600000_jam.jpg"
	content := []byte("traffic_alert_proof")

	if err := store.Put(ctx, nestedPath, bytes.NewReader(content)); err != nil {
		t.Fatalf("Put to nested path failed: %v", err)
	}

	// Verify physical file location
	fullPath := filepath.Join(tmpDir, filepath.FromSlash(nestedPath))
	if _, err := os.Stat(fullPath); err != nil {
		t.Fatalf("file not found on disk at %s: %v", fullPath, err)
	}

	rc, err := store.Get(ctx, nestedPath)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()

	readData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if !bytes.Equal(readData, content) {
		t.Fatalf("data mismatch")
	}
}

func TestLocalStorage_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	ctx := context.Background()
	targetPath := "atomic/sample.bin"
	data := bytes.Repeat([]byte("A"), 1024*64)

	if err := store.Put(ctx, targetPath, bytes.NewReader(data)); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Ensure no leftover .tmp files exist in the directory
	dir := filepath.Join(tmpDir, "atomic")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".tmp.") {
			t.Errorf("leftover temporary file found: %s", entry.Name())
		}
	}
}

func TestLocalStorage_DeleteAndNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	ctx := context.Background()
	path := "delete_me.txt"

	// Delete non-existent file
	if err := store.Delete(ctx, path); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for deleting non-existent file, got %v", err)
	}

	// Get non-existent file
	if _, err := store.Get(ctx, path); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for getting non-existent file, got %v", err)
	}

	// Exists for non-existent file
	exists, err := store.Exists(ctx, path)
	if err != nil || exists {
		t.Fatalf("expected exists=false, err=nil; got exists=%v, err=%v", exists, err)
	}

	// Create then delete
	if err := store.Put(ctx, path, strings.NewReader("hello")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if err := store.Delete(ctx, path); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Now should return ErrNotFound
	if _, err := store.Get(ctx, path); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after deletion, got %v", err)
	}
}

func TestLocalStorage_DirectoryAsPath(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	ctx := context.Background()
	// Create a file in a subfolder
	if err := store.Put(ctx, "folder/file.txt", strings.NewReader("hi")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Exists on directory path should return false
	exists, err := store.Exists(ctx, "folder")
	if err != nil {
		t.Fatalf("Exists returned error: %v", err)
	}
	if exists {
		t.Fatal("expected directory path to return exists=false")
	}

	// Get on directory path should return ErrNotFound
	_, err = store.Get(ctx, "folder")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for directory path, got %v", err)
	}
}

func TestLocalStorage_PathTraversal(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	ctx := context.Background()
	maliciousPaths := []string{
		"../secret.txt",
		"../../etc/passwd",
		"sub/../../secret.txt",
		"/etc/passwd",
		"/absolute/path",
		"\\windows\\path",
		"c:\\windows",
		".",
		"..",
		"...",
		"",
		"   ",
		"path\x00traversal.txt",
	}

	for _, p := range maliciousPaths {
		t.Run("Put_"+p, func(t *testing.T) {
			err := store.Put(ctx, p, strings.NewReader("bad"))
			if !errors.Is(err, ErrInvalidPath) {
				t.Errorf("path %q: expected ErrInvalidPath, got %v", p, err)
			}
		})

		t.Run("Get_"+p, func(t *testing.T) {
			_, err := store.Get(ctx, p)
			if !errors.Is(err, ErrInvalidPath) {
				t.Errorf("path %q: expected ErrInvalidPath, got %v", p, err)
			}
		})

		t.Run("Delete_"+p, func(t *testing.T) {
			err := store.Delete(ctx, p)
			if !errors.Is(err, ErrInvalidPath) {
				t.Errorf("path %q: expected ErrInvalidPath, got %v", p, err)
			}
		})

		t.Run("Exists_"+p, func(t *testing.T) {
			_, err := store.Exists(ctx, p)
			if !errors.Is(err, ErrInvalidPath) {
				t.Errorf("path %q: expected ErrInvalidPath, got %v", p, err)
			}
		})
	}
}

func TestLocalStorage_ContextCanceled(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	if err := store.Put(ctx, "test.txt", strings.NewReader("data")); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Put, got %v", err)
	}

	if _, err := store.Get(ctx, "test.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Get, got %v", err)
	}

	if err := store.Delete(ctx, "test.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Delete, got %v", err)
	}

	if _, err := store.Exists(ctx, "test.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Exists, got %v", err)
	}
}

func TestLocalStorage_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	ctx := context.Background()
	concurrency := 20
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		idx := i
		go func() {
			defer wg.Done()
			path := filepath.Join("worker", fmt.Sprintf("w_%d", idx), "data.txt")
			data := []byte("concurrency_payload")

			if err := store.Put(ctx, path, bytes.NewReader(data)); err != nil {
				t.Errorf("worker %d Put failed: %v", idx, err)
				return
			}

			exists, err := store.Exists(ctx, path)
			if err != nil || !exists {
				t.Errorf("worker %d Exists failed: exists=%v, err=%v", idx, exists, err)
				return
			}

			rc, err := store.Get(ctx, path)
			if err != nil {
				t.Errorf("worker %d Get failed: %v", idx, err)
				return
			}
			defer rc.Close()

			res, _ := io.ReadAll(rc)
			if !bytes.Equal(res, data) {
				t.Errorf("worker %d content mismatch", idx)
			}

			if err := store.Delete(ctx, path); err != nil {
				t.Errorf("worker %d Delete failed: %v", idx, err)
			}
		}()
	}

	wg.Wait()
}
