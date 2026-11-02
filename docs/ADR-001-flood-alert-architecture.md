# ADR-001: Architecture Review & Flood-Alert Extension for CAMERA VIEW

**Status**: Proposed
**Date**: 2026-09-20
**Context**: Independent architecture review of the current `hcm-traffic-cameras` monorepo, plus feasibility assessment of extending the system to real-time flood detection & alerting for HCMC motorbike riders.

---

## 1. Summary of Current Architecture (as-is)

- **Frontend**: `apps/web` — vanilla JS + Leaflet, no framework, i18n (vi/en), chained hierarchical filter (Zone → Corridor → Intersection), multi-camera pin dock.
- **Backend**: `services/node-server` — Express.js, single process, in-memory snapshot proxy cache (10s TTL per camera), no database.
- **Data**: `cameras_all.json` — static catalog of 837 cameras. Live imagery is **polled snapshot JPEGs** from `giaothong.hochiminhcity.gov.vn`, refreshed every 10s — **not** an RTSP/WebRTC live video stream.
- **Caching**: 3-tier (browser 10s → backend in-memory buffer → CDN edge 15s). Guarantees at most 1 upstream fetch per camera per 10s regardless of concurrent viewers.
- **Planned CV Engine** (`services/cv-engine`, not yet implemented): Python worker analyzing a "Hot Set" (actively viewed/pinned cameras + ~30 fixed choke points) on a 30–45s fixed schedule, decoupled from user request volume. Pluggable adapter: Mock → Cloud Vision API (Gemini/OpenAI) → Self-hosted YOLO/ONNX.
- **Go migration**: explicitly deferred until traffic/connection volume justifies it (thousands of cameras nationwide / tens of thousands of concurrent connections).

## 2. Verdict on Current Architecture

**Appropriate for current scope.** Node.js + in-memory cache + static JSON is the right level of engineering for a snapshot-based camera browser serving hundreds of concurrent users. No premature database, no premature microservices, no premature Go migration — good adherence to YAGNI.

**Key clarification required**: "Low latency" in the current design refers to **API/cache delivery speed**, not video frame latency. The data source itself is snapshot-based (10–45s freshness ceiling), which is a source limitation, not an architecture flaw. This distinction matters directly for Section 3 below.

**Strongest design decision**: The Hot Set Pool + Pluggable Adapter pattern for the CV Engine. It correctly decouples compute cost from user traffic and allows shipping a working feature (via Cloud Vision API) before investing in a self-hosted model.

**Gaps to address before adding any alerting feature**:
- No persistence layer (SQLite/Redis currently "future"). Alerting requires dedupe/history state — no longer optional once built.
- Single VPS running both proxy and CV worker — acceptable for a "nice to have" congestion viewer, not for a safety-alerting feature.
- Single upstream data source dependency (`giaothong.hochiminhcity.gov.vn`) — a single point of failure exactly when load (and need) is highest, during storms.

## 3. Feasibility: Real-Time Flood Detection & Alert for HCMC Riders

**Verdict: Feasible, and well-suited to the existing Hot Set Pool design — but it is a meaningfully larger scope than "add a flood model," not a drop-in addition.**

### 3.1 Why it fits well
Flood water rises over minutes, not milliseconds — the existing 30–45s (or even 2–5 min) Hot Set inference cycle is already fast enough. This is one case where the snapshot-based data source is *not* the bottleneck.

### 3.2 Why the CV problem is harder than vehicle density
Flood detection is a scene-classification problem (dry / wet / shallow flood / deep flood), not a standard pretrained object-detection class. Wet asphalt reflections and shadows are easy to confuse with flooding.

**Recommended phased approach (maps directly onto the existing adapter pattern):**
1. **Phase 1 (MVP)**: Cheap pixel/histogram baseline-diff pre-filter per camera → only call a multimodal Cloud Vision API (Gemini Flash / GPT-4o-mini) for a structured flood-severity verdict when anomaly exceeds threshold. Reuses the existing Cloud Vision adapter tier.
2. **Phase 2**: Once a rainy season's worth of labeled data is collected, train a lightweight CPU classifier (MobileNet/EfficientNet-lite via ONNX Runtime) to remove Cloud API dependency and cost.

### 3.3 Data coverage limitation (not fixable by architecture)
Many known flood-prone alleys/streets have no public DOT camera. This is a data-source gap, not a code problem — flag transparently to users rather than implying full-city coverage.

