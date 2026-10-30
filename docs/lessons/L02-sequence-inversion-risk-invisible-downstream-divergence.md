## Lesson 2: Sequence Inversion Risk & Invisible Downstream Divergence

- **Incident & Tracking:** Issue #136 (S2) deferred past Issue #137 (S3), #138 (S4), #140 (S5b); resolved via PR #159.
- **Symptom:**
  S2 (Hot Set camera curation) was scheduled in Wave 1 between S1 (capacity spike) and S3 (OpenVINO analyzer). S3 bypassed S2 by hardcoding numerical capacity targets (`HotsetFixedMaxCameras = 57`, `HotsetIntervalSeconds = 30`) in `config.go`.
- **Root Cause:**
  While decoupling allowed rapid development of S3, S4, and S5b, the Go backend's temporary fallback seeder in `main.go` selected cameras via crude keyword matching (`"điện biên phủ"`, `"võ văn kiệt"`) without verifying `len(road_keys) == 2`. Consequently, the OpenVINO model was running inference on straight-road mid-block cameras and arbitrary padding rather than genuine intersection choke points.
- **Mandatory Takeaway:**
  1. **Document Sequence Inversions**: If a dependency sequence is inverted to unblock velocity, the architectural divergence and its downstream implications must be explicitly disclosed in sprint documentation.
  2. **Catalog Reconciliation**: Verify that hardcoded assumptions exactly match empirical catalog data as soon as the deferred task is completed (PR #159 reconciled exactly 57 intersection cameras).
