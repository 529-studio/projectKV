package ws_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/529-studio/projectKV/services/go-server/internal/ws"
	"github.com/gin-gonic/gin"
	"nhooyr.io/websocket"
)

func setupTestServer(hub *ws.Hub) *httptest.Server {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	hub.RegisterRoutes(router)
	return httptest.NewServer(router)
}

func toWSURL(httpURL string, path string) string {
	wsURL := strings.Replace(httpURL, "http://", "ws://", 1)
	return wsURL + path
}

func TestHub_RegisterAndUnregister(t *testing.T) {
	hub := ws.NewHub()
	defer hub.Close()

	server := setupTestServer(hub)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if count := hub.ClientCount(); count != 0 {
		t.Fatalf("expected 0 clients, got %d", count)
	}

	conn, _, err := websocket.Dial(ctx, toWSURL(server.URL, ws.RouteLive), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}

	// Give a moment for registration to complete
	time.Sleep(30 * time.Millisecond)
	if count := hub.ClientCount(); count != 1 {
		t.Fatalf("expected 1 client after connect, got %d", count)
	}

	// Close client connection and verify unregistration
	_ = conn.Close(websocket.StatusNormalClosure, "done")
	time.Sleep(50 * time.Millisecond)

	if count := hub.ClientCount(); count != 0 {
		t.Fatalf("expected 0 clients after disconnect, got %d", count)
	}
}

func TestHub_Broadcast(t *testing.T) {
	hub := ws.NewHub()
	defer hub.Close()

	server := setupTestServer(hub)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn1, _, err := websocket.Dial(ctx, toWSURL(server.URL, ws.RouteLive), nil)
	if err != nil {
		t.Fatalf("dial client 1: %v", err)
	}
	defer conn1.Close(websocket.StatusNormalClosure, "")

	conn2, _, err := websocket.Dial(ctx, toWSURL(server.URL, ws.RouteLive), nil)
	if err != nil {
		t.Fatalf("dial client 2: %v", err)
	}
	defer conn2.Close(websocket.StatusNormalClosure, "")

	time.Sleep(30 * time.Millisecond)
	if count := hub.ClientCount(); count != 2 {
		t.Fatalf("expected 2 clients, got %d", count)
	}

	now := time.Now().Truncate(time.Second).UTC()
	expected := hotset.Verdict{
		CameraID:   "cam-42",
		Severity:   hotset.SeverityCongested,
		Confidence: 0.98,
		Timestamp:  now,
	}

	hub.Broadcast(expected)

	// Verify client 1 receives message
	_, data1, err := conn1.Read(ctx)
	if err != nil {
		t.Fatalf("client 1 read: %v", err)
	}
	var got1 hotset.Verdict
	if err := json.Unmarshal(data1, &got1); err != nil {
		t.Fatalf("client 1 unmarshal: %v", err)
	}
	if got1.CameraID != expected.CameraID || got1.Severity != expected.Severity || got1.Confidence != expected.Confidence {
		t.Errorf("client 1 verdict mismatch: got %+v, want %+v", got1, expected)
	}

	// Verify client 2 receives message
	_, data2, err := conn2.Read(ctx)
	if err != nil {
		t.Fatalf("client 2 read: %v", err)
	}
	var got2 hotset.Verdict
	if err := json.Unmarshal(data2, &got2); err != nil {
		t.Fatalf("client 2 unmarshal: %v", err)
	}
	if got2.CameraID != expected.CameraID || got2.Severity != expected.Severity || got2.Confidence != expected.Confidence {
		t.Errorf("client 2 verdict mismatch: got %+v, want %+v", got2, expected)
	}
}

func TestHub_ConcurrentBroadcast(t *testing.T) {
	hub := ws.NewHub()
	defer hub.Close()

	server := setupTestServer(hub)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const numClients = 5
	conns := make([]*websocket.Conn, numClients)
	for i := 0; i < numClients; i++ {
		conn, _, err := websocket.Dial(ctx, toWSURL(server.URL, ws.RouteLive), nil)
		if err != nil {
			t.Fatalf("dial client %d: %v", i, err)
		}
		conns[i] = conn
		defer conn.Close(websocket.StatusNormalClosure, "")
	}

	time.Sleep(30 * time.Millisecond)

	var wg sync.WaitGroup
	const numBroadcasts = 20

	for i := 0; i < numBroadcasts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			hub.Broadcast(hotset.Verdict{
				CameraID:   "cam-concurrent",
				Severity:   hotset.SeverityModerate,
				Confidence: 0.75,
				Timestamp:  time.Now(),
			})
		}(i)
	}

	wg.Wait()
}

func TestHub_SlowClientDrop(t *testing.T) {
	// Small buffer of 1 message
	hub := ws.NewHubWithConfig(1, 100*time.Millisecond)
	defer hub.Close()

	server := setupTestServer(hub)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, toWSURL(server.URL, ws.RouteLive), nil)
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	time.Sleep(30 * time.Millisecond)

	// Broadcast more messages than buffer capacity without reading on client
	for i := 0; i < 10; i++ {
		hub.Broadcast(hotset.Verdict{
			CameraID:   "cam-slow",
			Severity:   hotset.SeverityHeavy,
			Confidence: 0.9,
			Timestamp:  time.Now(),
		})
	}

	// The broadcast should complete immediately without blocking
}

func TestHub_Close(t *testing.T) {
	hub := ws.NewHub()
	server := setupTestServer(hub)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, toWSURL(server.URL, ws.RouteLive), nil)
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	time.Sleep(30 * time.Millisecond)
	if count := hub.ClientCount(); count != 1 {
		t.Fatalf("expected 1 client, got %d", count)
	}

	if err := hub.Close(); err != nil {
		t.Fatalf("hub close error: %v", err)
	}

	if count := hub.ClientCount(); count != 0 {
		t.Fatalf("expected 0 clients after hub close, got %d", count)
	}

	// New connection attempt should be rejected
	newConn, _, err := websocket.Dial(ctx, toWSURL(server.URL, ws.RouteLive), nil)
	if err == nil {
		defer newConn.Close(websocket.StatusNormalClosure, "")
		time.Sleep(30 * time.Millisecond)
		// Should not be registered in closed hub
		if hub.ClientCount() != 0 {
			t.Errorf("closed hub should not accept new clients")
		}
	}
}
