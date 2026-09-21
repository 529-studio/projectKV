package proxycache_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/proxycache"
	"github.com/gin-gonic/gin"
)

// ============================================================
// Cache unit tests
// ============================================================

func TestCache_HitBeforeTTL(t *testing.T) {
	c := proxycache.NewCache(30 * time.Second)
	calls := 0
	fetch := func() ([]byte, string, error) {
		calls++
		return []byte("img"), "image/jpeg", nil
	}

	data1, ct1, hit1, err1 := c.GetOrFetch("cam1", fetch)
	if err1 != nil {
		t.Fatalf("unexpected error on first call: %v", err1)
	}
	if hit1 {
		t.Error("first call must be a cache MISS")
	}
	if ct1 != "image/jpeg" {
		t.Errorf("unexpected content-type: %s", ct1)
	}

	data2, _, hit2, err2 := c.GetOrFetch("cam1", fetch)
	if err2 != nil {
		t.Fatalf("unexpected error on second call: %v", err2)
	}
	if !hit2 {
		t.Error("second call must be a cache HIT")
	}
	if string(data1) != string(data2) {
		t.Error("cached data must equal original data")
	}
	if calls != 1 {
		t.Errorf("fetch should have been called exactly once, got %d", calls)
	}
}

func TestCache_MissAfterTTL(t *testing.T) {
	c := proxycache.NewCache(50 * time.Millisecond)
	calls := 0
	fetch := func() ([]byte, string, error) {
		calls++
		return []byte("img"), "image/jpeg", nil
	}

	_, _, _, _ = c.GetOrFetch("cam2", fetch)
	time.Sleep(80 * time.Millisecond) // let TTL expire
	_, _, hit, _ := c.GetOrFetch("cam2", fetch)

	if hit {
		t.Error("entry should have expired — expected cache MISS")
	}
	if calls != 2 {
		t.Errorf("expected 2 fetch calls (miss + re-fetch), got %d", calls)
	}
}

func TestCache_SingleflightDedup(t *testing.T) {
	c := proxycache.NewCache(30 * time.Second)
	var callCount atomic.Int64

	// Use a ready channel so the fetch blocks until all goroutines have called GetOrFetch.
	ready := make(chan struct{})

	fetch := func() ([]byte, string, error) {
		callCount.Add(1)
		<-ready // wait until we explicitly release
		return []byte("img"), "image/jpeg", nil
	}

	n := 20
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, _, _, errs[i] = c.GetOrFetch("cam3", fetch)
		}()
	}

	// Give goroutines time to pile up inside GetOrFetch / singleflight.Do.
	time.Sleep(20 * time.Millisecond)
	close(ready) // release all at once
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			t.Errorf("unexpected error from concurrent GetOrFetch: %v", e)
		}
	}
	if callCount.Load() != 1 {
		t.Errorf("singleflight should deduplicate to exactly 1 upstream call, got %d", callCount.Load())
	}
}

func TestCache_FetchError_NotCached(t *testing.T) {
	c := proxycache.NewCache(30 * time.Second)
	calls := 0
	fetch := func() ([]byte, string, error) {
		calls++
		return nil, "", errors.New("upstream down")
	}

	_, _, _, err := c.GetOrFetch("cam4", fetch)
	if err == nil {
		t.Fatal("expected error from failing fetch")
	}
	// After an error the entry must not be cached — next call should retry.
	calls = 0
	_, _, _, _ = c.GetOrFetch("cam4", fetch)
	if calls != 1 {
		t.Errorf("failed fetch must not be cached; expected retry fetch, calls=%d", calls)
	}
}

func TestCache_Purge(t *testing.T) {
	c := proxycache.NewCache(30 * time.Millisecond)
	fetch := func() ([]byte, string, error) { return []byte("img"), "image/jpeg", nil }
	_, _, _, _ = c.GetOrFetch("cam5", fetch)
	time.Sleep(50 * time.Millisecond)
	c.Purge()
	// After purge, a fresh call must be a miss.
	_, _, hit, _ := c.GetOrFetch("cam5", fetch)
	if hit {
		t.Error("after Purge and TTL expiry, GetOrFetch must be a cache MISS")
	}
}

// ============================================================
// Handler unit tests
// ============================================================

func newTestRouter(handler *proxycache.Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	handler.RegisterRoutes(api)
	return r
}

func TestHandler_ReturnsImageFromUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEG_DATA"))
	}))
	defer upstream.Close()

	cache := proxycache.NewCache(30 * time.Second)
	h := proxycache.NewHandler(cache, upstream.URL, nil)
	router := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/snapshot/cam1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "JPEG_DATA" {
		t.Errorf("unexpected body: %q", w.Body.String())
	}
	if w.Header().Get("X-Cache-Status") != "MISS" {
		t.Errorf("first request must be MISS, got %q", w.Header().Get("X-Cache-Status"))
	}
}

func TestHandler_CacheHitOnSecondRequest(t *testing.T) {
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEG_DATA"))
	}))
	defer upstream.Close()

	cache := proxycache.NewCache(30 * time.Second)
	h := proxycache.NewHandler(cache, upstream.URL, nil)
	router := newTestRouter(h)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/snapshot/cam1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, w.Code)
		}
	}
	if upstreamCalls != 1 {
		t.Errorf("upstream should be called exactly once (singleflight/cache), got %d", upstreamCalls)
	}
}

func TestHandler_TouchCallbackFired(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("JPEG_DATA"))
	}))
	defer upstream.Close()

	var touchedID string
	cache := proxycache.NewCache(30 * time.Second)
	h := proxycache.NewHandler(cache, upstream.URL, func(id, name, zone, snapshotURL string) {
		touchedID = id
	})
	router := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/snapshot/camXYZ", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if touchedID != "camXYZ" {
		t.Errorf("Touch callback must be called with camera ID, got %q", touchedID)
	}
}

func TestHandler_UpstreamError_Returns502(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	cache := proxycache.NewCache(30 * time.Second)
	h := proxycache.NewHandler(cache, upstream.URL, nil)
	router := newTestRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/snapshot/cam1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", w.Code)
	}
}

func TestHandler_MissingID_Returns400(t *testing.T) {
	cache := proxycache.NewCache(30 * time.Second)
	h := proxycache.NewHandler(cache, "http://example.com", nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Register a route without :id to simulate empty param
	r.GET("/api/snapshot", func(c *gin.Context) {
		// manually call handler with no param set — simulate empty id
		c.Params = append(c.Params, gin.Param{Key: "id", Value: ""})
		h.GetSnapshot(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/snapshot", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing id, got %d", w.Code)
	}
}