### 3.4 The real missing piece: push notification, not more CV
Camera browsing is a *pull* model. Flood alerting must be *push* — reaching a person already on a motorbike, who cannot open a laptop or browser tab. This requires new infrastructure not present today:
- A subscription/state store (zone-based subscriptions, minimum viable — defer live GPS route-matching to a later phase).
- Deduplication/alert-state logic (avoid repeated alerts every cycle while a flood is ongoing).
- A **Notification Adapter**, mirroring the existing CV adapter pattern:

```
interface NotificationChannel {
  send(subscriberId, zoneId, verdict) -> DeliveryResult
}
├─ TelegramChannel   [Phase 1 — fastest to build, zero cost, zero approval friction]
├─ ZaloOAChannel     [Phase 2 — highest real-world reach among everyday motorbike riders in VN; requires OA registration + ZNS template approval, start early]
├─ WebPushChannel    [Parallel, low effort — bonus for existing web map users; unreliable on iOS]
└─ SmsChannel        [Later — broadcast-only, for severe events / potential city partnership; per-message cost]
```

**Channel recommendation**: Start with Telegram to validate the full pipeline (subscribe → detect → alert) cheaply and fast. In parallel, begin the Zalo OA approval process early since it is the channel that actually reaches the target audience (everyday riders, including non-technical and older users) — Telegram skews toward a tech-savvy subset of VN users. Do not rely on a single channel for a safety-relevant feature.

### 3.5 Complementary signals worth adding
- Rainfall / tide data (public) as an early-warning signal — detects risk *before* water is visibly on the road, unlike camera CV which is inherently reactive.
- User-submitted confirm/false-alarm reporting — mitigates camera coverage gaps, improves calibration, and reduces liability exposure from false positives/negatives in a safety-facing feature.

### 3.6 Competitive/regulatory context
HCMC already has UDI Maps (sensor/report-based flood alerts) and a disaster-monitoring platform reportedly targeted for September 2026. A camera-based visual-confirmation approach is a genuine differentiator, not a duplicate — but positioning should complement rather than compete, and a partnership angle with the city is worth exploring given this overlap.

## 4. Proposed Additive Architecture

```
[Existing — unchanged]
node-server (proxy/cache) ──▶ Hot Set Scheduler (extend list with known flood hotspots)
                                        │
                                        ▼
                          cv-engine/flood_worker.py
                          ├─ baseline diff (cheap pre-filter)
                          └─ Cloud Vision API call on anomaly
                                        │
                                        ▼
[New — required]          State store (SQLite/Redis)
                          - per-camera state, dedupe, zone subscriptions
                                        │
                                        ▼
[New — required]          Notification Service
                          - TelegramChannel (Phase 1)
                          - ZaloOAChannel (Phase 2)
                          - WebPushChannel (parallel)
                          - SmsChannel (later)
```

**No rewrite of the existing Node.js backend is required.** The above are additive services. Go migration criteria remain unchanged from the original design — revisit only if WebSocket/notification fan-out reaches tens of thousands of concurrent connections.

## 5. Recommended Phasing

1. Flood MVP: baseline-diff + Cloud Vision API on ~30–50 known hotspot cameras, Telegram bot alerting, zone-based subscription, manual confirm/false-alarm feedback button.
2. Begin Zalo OA registration in parallel (approval lead time is outside your control — start early).
3. Add rainfall/tide data as a leading indicator alongside camera CV.
4. Migrate to a self-trained lightweight classifier once labeled data from a rainy season is available.
5. Consider live GPS route-intersection alerting only after the zone-based model is validated and adopted.

## 6. Risks to Track

- Single upstream camera source outage risk during storms (highest-need moment).
- False positive/negative liability in a safety-facing feature — needs explicit confidence thresholds and disclaimers, not a silent beta rollout.
- Camera coverage gaps are a data limitation, not solvable by architecture.

## Related ADRs

ADR-002 and ADR-003 were originally appended below this document. Each is a separate
decision, and each now lives in its own file, copied byte for byte:

- [ADR-002: Backend Language Migration to Go](ADR-002-backend-language-migration-to-go.md)
- [ADR-003: Sprint 1 Scope — Live Updates (SSE), Hot-Set-Scoped Congestion Colors, Phường-Based Filter](ADR-003-sprint1-live-updates-sse-hot-set-phuong-filter.md)

The copy of ADR-004 that used to follow them was a stale duplicate of
[ADR-004: Route-Check Decomposition, Snapshot Archive Strategy, and Sprint 2 Backlog](ADR-004-route-check-and-snapshot-archive.md),
which carries ten further sections that the copy never had. The standalone file is the
maintained version, and it is the one every other document links to.
