# Sprint 1 — Hướng dẫn chạy agy CLI theo từng Wave

## Issue Map

| Wave | # | Title | Labels | URL |
|------|---|-------|--------|-----|
| **0** | #36 | A1 — `tools/geo-enrich` | tooling | https://github.com/529-studio/projectKV/issues/36 |
| **0** | #37 | B1 — `internal/live` SSE | backend | https://github.com/529-studio/projectKV/issues/37 |
| **1** | #38 | C1 — Phường field + `/api/phuong` | backend | https://github.com/529-studio/projectKV/issues/38 |
| **1** | #39 | D1 — EventSource client | frontend | https://github.com/529-studio/projectKV/issues/39 |
| **2** | #40 | D2 — Marker color (Hot Set) | frontend | https://github.com/529-studio/projectKV/issues/40 |
| **2** | #41 | D3 — Phường tree filter | frontend | https://github.com/529-studio/projectKV/issues/41 |
| **3** | #42 | E1 — Integration smoke test | cross-cutting | https://github.com/529-studio/projectKV/issues/42 |

---

## Chuẩn bị

Đảm bảo `dev` branch up to date:

```bash
cd ~/repos/projectKV
git checkout dev
git pull origin dev
```

---

## Wave 0 — A1 + B1 (song song, mở 2 terminal)

Hai ticket này hoàn toàn độc lập, không đụng file nhau (A1 tạo `tools/geo-enrich/`, B1 tạo `internal/live/`).

### Terminal 1 — A1 (geo-enrich)

```bash
cd ~/repos/projectKV

agy "Implement issue #36: tools/geo-enrich — batch Phường point-in-polygon enrichment.

Read the full issue at https://github.com/529-studio/projectKV/issues/36 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create tools/geo-enrich/ with a batch script (Python + shapely/geopandas preferred)
- Input: cameras_all.json + Phường boundary GeoJSON
- Output: enriched JSON with phuong_id/phuong_name per camera
- Idempotent: running twice = identical output
- Cameras outside all polygons get null, not fabricated values
- Add Makefile target 'make geo-enrich'
- Add README.md documenting usage and data source

IMPORTANT: First verify the Phường boundary polygon data source (OSM/GSO). If not available as ready download, document the gap in README.md — do NOT fabricate boundaries.

Branch: feat/a1-geo-enrich
Open PR against dev when done. Do NOT merge."
```

### Terminal 2 — B1 (SSE hub)

```bash
cd ~/repos/projectKV

agy "Implement issue #37: internal/live — SSE hub + endpoint replacing WebSocket.

Read the full issue at https://github.com/529-studio/projectKV/issues/37 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create services/go-server/internal/live/hub.go and constants.go
- SSE hub pattern similar to existing internal/ws/hub.go but using net/http + http.Flusher instead of nhooyr.io/websocket
- GET /api/live/stream emits 'event: camera_update' per Hot Set verdict
- Heartbeat every ~15s
- Non-blocking broadcast (drop for slow clients)
- Connection cleanup: no leak after N connect/disconnect cycles
- Wire hub.Broadcast into hotset.Scheduler OnVerdict callback in cmd/api/main.go
- NO nhooyr.io/websocket import — stdlib + gin only
- Do NOT delete internal/ws/ — just add internal/live/ alongside it
- Run architecture-guardian skill review before finishing

Branch: feat/b1-sse-live-hub
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 0 xong

1. Review cả 2 PR trên GitHub
2. Merge cả 2 vào `dev`
3. Cập nhật local:
   ```bash
   git checkout dev && git pull origin dev
   ```

---

## Wave 1 — C1 + D1 (song song, mở 2 terminal)

C1 (backend) và D1 (frontend) không đụng file nhau.

### Terminal 1 — C1 (Phường field)

```bash
cd ~/repos/projectKV

agy "Implement issue #38: internal/camera — expose Phường field + /api/phuong endpoint.

