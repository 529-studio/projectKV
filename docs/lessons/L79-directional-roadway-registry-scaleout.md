# Lesson 79: Directional Roadway Registry Scale-Out & Geometry Invariants

## Navigation Aid & Quick Reference
- **Domain**: Directional Geometry, Computer Vision Registry, ADR-012 D1, Roadway Segmentation, Leaf Modulith Backend
- **Relevant Issues**: #397 (Sprint 11 Wave 1 / S11-T3)
- **Key Files**:
  - `data/roadway_regions/<camera_id>.json` (59 dual carriageway configurations)
  - `tools/eval/data/hotset_roadway_calibration.json` (Committed calibration manifest for clean clone reproducibility)
  - `tools/eval/build_roadway_registry.py` (Registry generation & verification CLI)
  - `tools/eval/build_roadway_registry_test.py` (Geometry invariant and IoU verification suite)
  - `services/go-server/internal/camera/roadway_registry.go` (In-memory concurrent registry)
  - `services/go-server/internal/camera/roadway_registry_test.go` (Go backend integration tests)
- **Core Invariants**:
  - Full Hot Set dual coverage: All 59 priority corridor Hot Set cameras (57 canonical + 2 supplemental) define `direction_coverage: "dual"` with exactly 2 directional regions (`dir_a` and `dir_b`), eliminating asymmetric jam dilution across major arterial corridors (`cong_hoa`, `truong_chinh`, `vo_van_kiet`, `mai_chi_tho`, `dien_bien_phu`).
  - 100% Camera-specific perspective calibration: 56 cameras calibrated from authentic photographic proof snapshots via horizon inflection and carriageway bounds detection, plus 3 curated Sprint 10 pilot corridors. Cloned template fallbacks are strictly prohibited and rejected by `--verify`.
  - Carriageway separation ($\text{IoU} < 0.05$): Polygon A and Polygon B maintain distinct spatial extents with zero bounding box collision ($\text{IoU} = 0.0000$, median separation gap $\ge 0.03$).
  - Capacity conservation: Vertices normalized in $[0.0, 1.0]$ with 4 vertices per polygon; capacity shares allocate 0.50 per carriageway summing to $1.0 \pm 0.01$.
  - Clean degradation: Unregistered cameras degrade cleanly to `direction_coverage: "none"` rather than fabricating directional splits without geometry.

---

## 1. Context & The Problem
In Sprint 10, ADR-012 D1 introduced directional carriageway ingestion to eliminate asymmetric jam dilution (where heavy congestion in one direction is averaged against an empty opposing direction, reporting false "moderate" conditions). However, only 3 pilot cameras (5.26%) were calibrated with directional polygons. The remaining 54 cameras fell back to frame-wide metrics (`direction_coverage: "none"`), leaving critical choke points such as the Cộng Hòa - Trường Chinh intersection (`586e1f18f9fab7001111b0a5`) vulnerable to diluted traffic saturation.

## 2. Root Cause Analysis
1. **Manual Polygon Bottleneck**: High-fidelity dual polygons required manual coordinate annotation, slowing scale-out to the full Hot Set scope.
2. **Reproducibility Gap**: Calibration relying solely on gitignored raw snapshot images (`data/proofs/**`) risks non-reproducible rebuilds on fresh git checkouts if a committed manifest is absent.
3. **Absence of Carriageway Separation & Uniqueness Guards**: Early generators did not test for duplicate templates or geometric overlap, risking intersecting polygons where a single vehicle could trigger saturation in both opposing directions.

## 3. Architecture & Solution

### A. Snapshot Perspective Calibration & Calibration Manifest
- **Authentic Perspective Calibration**: Analyzed authentic camera-view proof frames in `data/proofs/` and `services/go-server/data/proofs/` using horizon inflection ($y_{\text{horizon}} \approx 0.38 - 0.48$) and column luminance gradient detection in the lower road crop, yielding 56 camera-specific geometry pairs.
- **Reproducible Manifest**: Committed `tools/eval/data/hotset_roadway_calibration.json` recording polygons and source snapshot stems for all 59 cameras, guaranteeing clean-clone reproducibility without raw snapshot dependencies.
- **Fail-Loudly Policy**: Removed silent fallback constants. If a camera lacks both snapshot and manifest calibration, the build raises an immediate error rather than cloning a template.

### B. Geometry Invariant, IoU, and Uniqueness Verification
- **Uniqueness & Drift Guard**: `verify_registry_dir` (`--verify`) scans all registered polygon pairs with a hardened pairwise distance threshold ($< 10^{-3}$), rejecting duplicate or near-identical templates. It also verifies zero drift against the canonical calibration manifest, asserting schema, capacity shares, direction coverage, and standardized neutral descriptions.
- **Bounding Box IoU Algorithm**: Implemented `compute_bbox_iou` in `tools/eval/build_roadway_registry_test.py` to assert non-overlapping bounding boxes ($\text{IoU} < 0.05$). All 59 cameras achieve $\text{IoU} = 0.0000$.
- **Corridor Verification**: Explicitly tested critical arterial corridors: Cộng Hòa (`586e1f18f9fab7001111b0a5`, `5874769eb807da0011e33cec`, `587476e3b807da0011e33cee`) and Trường Chinh choke point.

### C. Leaf Modulith Go Server Integration
- **`LoadRoadwayRegistryFromDir`**: Exposed directory loader alias in `services/go-server/internal/camera/roadway_registry.go`.
- **Integration Test (`roadway_registry_test.go`)**: Added `TestLoadRoadwayRegistryFromDir_HotSetCameras` verifying that all 59 Hot Set cameras load into memory, zero cameras degrade to `none`, and all return valid dual directional geometry with `-race` enabled.

## 4. Verification & Prevention Rules
1. **Never ship duplicate geometry templates**: Hot Set registries must maintain unique, perspective-calibrated geometries. The `--verify` gate rejects template cloning and near-identical jitter ($< 10^{-3}$ deviation).
2. **Mandatory bounding box overlap check**: Every directional geometry must pass automated non-overlap assertions ($\text{IoU} < 0.05$) before inclusion in production registries.
3. **Committed Calibration Manifest & Drift Gate**: Any pipeline generating geometry from non-tracked media must commit a lightweight JSON manifest so rebuilds are 100% reproducible from a clean git checkout, protected by an automated drift gate.
