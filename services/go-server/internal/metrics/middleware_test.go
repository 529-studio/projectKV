package metrics

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestMiddleware(t *testing.T) {
	reg := NewRegistry()

	r := gin.New()
	r.Use(RequestMiddleware(reg))

	r.GET("/api/test-route", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	r.POST("/api/test-post", func(c *gin.Context) {
		c.String(http.StatusCreated, "created")
	})

	// Perform requests
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/test-route", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	}

	reqPost := httptest.NewRequest(http.MethodPost, "/api/test-post", nil)
	wPost := httptest.NewRecorder()
	r.ServeHTTP(wPost, reqPost)
	if wPost.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", wPost.Code)
	}

	// Verify metrics
	var buf bytes.Buffer
	if err := reg.WriteOpenMetrics(&buf); err != nil {
		t.Fatalf("WriteOpenMetrics error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, `warded_http_requests_total{method="GET",path="/api/test-route",status="200"} 3`) {
		t.Errorf("missing GET request count in output:\n%s", out)
	}
	if !strings.Contains(out, `warded_http_requests_total{method="POST",path="/api/test-post",status="201"} 1`) {
		t.Errorf("missing POST request count in output:\n%s", out)
	}
	if !strings.Contains(out, `warded_http_request_duration_seconds_count 4`) {
		t.Errorf("missing duration histogram count in output:\n%s", out)
	}
}
