## Lesson 5: Zero-Emoji Standard & Telemetry Stream Corruption

- **Incident & Tracking:** PR #152 (`fix(web): replace hourglass emoji with svg and enforce zero-emoji rule`).
- **Symptom:**
  Unicode emojis (such as hourglass, pin, warning, and rain characters) were originally used in loading states, map chips, and backend logging.
- **Root Cause:**
  Emojis rendered inconsistently across operating systems (iOS vs Android vs Windows vs macOS), violated the monochromatic high-contrast design philosophy, caused multi-byte UTF-8 encoding corruption in headless environments, and broke regex parsing in CI/CD telemetry aggregators (e.g. Loki, Datadog).
- **Mandatory Takeaway:**
  1. **Strictly Zero Emoji**: All UI controls and indicators MUST use monochromatic, scalable vector SVGs (`fill="none" stroke="currentColor"`).
  2. **Structured Logging**: Backend logs MUST strictly use standardized uppercase prefix tags (`[INIT]`, `[INFO]`, `[WARN]`, `[ERROR]`, `[FATAL]`) and key-value attributes.
