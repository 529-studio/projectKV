## Lesson 9: Competing Sources of Truth & Silent Drift (Ghost Camera Catalog Resync)

- **Incident & Tracking:** Issue #178, Pull Request #179 (`fix/178-camera-catalog-ghost-resync`).
- **Symptom:**
  Users clicking cameras that the map showed as online were greeted with the snapshot failure modal (`KHÔNG THỂ TẢI HÌNH ẢNH SNAPSHOT — Máy chủ proxy báo lỗi 502/503`). A full live audit of all 837 feeds found **712 live** and **125 placeholder/ghost** at scan time, while the committed catalog still advertised 711 cameras as `UP` — including **21 cameras returning the 2,638-byte "IMAGE NOT AVAILABLE" placeholder**.
- **Root Cause:**
  1. **Two competing sources of truth**: static liveness lived both in the JSON catalog (`isOnline`/`status`, sourced from the DOT portal's own — unreliable — status field) and in a hardcoded `KnownGhostCameras` map in `internal/camera/catalog.go`. The two drifted independently: the map marked several *recovered* cameras (`5a607e298576340017d0666b`, `5deb576d1dc17d7c5515ad02`, …) as ghost (false positives) while missing the actual failing set.
  2. **Trusting an upstream status field**: the portal reported `UP` for cameras that only served placeholder frames, so the catalog's `isOnline` could not be trusted without byte-level probing.
  3. **A verifier that could not detect the failure**: the audit script's `brokenOnlineCams` filter only matched `TIMEOUT` / `HTTP_*` / `NETWORK_ERROR`, never `GHOST_PLACEHOLDER`, so its report computed `brokenOnlineCamerasCount: 0` — silently masking the 21 real failures.
- **Mandatory Takeaway:**
  1. **One source of truth for derived state**: never hardcode a second copy of state that can be derived (e.g. ghost membership). Derive it from a generated catalog produced by live probes, and keep a single authoritative path.
  2. **Verify the verifier**: any audit/telemetry filter must be exercised against a known-positive case. A report claiming "0 failures" is a red flag when field reality contradicts it.
  3. **Classify deterministic vs. transient outcomes**: only promote deterministic signals (placeholder/empty body, live JPEG) into persisted state; transient network faults must not churn the catalog.
  4. **Feedback runtime truth into shared state**: the web client now promotes a `503 GHOST_CAMERA` response into shared UI state so the map, sidebar, dock and modal converge immediately, instead of waiting for the next catalog resync.
