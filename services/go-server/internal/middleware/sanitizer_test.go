package middleware_test

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/middleware"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestSecretSanitizer_RedactsGoogleAPIKey(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/test-leak", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "upstream call failed: Post \"https://generativelanguage.googleapis.com/v1beta/models/gemini:generateContent?key=AIzaSyD1234567890abcdef1234567890ab\": context deadline exceeded",
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test-leak", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, "AIzaSyD1234567890abcdef1234567890ab") {
		t.Fatalf("expected Google API key to be redacted, got raw body: %s", body)
	}
	if !strings.Contains(body, "[REDACTED_KEY]") && !strings.Contains(body, "?key=[REDACTED]") {
		t.Errorf("expected redacted token marker in response, got: %s", body)
	}
}

func TestSecretSanitizer_RedactsAWSAccessKey(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/test-aws", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "configured with key AKIAIOSFODNN7EXAMPLE successfully",
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test-aws", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("expected AWS access key to be redacted, got raw body: %s", body)
	}
	if !strings.Contains(body, "[REDACTED_KEY]") {
		t.Errorf("expected [REDACTED_KEY] marker, got: %s", body)
	}
}

func TestSecretSanitizer_PreservesCleanResponses(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/clean", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"count":  42,
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/clean", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if !strings.Contains(body, `"status":"ok"`) || !strings.Contains(body, `"count":42`) {
		t.Errorf("expected clean response body preserved, got: %s", body)
	}
}

func TestSecretSanitizer_BypassesImageStreams(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46} // Fake JPEG header
	r.GET("/snapshot.jpg", func(c *gin.Context) {
		c.Data(http.StatusOK, "image/jpeg", imageData)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/snapshot.jpg", nil)
	r.ServeHTTP(w, req)

	if w.Body.Len() != len(imageData) {
		t.Errorf("expected byte length %d, got %d", len(imageData), w.Body.Len())
	}
}

func TestSecretSanitizer_PreservesStorageObjectKeys(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/snapshots", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"raw_url": "/api/archiver/snapshots/raw?key=2026-10-21%2Fcam1%2F123_sample.jpg",
			"key":     "2026-10-21/cam1/123_sample.jpg",
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/snapshots", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, "[REDACTED]") {
		t.Errorf("expected storage object key in raw_url to NOT be redacted, got: %s", body)
	}
	if !strings.Contains(body, "2026-10-21%2Fcam1%2F123_sample.jpg") {
		t.Errorf("expected original storage object key preserved, got: %s", body)
	}
}

// TestSecretSanitizer_RedactsTelegramBotToken verifies the Issue #246 / S8-T9
// defense-in-depth requirement: a Telegram bot token echoed back in any HTTP
// response body (e.g. a misconfigured upstream error) is scrubbed to
// [REDACTED_BOT_TOKEN] before the bytes leave the server.
func TestSecretSanitizer_RedactsTelegramBotToken(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	rawToken := "7359930142:ZHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw"
	r.GET("/notify-status", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "telegram http post: dial tcp: lookup api.telegram.org: Post /bot" + rawToken + "/sendMessage failed",
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/notify-status", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	if strings.Contains(body, rawToken) {
		t.Fatalf("expected Telegram bot token to be redacted, got raw body: %s", body)
	}
	if !strings.Contains(body, "[REDACTED_BOT_TOKEN]") {
		t.Errorf("expected [REDACTED_BOT_TOKEN] marker in response, got: %s", body)
	}
}

// TestSecretSanitizer_PreservesClockTimes guards against false positives: the
// token pattern requires 8-10 digits followed by a 35-character base64url tail,
// so ordinary clock times, ratios, and short tails must survive untouched.
func TestSecretSanitizer_PreservesClockTimes(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	r.GET("/timings", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"observed_at": "12:34:56",
			"ratio":       "10:1",
			"duration":    "12345678:shorttail",
			"message_id":  7359930142,
		})
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/timings", nil)
	r.ServeHTTP(w, req)

	body := w.Body.String()
	for _, want := range []string{"12:34:56", "10:1", "12345678:shorttail", "7359930142"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected benign value %q preserved, got: %s", want, body)
		}
	}
}

