## Lesson 18: High-Frequency Edge Telemetry vs. Cloud VLM Economics (The Inference Platform Decision)

- **Incident & Tracking:** Issue #197, ADR-005 Finding B/D6 / row T9 (`docs/197-decision-openvino-vs-vlm`).
- **Symptom:**
  Following Sprint 4 T4's initial accuracy benchmark (Macro F1 = 0.209, Flood FNR = 100%), the project evaluated whether to keep self-hosted OpenVINO or revert traffic cameras back to Gemini VLM.
- **Root Cause:**
  Conflating the architectural requirements of continuous, high-frequency edge telemetry (polling 57 Hot Set cameras every 30s = 114 RPM, 164,160 RPD) with low-frequency multimodal reasoning. Reverting continuous polling to cloud VLM APIs creates an operational and economic landmine:
  1. **Free-Tier Impossibility**: Gemini's 15 RPM / 500 RPD quota exhausts within 4 hours on just 2 cameras.
  2. **Unsustainable Paid-Tier Economics**: Polling 57 cameras continuously costs ~$111/month; scaling citywide to 712 cameras costs ~$1,385/month.
  3. **Latency & SLA Breach**: Cloud VLM P50 latency (600–1200ms) over WAN causes the 57-camera batch to take 228 seconds, violating the 30-second cycle budget by 7.6x. In contrast, self-hosted OpenVINO on `inference worker` runs at **24.01 FPS** (18.2ms P50 latency, 2.24s total execution for 50 cameras, <100MB RAM, $0 API cost).
- **Mandatory Takeaway:**
  1. **Decoupled Hybrid Architecture (Option C)**: Retain self-hosted OpenVINO for continuous, automated traffic telemetry. Never use external cloud VLMs for high-frequency scheduled loops.
  2. **Targeted Multimodal Deployment**: Position VLMs strictly where their reasoning capabilities are needed and request volumes are bounded: on-demand user modal clicks and low-frequency rain-triggered flood audits.
  3. **Resolution Scaling Over Cloud Reversion**: Rather than offloading to cloud APIs to fix small-object recall, utilize host compute headroom on `inference worker` to scale model input resolution ($320 \times 320 \rightarrow 416 \times 416$), capturing distant motorcycles while maintaining $0 cost.
