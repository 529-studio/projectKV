# Lesson 512: My-Route Personal Journey Glance, Hazard Bottleneck Isolation & Avoidance Detours

**Sprint**: Sprint 17 (Commuter Transformation)  
**Issue**: [#512](https://github.com/529-studio/projectKV/issues/512)  
**Category**: Frontend & Backend Routing / Commuter Safety Architecture  
**Author**: Antigravity Assistant  
**Date**: 2026-11-21  

---

## 1. Problem Statement & Commuter Context

Real-world commuters in Ho Chi Minh City do not navigate an expansive 837-camera CCTV grid or click into isolated individual corridors while getting ready to leave home or work. Instead, commuters travel specific **origin-destination paths** (e.g. Bình Thạnh $\leftrightarrow$ District 1, District 7 $\leftrightarrow$ District 1, or Gò Vấp $\leftrightarrow$ Bình Thạnh) spanning connected arterial corridors.

When preparing to depart, a commuter needs an instant, definitive **3-second safety glance**:
> *"Can I commute safely between my Home and Work right now with my specific vehicle, or will I encounter impassable water that will flood my engine?"*

If a hazard exists, the commuter requires two critical pieces of proactive guidance:
1. **The single worst hazard bottleneck** isolated along their trajectory (e.g. exact location, peak flood depth in cm, and physical consequence for their vehicle profile).
2. **An immediate dry bypass corridor recommendation** (e.g. bypassing flooded Nguyễn Hữu Cảnh via dry Điện Biên Phủ).

---

## 2. Technical Architecture & Invariants

```
+---------------------------------------------------------------------------------+
|                       COMMUTER PERSONAL JOURNEY ENGINE                          |
+---------------------------------------------------------------------------------+
                                      |
         +----------------------------+----------------------------+
         |                                                         |
         v                                                         v
[Client Local-First Storage]                               [Go Modulith Backend]
  • Key: warded_commute_route                                • GET /api/corridors/commute-eval
  • Home & Work pins + corridors                             • corridors=k1,k2&vehicle=v
  • 100% Local Privacy (0 telemetry)                        • Aggregated duration, distance & delay
         |                                                         |
         +----------------------------+----------------------------+
                                      |
                                      v
                        [Worst Bottleneck Extraction]
                          • Depth-first priority: max(depth_cm)
                          • Traffic congestion fallback
                          • Vehicle clearance threshold mapping
                                      |
                                      v
                       [Arterial Bypass Detour Graph]
                          • nguyen_huu_canh <-> dien_bien_phu
                          • cach_mang_thang_tam <-> nam_ky_khoi_nghia
                          • cong_hoa <-> pham_van_dong
                          • vo_van_kiet <-> nguyen_van_linh
                                      |
                                      v
                       [One-Tap 3-Second My-Route Card]
                          • Visual Verdict Badge: PASSABLE / CAUTION / IMPASSABLE
                          • Time (+delay), Distance, Weather
                          • Bottleneck Callout + One-Click Detour Switcher
```

### A. Local-First Privacy Invariant
Commuters' daily home and workplace locations constitute sensitive personally identifiable geographic data. In strict adherence to ADR-004 and local-first privacy standards:
- All route configurations are stored in `localStorage` under `warded_commute_route`.
- Zero user coordinates, address names, or travel logs are transmitted to external analytics or centralized profiling servers.
- The backend API (`/api/corridors/commute-eval`) operates solely on anonymized, public arterial corridor keys (`corridors=nguyen_huu_canh,dien_bien_phu`).

### B. Dynamic Clearance Adaptation
Passability verdicts across the multi-corridor journey strictly evaluate against the commuter's selected vehicle profile (Issue #510):
- **Scooter (Vision/Lead/AirBlade)**: Caution at 10 cm, Impassable at 20 cm.
- **Motorbike (Wave/Future/Exciter)**: Caution at 15 cm, Impassable at 28 cm.
- **Sedan (Vios/City/Mazda3)**: Caution at 15 cm, Impassable at 25 cm.
- **SUV (Everest/Fortuner/Ranger)**: Caution at 25 cm, Impassable at 40 cm.

### C. Bottleneck Isolation & Detour Graph
When multiple hazards exist along a commute path:
1. **Bottleneck Isolation**: The engine isolates the single highest water depth checkpoint (or worst traffic congestion node if dry) and constructs a bilingual contextual description.
2. **Detour Graph**: If a corridor on the path becomes `IMPASSABLE` or `CAUTION`, the detour engine queries canonical bypass pairs (`CorridorDetourPairs` in Go and `CORRIDOR_DETOUR_MAP` in TypeScript). If a bypass is `PASSABLE`, it is surfaced on the card with a one-tap action to view and highlight the detour corridor on the interactive map.

---

## 3. Empirical Verification Evidence

1. **Go Server Backend Tests**:
   - `services/go-server/internal/corridor/corridor_test.go`:
     - `TestEvaluateCommute_MultiCorridorAndDetour`: Verifies multi-corridor aggregation, bottleneck isolation, vehicle clearance threshold adaptation, and detour recommendation (Nguyễn Hữu Cảnh $\rightarrow$ Điện Biên Phủ).
     - Validates HTTP status codes: 400 Bad Request on empty corridors, 200 OK with `CommuteEvaluationResponse`.
2. **Frontend Unit Tests**:
   - `apps/web/src/utils/commuteRouteUtils.test.ts`:
     - 7 unit test scenarios covering path summation, bottleneck isolation, vehicle clearance adaptation, detour suggestions, spatial coordinate interpolation, and bilingual formatting.
     - All 466 frontend tests pass (`npm test`).
3. **Playwright E2E Visual Verification**:
   - `docs/proofs/512-my-route-glance/01_commute_setup_modal.png`: Configuration modal with quick presets and custom pins.
   - `docs/proofs/512-my-route-glance/02_my_route_verdict_card.png`: Docked 3-second glance card with passability verdict, vehicle profile, and travel duration.
   - `docs/proofs/512-my-route-glance/03_avoidance_detour_suggestion.png`: Hazard bottleneck callout and recommended detour route bypass.
4. **Docs & Evidence Linting**:
   - `tools/docs/docs_lint.py`: 0 new violations.
   - `tools/ci/check_evidence_integrity.py`: 122 artifacts evaluated, 100% green.
