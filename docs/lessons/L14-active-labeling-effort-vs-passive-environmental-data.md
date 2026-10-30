## Lesson 14: Active Labeling Effort vs. Passive Environmental Data Accumulation

- **Incident & Tracking:** Issue #194, ADR-005 Finding B/E (`feat/194-label-real-dataset`).
- **Symptom:**
  Engineers and product teams frequently estimate dataset labeling tasks as a simple function of developer hours (e.g., "1 day to label 300 images"). However, for physical environmental events like urban flooding, positive ground-truth frames cannot be created on demand without fabricating data—they are strictly bounded by natural precipitation occurrences over Ho Chi Minh City.
- **Root Cause:**
  Conflating *active labeling effort* (tooling, human annotation, schema validation) with *passive calendar accumulation time* (waiting for rainstorms to submerge roads). In Sprint 3, this failure caused developers to accept 73k placeholder mock frames (107 bytes) just to satisfy a nominal file count metric.
- **Mandatory Takeaway:**
  1. **Strict Dataset Authenticity**: Never fabricate labels or substitute mock frames to pass a gate deadline. Invalidate and discard all synthetic data.
  2. **Decouple Active Tooling from Passive Accumulation**: Complete data ingestion and auditing tools (`collect_real_frames.py`, `dataset_progress.py`, `labeler.html`) during active work, while explicitly tracking the ongoing passive accumulation window.
  3. **Gate Integrity**: Safety-critical gates (ADR-004 §D6: FNR $\le 5\%$) must remain blocked until genuine environmental samples arrive, preventing premature deployment of untested models.
- **Resolution (2026-10-08 Rainstorm Event)**: During the active HCMC evening rainstorm on 2026-10-08, 764 authentic frames were collected and annotated across 206 unique cameras, including 120 flood-positive frames (98 shallow, 22 deep) across chronic inundation bottlenecks (Ung Văn Khiêm, Nguyễn Xí Cầu Đỏ, Hồ Học Lãm, Huỳnh Tấn Phát, Tô Ngọc Vân). This fully satisfied all ADR-004 §D6 dataset quotas without resorting to synthetic or mock frames.
