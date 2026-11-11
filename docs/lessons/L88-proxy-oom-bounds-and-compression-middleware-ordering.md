# Lesson 88: Proxy OOM Bounds, Middleware Ordering Invariants, and Catalog Caching

## Navigation Aid & Quick Reference
- **Domain**: Go backend HTTP infrastructure, streaming proxy safety, gzip middleware pipeline ordering, HTTP cache headers.
- **Relevant Issues**: Issue #424 (Sprint 12), ADR-002, Lesson L33.
- **Key Files**:
  - `services/go-server/internal/proxycache/handler.go` (`MaxSnapshotBytes = 5*1024*1024`, `io.LimitReader`, `url.QueryEscape`)
  - `services/go-server/internal/camera/handler.go` (`CatalogCacheControl`, strong SHA-256 `ETag`, `If-None-Match` 304 handling)
  - `services/go-server/cmd/server/main.go` (outer `gin-contrib/gzip` middleware ordering, exclusions for `/api/snapshot`, `/api/live/stream`, `/ws`)
  - `services/go-server/cmd/server/server_test.go` (`TestStartup_CatalogGzipCompressionAndCachingHeaders`)
- **Core Invariants**:
  - **Bounded Upstream Reads**: Proxy endpoints reading upstream HTTP responses must strictly bound memory allocation via `io.LimitReader(resp.Body, maxBytes)` to eliminate OOM vectors from unexpected upstream streams or payloads.
  - **Query Escaping on Upstream URLs**: Route parameters forwarded to upstream query strings must be escaped with `url.QueryEscape(id)`.
  - **Compression Middleware Ordering**: `gzip.Gzip` middleware must be mounted **outside** response buffering and content-sanitizing middlewares (`SecretSanitizer`). Inner sanitizers operate on plaintext; outer compression wraps clean output into valid gzip frames.
  - **Streaming & Binary Exclusions**: Streaming protocols (SSE `/api/live/stream`, WebSocket `/ws`) and pre-compressed media (`/api/snapshot`) must be excluded from gzip middleware to prevent buffer stalls and CPU waste.

---

## 1. Context & Problem Statement

Production audits of the Go backend snapshot proxy and catalog endpoints identified three critical operational vulnerabilities:
1. **Unbounded Upstream Reads in Snapshot Proxy**: `services/go-server/internal/proxycache/handler.go` read upstream camera responses using `io.ReadAll(resp.Body)` without an upper bound. A malicious or malfunctioning upstream server returning an infinite stream or oversized payload could exhaust host RAM, triggering an Out-Of-Memory (OOM) kernel kill.
2. **Unescaped Upstream Query Parameters**: Camera IDs containing special characters (`+`, `&`, `?`, spaces) were appended directly to upstream portal URLs (`fmt.Sprintf("%s?id=%s&t=%d", ...)`), causing query parameter injection or corrupted upstream requests.
3. **Bandwidth Inefficiency on Static Catalogs**: The 837-camera enriched catalog (`cameras_enriched.json`, 548 KB raw JSON) was served uncompressed and without HTTP cache headers on `/api/cameras` and `/api/zones`. This created excessive wire transfer overhead on mobile 4G/5G connections and forced clients to re-download the complete catalog on every reload.

---

## 2. Core Architectural Decisions

### A. Strict 5 MB Read Bounds with `io.LimitReader`
In `proxycache/handler.go`, upstream reads are bounded by constant `MaxSnapshotBytes = 5 * 1024 * 1024`:
```go
body, err := io.ReadAll(io.LimitReader(resp.Body, MaxSnapshotBytes))
```
If the upstream responds with a payload exceeding 5 MB, `io.LimitReader` guarantees that at most 5 MB is buffered into server memory, preserving system stability under upstream failure or adversarial payload injection. Additionally, camera IDs are safely encoded via `url.QueryEscape(id)`:
```go
url := fmt.Sprintf("%s?id=%s&t=%d", h.upstreamURL, url.QueryEscape(id), time.Now().UnixMilli())
```

### B. Middleware Pipeline Ordering Invariant (Gzip vs SecretSanitizer)
When mounting `github.com/gin-contrib/gzip`, middleware ordering is critical:
```go
router.Use(middleware.CORSMiddleware())
router.Use(gzip.Gzip(gzip.DefaultCompression,
    gzip.WithExcludedPaths([]string{"/api/snapshot", "/api/cameras/snapshot", "/api/live/stream", "/ws"}),
))
router.Use(middleware.SecretSanitizer())
router.Use(middleware.RateLimitMiddleware(cfg.RateLimitRPS, cfg.RateLimitBurst))
```

If `SecretSanitizer` is placed outer of `gzip.Gzip`:
- `SecretSanitizer` buffers the response body to perform regex credential scrubbing on JSON/text.
- It receives raw binary gzip chunks, attempts regex replacements on compressed bytes (corrupting binary frames), and suppresses the underlying writer size (`size = -1`).
- The inner gzip writer evaluates `c.Writer.Size() < 0`, assumes an empty response, calls `gz.Reset(io.Discard)`, and drops the gzip trailer, causing `unexpected EOF` on client decompression.

Placing `gzip.Gzip` as the **outer** wrapper guarantees:
1. Handler writes JSON to `SecretSanitizer`.
2. `SecretSanitizer` inspects and redacts uncompressed plaintext JSON strings.
3. Sanitized plaintext is flushed to `gzipWriter`, which compresses the clean stream and appends the standard 8-byte CRC32/ISIZE trailer upon completion.

### C. Strong ETag and Stale-While-Revalidate Caching
In `camera/handler.go`, `/api/cameras` and `/api/zones` return:
- `Cache-Control: public, max-age=300, stale-while-revalidate=600`
- Strong SHA-256 `ETag: "<hex-hash>"`
- Conditional request evaluation: if `If-None-Match` matches the entity tag (including weak prefix `W/`), the server terminates immediately with `304 Not Modified` and zero body bytes.

---

## 3. Empirical Verification & Results

End-to-end integration tests (`server_test.go:TestStartup_CatalogGzipCompressionAndCachingHeaders`) and live process telemetry verify:
- **Wire Size Reduction**: `/api/cameras` raw JSON is 548,012 bytes (535.2 KB). Gzip compression reduces wire transfer to 79,983 bytes (78.1 KB), an **85.4% bandwidth reduction**.
- **Conditional 304 Performance**: Clients sending `If-None-Match: <etag>` receive an instantaneous `304 Not Modified` response with 0 body bytes.
- **Excluded Paths**: Streaming endpoints (`/api/live/stream`) and binary snapshots (`/api/snapshot/:id`) bypass gzip, preventing buffer latency.
- **Race Condition Safety**: `go test ./... -race` passed cleanly across all 23 backend packages.
