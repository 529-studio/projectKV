package live

import "time"

// RouteStream is the SSE endpoint path.
// Supersedes ws.RouteLive ("/ws/live") per ADR-003 Decision 1.
const RouteStream = "/api/live/stream"

// HeartbeatInterval is how often a keep-alive comment is sent to each client
// so proxies and load-balancers do not close idle SSE connections.
const HeartbeatInterval = 15 * time.Second

// DefaultClientBuffer is the number of serialized events buffered per client
// before a slow client is considered dropped (non-blocking broadcast).
const DefaultClientBuffer = 32

// SSE event names emitted over RouteStream.
const (
	EventCameraUpdate     = "camera_update"
	EventHexUpdate        = "hex_update"
	EventFloodHexUpdate   = "flood_hex_update"
	EventUserFloodReport  = "user_flood_report"
)
