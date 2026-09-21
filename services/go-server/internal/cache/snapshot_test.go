package cache_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/cache"
)

func TestSnapshotCache_TTLAndHit(t *testing.T) {
	c := cache.NewSnapshotCache(100 * time.Millisecond)

	c.Set("cam1", []byte("image-data-1"), "image/jpeg")

	// Immediate get: Hit
	item, hit := c.Get("cam1")
	if !hit || string(item.Buffer) != "image-data-1" || item.ContentType != "image/jpeg" {
		t.Fatalf("expected cache hit with image-data-1, got hit=%v", hit)
	}

	// After TTL: Miss
	time.Sleep(120 * time.Millisecond)
	_, hit = c.Get("cam1")
	if hit {
		t.Fatalf("expected cache miss after TTL expiration, but got hit")
	}
}

func TestSnapshotCache_SingleflightDedup(t *testing.T) {
	c := cache.NewSnapshotCache(500 * time.Millisecond)

	var upstreamCalls int32
	fetchFunc := func() ([]byte, string, error) {
		atomic.AddInt32(&upstreamCalls, 1)
		time.Sleep(50 * time.Millisecond) // Simulate network I/O
		return []byte("fresh-snapshot"), "image/jpeg", nil
	}

	// Launch 20 concurrent goroutines requesting the same camera ID
	var wg sync.WaitGroup
	const concurrentRequests = 20

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf, cType, _, err := c.FetchOrGet("cam_concurrent", fetchFunc)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if string(buf) != "fresh-snapshot" || cType != "image/jpeg" {
				t.Errorf("unexpected response content")
			}
		}()
	}

	wg.Wait()

	// Upstream fetch should be called EXACTLY once due to singleflight!
	calls := atomic.LoadInt32(&upstreamCalls)
	if calls != 1 {
		t.Fatalf("expected exactly 1 upstream call, got %d", calls)
	}
}

func TestSnapshotCache_ErrorHandling(t *testing.T) {
	c := cache.NewSnapshotCache(500 * time.Millisecond)

	expectedErr := errors.New("upstream timeout")
	fetchFunc := func() ([]byte, string, error) {
		return nil, "", expectedErr
	}

	_, _, _, err := c.FetchOrGet("cam_err", fetchFunc)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}

	// Cache should not hold failed responses
	_, hit := c.Get("cam_err")
	if hit {
		t.Fatalf("error response should not be cached")
	}
}
