package ws

import "time"

// Route constants for WebSocket endpoints.
const (
	RouteLive = "/ws/live"
)

// Operational buffer and timeout constants.
const (
	DefaultBufferSize   = 32
	DefaultWriteTimeout = 5 * time.Second
)
