## Lesson 17: Instantaneous CCTV Camera Viewport Physical Limits vs. Hourly Link Capacity (PCU Calibration)

- **Incident & Tracking:** Issue #196, ADR-005 Finding C / row T5 (`feat/196-calibrate-pcu-weights`).
- **Symptom:**
  During the Sprint 4 T4 benchmark, 94.4% of actual `HEAVY` frames and 94.5% of `CONGESTED` frames were erroneously classified as `CLEAR` by the PCU Saturation Index calculator.
- **Root Cause:**
  1. **Conflating Instantaneous Optical Viewport with Hourly Link Capacity**: The initial `CapacityFloor = 25.0` PCU was an uncalibrated estimate based on hourly road design throughput (1,500–2,000 PCU/hour). However, an intersection CCTV camera captures an instantaneous still snapshot of a 20–30 meter road segment. A typical 3-lane approach physically saturates bumper-to-bumper at 12–15 PCU (3 lanes x 4–5 queue vehicles). Requiring 25 PCU for cold-start capacity forced `CONGESTED` ($SI \ge 0.90$) to demand $>22.5$ PCU—an impossible vehicle volume in a 25-meter frame.
  2. **Western Single-File vs. Vietnamese Mixed Motorcycle Flow**: Western traffic models assume single-file lane alignment ($PCU \approx 0.20$). In HCMC, motorcycles ride 3–4 abreast per lane, filtering through gaps and generating lateral clearance buffers. Under Vietnamese transport standards (**TCVN 4054:2005** Table 4 and **TCVN 13592:2022** §6.2), mixed motorcycle streams require $PCU = 0.30$, and urban buses require $PCU = 2.50$ due to curbside dwelling friction.
- **Mandatory Takeaway:**
  1. **Optical Viewport Physical Geometry**: Always calibrate visual saturation floors based on physical camera field of view geometry ($N_{\text{lanes}} \times \text{FOV} / \text{Headway}$) rather than hourly highway link capacity.
  2. **Regional Standards Grounding**: Align vehicle coefficients with local engineering standards (TCVN 4054:2005 / TCVN 13592:2022) to account for regional modal split and driver behavior.
  3. **Empirical Parameterization**: Calibrated constants (`CapacityFloor = 15.0`, `Motorcycle = 0.30`, `Bus = 2.50`, `Moderate = 0.30`, `Heavy = 0.60`, `Congested = 0.80`) documented in `docs/pcu-calibration-report.md` ensure realistic saturation detection while runtime environment variables (`PCU_WEIGHT_*`, `PCU_CAPACITY_FLOOR`, `PCU_THRESHOLD_*`) provide operator override flexibility.
