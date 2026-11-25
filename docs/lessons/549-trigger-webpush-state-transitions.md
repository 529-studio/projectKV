# Lesson 549: Hazard State Transition Tracking and Debounce-Bypass Escalation

## Navigation Aid & Quick Reference
- **Domain**: Real-time notification subsystem, state transition evaluation, debounce-bypass hazard escalation, arterial corridor alert dispatch.
- **Relevant Issues**: Issue #549 (S19-T1), Issue #525 (VAPID Web Push), Issue #478 (Corridor Alerts).
- **Key Files**:
  - `services/go-server/internal/notify/transition.go` (Thread-safe `StateTracker` for camera severities and corridor passabilities)
  - `services/go-server/internal/notify/transition_test.go` (Unit tests verifying escalation, recovery, and transition idempotency)
  - `services/go-server/internal/notify/manager.go` (Integrated transition evaluation with escalation debounce bypass in `Dispatch` and `DispatchCorridor`)
  - `services/go-server/internal/corridor/corridor.go` (`GetCorridorRouteSummary` public helper for instant corridor assessment)
  - `services/go-server/cmd/server/adapters.go` (`makeFloodAlertDispatcher` wired with `corridorService` and corridor alert dispatch)
  - `services/go-server/cmd/server/transition_alert_integration_test.go` (End-to-end integration test verifying full multi-channel dispatch lifecycle)
- **Core Invariants**:
  - **Escalation Debounce Bypass**: When water depth or hazard severity escalates (e.g. `CLEAR` -> `FLOODED_SHALLOW` or `FLOODED_SHALLOW` -> `FLOODED_DEEP`, and `PASSABLE` -> `CAUTION`/`IMPASSABLE`), debounce suppression is bypassed immediately to ensure critical safety warnings reach citizens in real-time. The debounce quiet period is simultaneously refreshed.
  - **State Recovery Tracking**: Transitions back to non-hazard states (`CLEAR`, `DRY`, `PASSABLE`) are recorded in `StateTracker` silently without firing network alerts. This guarantees subsequent flood events are recognized as genuine new escalations rather than suppressed as repeat verdicts.
  - **Zero-Emoji Monochromatic Contract**: Conforms strictly to AGENTS.md Rule 1 with clean uppercase titles and no emoji codepoints in dispatch payloads.

---

## 1. Context & Operational Problem

In Sprint 18, RFC 8291 / RFC 8292 VAPID Web Push delivery was established (`internal/push`), but alert triggering suffered from two architectural limitations:
1. **Blind Time Debouncing**: Notifications were dispatched based on a static 30-minute sliding window without understanding state changes. If a camera experienced a sudden surge from shallow puddles (`FLOODED_SHALLOW`) to engine-drowning depths (`FLOODED_DEEP`) 5 minutes later, the alert was suppressed, endangering motorists.
2. **Corridor Alert Disconnect**: Background verdict processing triggered camera-level alerts, but arterial corridor route passabilities (`corridor.Service`) were not wired to trigger `notifyManager.DispatchCorridor`. Commuters subscribed to arterial routes received no push notifications when entire roadways became impassable.

---

## 2. Architectural Design & Implementation

### A. Thread-Safe `StateTracker` in Leaf `notify` Package
A dedicated `StateTracker` was implemented in `services/go-server/internal/notify/transition.go` without introducing cross-module imports (maintaining leaf package status per Rule 7):
- Evaluates camera severity ranks (`FLOODED_DEEP` [3] > `FLOODED_SHALLOW` [2] > `CLEAR` [1] > unknown [0]).
- Evaluates corridor passability ranks (`IMPASSABLE` [3] > `CAUTION` [2] > `PASSABLE` [1] > unknown [0]).
- Determines whether a new observation represents `IsEscalation`, `IsRecovery`, or a steady-state repetition.

### B. Escalation Debounce-Bypass Protocol
Inside `notify.Manager.Dispatch` and `notify.Manager.DispatchCorridor`:
- If `tr.IsEscalation == true`, the event calls `debouncer.RecordAlert` to reset the quiet window and dispatches immediately to all registered delivery channels.
- If `tr.IsEscalation == false` (steady-state repeat flood), the event undergoes standard debounce checks (`30m` for cameras, `15m` for corridors) to prevent alert fatigue.

### C. Corridor State Dispatch Wiring
`makeFloodAlertDispatcher` in `cmd/server/adapters.go` was extended to accept `corridorService *corridor.Service`. When a camera verdict arrives:
1. Camera-level `notify.FloodAlert` is evaluated and dispatched.
2. If the camera is mapped to an arterial corridor (`cam.RoadKey != ""`), `corridorService.GetCorridorRouteSummary(roadKey)` computes the latest passability verdict, estimated delay, and max depth, instantly dispatching `notify.CorridorAlert` across channels.

---

## 3. Verification & Empirical Proof

1. **Unit Tests**:
   - `services/go-server/internal/notify/transition_test.go`: 100% passing.
   - `services/go-server/internal/notify/manager_test.go`: Verified `TestManager_Dispatch_EscalationBypassesDebounce`.
2. **End-to-End Integration Test**:
   - `services/go-server/cmd/server/transition_alert_integration_test.go`: Tested all 5 real-world scenarios under `go test -race` with zero data races.
3. **Monolith & Leaf Module Compliance**:
   - Zero internal imports under `services/go-server/internal/notify/`.
   - All tests in `services/go-server/...` pass 100% green.
