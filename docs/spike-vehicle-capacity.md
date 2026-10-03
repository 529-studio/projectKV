# Spike S1 — Vehicle-Analysis Capacity Decision

**Status**: Complete — awaiting human decision before S2 starts  
**Date**: 2026-10-03  
**Branch**: `spike/s1-vehicle-capacity`  
**Related**: ADR-004 §9 D14, §10 S1, §14 Finding 2 & 4, §15  
**Closes**: #135

---

## ⏸️ Human Gate — Required Before S2 Starts

This spike intentionally does **not** auto-select an option. The choice between
Option 1 and Option 3 is a **product trade-off** (coverage breadth vs. update
frequency), not a purely technical decision an agent should make unilaterally.

**Action required**: After reviewing §5 below, confirm one of the following in
a comment on this PR or issue #135 before opening Wave 1 (S2):

> "Proceed with **Option \_\_** (\_\_\_ cameras, \_\_\_s cycle)"

---

## 1. Baseline — Confirmed Throughput

From spike #104 (single-stream measurement on `warded-model` container):

| Metric | Value |
|---|---|
| **Single-stream FPS** | **2.27 FPS** |
| **ms per frame** | **~440 ms** |
| CPU cgroup limit | 1.0 CPU (deliberately protective, not final — ADR-004 §14 Finding 1) |
| RAM cgroup limit | 512 MB |
| Host | `local-edge host` (ZOTAC ZBOX-CI320NANO, Celeron N2930, Debian 13, Docker) |

> **§15 constraint**: Do **not** raise the CPU limit this sprint. `local-edge host`
> is a confirmed SPOF for the entire public `warded-stg` service (ADR-004 §14
> Finding 2 — WiFi-only uplink, all 3 containers on the same box). Preserving
> headroom takes priority over broader coverage.

---

## 2. Camera Counts — From Real Data

Counts derived from `cameras_enriched.json` using the A6 intersection filter
(§13): `road_keys.length >= 2 AND road_keys ∩ target_corridors ≠ ∅`, after
excluding offline, ghost, and duplicate cameras.

### Priority Corridors (Option 1 candidates)

| Corridor | `road_key` | Intersection cameras |
|---|---|---:|
| Võ Văn Kiệt | `vo_van_kiet` | 23 |
| Mai Chí Thọ | `mai_chi_tho` | 13 |
| Phạm Văn Đồng | `pham_van_dong` | 18 |
| Điện Biên Phủ | `dien_bien_phu` | 11 |
| Cộng Hòa | `cong_hoa` | 10 |

> Note: "Xa Lộ Hà Nội" as named in ADR-004 §14 Finding 4 maps to only 5
> intersection cameras under `xa_lo_ha_noi` — the road's main camera cluster is
> captured under `mai_chi_tho` (13 cameras). ADR-004's ~55-camera estimate was
> correct for the Mai Chí Thọ variant.

### Full scope

| Scope | N cameras | Processing time at 440ms/frame |
|---|---:|---:|
| All 16 arterials (intersection-only) | **256** | **112.8 s** |

---

## 3. Capacity Arithmetic

```
FPS (1.0 CPU, 1 stream) = 2.27
Time to process N cameras serially = N × (1000ms / 2.27) / 1000
                                    = N × 0.441 seconds
```

| N cameras | Time needed | Fits 30s? | Fits 45s? | Fits 60s? |
|---:|---:|:---:|:---:|:---:|
| 49 (Option 1a) | 21.6 s | ✅ | ✅ | ✅ |
| 57 (Option 1b — 4 corridors) | 25.1 s | ✅ | ✅ | ✅ |
| 68 (max-safe at 30s) | 30.0 s | ✅ | ✅ | ✅ |
| 75 (Option 1c — 5 corridors) | 33.0 s | ❌ | ✅ | ✅ |
| 102 (max-safe at 45s) | 45.0 s | ❌ | ✅ | ✅ |
| 136 (max-safe at 60s) | 60.0 s | ❌ | ❌ | ✅ |
| 245–256 (all 16 corridors) | 108–113 s | ❌ | ❌ | ❌ |

**Safe maximum N per cycle interval:**

| Cycle | Max safe N | Headroom vs. 1.0 CPU |
|---|---:|---|
| 30 s | **68** | 8% buffer above 57 (Option 1b) |
| 45 s | **102** | 36% buffer above 75 (Option 1c) |
| 60 s | **136** | 81% buffer above 75 (Option 1c) |

---

## 4. Two Options Evaluated

### Option 1 — Narrow to 4–5 Priority Corridors, 30s Cycle

**Recommended sub-variants:**

| Sub-variant | Corridors | N cameras | Cycle | Time used | Headroom |
|---|---|---:|---:|---:|---:|
| **1b** (4 corridors) | DBP + CH + MCT + VVK | **57** | 30 s | 25.1 s | **4.9 s (16%)** |
| **1c** (5 corridors) | DBP + CH + MCT + VVK + PVD | **75** | 45 s | 33.0 s | **12 s (27%)** |

> DBP = Điện Biên Phủ · CH = Cộng Hòa · MCT = Mai Chí Thọ · VVK = Võ Văn Kiệt · PVD = Phạm Văn Đồng

