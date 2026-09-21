package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/529-studio/projectKV/services/go-server/internal/hotset"
	"github.com/gin-gonic/gin"
	"nhooyr.io/websocket"
)

// client represents a single active WebSocket subscriber.
type client struct {
	conn *websocket.Conn
	send chan []byte
}

// Hub manages active WebSocket client connections and broadcasts
// real-time traffic analysis verdicts to all connected clients.
type Hub struct {
	mu           sync.RWMutex
	clients      map[*client]struct{}
	bufferSize   int
	writeTimeout time.Duration
	closed       bool
}

// NewHub constructs a Hub with default buffer and timeout parameters.
func NewHub() *Hub {
	return NewHubWithConfig(DefaultBufferSize, DefaultWriteTimeout)
}

// NewHubWithConfig constructs a Hub with custom operational settings.
func NewHubWithConfig(bufferSize int, writeTimeout time.Duration) *Hub {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	if writeTimeout <= 0 {
		writeTimeout = DefaultWriteTimeout
	}
	return &Hub{
		clients:      make(map[*client]struct{}),
		bufferSize:   bufferSize,
		writeTimeout: writeTimeout,
	}
}

// RegisterRoutes registers the WebSocket upgrade route on the given Gin router.
func (h *Hub) RegisterRoutes(r gin.IRouter) {
	r.GET(RouteLive, h.HandleWebSocket)
}

// HandleWebSocket handles incoming HTTP requests and upgrades them to WebSocket.
func (h *Hub) HandleWebSocket(c *gin.Context) {
	conn, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}

	cl := &client{
		conn: conn,
		send: make(chan []byte, h.bufferSize),
	}

	if !h.register(cl) {
		_ = conn.Close(websocket.StatusGoingAway, "server is shutting down")
		return
	}
	defer h.unregister(cl)

	ctx := c.Request.Context()

	// Goroutine to drain messages from send channel and write to WebSocket
	go cl.writePump(ctx, h.writeTimeout)

	// Block in readPump until client disconnects or an error occurs
	cl.readPump(ctx)
}

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
		close(cl.send)
	}
}

// Broadcast serializes a traffic verdict to JSON and pushes it to all connected clients.
// Delivery is non-blocking: if a client's send buffer is full, the message is dropped
// for that client to avoid stalling the broadcast loop for other clients.
func (h *Hub) Broadcast(v hotset.Verdict) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for cl := range h.clients {
		select {
		case cl.send <- data:
		default:
			// Buffer full: drop frame for slow client without blocking
		}
	}
}

// ClientCount returns the number of currently connected WebSocket clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Close gracefully closes all active client connections and shuts down the hub.
func (h *Hub) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true

	for cl := range h.clients {
		delete(h.clients, cl)
		close(cl.send)
		_ = cl.conn.Close(websocket.StatusNormalClosure, "server shutting down")
	}
	return nil
}

func (cl *client) writePump(ctx context.Context, timeout time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-cl.send:
			if !ok {
				return
			}
			writeCtx, cancel := context.WithTimeout(ctx, timeout)
			err := cl.conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func (cl *client) readPump(ctx context.Context) {
	defer func() {
		_ = cl.conn.Close(websocket.StatusNormalClosure, "")
	}()

	for {
		_, _, err := cl.conn.Read(ctx)
		if err != nil {
			return
		}
	}
}
