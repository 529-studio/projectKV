# L420 — SSE Stream Buffering Deadlocks, Secret Sanitization Bypasses, and Explicit Flusher Delegation

**Issue**: [#420](https://github.com/529-studio/projectKV/issues/420) · **Sprint**: 12 · **Date**: 2026-11-12

## 1. Response-buffering middleware deadlocks long-lived streaming responses

In HTTP frameworks with middleware wrapping `ResponseWriter` (like Gin), middleware that intercepts response bodies for inspection or redaction (`SecretSanitizer`) must know whether a handler writes a discrete response (JSON, HTML) or a persistent stream (`text/event-stream`, `image/*`).

When `sanitizingResponseWriter` intercepted all non-image responses into a memory buffer (`w.buf.Write(b)`), Server-Sent Events handlers entering an event loop (`HandleSSE`) never completed `c.Next()`. Because the underlying wire writer received 0 bytes, `http.Flusher.Flush()` calls flushed an empty buffer while megabytes of unread frames accumulated indefinitely inside server memory.

**Guardrail**:
- Check response `Content-Type` before writing to memory buffers: any streaming content types (`text/event-stream`, `image/`, `application/octet-stream`) MUST bypass internal buffering and write directly to the underlying `ResponseWriter`.
- Ensure streaming handlers set `Content-Type` headers before the first write.

## 2. Wrapped response writers must explicitly delegate Flusher and StringWriter

When wrapping `gin.ResponseWriter` or standard `http.ResponseWriter`, relying solely on struct embedding for interface promotion can obscure interface assertions or introduce subtle delegation bugs if methods like `WriteString` or `Flush` are partially overridden or wrapped.

**Guardrail**:
- Explicitly declare compile-time interface assertions:
  ```go
  var (
      _ gin.ResponseWriter = (*sanitizingResponseWriter)(nil)
      _ http.Flusher       = (*sanitizingResponseWriter)(nil)
  )
  ```
- Implement `Flush()` explicitly delegating to the underlying writer:
  ```go
  func (w *sanitizingResponseWriter) Flush() {
      w.ResponseWriter.Flush()
  }
  ```
- Implement `WriteString(s string)` with the same content-type bypass logic to prevent avoidable string-to-byte allocations (`[]byte(s)`).

## 3. Passing unit tests without full middleware chains mask streaming bugs

Unit tests written for isolated handler packages (e.g. `internal/live/hub_test.go`) often test the handler in isolation with a minimal test router that omits global production middleware like `SecretSanitizer()`. As a consequence, all tests in `hub_test.go` passed even though production SSE streams were completely deadlocked.

**Guardrail**:
- Test middleware with real streaming handlers under concurrent conditions: verify that `httptest.ResponseRecorder` or a real `httptest.Server` client receives bytes and flushes immediately while the handler is still active (`w.Body.Len() > 0` before handler return).
- Test streaming with race detection (`go test -race`).

## Verification

- `go test -v -race ./internal/middleware`:
  - `TestSecretSanitizer_BypassesSSEStream`: asserts immediate frame receipt and flush on active handler.
  - `TestSecretSanitizer_SSEStreamingEndToEnd`: end-to-end real HTTP server test asserting real-time SSE event and heartbeat reception.
  - `TestSecretSanitizer_SSEZeroMemoryBuildup`: verifies zero memory accumulation across 500 stream frames.
- Live verification with running Go server: curl stream client connected to `http://localhost:8089/api/live/stream` and received live chunked SSE frames and `: heartbeat` telemetry in real time.