**Pros:**
- Fits inside the 30s cycle with positive headroom — update latency ≤ 30s
- No infrastructure change required
- Headroom absorbs occasional slow frames (network latency, inference jitter)
- SPOF risk (Finding 2) is not aggravated: load stays well below capacity

**Cons:**
- Covers only 57–75 of 256 intersection cameras (~22–29%)
- The 4 named corridors are from ADR-004's earlier rough estimate; they may not
  be the 4 highest-value corridors for this sprint's goals
- QL1 (65 cameras, the busiest single road) is excluded unless added as a
  5th/6th corridor — its 30 intersection cameras fit within Option 1b's budget

**Risk:** 16% headroom at 30s/57 cameras is thin. A batch of slow upstream
fetches (e.g. DOT portal rate-limiting) could cause cycle overlap. The scheduler
already handles this via `maxParallel` semaphore and per-camera context
cancellation — overlap is bounded, not catastrophic.

---

### Option 3 — All 16 Arterials, Widened Cycle (45–60s)

| Variant | N cameras | Cycle needed | Time used | Headroom |
|---|---:|---:|---:|---:|
| 3a (45s) | **102** | 45 s | 45.0 s | **0 s (0%)** |
| 3b (60s) | **136** | 60 s | 60.0 s | **0 s (0%)** |
| 3c (all 256, 60s) | **256** | would need 113s | — | **❌ not viable** |

> There is no safe variant of Option 3 that covers all 256 intersection cameras
> within a 60s cycle at 1.0 CPU / 2.27 FPS. The arithmetic is definitive.

Viable Option 3 variants require **narrowing to ≤ 102 cameras (45s cycle)** or
**≤ 136 cameras (60s cycle)**, which means selecting which corridors to include —
the same problem as Option 1, just with a larger budget and longer refresh lag.

**Pros:**
- Covers more corridors (up to ~40% of intersection cameras at 60s)
- Update interval of 45–60s is still acceptable for pre-trip planning use case

**Cons:**
- 0% headroom at the max-N boundary for each cycle — any slowdown causes
  accumulation of unprocessed cameras
- 45s update cycle means the displayed severity can be nearly a minute stale by
  the time a user acts on it
- Adding load to the SPOF box (Finding 2) with no headroom is risky

---

## 5. Recommendation

**Recommended: Option 1b — 57 cameras, 30s cycle**

Rationale:
1. **30s update cadence** is materially better UX for live traffic monitoring
   than 45–60s. A user checking a corridor before entering it sees data that is
   at most 30s old vs. up to 60s old.
2. **16% headroom** at 57 cameras/30s is tight but workable. The scheduler's
   per-camera context timeout (10s) prevents a single stuck camera from blocking
   the cycle. True risk is sustained slow batches — monitor in S7.
3. **SPOF alignment**: deliberate, conservative load. Keeps the box stable while
   S8's resilience/fallback work is pending.
4. **QL1 optionality**: the 4-corridor set (57 cameras) leaves 11 slots before
   hitting the 68-camera safe ceiling. If QL1 is added as a 5th corridor
   (10–15 intersection cameras), the budget is still within 30s — this decision
   is for S2 to make with the curated list.

**Escalate to Option 1c (75 cameras, 45s cycle)** only if S2's intersection
filter on the 4 corridors yields fewer than 40 cameras and coverage is deemed
too sparse. In that case, the 5th corridor (Phạm Văn Đồng) buys 18 more cameras
at the cost of a 15s longer refresh.

**Do not proceed to Option 3** this sprint. No variant of Option 3 that covers
the full 256-camera set is viable at 1.0 CPU. Variants that narrow Option 3 to
≤136 cameras still incur a 60s cycle with zero headroom — that is a worse
trade-off than Option 1b.

---

## 6. What Does NOT Change

- `warded-model` CPU / RAM limits: **unchanged** (1.0 CPU / 512 MB)
- Analysis cycle for **flood** verdicts: remains on its own VLM cadence,
  separate from the vehicle detection cycle (ADR-004 §9 D16)
- The final camera list: determined by S2 (intersection filter over the chosen
  corridors from `cameras_enriched.json`)
- Scheduler `maxParallel`: currently 1 for `vlm`, will need tuning in S3 for
  the OpenVINO engine — not this spike's scope

---

## 7. Definition of Done — Checklist

- [x] Single-stream FPS confirmed at 2.27 FPS / 440ms per frame (spike #104,
      reproduced here)
- [x] Safe N calculated for 30s, 45s, and 60s cycle intervals (§3 table)
- [x] Real camera counts computed from `cameras_enriched.json` using A6
      intersection filter (§2 table)
- [x] Option 1 and Option 3 evaluated quantitatively with headroom and cons
      documented (§4)
- [x] Recommendation stated with rationale (§5)
- [x] Decision document committed to `docs/spike-vehicle-capacity.md`
- [x] CPU limit NOT raised — constraint respected throughout
- [ ] **Human confirmation of chosen option** — required before S2 starts (§ top)
- [ ] Feature branch pushed, PR opened against `dev`
