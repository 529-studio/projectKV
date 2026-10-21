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

func TestLocalStorage_List(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorage failed: %v", err)
	}

	ctx := context.Background()

	files := map[string][]byte{
		"2026-10-10/cam1/frame1.jpg": []byte("1234567890"),
		"2026-10-10/cam1/frame2.jpg": []byte("12345678901234567890"),
		"2026-10-10/cam2/frame3.jpg": []byte("123456789012345"),
		"2026-10-11/cam1/frame4.jpg": []byte("123456789012345678901234567890"),
	}

	for p, data := range files {
		if err := store.Put(ctx, p, bytes.NewReader(data)); err != nil {
			t.Fatalf("Put %s failed: %v", p, err)
		}
	}

	t.Run("List all with empty prefix", func(t *testing.T) {
		items, err := store.List(ctx, "")
		if err != nil {
			t.Fatalf("List empty prefix failed: %v", err)
		}
		if len(items) != 4 {
			t.Fatalf("expected 4 items, got %d", len(items))
		}
	})

	t.Run("List prefix 2026-10-10", func(t *testing.T) {
		items, err := store.List(ctx, "2026-10-10")
		if err != nil {
			t.Fatalf("List prefix failed: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		for _, it := range items {
			if !strings.HasPrefix(it.Key, "2026-10-10/") {
				t.Errorf("expected key to start with 2026-10-10/, got %s", it.Key)
			}
			if it.Size <= 0 {
				t.Errorf("expected positive size for %s, got %d", it.Key, it.Size)
			}
			if it.LastModified.IsZero() {
				t.Errorf("expected non-zero LastModified for %s", it.Key)
			}
		}
	})

	t.Run("List prefix 2026-10-10/cam1", func(t *testing.T) {
		items, err := store.List(ctx, "2026-10-10/cam1")
		if err != nil {
			t.Fatalf("List prefix failed: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}
	})

	t.Run("List nonexistent prefix returns empty slice", func(t *testing.T) {
		items, err := store.List(ctx, "2026-01-01")
		if err != nil {
			t.Fatalf("List nonexistent prefix failed: %v", err)
		}
		if len(items) != 0 {
			t.Fatalf("expected 0 items, got %d", len(items))
		}
	})

	t.Run("List with path traversal returns ErrInvalidPath", func(t *testing.T) {
		traversals := []string{"../", "../../etc", "/absolute/path"}
		for _, p := range traversals {
			_, err := store.List(ctx, p)
			if !errors.Is(err, ErrInvalidPath) {
				t.Errorf("expected ErrInvalidPath for %q, got %v", p, err)
			}
		}
	})
}

func TestLocalStorage_ListDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewLocalStorage(tmpDir)
	if err != nil {
		t.Fatalf("failed to create LocalStorage: %v", err)
	}

	ctx := context.Background()

	files := map[string][]byte{
		"2026-10-10/cam1/frame1.jpg": []byte("sample1"),
		"2026-10-10/cam2/frame2.jpg": []byte("sample2"),
		"2026-10-11/cam1/frame3.jpg": []byte("sample3"),
		"feedback/report1.json":      []byte("{}"),
		"root_file.txt":              []byte("root"),
	}

	for p, data := range files {
		if err := store.Put(ctx, p, bytes.NewReader(data)); err != nil {
			t.Fatalf("Put %s failed: %v", p, err)
		}
	}

	t.Run("List root directories with empty prefix", func(t *testing.T) {
		dirs, err := store.ListDirectories(ctx, "")
		if err != nil {
			t.Fatalf("ListDirectories empty prefix failed: %v", err)
		}
		expected := []string{"2026-10-10", "2026-10-11", "feedback"}
		if len(dirs) != len(expected) {
			t.Fatalf("expected %d dirs, got %d: %v", len(expected), len(dirs), dirs)
		}
		for i, exp := range expected {
			if dirs[i] != exp {
				t.Errorf("index %d: expected %q, got %q", i, exp, dirs[i])
			}
		}
	})

	t.Run("List subdirectories under 2026-10-10", func(t *testing.T) {
		dirs, err := store.ListDirectories(ctx, "2026-10-10")
		if err != nil {
			t.Fatalf("ListDirectories sub prefix failed: %v", err)
		}
		expected := []string{"2026-10-10/cam1", "2026-10-10/cam2"}
		if len(dirs) != len(expected) {
			t.Fatalf("expected %d subdirs, got %d: %v", len(expected), len(dirs), dirs)
		}
		for i, exp := range expected {
			if dirs[i] != exp {
				t.Errorf("index %d: expected %q, got %q", i, exp, dirs[i])
			}
		}
	})

	t.Run("List invalid path traversals", func(t *testing.T) {
		bad := []string{"../", "../../etc", "/absolute/path", "path\x00null"}
		for _, b := range bad {
			_, err := store.ListDirectories(ctx, b)
			if !errors.Is(err, ErrInvalidPath) {
				t.Errorf("expected ErrInvalidPath for %q, got %v", b, err)
			}
		}
	})

	t.Run("List non-existent prefix returns empty slice", func(t *testing.T) {
		dirs, err := store.ListDirectories(ctx, "nonexistent-date")
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if len(dirs) != 0 {
			t.Errorf("expected 0 dirs, got %d: %v", len(dirs), dirs)
		}
	})

	t.Run("ListDirectories context canceled", func(t *testing.T) {
		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := store.ListDirectories(cancelCtx, "")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	})
}
