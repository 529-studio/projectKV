// Package live implements a Server-Sent Events (SSE) hub that broadcasts
// Hot Set verdict updates to all connected map clients in real time.
//
// This package supersedes internal/ws (WebSocket) per ADR-003 Decision 1.
// The traffic is one-directional (server → client); SSE runs over plain HTTP,
// the browser's native EventSource handles reconnect automatically, and this
// removes the nhooyr.io/websocket third-party dependency from the live-push path.
//
// Allowed imports (§2 import graph, updated by ADR-003):
//
//	live → hotset  (Verdict type only)
//	live → stdlib + gin only
package live

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/gin-gonic/gin"
)

// client represents a single active SSE subscriber.
// The send channel holds pre-serialised SSE frames ready to flush.
type client struct {
	send chan []byte
}

// Hub manages active SSE connections and broadcasts hotset.Verdict updates
// to all connected clients. Safe for concurrent use.
type Hub struct {
	mu        sync.RWMutex
	clients   map[*client]struct{}
	closed    bool
	buf       int // per-client channel capacity
	heartbeat time.Duration
	log       *slog.Logger
}

// NewHub constructs a Hub with default settings.
func NewHub() *Hub {
	return NewHubWithConfig(DefaultClientBuffer, HeartbeatInterval, slog.Default())
}

// NewHubWithConfig constructs a Hub with explicit per-client buffer size, heartbeat interval, and logger.
// bufSize must be > 0; values ≤ 0 are silently clamped to DefaultClientBuffer.
// heartbeat must be > 0; values ≤ 0 are silently clamped to HeartbeatInterval.
func NewHubWithConfig(bufSize int, heartbeat time.Duration, log *slog.Logger) *Hub {
	if bufSize <= 0 {
		bufSize = DefaultClientBuffer
	}
	if heartbeat <= 0 {
		heartbeat = HeartbeatInterval
	}
	if log == nil {
		log = slog.Default()
	}
	return &Hub{
		clients:   make(map[*client]struct{}),
		buf:       bufSize,
		heartbeat: heartbeat,
		log:       log,
	}
}

// RegisterRoutes mounts the SSE stream endpoint onto the given Gin router.
// Expects rg to already be scoped to the API prefix; registers GET /api/live/stream.
func (h *Hub) RegisterRoutes(r gin.IRouter) {
	r.GET(RouteStream, h.HandleSSE)
}

// HandleSSE is the Gin handler for GET /api/live/stream.
// It upgrades the HTTP connection to an SSE stream and blocks until the client
// disconnects (r.Context().Done()) or the hub is closed.
func (h *Hub) HandleSSE(c *gin.Context) {
	// SSE requires these three headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	// Disable buffering at nginx / proxy layer if present.
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.String(http.StatusInternalServerError, "streaming unsupported")
		return
	}

	cl := &client{send: make(chan []byte, h.buf)}

	if !h.register(cl) {
		// Hub is shutting down; reject gracefully.
		c.String(http.StatusServiceUnavailable, "server shutting down")
		return
	}
	defer h.unregister(cl)

	ctx := c.Request.Context()
	ticker := time.NewTicker(h.heartbeat)
	defer ticker.Stop()

	h.log.Info("live: SSE client connected", "remote", c.Request.RemoteAddr)

	for {
		select {
		case <-ctx.Done():
			// Client disconnected.
			h.log.Info("live: SSE client disconnected", "remote", c.Request.RemoteAddr)
			return

		case frame, ok := <-cl.send:
			if !ok {
				// Channel closed by hub.Close() — send a final comment and exit.
				fmt.Fprintf(c.Writer, ": server closing\n\n")
				flusher.Flush()
				return
			}
			// Write the full SSE frame (already formatted by Broadcast).
			_, _ = c.Writer.Write(frame)
			flusher.Flush()

		case <-ticker.C:
			// SSE comment lines (": …") keep the connection alive through
			// proxies without being interpreted as events by EventSource.
			fmt.Fprintf(c.Writer, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

// Broadcast serialises v as a named SSE event and delivers it to all connected
// clients. Delivery is non-blocking: if a client's send buffer is full the
// event is dropped for that client to avoid stalling the broadcast loop.
//
// SSE frame format:
//
//	event: camera_update\n
//	data: <json>\n
//	\n
func (h *Hub) Broadcast(v hotset.Verdict) {
	data, err := json.Marshal(v)
	if err != nil {
		h.log.Error("live: failed to marshal verdict", "error", err)
		return
	}

	frame := fmt.Sprintf("event: camera_update\ndata: %s\n\n", data)
	frameBytes := []byte(frame)

	h.mu.RLock()
	defer h.mu.RUnlock()

	for cl := range h.clients {
		select {
		case cl.send <- frameBytes:
		default:
			// Buffer full — drop this frame for this slow client.
			// The next verdict will arrive within the scheduler interval anyway.
		}
	}
}

// ClientCount returns the number of currently active SSE connections.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close gracefully terminates all active SSE connections and prevents new ones.
// After Close returns, ClientCount() == 0. Safe to call from any goroutine.
func (h *Hub) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closed = true
	for cl := range h.clients {
		delete(h.clients, cl)
		close(cl.send)
	}
	return nil
}

// --- internal helpers ---

func (h *Hub) register(cl *client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.clients[cl] = struct{}{}
	return true
}

func (h *Hub) unregister(cl *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[cl]; ok {
		delete(h.clients, cl)
		// Do not close cl.send here — it may still be drained in HandleSSE.
		// Closing is only done by hub.Close() for a hard shutdown.
	}
}
