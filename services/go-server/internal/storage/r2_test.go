package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// mockS3Client implements S3API for unit testing without live network calls.
type mockS3Client struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
}

func newMockS3Client() *mockS3Client {
	return &mockS3Client{
		objects: make(map[string][]byte),
		types:   make(map[string]string),
	}
}

func (m *mockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key := aws.ToString(params.Key)
	data, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	m.objects[key] = data
	if params.ContentType != nil {
		m.types[key] = *params.ContentType
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key := aws.ToString(params.Key)
	data, ok := m.objects[key]
	if !ok {
		return nil, &types.NoSuchKey{
			Message: aws.String("The specified key does not exist."),
		}
	}
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(m.types[key]),
	}, nil
}

func (m *mockS3Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key := aws.ToString(params.Key)
	delete(m.objects, key)
	delete(m.types, key)
	return &s3.DeleteObjectOutput{}, nil
}

func (m *mockS3Client) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key := aws.ToString(params.Key)
	data, ok := m.objects[key]
	if !ok {
		return nil, &types.NotFound{
			Message: aws.String("NotFound"),
		}
	}
	return &s3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(m.types[key]),
	}, nil
}

func TestR2Storage_InterfaceCompliance(t *testing.T) {
	store := NewR2StorageWithClient(newMockS3Client(), "test-bucket", "https://file.529studio.site")
	var _ Storage = store
}

func TestR2Storage_PutAndGet(t *testing.T) {
	mockClient := newMockS3Client()
	store := NewR2StorageWithClient(mockClient, "warded-proofs", "https://file.529studio.site")

	ctx := context.Background()
	key := "2026-10-10/cam123/1791550000_sample.jpg"
	payload := []byte("JPEG_SAMPLE_IMAGE_DATA")

	err := store.Put(ctx, key, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify content type
	if mockClient.types[key] != "image/jpeg" {
		t.Errorf("expected ContentType='image/jpeg', got %q", mockClient.types[key])
	}

	// Exists
	exists, err := store.Exists(ctx, key)
	if err != nil || !exists {
		t.Fatalf("expected Exists=true, got exists=%v err=%v", exists, err)
	}

	// Get
	rc, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()

	readData, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if !bytes.Equal(readData, payload) {
		t.Fatalf("content mismatch: expected %q, got %q", payload, readData)
	}
}

func TestR2Storage_Delete(t *testing.T) {
	mockClient := newMockS3Client()
	store := NewR2StorageWithClient(mockClient, "warded-proofs", "https://file.529studio.site")

	ctx := context.Background()
	key := "2026-10-10/cam123/delete_me.jpg"

	if err := store.Put(ctx, key, strings.NewReader("hello")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	exists, err := store.Exists(ctx, key)
	if err != nil || exists {
		t.Fatalf("expected exists=false after deletion, got exists=%v err=%v", exists, err)
	}

	_, err = store.Get(ctx, key)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on Get after deletion, got %v", err)
	}
}

func TestR2Storage_PathSanitization(t *testing.T) {
	mockClient := newMockS3Client()
	store := NewR2StorageWithClient(mockClient, "warded-proofs", "https://file.529studio.site")
	ctx := context.Background()

	badPaths := []string{
		"../secret.txt",
		"../../etc/passwd",
		"sub/../../secret.txt",
		"",
		"   ",
		"null\x00byte.jpg",
		"c:\\windows",
	}

	for _, p := range badPaths {
		t.Run("Put_"+p, func(t *testing.T) {
			err := store.Put(ctx, p, strings.NewReader("data"))
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

func TestR2Storage_PublicURL(t *testing.T) {
	mockClient := newMockS3Client()
	store := NewR2StorageWithClient(mockClient, "warded-proofs", "https://file.529studio.site")

	url := store.PublicURL("2026-10-10/cam123/1791550000_sample.jpg")
	expected := "https://file.529studio.site/2026-10-10/cam123/1791550000_sample.jpg"
	if url != expected {
		t.Errorf("expected %q, got %q", expected, url)
	}

	// Should strip leading slash cleanly
	urlSlash := store.PublicURL("/2026-10-10/cam123/1791550000_sample.jpg")
	if urlSlash != expected {
		t.Errorf("expected %q, got %q", expected, urlSlash)
	}
}

func TestR2Storage_ContextCanceled(t *testing.T) {
	mockClient := newMockS3Client()
	store := NewR2StorageWithClient(mockClient, "warded-proofs", "https://file.529studio.site")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled context

	key := "2026-10-10/cam123/test.jpg"
	if err := store.Put(ctx, key, strings.NewReader("test")); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Put, got %v", err)
	}

	if _, err := store.Get(ctx, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Get, got %v", err)
	}

	if err := store.Delete(ctx, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Delete, got %v", err)
	}

	if _, err := store.Exists(ctx, key); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled on Exists, got %v", err)
	}
}
