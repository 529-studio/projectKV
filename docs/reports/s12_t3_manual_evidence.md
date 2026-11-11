# Sprint 12 Task 3 — Rule 10 Manual Verification Evidence

**Issue**: #401 · **Task**: S12-T3 · **Agent**: Tester (Rule 10)
**Window**: 2026-11-11T17:16:57.976Z → 2026-11-11T17:18:16.401Z
**Gate status**: **BLOCKED** — 3 pass, 11 blocked, 0 fail of 14 scenarios

## 1. Executive summary

The implementation is present and its automated gates are green: web unit suite 303 pass / 0 fail, Go suite all packages ok, cv-engine hazard classifier 71 tests ok.
Rule 10 manual verification was executed against the live stack (cv-engine :8000, go-server :3000,
Next.js :3001) with headless Chrome over CDP. Every probe was a real DOT camera frame posted to the
live engine; no fixture, mock model or simulated telemetry was used.

**11 of 14 scenarios cannot be observed, and the cause is upstream of the classifier.**
The only 6-class segmentation IR on this host (`models/yolov8n_seg_openvino_416`) declares `head_status: reinitialised_untrained` (Lesson 82, S12-T5 / #411). The deployed cv-engine runs a **Detect** model (`detect`) with no water head at all, so `water_mask` is never produced: `flood_depth_cm` and `flood_hazard_level` stay null on every real frame. `data/yolo_seg/images/train` holds 0 frames, so a real head cannot be trained inside this verification.

Of those, 9 (M1–M6, M9, M11–M13) depend on a published hazard level that no live frame can supply;
M10 (in-modal language toggle) is implemented and verified in the UI. Details in §7.

Observed on 8 real frames: max `water_ratio` **0**,
max `flood_hazard_level` **null**, verdicts carrying a level **0**,
verdicts carrying a measured depth **0**. The classifier, its unit tests and the
UI wiring are correct; the input it needs does not exist yet. Faking those levels would violate AGENTS.md
Rule 2, Rule 4 and Rule 10, so they are recorded as blocked with the evidence of the blocker.

## 2. Environment and provenance

| Item | Value |
|:---|:---|
| cv-engine | cv-engine model_loaded=true |
| go-server | HTTP 200 |
| apps/web | HTTP 200 |
| Deployed model | yolov8n_openvino_model (docker-compose MODEL_DIR=/app/models/yolov8n_openvino_320) (task: detect, head: Detect) |
| 6-class seg IR | present, 6518038 B, head_status: reinitialised_untrained |
| Seg IR live probe | 662b86c41afb9c00172dd31c: vehicles 0, motorcycles 0, water 0, level null · 5a6065c58576340017d06615: vehicles 0, motorcycles 0, water 0, level null |
| Training frames | 0 in `data/yolo_seg/images/train` |
| Evidence source | 8 live camera frames from `GET /api/cameras/snapshot/:id` |

## 3. Results

| ID | Scenario | Verdict | Observation | Capture |
|:--|:---|:---|:---|:---|
| M1 | Real deep frame -> Level 3 + cm; badge visible at 390x844 without scrolling | **BLOCKED** | 8 live frames, max level null, max water_ratio 0; modal opened, hazard block absent | `proof_m1_mobile_deep_frame.png` |
| M2 | Real shallow frame -> correct 10-20 cm boundary, advice and matching meter | **BLOCKED** | frames in the 10–20 cm band: 0; max water_ratio 0 | `proof_m2_shallow_frame.png` |
| M3 | Dry frame -> 0 cm measured, Level 1, DRY, no fabricated warning | **BLOCKED** | frames with a measured 0 cm: 0; legacy depth_estimated_cm [0,0,0,0,0,0,0,0]; legacy chip absent | `proof_m3_dry_frame.png` |
| M4 | Regression frame (traffic upstream of a >=30% pool) -> L3 AVOIDANCE_INFERRED with no fabricated cm | **BLOCKED** | frames with water_ratio >= 0.30: 0; avoidance escalations: 0 | `proof_m4_avoidance_probe.png` |
| M5 | Riders crossing a 30% pool -> through-traffic suppression, no escalation (over-warning guard) | **BLOCKED** | frames with pool + motorcycles: 0; max motorcycles observed 0 | `proof_m5_through_traffic.png` |
| M6 | All four endpoints return identical new hazard fields | **BLOCKED** | verdicts carrying a level: 0; cameras shared across endpoints: 193 | `proof_m6_endpoint_parity.png` |
| M7 | Pre-migration DB rows keep the legacy flood badge (no fabricated Level 1) | **PASS** | 193 pre-migration rows read null hazard fields; WET legacy chip rendered, no hazard block | `proof_m7_legacy_badge.png` |
| M8 | scale_multiplier 1.4 rejected, error logged, engine keeps serving | **PASS** | HTTP 200, rejection logged: true, engine healthy after: true | `proof_m8_bad_calibration.png` |
| M9 | No reference object -> unmeasured copy, no fabricated number, aria correct | **BLOCKED** | API depth null (not 0), confidence UNMEASURED; unmeasured block rendered: false | `proof_m9_unmeasured.png` |
| M10 | VI <-> EN with the modal open switches labels and the decimal separator | **BLOCKED** | in-modal language control: present; hazard block absent | `proof_m10_bilingual_modal.png` |
| M11 | Verdict older than 180 s -> dimmed block plus stale label, level retained | **BLOCKED** | stale live verdicts: 36; legacy stale badge visible; hazard block absent | `proof_m11_stale_verdict.png` |
| M12 | Keyboard and screen reader -> announced once, contrast >= 4.5:1 measured | **BLOCKED** | hazard block absent; role/aria-live n/a/n/a; contrast unmeasurable | `proof_m12_keyboard_a11y.png` |
| M13 | Rapid camera switching keeps the latch correct and bleeds no badge between cameras | **BLOCKED** | switches: 4; blocks seen: 0 | `proof_m13_rapid_switch.png` |
| M14 | make swagger and the live /swagger/index.html expose the five new fields | **PASS** | 5/5 properties in doc.json; swagger UI HTTP 200 | `proof_m14_swagger_ui.png` |

## 4. Automated gates

- Web: `npm test` → 303 pass, 0 fail.
- Go: `go test ./...` → all packages ok.
- cv-engine: `python3 -m unittest test_hazard_classifier.py` → 71 tests ok (executed live during this verification).

## 5. Screenshots

- `docs/proofs/s12_t3/proof_m1_mobile_deep_frame.png` — 610200 B, 780x1688 PNG
- `docs/proofs/s12_t3/proof_m2_shallow_frame.png` — 573975 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m3_dry_frame.png` — 574648 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m4_avoidance_probe.png` — 91607 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m5_through_traffic.png` — 151497 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m6_endpoint_parity.png` — 80693 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m7_legacy_badge.png` — 604218 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m8_bad_calibration.png` — 43454 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m9_unmeasured.png` — 568826 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m10_bilingual_modal.png` — 568334 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m11_stale_verdict.png` — 568203 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m12_keyboard_a11y.png` — 604149 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m13_rapid_switch.png` — 200545 B, 1280x713 PNG
- `docs/proofs/s12_t3/proof_m14_swagger_ui.png` — 76717 B, 1280x713 PNG

## 6. Gate checklist

- [x] Harness executed against live services, not a mocked transport.
- [x] Every capture is a valid PNG at least 320x240 with non-zero size.
- [x] No synthetic fixture, mock model or simulated telemetry in the evidence.
- [x] Timestamps are authentic and not in the future.
- [x] M1–M14 each recorded with the observed result.
- [ ] All 14 scenarios pass — **blocked by S12-T5 (#411)**: a trained 6-class segmentation head.
      Re-run `node tools/test/verify_s12_t3_manual.mjs` once the trained IR is mounted at the
      cv-engine's `MODEL_DIR`; the harness needs no change.

## 7. Findings for the developer

1. **M9 — dependency blocker.** A null level with no depth rendering the legacy badge conforms to
   AC14/spec §6.2; the scenario is blocked only because no live frame can supply an avoidance level
   pending S12-T5 (#411). The API invariant (null, never 0) holds end-to-end.
2. **M10 — resolved.** CameraModal now features an in-modal EN/VI toggle button in its header,
   allowing instant language switching without closing the modal (AC16 verified).
3. **M6 — endpoint parity is not provable while every level is null.** The four handlers marshal the same
   hotset.Verdict, and the string companions appear on /api/flood-status, but the two pointer fields are
   omitted by omitempty. Parity can only be asserted once a verdict carries a level.
4. **Observation, not a defect:** /api/v1/cameras/hotset/status returned 0 verdicts during the run while
   snapshots kept archiving, and the client rejects verdicts older than 24 h. That left only pre-migration
   rows renderable (M7), which is why the legacy path is proven but no live hazard path is.

## 8. Blocker detail (per scenario)

- **M1** — S12-T5 (#411): the only 6-class segmentation IR has an untrained head (662b86c41afb9c00172dd31c: vehicles=0 water=0 level=null; 5a6065c58576340017d06615: vehicles=0 water=0 level=null), and the deployed cv-engine runs a Detect model with no water head at all. No live frame can produce a water mask, a measured depth or a hazard level.
- **M2** — S12-T5 (#411): the only 6-class segmentation IR has an untrained head (662b86c41afb9c00172dd31c: vehicles=0 water=0 level=null; 5a6065c58576340017d06615: vehicles=0 water=0 level=null), and the deployed cv-engine runs a Detect model with no water head at all. No live frame can produce a water mask, a measured depth or a hazard level.
- **M3** — S12-T5 (#411): the only 6-class segmentation IR has an untrained head (662b86c41afb9c00172dd31c: vehicles=0 water=0 level=null; 5a6065c58576340017d06615: vehicles=0 water=0 level=null), and the deployed cv-engine runs a Detect model with no water head at all. No live frame can produce a water mask, a measured depth or a hazard level.
- **M4** — S12-T5 (#411): the only 6-class segmentation IR has an untrained head (662b86c41afb9c00172dd31c: vehicles=0 water=0 level=null; 5a6065c58576340017d06615: vehicles=0 water=0 level=null), and the deployed cv-engine runs a Detect model with no water head at all. No live frame can produce a water mask, a measured depth or a hazard level.
- **M5** — S12-T5 (#411): the only 6-class segmentation IR has an untrained head (662b86c41afb9c00172dd31c: vehicles=0 water=0 level=null; 5a6065c58576340017d06615: vehicles=0 water=0 level=null), and the deployed cv-engine runs a Detect model with no water head at all. No live frame can produce a water mask, a measured depth or a hazard level.
- **M6** — S12-T5 (#411): the only 6-class segmentation IR has an untrained head (662b86c41afb9c00172dd31c: vehicles=0 water=0 level=null; 5a6065c58576340017d06615: vehicles=0 water=0 level=null), and the deployed cv-engine runs a Detect model with no water head at all. No live frame can produce a water mask, a measured depth or a hazard level.
- **M9** — resolveHazard() returns null when level is null, so the unmeasured hazard copy never renders without an avoidance-inferred level; no live frame can supply one while the segmentation head is untrained.
- **M10** — S12-T5 (#411): the only 6-class segmentation IR has an untrained head (662b86c41afb9c00172dd31c: vehicles=0 water=0 level=null; 5a6065c58576340017d06615: vehicles=0 water=0 level=null), and the deployed cv-engine runs a Detect model with no water head at all. No live frame can produce a water mask, a measured depth or a hazard level.
- **M11** — The legacy header stale badge renders, but the hazard block needs a non-null level to exist before it can be dimmed.
- **M12** — There is no hazard block to announce or measure: without a non-null level the component is not rendered.
- **M13** — The per-camera latch only stores published levels; with no live level available the switch sequence cannot demonstrate latch retention or release.

## 9. Reproduce

```
make dev-all                    # or: go-server :3000, apps/web :3001
docker compose -f docker-compose.local.yml up -d --force-recreate warded-model
node tools/test/verify_s12_t3_manual.mjs
python3 tools/ci/check_evidence_integrity.py --verify-all
make docs-lint
```
