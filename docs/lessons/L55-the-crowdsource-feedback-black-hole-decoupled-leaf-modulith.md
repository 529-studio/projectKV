## Lesson 55: The Crowdsource Feedback Black Hole & Decoupled Leaf Modulith Bridging

- **Incident & Tracking:** Issue #289 / PR targeting `dev` (`feat/s8-t7-crowdsource-runtime-bridge`, [KV-19]).
- **Symptom:**
  1. In Sprint 7, `internal/feedback` and `CameraModal.tsx` were implemented to accept commuter flood reports.
  2. However, reports were merely written to Cloudflare R2 as a passive archive with zero connection to the live runtime system. When citizens submitted flood reports, the live map remained completely unchanged (the "Crowdsource Black Hole" identified in ADR-010).
- **Architectural Conflict:**
  - `internal/feedback` is designed as a strict leaf package with zero sibling imports to preserve the leaf modulith architecture standard (Rule 7).
  - Directly importing `internal/live` or `internal/hotset` into `internal/feedback` would introduce illegal circular or horizontal package couplings.
- **Root Cause & Engineering Resolution:**
  1. **Dependency Inversion via Observer Callback**:
     - Extended `feedback.Handler` with an observer hook:
       ```go
       type Report struct {
           CameraID     string    `json:"camera_id"`
           ClientIPHash string    `json:"client_ip_hash"`
           UserAgent    string    `json:"user_agent"`
           Timestamp    time.Time `json:"timestamp"`
       }
       func (h *Handler) SetOnReportAccepted(fn func(cameraID string, report Report))
       ```
     - Preserved 0 sibling imports in `internal/feedback` while enabling the composition root (`cmd/server/main.go`) to bridge reports to runtime hubs.
  2. **Runtime Event Broadcast & Multi-Reporter Escalation**:
     - In `services/go-server/internal/live/`:
       - Emitted named SSE event `user_flood_report` over `/api/live/stream`.
       - Implemented `OnUserReport` in `FloodHexAggregator`:
         - Tracked a 15-minute sliding TTL per camera (`userReportTTL = 15m`).
         - Maintained a 10-minute sliding window to evaluate distinct commuter IP hashes (`escalationWindow = 10m`).
         - Escalated prominence when $\ge 2$ distinct IP hashes report flooding at the same camera within 10 minutes (`escalated = true`).
       - Added snapshot endpoint `GET /api/flood/user-reports` to hydrate web clients immediately on page refresh without waiting for new events.
  3. **High-Contrast Amber Candidate State & Zero-Emoji Presentation**:
     - In `apps/web/`:
       - In `useFlood.ts`: Hydrated active reports from `/api/flood/user-reports` and maintained reactive map via SSE `user_flood_report` with periodic 15s pruning of expired reports.
       - In `MapComponent.tsx`: Promoted reported cameras to high-contrast amber (`#f59e0b`) with pulse animation and remaining countdown (`[Báo cáo từ người dân: Có ngập] (còn 14m)`).
       - In `CameraModal.tsx`: Rendered prominent amber status badge and action chip in both header and report control sections.
       - Enforced Rule 1 strictly: Zero emojis, bilingual i18n (`vi` default with full Vietnamese diacritics).






