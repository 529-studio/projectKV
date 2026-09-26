package live_test

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/live"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newTestHub constructs a Hub with a short heartbeat interval (50ms)
// so tests run quickly without waiting 15s for heartbeat/cleanup ticks.
func newTestHub() *live.Hub {
	return live.NewHubWithConfig(live.DefaultClientBuffer, 50*time.Millisecond, nil)
}

// newTestServer spins up a real httptest.Server backed by a Gin router with
// the hub's SSE route mounted.
func newTestServer(hub *live.Hub) *httptest.Server {
	r := gin.New()
	hub.RegisterRoutes(r)
	return httptest.NewServer(r)
}

// openSSE opens an SSE connection to the test server and returns the response.
// Caller is responsible for closing resp.Body.
func openSSE(t *testing.T, serverURL string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, serverURL+live.RouteStream, nil)
	if err != nil {
		t.Fatalf("create SSE request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
		// Do not follow redirects; we need the raw stream.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("open SSE connection: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for SSE, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected Content-Type text/event-stream, got %q", ct)
	}
	return resp
}

// readEvent reads the next non-comment SSE frame from resp.Body.
// Returns the event name and data fields.
func readEvent(resp *http.Response, timeout time.Duration) (event, data string, err error) {
	type result struct {
		event, data string
		err         error
	}
	ch := make(chan result, 1)

	go func() {
		scanner := bufio.NewScanner(resp.Body)
		var ev, da string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, ":") {
				// SSE comment (e.g. heartbeat) — skip.
				continue
			}
			if strings.HasPrefix(line, "event:") {
				ev = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				da = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			} else if line == "" && (ev != "" || da != "") {
				// Empty line = event boundary.
				ch <- result{event: ev, data: da}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- result{err: err}
		} else {
			ch <- result{err: fmt.Errorf("SSE stream closed before event")}
		}
	}()

	select {
	case r := <-ch:
		return r.event, r.data, r.err
	case <-time.After(timeout):
		return "", "", fmt.Errorf("timeout waiting for SSE event")
	}
}

// ---------- Tests ----------

func TestHub_DefaultHeartbeatInterval(t *testing.T) {
	if live.HeartbeatInterval != 15*time.Second {
		t.Fatalf("expected HeartbeatInterval to be 15s, got %v", live.HeartbeatInterval)
	}
	hub := live.NewHub()
	defer hub.Close()
	if c := hub.ClientCount(); c != 0 {
		t.Fatalf("expected 0 clients at start, got %d", c)
	}
}

func TestHub_Heartbeat(t *testing.T) {
	hub := newTestHub()
	defer hub.Close()

	srv := newTestServer(hub)
	defer srv.Close()

	resp := openSSE(t, srv.URL)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	receivedHeartbeat := make(chan bool, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == ": heartbeat" {
				receivedHeartbeat <- true
				return
			}
		}
	}()

	select {
	case <-receivedHeartbeat:
		// Heartbeat received successfully
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for heartbeat comment")
	}
}

func TestHub_SSE_ConnectAndDisconnect(t *testing.T) {
	hub := newTestHub()
	defer hub.Close()

	srv := newTestServer(hub)
	defer srv.Close()

	if c := hub.ClientCount(); c != 0 {
		t.Fatalf("expected 0 clients at start, got %d", c)
	}

	resp := openSSE(t, srv.URL)

	time.Sleep(30 * time.Millisecond)
	if c := hub.ClientCount(); c != 1 {
		t.Fatalf("expected 1 client after connect, got %d", c)
	}

	resp.Body.Close()
	// Allow heartbeat tick (50ms) to detect connection closure and unregister
	time.Sleep(100 * time.Millisecond)

	if c := hub.ClientCount(); c != 0 {
		t.Fatalf("expected 0 clients after disconnect, got %d", c)
	}
}

func TestHub_Broadcast_DeliversCameraUpdateEvent(t *testing.T) {
	hub := newTestHub()
	defer hub.Close()

	srv := newTestServer(hub)
	defer srv.Close()

	resp := openSSE(t, srv.URL)
	defer resp.Body.Close()

	time.Sleep(30 * time.Millisecond)

	now := time.Now().Truncate(time.Second).UTC()
	verdict := hotset.Verdict{
		CameraID:   "cam-sse-42",
		Severity:   hotset.SeverityCongested,
		Confidence: 0.95,
		Timestamp:  now,
	}
	hub.Broadcast(verdict)

	ev, data, err := readEvent(resp, 2*time.Second)
	if err != nil {
		t.Fatalf("read SSE event: %v", err)
	}
	if ev != "camera_update" {
		t.Errorf("expected event name 'camera_update', got %q", ev)
	}
	if !strings.Contains(data, "cam-sse-42") {
		t.Errorf("expected camera_id in data, got %q", data)
	}
	if !strings.Contains(data, hotset.SeverityCongested) {
		t.Errorf("expected severity in data, got %q", data)
	}
}

