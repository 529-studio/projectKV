## Lesson 58: Parity Verification Illusion & Single-Source Alert Generation

- **Incident & Tracking:** Sprint 9 Wave 3 / Issue #301 / PR #327 (`feat/s9-t5-notify-preview`).
- **Symptom:**
  1. The pre-flight staging verification script (`scripts/verify_staging_telegram.sh`) claimed "byte-level format parity" with production Go alerts by hand-crafting the HTML payload using `printf` in Bash.
  2. The script hardcoded administrative district `Bình Thạnh` from the static chokepoints fixture, whereas production `dispatchFloodAlert` in `main.go` assigns `cam.Zone` (`Khu vực Nội thành TP.HCM`). The verification check claimed 100% parity against a format that differed from runtime.
- **Root Cause:**
  1. **Dual-Implementation Fallacy:** Attempting to verify message contracts by re-implementing formatting logic in a consumer or verification script (Bash `printf`) creates an illusion of verification. Any future change to the Go formatter silences drift or causes false positives.
  2. **Coupled Business Logic in `main.go`:** Camera catalog resolution precedence (`DisplayName` $\rightarrow$ `Title` $\rightarrow$ `CameraID`) and district/ward mapping were tightly inlined inside an anonymous closure in `main.go`, preventing external CLI tooling or test scripts from calling the exact production mapping logic.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Leaf Modulith Pure Helper (`notify.BuildFloodAlert`)**: Extracted alert building into an exported pure function in `internal/notify/alert_builder.go`. Because `notify` is a strict leaf package, the helper receives already-resolved primitives without introducing package dependency tangles.
  2. **Single-Source CLI Generator (`cmd/notify-preview`)**: Introduced `services/go-server/cmd/notify-preview`, which loads the camera catalog and chokepoints fixture to run the exact same `BuildFloodAlert` and `FormatTelegramMessage` logic used in production.
  3. **Script Parity via Direct Invocation**: `scripts/verify_staging_telegram.sh` Check 3 now invokes `go run -C services/go-server ./cmd/notify-preview` directly. Hand-crafted bash message templates were removed entirely.
