## Lesson 48: Zero-VLM Self-Hosted Flood Segmentation, OpenVINO Model Benchmarking, and Dual-Layer Physical Triangulation

- **Incident & Tracking:** Issue #254, Kanban **[KV-7]**, Sprint 7 Task S7-T9 (`docs/s7-t9-adr-009-zero-vlm-flood`, PR targeting `dev`).
- **Symptom:**
  1. Real-time telemetry logs during active monsoon rainfall recorded continuous verification skips: `WARN flood: skipping chokepoint environmental verification due to rate/quota limits camera_id=...`.
  2. Cloud Vision Language Model (Google Gemini) quotas (300 Requests Per Day) collapsed within 6 minutes of monitoring 50 chokepoint cameras on a 60-second polling cycle.
  3. Specular reflections from streetlights on wet asphalt caused false-positive flood alerts under naive image-level thresholding.
- **Root Cause:**
  1. **Architectural Mismatch of Cloud VLMs for High-Frequency Telemetry**: Polling municipal camera networks during multi-hour rainstorms generates thousands of requests per hour, structurally exceeding cloud API quotas and incurring unpredictable latency spikes (1.5s - 4.5s) and financial overhead.
  2. **The Wet Asphalt Mirror Dilemma**: Rain-slicked pavement mirrors headlights and evening skies. Qualitative LLMs and color-based heuristics cannot reliably distinguish surface dampness from true standing water accumulation without physical interaction cues.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Empirical Model Benchmarking (ADR-009)**:
     - Benchmarked three candidate architectures on the `inference worker` host (AMD Ryzen 7 6800HS, 2 vCPU / 2 GB RAM slice) using Intel OpenVINO 2024.3 FP16:
       - **YOLOv8n-seg**: 24.6 ms neural latency, 38.5 ms total pipeline cycle, 118.5 MB RSS, 51.9% compute headroom under the 80 ms ceiling. Selected as primary production engine for its unified multitask forward pass (simultaneous vehicle bounding boxes and water/roadway prototype masks).
       - **BiSeNet v2**: 42.8 ms neural latency, 165 MB RSS. Superior curb detail via its dual-branch architecture, retained as secondary offline validation baseline.
       - **SegFormer-B0**: 61.2 ms neural latency, 190 MB RSS. Higher CPU latency variance on 2-thread AVX2 budgets due to self-attention matrix multiplication overhead.
  2. **Dual-Layer Physical Triangulation Engine**:
     - **Layer 1 (Roadway Surface Water Ratio)**: $R_{\text{water}} = \text{Area}(\mathcal{M}_{\text{water}} \cap \mathcal{M}_{\text{road}}) / \text{Area}(\mathcal{M}_{\text{road}})$. Values below 0.05 indicate dry/damp asphalt; values $\ge 0.15$ confirm significant accumulation and trigger Layer 2.
     - **Layer 2 (Motorcycle Wheel Waterline Occlusion)**: Leverages the natural depth gauge provided by Ho Chi Minh City's predominant motorcycle traffic (17-inch rims, ~60 cm outer wheel diameter). Measures relative vertical submersion ratio $\delta_{\text{sub}}$ of motorcycle tire bounding boxes against the local waterline. Submersion $\ge 18\%$ ($\ge 20\text{ cm}$ physical depth, immersing wheel hubs and approaching motorcycle exhaust pipes) verifies `FLOODED_DEEP`, triggering emergency alerts while filtering out specular wet pavement reflections.
  3. **Architectural Governance**: Formally codified the complete design specification in `docs/ADR-009-zero-vlm-flood-segmentation.md` with zero emojis and 100% English documentation per Rule 1.
