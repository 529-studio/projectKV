## Lesson 33: Defense-in-Depth Secret Sanitization & Hysteresis Filtering for Telematics Jitter Mitigation

- **Incident & Tracking:** S6 Follow-up Refinement (`fix/secret-sanitizer-traffic-hysteresis`).
- **Symptom:**
  1. Commuter UI exposed to raw backend error dumps and potential credential leakage if upstream timeouts occur.
  2. Traffic state chattering / ping-pong jitter: clicking on a yellow dot (`[ĐÔNG XE]`) at a signalized intersection (e.g. Cộng Hòa - Tân Kỳ Tân Quý), and observing it flip back to `[BÌNH THƯỜNG]` after ~10–30s as vehicles disperse after a green light.
- **Root Cause:**
  1. **Single-Point Sanitization Vulnerability**: Relying solely on individual handler error strings allows new or uninspected endpoints to accidentally leak raw error traces or transport URLs.
  2. **Boundary Sensitivity in Single-Frame Telematics**: Traffic light cycles cause natural vehicle queue fluctuations around the $0.30$ Saturation Index cutoff ($7.5$ PCU on a 25.0 capacity corridor). Evaluating isolated static snapshots without hysteresis causes rapid alternating states (`MODERATE` $\leftrightarrow$ `CLEAR`) every 30-second cycle.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Global Fail-Safe Response Sanitization**: Implemented `middleware.SecretSanitizer()` wrapping Gin's response writer to intercept outgoing HTTP responses, scrubbing Google API keys (`AIza...`), AWS/R2 Access Key IDs (`AKIA...`), and query parameter tokens (`?key=...`) into `[REDACTED_KEY]` before transmission. Sanitized all 5xx responses across `weather`, `archiver`, and `flood` handlers to emit generic status strings.
  2. **Schmitt Trigger Hysteresis in PCU Saturation Index (`HysteresisDelta = 0.05`)**: Enhanced `cvclient.PCUCalculator` with stateful hysteresis deadband:
     - Rising transitions detect congestion immediately at nominal thresholds: `CLEAR` $\rightarrow$ `MODERATE` at $SI \ge 0.30$, `HEAVY` at $SI \ge 0.60$, `CONGESTED` at $SI \ge 0.80$.
     - Falling transitions require traffic to definitively clear below threshold minus deadband: `MODERATE` $\rightarrow$ `CLEAR` requires $SI < 0.25$, `HEAVY` $\rightarrow$ `MODERATE` requires $SI < 0.55$, `CONGESTED` $\rightarrow$ `HEAVY` requires $SI < 0.75$.
     - Completely eliminates high-frequency chattering while preserving rapid alert onset.