// TestSecretSanitizer_BypassesSSEStream verifies Issue #420:
// text/event-stream responses bypass memory buffering so frames and comments
// reach the wire immediately via http.Flusher without blocking on handler completion.
func TestSecretSanitizer_BypassesSSEStream(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	started := make(chan struct{})
	done := make(chan struct{})

	const eventFrame = "event: camera_update\ndata: {\"cam_id\":\"cam-test\",\"severity\":\"low\"}\n\n"

	r.GET("/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			t.Error("expected c.Writer to implement http.Flusher")
			return
		}

		_, err := c.Writer.Write([]byte(eventFrame))
		if err != nil {
			t.Errorf("write error: %v", err)
			return
		}
		flusher.Flush()

		close(started)
		<-done
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/stream", nil)

	go func() {
		r.ServeHTTP(w, req)
	}()

	<-started
	defer close(done)

	// Handler is still running (blocked on <-done).
	// Verify frames bypassed memory buffering and arrived at recorder immediately.
	if w.Body.Len() == 0 {
		t.Fatalf("expected non-empty recorder body while handler is active, got 0 bytes")
	}
	if !strings.Contains(w.Body.String(), eventFrame) {
		t.Errorf("expected body to contain %q, got: %s", eventFrame, w.Body.String())
	}
	if !w.Flushed {
		t.Errorf("expected flusher to mark recorder as flushed")
	}
}

// TestSecretSanitizer_SSEStreamingEndToEnd spins up a real HTTP server with
// SecretSanitizer enabled and verifies real-time frame and heartbeat delivery
// over a persistent connection from a client perspective.
func TestSecretSanitizer_SSEStreamingEndToEnd(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	eventsToSend := make(chan string, 5)
	clientClosed := make(chan struct{})

	r.GET("/api/live/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")

		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			c.String(http.StatusInternalServerError, "streaming unsupported")
			return
		}
		flusher.Flush()

		ctx := c.Request.Context()
		for {
			select {
			case <-ctx.Done():
				close(clientClosed)
				return
			case msg, ok := <-eventsToSend:
				if !ok {
					return
				}
				_, _ = fmt.Fprint(c.Writer, msg)
				flusher.Flush()
			}
		}
	})

	server := httptest.NewServer(r)
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/api/live/stream", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected Content-Type text/event-stream, got %q", ct)
	}

	reader := bufio.NewReader(resp.Body)

	// Step 1: Send a camera_update event frame and read line-by-line immediately.
	eventsToSend <- "event: camera_update\ndata: {\"cam_id\":\"cam-42\"}\n\n"

	line1, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read line 1: %v", err)
	}
	if strings.TrimSpace(line1) != "event: camera_update" {
		t.Errorf("expected 'event: camera_update', got %q", line1)
	}

	line2, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read line 2: %v", err)
	}
	if strings.TrimSpace(line2) != "data: {\"cam_id\":\"cam-42\"}" {
		t.Errorf("expected 'data: {\"cam_id\":\"cam-42\"}', got %q", line2)
	}

	line3, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read empty line 3: %v", err)
	}
	if strings.TrimSpace(line3) != "" {
		t.Errorf("expected empty line separator, got %q", line3)
	}

	// Step 2: Send a heartbeat comment and verify receipt.
	eventsToSend <- ": heartbeat\n\n"

	hbLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read heartbeat line: %v", err)
	}
	if strings.TrimSpace(hbLine) != ": heartbeat" {
		t.Errorf("expected ': heartbeat', got %q", hbLine)
	}

	// Close client body to trigger context cancellation on server.
	resp.Body.Close()

	select {
	case <-clientClosed:
		// Clean disconnect observed.
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to observe client disconnect")
	}
}

// TestSecretSanitizer_SSEZeroMemoryBuildup verifies that high-volume SSE frames
// stream directly to the underlying response writer without accumulating inside
// the sanitizingResponseWriter memory buffer.
func TestSecretSanitizer_SSEZeroMemoryBuildup(t *testing.T) {
	r := gin.New()
	r.Use(middleware.SecretSanitizer())

	const totalFrames = 500
	expectedBytes := 0

	r.GET("/stream-bulk", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		flusher, ok := c.Writer.(http.Flusher)
		if !ok {
			t.Error("expected http.Flusher")
			return
		}

		for i := range totalFrames {
			frame := fmt.Sprintf("event: camera_update\ndata: {\"seq\":%d}\n\n", i)
			expectedBytes += len(frame)
			_, _ = fmt.Fprint(c.Writer, frame)
			flusher.Flush()
		}
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/stream-bulk", nil)
	r.ServeHTTP(w, req)

	if w.Body.Len() != expectedBytes {
		t.Fatalf("expected exactly %d bytes in body, got %d", expectedBytes, w.Body.Len())
	}
	if !w.Flushed {
		t.Errorf("expected recorder to be marked as flushed")
	}
}
