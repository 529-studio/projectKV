## Lesson 38: Trust & Safety for Arterial Saturation & Status Degradation — Never Default Unanalyzed or Stale Metrics to Clear/Green

- **Incident & Tracking:** Issue #251, Sprint 7 Task S7-T6 (`feat/s7-t6-map-marker-saturation`, PR #267).
- **Symptom:**
  During exploratory verification of the real-time arterial saturation coloring on the web explorer map:
  1. Cameras in an `UNKNOWN` traffic status (e.g. backend cold boot, CV-engine standby, snapshot failure, or circuit breaker degradation) were rendered with bright Emerald Green dots (`#10b981`), and tooltips displayed `Giao thông: UNKNOWN` in green text alongside `Độ bão hòa (SI): 0% • PCU: 0`.
  2. Cameras with `Dữ liệu cũ` (stale observations > 3 minutes) also rendered with Green dots on the map explorer.
- **Root Cause:**
  1. **Naive Numeric Defaulting**: In `apps/web/src/utils/hotsetUtils.ts`, `getHotsetMarkerStyle` evaluated saturation index as `getSaturationColor(status.saturation_index ?? 0)`. When a camera is unanalyzed, on standby, or degraded by circuit breaker, the backend yields `saturation_index = 0.0`. Because 0.0 < 0.30, the condition `si < 0.30` evaluated to `TRUE`, classifying unanalyzed and stale cameras as `CLEAR` (`#10b981`).
  2. **Staleness Blindness in Coloring**: While `isCongested` suppressed pulsating animation for stale markers, `fillColor` was still calculated from raw `saturation_index`, coloring stale markers with real-time green/yellow/red dots.
  3. **Missing Enum Localization**: `getSeverityLabel` had mappings for `CLEAR`, `MODERATE`, `HEAVY`, and `CONGESTED`, but lacked an entry for `UNKNOWN`, falling through to the raw English string `UNKNOWN` instead of the localized `Chưa xác định` (vi) or `Unknown` (en).
  4. **Misleading 0% Display**: Tooltips rendered `Độ bão hòa (SI): 0% • PCU: 0` for `UNKNOWN` cameras, falsely communicating to commuters that 0 vehicles were detected on the road, rather than that no valid analysis had been performed.
- **Mandatory Takeaway & Trust & Safety Gate:**
  1. **Strict Verdict Validity Gating (`isHotsetStatusValid`)**:
     - Status markers must never apply arterial traffic colors unless `isHotsetStatusValid` passes (`!status.stale && status.severity && status.severity !== 'UNKNOWN'`).
     - Stale or unknown cameras MUST gracefully fall back to neutral online/offline styling (online white `#ffffff`, offline dark zinc `#52525b`).
  2. **Bilingual Enum Completeness**:
     - All server severity enums (including `UNKNOWN`) must have explicit bilingual entries (`Chưa xác định` / `Unknown`) with zero untranslated English leaking into the Vietnamese UI.
  3. **Zero False Measurement Indicators**:
     - When severity is `UNKNOWN`, saturation index and PCU metrics must be omitted (`""`) to prevent misleading commuters into believing unmonitored roads are completely empty.
