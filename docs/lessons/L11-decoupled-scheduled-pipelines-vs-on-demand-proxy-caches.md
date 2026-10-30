## Lesson 11: Decoupled Scheduled Pipelines vs. On-Demand Proxy Caches (Archiver Integration Gap)

- **Incident & Tracking:** Issue #182, Pull Request (`feat/182-archiver-direct-frame-delivery`).
- **Symptom:**
  Following the production migration to OpenVINO (`ANALYZER=onnx`, 2026-10-06), real frame archiving dropped to ~40 frames/day (54 on 10-06, 26 on 10-07), far below the expected ~57 cameras / 5-minute sampling rate (~16,000 frames/day). This severely choked dataset accumulation needed for model evaluation (ADR-004 D6 gate).
