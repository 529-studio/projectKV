package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSwaggerEndpoints(t *testing.T) {
	r := NewServer()

	tests := []struct {
		url          string
		expectedCode int
	}{
		{"/healthz", http.StatusOK},
		{"/swagger", http.StatusMovedPermanently},
		{"/swagger/", http.StatusMovedPermanently},
		{"/swagger/index.html", http.StatusOK},
		{"/swagger/doc.json", http.StatusOK},
	}

	for _, tt := range tests {
		req, _ := http.NewRequest(http.MethodGet, tt.url, nil)
		req.RequestURI = tt.url
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != tt.expectedCode {
			t.Errorf("url %s: expected status %d, got %d", tt.url, tt.expectedCode, w.Code)
		}
	}
}
