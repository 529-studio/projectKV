# S18 Handoff & Sprint 19 Horizon

**Document**: `docs/HANDOFF-s18.md`  
**Date**: 2026-11-24  
**Repository**: `529-studio/projectKV`  
**Charter**: Transition ledger for a fresh conversation: branch state, merged deliverables, open work, settled decisions, pitfalls, and the next sprint's tasks.

---

## 1. Branch & Commit Status

- **Active branch**: `dev`
- **Head commit**: `9e0150a` (`chore(catalog): automated daily camera synchronization and dead feed health culling (#547)`)
- **Worktree**: clean
- **Merged in this sprint**:
  - PR #524 (`feat/523-turn-by-turn-navigation`)
  - PR #534 (`fix/healthz-liveness-readiness-split`)
  - PR #536 (`fix/cv-engine-cuda-concurrent-run-serialization`)
  - PR #537 (`fix/staging-deploy-serialization`)
  - PR #538 (`feat/525-web-push-notifications`)
  - PR #539 (`chore/camera-catalog-resync-20261123-44`)
  - PR #540 (`fix/warded-backup-vacuum-into`)
  - PR #541 (`chore/camera-catalog-resync-20261124-45`)
  - PR #542 (`feat/526-corridor-flood-trends`)
  - PR #543 (`feat/527-crowd-report-verification`)
  - PR #544 (`feat/528-int8-quantization`)
  - PR #545 (`feat/529-prometheus-metrics-endpoint`)
  - PR #546 (`feat/530-cors-and-trusted-proxies`)
  - PR #547 (`feat/531-dead-camera-culling`)

---

## 2. Merged Deliverables

| PR | Task | Subsystem | Delivered capability |
| :- | :--- | :-------- | :------------------- |
| #524 | S18-T1 | `apps/web/src/components` | Turn-by-turn navigation hazard intersection alerts along corridor polylines with vehicle clearance adaptation |
| #534 | Hotfix | `cmd/server/routes.go` | Split `/healthz` (liveness) from `/readyz` (readiness) to prevent container restart cascade during peripheral mesh outages |
| #536 | Hotfix | `cv-engine/` | Serialized ONNX Runtime CUDA session initialization and concurrent run calls with process mutex |
| #537 | Hotfix | `.github/workflows` | Serialized staging CD pipeline deployments to eliminate concurrent Portainer stack race conditions |
| #538 | S18-T2 | `internal/push` | Pure Go stdlib RFC 8291/8292 VAPID Web Push subscription manager, service worker notifications, SSRF allowlist, and dead endpoint pruning |
| #540 | Hotfix | `internal/db` | SQLite snapshot backup using `VACUUM INTO` for zero-lock hot database convergence |
| #542 | S18-T3 | `internal/corridor` | Multi-hour historical flood depth trends & astronomical tidal harmonics correlation modal (`CorridorFloodTrendsModal.tsx`) |
| #543 | S18-T4 | `internal/feedback` | Spatial crowd-sourced hazard report clustering (300m) and multi-sensor Bayesian confidence corroboration against camera feeds |
| #544 | S18-T5 | `cv-engine/` | INT8 post-training static quantization (PTQ) on ONNX models reducing artifact footprint 4x (42MB to 10.6MB) and accelerating inference 2.1x |
| #545 | S18-T6 | `internal/metrics` | Prometheus / OpenMetrics `/metrics` endpoint with leaf collector architecture and `METRICS_ALLOWED_CIDR` protection |
| #546 | S18-T7 | `internal/middleware` | Strict production CORS origin gating (`CORS_ALLOWED_ORIGINS` non-wildcard, non-loopback) and Gin trusted proxy CIDR configuration (SEC-04, SEC-05) |
| #547 | S18-T8 | `tools/eval` | Automated daily camera feed health audit tool (`cull_dead_cameras.py`) with arterial corridor coverage floor protection (>=50%) |

---

## 3. Open Issues & Backlog

- **All Sprint 18 issues closed**: 0 open issues remain in `529-studio/projectKV`.
- Backlog items for Sprint 19 will be opened based on the horizon roadmap in Section 6.

---

## 4. Settled Architectural Decisions

- **Pure Go Web Push**: `crypto/ecdsa` and `crypto/aes` in standard library form the permanent Web Push cryptographic core. Zero external CGO or third-party wrappers (`internal/push`).
- **INT8 Edge Standard**: INT8 quantized ONNX weights serve as standard edge runtime models. FP32 models remain restricted to training and ground truth ablation (`cv-engine/quantize_ptq.py`).
- **Liveness vs Readiness Probe Separation**: Kubernetes/Docker probes must target `/healthz` for process liveness and `/readyz` for traffic routing (`cmd/server/routes.go`).
- **Production CORS & Proxy Security Gates**: Wildcard `*` origins and loopback hosts in production trigger immediate fail-fast termination (`internal/middleware/cors.go`). Gin trusted proxies must be explicitly bound (`cmd/server/routes.go`).
- **Corridor Coverage Floor**: Camera culling algorithms must enforce a minimum 50% active camera ratio on all arterial corridors to avoid blinding route navigation (`tools/eval/cull_dead_cameras.py`).

---

## 5. Pitfalls Encountered

- **CodeQL SSRF Taint Tracking on Push Gateways**: Web Push delivery requires sending payloads to user-provided push endpoints. CodeQL flags these as potential SSRF unless gateway domains are strictly matched against a constant allowlist prefix (`https://fcm.googleapis.com`, `https://*.push.apple.com`, `https://*.notify.windows.com`).
- **CodeQL Integer Overflow on Historical Bounds (CWE-190)**: Query parameters controlling historical window ranges must be explicitly clamped using `min/max` guards before slice allocations.
- **Gin Default Untrusted Proxy Warning**: Failing to invoke `router.SetTrustedProxies` causes Gin to trust all headers (`0.0.0.0/0`), allowing attackers to spoof client IPs and bypass rate limits.
- **Production CORS Validation Ordering**: When validating production CORS origins on server boot, ensure required baseline configs (`SNAPSHOT_UPSTREAM_BASE_URL`) are populated first to prevent early test aborts.
- **Corridor Blinding from Unconstrained Culling**: Culling cameras based purely on consecutive offline days risks wiping out entire arterial stretches. Always protect multi-camera corridors with a minimum coverage ratio.

---

## 6. Sprint 19 Horizon: next tasks

1. Trigger Web Push localized flood alerts on camera hazard state transitions (`services/go-server/internal/notify/`)
2. Deploy INT8 quantized model artifacts to secondary edge inference nodes (`cv-engine/`)
3. Provision Grafana dashboard templates consuming Prometheus telemetry from `/metrics` (`docs/ops/grafana/`)
4. Automate daily camera catalog synchronization via scheduled GitHub Actions workflow (`.github/workflows/`)
5. Implement localized tidal gate status indicators in corridor route cards (`apps/web/src/components/`)
6. Add client-side Web Push notification subscription prompt and preference drawer (`apps/web/src/components/`)
7. Benchmarking and latency stress testing of INT8 CV engine under 60-camera concurrency (`tools/eval/`)
8. Harden Redis / SQLite replication failover drill automation (`tools/dr/`)

---

## 7. Open Questions For The Human

- For Sprint 19 push notifications: Should localized alerts be gated strictly by user-saved commute routes (`My-Route`), or also support radius-based subscriptions around current device geolocation?
- Should the daily camera health audit tool run as an in-cluster cronjob inside `warded-backend`, or remain an external scheduled GitHub Action?
