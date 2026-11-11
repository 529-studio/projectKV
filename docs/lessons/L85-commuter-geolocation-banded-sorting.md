# Lesson 85: Commuter Geolocation, Multi-Tier Distance Band Sorting, and Mobile Kiosk Ergonomics

## Navigation Aid & Quick Reference
- **Domain**: Frontend geospatial calculation, mobile kiosk ergonomics, resilient UI state machines, privacy-first client telemetry.
- **Relevant Issues**: Issue #400 (S12-T2), Issue #64, Issue #247, ADR-013 D6, Lesson L84.
- **Key Files**:
  - `apps/web/src/utils/geoDistance.ts` (Haversine calculation, 250 m distance bands, multi-tier comparator, float quantization)
  - `apps/web/src/hooks/useGeolocation.ts` (SSR-safe browser geolocation wrapper, watch lifecycle, pure error mapping)
  - `apps/web/src/components/NearMeModal.tsx` (3 mandatory UI states, $\ge 48\text{px}$ touch targets, monochromatic dark UI, zero emoji)
  - `apps/web/src/components/MapComponent.tsx` (dedicated Leaflet `nearMePane` at z=550 with accuracy halo and 1500 m ring)
  - `apps/web/src/app/page.tsx` (single-flight trigger, non-overlapping FAB ergonomics, bilingual synchronization)
- **Core Invariants**:
  - **Multi-Tier Banded Sorting**: Distance grouped into 250 m bands avoids distant severe hazards outranking immediate minor hazards, while prioritizing flood severity inside the commuter's immediate block.
  - **Clock-Free Comparator**: Staleness is resolved once per query using injected timestamp; comparators remain strictly pure, reflexive, and antisymmetric.
  - **Zero Coordinate Exfiltration**: User latitude/longitude coordinates stay in browser volatile memory; never persisted, never logged, never transmitted across network boundaries.
  - **Wet-Screen Ergonomics**: All interactive elements satisfy $\ge 48\times 48\text{px}$ touch targets ($\ge 64\text{px}$ for list items) for reliable operation under rainfall.

---

## 1. Context & Problem Statement

Commuters navigating Ho Chi Minh City during severe monsoon downpours require instant visibility into surrounding flood and traffic hazards. Previously, the web map required manual pinch-and-zoom exploration to locate nearby cameras, an onerous and error-prone interaction on rain-soaked mobile screens.

Implementing last-mile commuter geolocation presented three specific architectural challenges:
1. **The Distance vs. Hazard Trade-Off**: Sorting solely by distance prioritizes dry cameras 50 m away over severe floods 200 m away. Conversely, sorting solely by flood severity prioritizes deep floods 1.4 km away over shallow waters 100 m away that the commuter must navigate first.
2. **Comparator Clock Instability & Float Noise**: In pure sorting comparators, computing verdict staleness via `Date.now()` or comparing raw floating-point distances can violate transitivity due to clock ticks or precision drift (e.g. $249.9999999998\text{ m}$ vs $250.0\text{ m}$).
3. **Resilient Touch Ergonomics**: Touch accuracy degrades significantly on wet mobile screens; standard 32px or 36px controls lead to missed taps and commuter frustration.

---

## 2. Core Architectural Decisions

### A. Multi-Tier 250 m Distance Band Sorting
`geoDistance.ts` resolves the proximity vs. hazard trade-off using 250 m bands ($\min(5, \lfloor d / 250 \rfloor)$):
1. **Distance Band** (Band 0: 0–249 m, Band 1: 250–499 m, ..., Band 5: 1250–1500 m).
2. **Flood Severity** (`FLOODED_DEEP` [4] > `FLOODED_SHALLOW` [3] > `WET` [2] > `DRY` [1] > unknown/stale [0]).
3. **Traffic Severity** (`CONGESTED` [3] > `CROWDED` [2] > `NORMAL` [1] > unknown/stale [0]).
4. **Exact Distance Tiebreak** (metres).
5. **Stable Identifier Tiebreak** (`camera.id` string comparison).

This guarantees that within the immediate traversal block (250 m), hazards dominate order; between blocks, physical proximity dominates.

### B. Float Quantization & Deterministic Staleness Gating
To eliminate sorting jitter:
- Distances are rounded to millimetre precision ($0.001\text{ m}$) prior to band assignment.
- `buildNearMeResults` consumes an explicit `now` parameter and tags each item with boolean `floodStale` and `trafficStale` flags. The comparator inspects boolean flags rather than evaluating wall-clock elapsed time, ensuring mathematical determinism and strict idempotency.

### C. Separate Error Pathways & Resilient Recovery
When network or sensor failures occur:
- Camera catalog failures trigger `onRetryCameras` (reloading catalog without re-prompting the GPS sensor).
- Geolocation permissions or hardware timeouts trigger `onRetryGeo` (re-prompting browser geolocation without thrashing API state).
- The modal provides instant inline bilingual toggle (`VI` / `EN`) to adapt copy immediately without closing or reloading state.

---

## 3. Verification & Metrics

- **Unit Test Matrix**: 77 automated tests in `geoDistance.test.ts` and `useGeolocation.test.ts` validating Haversine accuracy, band transitions ($249.9\text{ m} \to 250.0\text{ m}$, $1499.9\text{ m} \to 1500.0\text{ m}$), catalog diacritics, and zero-emoji compliance.
- **Ergonomic Audit**: All tap targets measured $\ge 48\times 48\text{px}$, drawer items $\ge 64\text{px}$, with floating trigger positioned at `left-3 bottom-3.5` ($x=12\text{px}, y=782\text{px}$ on $390\times 844$ viewports) ensuring zero overlap with mobile search FAB.
- **Privacy Verification**: Zero coordinate network payloads; zero browser persistent storage usage.
