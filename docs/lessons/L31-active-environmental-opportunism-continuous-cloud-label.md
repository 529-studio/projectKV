## Lesson 31: Active Environmental Opportunism & Continuous Cloud Label Synchronization

- **Incident & Tracking:** Branch `tooling/collect-and-label-rain-frames-2026-10-13`.
- **Symptom:**
  Gathering high-value real-world environmental edge cases (standing water reflections, tire spray, raincoat detection, ponding along curbs) cannot be artificially synthesized or simulated in laboratory settings. When active weather events occur (such as the afternoon rainfall in HCMC on 2026-10-13), passive waiting misses rare environmental windows, while manual SFTP-based file pulling creates operational friction for distributed team labeling.
- **Root Cause:**
  Ground-truth evaluation datasets traditionally relied on monolithic historical collection windows (e.g. the 2026-10-08 rainstorm). Without active ingestion tooling hooked to live meteorological conditions, datasets fail to accumulate fresh environmental variety.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Meteorological Event Ingestion Pipeline**: Leveraged real-time weather triggers (`rain >= 0.10 mm`, `weather_code 51`) to automatically execute targeted snapshot harvesting across 10 authentic municipal flood chokepoints and flood-prone corridors (201 frames captured in 3 minutes, 97.1% success rate).
  2. **Multi-Modal Ground-Truth Verification**: Automated high-throughput vehicle detection (OpenVINO YOLOv8n FP16 416x416 at 31.2 frames/sec) combined with visual road surface auditing (puddles, ponding, tire spray, raincoat detection).
  3. **Dual-Format Cloud Synchronization**: Concurrently emitted labels in both Team Web Labeler format (`labels/<YYYY-MM-DD>/<camera_id>_<annotator>_<timestamp>.json`) and Benchmark Evaluation format (`labels_today_benchmark/`), syncing 201 JPEGs and 201 JSONs directly to Cloudflare R2 blob storage via `rclone` with Zero Trust edge protection.
