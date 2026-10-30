## Lesson 24: Decoupled Multimodal Hybrid Architecture & Quota-Guarded Dual Triggers

- **Incident & Tracking:** Issue #209, Sprint 5 Task S5-T3, Branch `feat/s5-t3-hybrid-flood-pipeline`.
- **Symptom:**
  Attempting to run cloud VLM vision analysis continuously across 57+ cameras quickly exhausted the free-tier quota (15 RPM / 1,500 RPD), triggering upstream HTTP 429 throttling and incurring unacceptable latency and fragility. Conversely, relying strictly on standard YOLOv8n object detection (COCO weights) yielded 0% flood recall, violating the ADR-004 §D6 safety gate ($\text{FNR} \le 5.0\%$). Furthermore, domain definitions diverged: frontend spatial rain overlay used 1.0 mm (wet pavement onset), 4.0 mm (shallow flood), 15.0 mm (deep flood), whereas backend ADR documents referenced a 2.0 mm/h heuristic and conflated the 10 traffic smoke cameras with municipal flood chokepoints.
- **Root Cause:**
  1. Conflating continuous high-frequency traffic detection (which requires local high-FPS processing like OpenVINO on `inference worker` at 24.01 FPS and $0 API cost) with event-driven flood perception (which requires multimodal semantic scene understanding).
  2. Uncoordinated threshold heuristics between frontend spatial hex aggregation and backend architectural documentation.
  3. Lack of strict sliding-window rate limiting (10 RPM) and daily quota budgeting (300 RPD) in the application layer before calling external cloud VLMs.
- **Mandatory Takeaway:**
  1. **Decoupled Architecture Pattern (Option C)**: Retain high-throughput, $0-API-cost local inference (OpenVINO) for continuous 30-second vehicle scoring. Decouple cloud VLM into dual triggers:
     - *Trigger 1 (Environmental)*: Background polling of authentic municipal flood chokepoints (`tools/eval/fixtures/flood_chokepoints.json`) ONLY during active rain events ($\text{rain\_mm} \ge 1.0\text{ mm}$ or active rain WMO codes), consuming 0 calls during dry weather.
     - *Trigger 2 (User On-Demand)*: Commuter-initiated inspection via modal click in the web app (`POST /api/flood/verify`).
  2. **Domain Model Reconciliation**: Establish a unified source of truth across frontend, backend, and documentation for environmental events (1.0 mm wet onset, 4.0 mm shallow accumulation, 15.0 mm deep flood).
  3. **Strict In-Process Quota Enforcers**: Cloud API calls must always be guarded by application-level rate limiters (thread-safe sliding window and daily quota rollover) to prevent accidental quota exhaustion, cascade failures, or billing spikes.