Read the full issue at https://github.com/529-studio/projectKV/issues/38 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Add PhuongID *string and PhuongName *string (pointer types for nullable JSON) to Camera struct in internal/camera/catalog.go
- Add Catalog.Phuongs() and Catalog.FilterByPhuong() methods
- Add GET /api/phuong endpoint in handler.go (new endpoint, don't break existing /api/zones)
- Graceful fallback: if enriched JSON missing, log warning, proceed with nil Phường fields
- JSON keys: phuong_id, phuong_name (snake_case per §4 convention)
- camera is a LEAF package — no internal imports, stdlib + gin only
- Run architecture-guardian skill review before finishing

Branch: feat/c1-phuong-field
Open PR against dev when done. Do NOT merge."
```

### Terminal 2 — D1 (EventSource)

```bash
cd ~/repos/projectKV

agy "Implement issue #39: apps/web — replace WebSocket with EventSource (SSE) in useTraffic.ts.

Read the full issue at https://github.com/529-studio/projectKV/issues/39 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- In apps/web/src/hooks/useTraffic.ts: replace new WebSocket(getWsUrl(...)) with new EventSource(getApiUrl('/api/live/stream'))
- Listen for named event 'camera_update': es.addEventListener('camera_update', ...)
- Remove manual reconnect setTimeout logic — EventSource reconnects natively
- Keep initial fetch of GET /api/traffic-status for hydration
- Cleanup: es.close() in useEffect cleanup
- Add SSE_LIVE: '/api/live/stream' to API_ENDPOINTS in constants.ts
- Do NOT change marker colors or legend (that's D2)
- Do NOT add any npm dependency
- No console errors on load, connect, or reconnect

Branch: feat/d1-eventsource
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 1 xong

1. Review cả 2 PR
2. Merge cả 2 vào `dev`
3. `git checkout dev && git pull origin dev`

---

## Wave 2 — D2 + D3 (song song, mở 2 terminal)

### Terminal 1 — D2 (marker color)

```bash
cd ~/repos/projectKV

agy "Implement issue #40: apps/web — verify and fix marker color behavior for Hot Set only.

Read the full issue at https://github.com/529-studio/projectKV/issues/40 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Verify MapComponent.tsx getTrafficColor works end-to-end: Hot Set cameras (with verdict) show green/yellow/red, all others stay white/gray
- Fix any code path that incorrectly colors non-Hot-Set cameras (stale verdicts, etc.)
- Verify MapLegend.tsx text matches actual runtime behavior
- Verify Vietnamese diacritics are correct in legend
- Only approved colors: #22c55e green, #eab308 yellow, #ef4444 red, #ffffff white, #52525b gray, #000000 stroke
- Do NOT add per-camera polling for non-Hot-Set cameras
- Do NOT implement flood-specific markers
- Test with Go backend Mock analyzer running

Branch: feat/d2-marker-color
Open PR against dev when done. Do NOT merge."
```

### Terminal 2 — D3 (tree filter)

```bash
cd ~/repos/projectKV

agy "Implement issue #41: apps/web — Phường → Đường → Camera tree filter.

Read the full issue at https://github.com/529-studio/projectKV/issues/41 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create apps/web/src/components/PhuongTreeFilter.tsx with 3 collapsible tiers
- Tier 1: Phường (from GET /api/phuong), Tier 2: Đường (derived from displayName, split on ' - '), Tier 3: Camera
- Cameras with null phuong_name → 'Ngoài TP.HCM' / 'Outside HCMC' group
- Add phuong_id/phuong_name to Camera interface in useCameras.ts
- Integrate in page.tsx: sidebar panel (desktop) / collapsible drawer (mobile ≤375px)
- Wire onCameraClick to existing flow, filter selection narrows map cameras
- Monochromatic styling matching MapLegend.tsx (bg-zinc-950, text-zinc-200, border-zinc-800)
- Animation: Framer Motion (already in project) or Tailwind transition
- i18n strings (vi/en): 'Lọc theo Phường'/'Filter by Ward', 'Đường'/'Street', 'Ngoài TP.HCM'/'Outside HCMC', 'Không có camera'/'No cameras'
- NO manual Phường mappings in frontend — read phuong_name from API only
- NO new npm dependency

Branch: feat/d3-phuong-tree-filter
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 2 xong

1. Review cả 2 PR
2. Merge cả 2 vào `dev`
3. `git checkout dev && git pull origin dev`

---

## Wave 3 — E1 (solo, 1 terminal)

```bash
cd ~/repos/projectKV

agy "Run issue #42: Sprint 1 integration smoke test.

Read the full issue at https://github.com/529-studio/projectKV/issues/42 for the complete checklist.

This is a MANUAL smoke test against dev branch (all 6 prior PRs #36-#41 merged).

Steps:
1. Start the Go backend with Mock analyzer: cd services/go-server && go run ./cmd/api/
2. Start the frontend: cd apps/web && npm run dev
3. Run through every checklist item in the issue and report pass/fail for each:
   - Map loads with all cameras, no tile errors
   - Hot Set cameras show live green/yellow/red, updating without refresh
   - DevTools Network: ONE SSE connection to /api/live/stream, NO WebSocket to /ws/live
   - Tree filter: Phường → Đường → Camera works, expand/collapse functional
   - Filter narrows visible map cameras
   - Non-Hot-Set cameras confirmed white/gray only
   - No console errors throughout
   - Mobile viewport (375px) verified
4. Post results as a comment on issue #42
5. If any check fails: file a bug issue with reproduction steps, do NOT close E1

⛔ HARD STOP: Do NOT open any route-check issue or ADR. That is explicitly gated behind E1 passing."
```

---

## Tổng kết workflow

```
Wave 0:  Terminal 1 (A1 #36)  ┃  Terminal 2 (B1 #37)
         ────────────────────────────────────────────
                     ⏸️  Review + Merge to dev
         ────────────────────────────────────────────
Wave 1:  Terminal 1 (C1 #38)  ┃  Terminal 2 (D1 #39)
         ────────────────────────────────────────────
                     ⏸️  Review + Merge to dev
         ────────────────────────────────────────────
Wave 2:  Terminal 1 (D2 #40)  ┃  Terminal 2 (D3 #41)
         ────────────────────────────────────────────
                     ⏸️  Review + Merge to dev
         ────────────────────────────────────────────
Wave 3:  Terminal 1 (E1 #42)
         ────────────────────────────────────────────
                  ⛔ Gate: route-check blocked
```

> **IMPORTANT**: Giữa mỗi wave, **bắt buộc** phải merge PRs vào `dev` và `git pull` trước khi bắt đầu wave tiếp theo. Agent ở wave sau cần code từ wave trước đã có trong `dev` branch.

> **CAUTION**: Không cho agent tự merge PR. Mọi PR đều phải human review trước khi merge (per `AGENTS.md` Zero Direct Push Policy).
