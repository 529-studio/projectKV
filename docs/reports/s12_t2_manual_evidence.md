# Sprint 12 Task 2: Commuter Geolocation Verification Report

**Issue**: Close #400  
**Task**: S12-T2 Commuter Last-Mile Geolocation (GPS 'Near-Me' View)  
**Date**: 2026-11-11  
**Status**: Verified & Compliant (All 14 Scenarios M1–M14 Verified)

---

## 1. Overview & Verification Summary

Sprint 12 Task 2 delivers the 1-click commuter last-mile geolocation capability, centering map coordinates, computing Haversine proximity, sorting cameras into 250 m bands by flood/traffic hazard severity, and rendering a shop-floor ergonomic bottom sheet ($ge 48	imes 48\text{px}$ touch targets).

All automated gates pass 100%:
- **Web Test Suite**: 258/258 unit tests pass in 0.45s (`apps/web/src/**/*.test.ts`).
- **TypeScript Gate**: `npx tsc --noEmit` exits 0 with zero errors across all modules and tests.
- **Next.js Production Build**: `npm run build` exports static pages cleanly with zero bundle errors.
- **Evidence Integrity CI Gate**: `python3 tools/ci/check_evidence_integrity.py` passes with zero synthetic data violations (`fit_source == 'live_history'`).
- **Docs Lint**: `python3 tools/docs/docs_lint.py` passes with 0 new violations and `docs/lessons/INDEX.md` strictly within the 20,480 B cap.

---

## 2. Empirical Verification Scenarios (Rule 10)

| ID | Scenario | Verification Result | Empirical Evidence |
|:--|:---|:---|:---|
| **M1** | Desktop 1280x800 In-Bounds Near-Me View | **PASS** | 58 cameras in radius, 10 visible, all tap targets $ge 48\text{px}$, map rings rendered |
| **M2** | Geolocation Permission Denied | **PASS** | Denied state with step-by-step recovery steps, manual retry & view full map |
| **M3** | Geolocation Timeout | **PASS** | Timeout state with open-sky advice, non-blocking fallback |
| **M4** | Out-of-Service Area (Hà Nội coordinates) | **PASS** | Explicit out-of-area state with one-click 'Quay lại TP.HCM' action |
| **M5** | Camera-Free Area (Mekong Delta) | **PASS** | Empty state naming 1.5 km radius, view-full-map action |
| **M6** | Mobile 390x844 Bottom Sheet Ergonomics | **PASS** | Sheet height $le 80\text{vh}$, trigger at `left-3` with zero overlap with search FAB, tap targets $ge 48\text{px}$ |
| **M7** | Row Click Camera Selection | **PASS** | Closes NearMeModal, opens CameraModal, pans map to zoom 15 |
| **M8** | Bilingual Language Switch (VI <-> EN) | **PASS** | Instant toggle inside modal header; transforms all labels & distance formats |
| **M9** | Map Panning Invariant | **PASS** | Manual map dragging preserves user marker; never snaps back unexpectedly |
| **M10** | Single-Flight Rapid Click Concurrency | **PASS** | Ref-based single-flight guard suppresses duplicate sensor prompts |
| **M11** | Camera API Catalog Failure & Split Retry | **PASS** | Catalog errors call `refetchCameras()` without re-prompting GPS sensor |
| **M12** | Reduced Motion Mode | **PASS** | `prefers-reduced-motion` uses instantaneous `setView` instead of animated `flyTo` |
| **M13** | Dynamic Offline Camera Toggle | **PASS** | Toggle updates list count reactively |
| **M14** | Keyboard Focus Trap & Esc Listener | **PASS** | Tab cycles within modal; Esc key closes sheet and restores trigger focus |

---

## 3. Visual Evidence Artifacts

- `docs/proofs/s12_t2/proof_m1_desktop_sheet.png` (M1: Desktop in-bounds sheet)
- `docs/proofs/s12_t2/proof_m2_permission_denied.png` (M2: Denied recovery guidance)
- `docs/proofs/s12_t2/proof_m3_timeout.png` (M3: Timeout fallback)
- `docs/proofs/s12_t2/proof_m4_outside_service_area.png` (M4: Outside HCMC warning)
- `docs/proofs/s12_t2/proof_m5_empty_radius.png` (M5: Zero-camera empty state)
- `docs/proofs/s12_t2/proof_m6_mobile_bottom_sheet.png` (M6: Mobile 390x844 layout)
- `docs/proofs/s12_t2/proof_m7_camera_modal_select.png` (M7: Modal camera selection)
- `docs/proofs/s12_t2/proof_m8_bilingual_en.png` (M8: English translation toggle)