func TestHub_Broadcast_MultipleClients(t *testing.T) {
	hub := newTestHub()
	defer hub.Close()

	srv := newTestServer(hub)
	defer srv.Close()

	resp1 := openSSE(t, srv.URL)
	defer resp1.Body.Close()
	resp2 := openSSE(t, srv.URL)
	defer resp2.Body.Close()

	time.Sleep(30 * time.Millisecond)
	if c := hub.ClientCount(); c != 2 {
		t.Fatalf("expected 2 clients, got %d", c)
	}

	verdict := hotset.Verdict{
		CameraID:   "cam-multi",
		Severity:   hotset.SeverityModerate,
		Confidence: 0.7,
		Timestamp:  time.Now(),
	}
	hub.Broadcast(verdict)

	for i, resp := range []*http.Response{resp1, resp2} {
		_, data, err := readEvent(resp, 2*time.Second)
		if err != nil {
			t.Fatalf("client %d read SSE event: %v", i+1, err)
		}
		if !strings.Contains(data, "cam-multi") {
			t.Errorf("client %d: expected camera_id in data, got %q", i+1, data)
		}
	}
}

func TestHub_Broadcast_SlowClientDrop(t *testing.T) {
	// Buffer of 1: overflow must not block the Broadcast caller.
	hub := live.NewHubWithConfig(1, 50*time.Millisecond, nil)
	defer hub.Close()

	srv := newTestServer(hub)
	defer srv.Close()

	resp := openSSE(t, srv.URL)
	defer resp.Body.Close()

	time.Sleep(30 * time.Millisecond)

	// Send more messages than the buffer without reading — must complete fast.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 20; i++ {
			hub.Broadcast(hotset.Verdict{
				CameraID:   "cam-slow",
				Severity:   hotset.SeverityHeavy,
				Confidence: 0.9,
				Timestamp:  time.Now(),
			})
		}
	}()

	select {
	case <-done:
		// Good — Broadcast returned without blocking.
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Broadcast blocked on slow client — should have dropped frames")
	}
}

func TestHub_NoLeak_AfterNConnectDisconnectCycles(t *testing.T) {
	const cycles = 10
	hub := newTestHub()
	defer hub.Close()

	srv := newTestServer(hub)
	defer srv.Close()

	for i := 0; i < cycles; i++ {
		resp := openSSE(t, srv.URL)
		time.Sleep(20 * time.Millisecond)
		resp.Body.Close()
		time.Sleep(100 * time.Millisecond)
	}

	if c := hub.ClientCount(); c != 0 {
		t.Fatalf("expected 0 clients after %d cycles, got %d (goroutine/client leak)", cycles, c)
	}
}

func TestHub_Close_RejectsNewConnections(t *testing.T) {
	hub := newTestHub()

	srv := newTestServer(hub)
	defer srv.Close()

	// Connect one client before close.
	resp1 := openSSE(t, srv.URL)
	defer resp1.Body.Close()
	time.Sleep(30 * time.Millisecond)

	if err := hub.Close(); err != nil {
		t.Fatalf("hub.Close(): %v", err)
	}
	if c := hub.ClientCount(); c != 0 {
		t.Fatalf("expected 0 clients after Close, got %d", c)
	}
}

func TestHub_ConcurrentBroadcast(t *testing.T) {
	hub := newTestHub()
	defer hub.Close()

	srv := newTestServer(hub)
	defer srv.Close()

	const numClients = 5
	resps := make([]*http.Response, numClients)
	for i := range resps {
		resps[i] = openSSE(t, srv.URL)
		defer resps[i].Body.Close()
	}

	time.Sleep(40 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hub.Broadcast(hotset.Verdict{
				CameraID:   fmt.Sprintf("cam-%d", idx),
				Severity:   hotset.SeverityModerate,
				Confidence: 0.5,
				Timestamp:  time.Now(),
			})
		}(i)
	}
	wg.Wait()
	// No deadlock, no panic = pass.
}
