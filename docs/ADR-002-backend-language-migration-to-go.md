# ADR-002: Backend Language Migration to Go

**Status**: Accepted
**Date**: 2026-09-20
**Context**: Decision to build the backend in Go going forward, superseding the "defer Go until traffic justifies it" stance in the original `SYSTEM_DESIGN.md`.

## Decision

Adopt Go for all **new** backend components (Hot Set Scheduler, State Store, Notification Service, WebSocket fan-out). Migrate the existing `node-server` proxy/cache to Go **after** the new components are stable, not as a big-bang rewrite. Keep the CV model-inference layer in Python.

## Rationale

This is not a traffic-driven migration (the original trigger — thousands of cameras / tens of thousands of concurrent connections — has not been reached). The justification is different: the flood-alert extension requires building several components that do not exist yet (scheduler, state store, notification fan-out). Writing new code in Go carries far less risk than rewriting an already-working `server.js`, and these components — bounded worker pools, ticker-based scheduling, concurrent fan-out to subscribers — are a strong idiomatic fit for goroutines and channels.

The existing `node-server` proxy/cache is low-risk to port later: it is a small, well-isolated reverse-proxy-with-TTL-cache, not deeply coupled to anything else.

## What stays in Python

Model inference (self-hosted YOLO/ONNX, OpenCV-based video/image processing) remains a separate Python service (`cv-engine`). Go's CV/ML ecosystem (`gocv` via cgo, limited ONNX tooling) is significantly less mature than Python's, and `gocv` requires a native OpenCV system dependency that conflicts with the project's "clone and run, zero config" static-binary philosophy. Cheap pre-filtering (pixel/histogram baseline diff) and Cloud Vision API calls, however, are simple enough to implement in pure Go (`image` stdlib, plain HTTP) and stay in the Go control plane to avoid an extra network hop per camera per cycle.

## Consequences

- Two runtimes in the stack (Go control plane + Python ML plane) — accepted as a deliberate polyglot split, not incidental sprawl.
- Deployment becomes two binaries/processes: a single static Go binary + a Python service, communicating over internal HTTP.
- `node-server` remains in place until the Go proxy/cache is verified equivalent, then is retired.
- Library choices for the Go side favor the stdlib and minimal dependencies (`net/http`, `modernc.org/sqlite` — pure Go, no cgo, `nhooyr.io/websocket` or `gorilla/websocket`) to preserve the "zero external dependency, single static binary" deployment story on low-cost VPS.

---

*Extracted verbatim from [`docs/ADR-001-flood-alert-architecture.md`](ADR-001-flood-alert-architecture.md),
where this decision had been appended below ADR-001 as a second `# ADR-` heading. The body above
is byte-identical to that slice; only this note is new.*
