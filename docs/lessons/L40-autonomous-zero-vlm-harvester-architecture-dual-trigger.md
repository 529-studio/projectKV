## Lesson 40: Autonomous Zero-VLM Harvester Architecture & Dual-Trigger Meteorological Watcher with Sliding Window Cooldown

- **Incident & Tracking:** Issue #252 (S7-T7 Superseded), Issue #261 (ADR-009 / S7-T9b, PR #265), and Issue #262 (S7-T7b / `internal/harvester`, PR #268).
- **Symptom:**
  Initial Sprint 7 architecture proposed calling Google Gemini VLM API to verify flood status on the 10 known flood chokepoints when rain was detected. At production scale, this would exhaust the 300 RPD quota within 1-2 hours of a tropical downpour, introduce unpredictable 1.5–4.0s network latency, and incur recurring cloud API expenses ($111/mo).
- **Root Cause:**
  Relying on external cloud vision APIs for continuous high-frequency municipal telemetry creates severe operational fragility, network partition vulnerability, and cost scaling cliffs.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Zero-VLM Runtime Directive**: Transition all runtime telemetry to self-hosted, edge-optimized runtimes (OpenVINO YOLOv8n on `inference worker`). Zero cloud VLM calls during live monitoring.
  2. **Dual-Trigger Autonomous Data Flywheel (`internal/harvester`)**:
     - Scheduled peak commute sweeps (07:30, 12:00, 17:30 ICT) across 57 Hot Set cameras.
     - Meteorological rain watcher polling `weather.Service` every 5 minutes; triggers harvesting when precipitation $\ge 0.10\text{ mm/h}$ across 207 flood corridor cameras.
  3. **Sliding Window Cooldown & Concurrency Caps**:
     - Enforce a 60-minute sliding window cooldown on rain triggers and cap concurrent snapshot workers to 15, respecting upstream government portal courtesy and preventing self-inflicted IP throttling.
  4. **Strict Leaf-Modulith Architecture**:
     - Package `internal/harvester` defines consumer interfaces locally and imports zero sibling internal packages (`weather`, `proxycache`, `cvclient`, `storage` are injected into coordinator), maintaining pure leaf independence and zero circular dependencies.
