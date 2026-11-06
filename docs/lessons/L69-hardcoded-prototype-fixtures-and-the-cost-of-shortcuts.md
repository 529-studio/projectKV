# L69: Hardcoded Prototype Fixtures and the Cost of Shortcuts

## Context & Problem

During the initial implementation of the multi-horizon traffic forecast service
(Sprint 10 Task 1 / Issue #372), candidate model parameters, evaluation metrics,
and cryptographic digests were hand-typed as static Go constants inside
`services/go-server/internal/forecast/model.go`. While this shortcut allowed
unit tests to pass quickly against static fixtures, it introduced subtle bugs,
data drift, and architectural debt:

- **Data Drift**: Horizon sample counts (338 for 20m, 334 for 30m) were
  erroneously hardcoded as 342, and the artifact SHA256 digest was mistakenly
  copied from the training CSV input rather than computed from the model JSON.
- **Decoupled Weights Flaw**: Global static coefficient maps decoupled inference
  weights from runtime metadata configuration. Overriding `ModelMetadata` never
  changed the regression weights used by `PredictMulti`.
- **Rework Tax**: Remedying the shortcut required rewriting model loading into a
  dedicated artifact pipeline, updating handlers, and auditing tests—costing far
  more engineering effort than building the sound architecture initially.

## Root Cause

An engineering shortcut was chosen to satisfy immediate task requirements and
pass tests rapidly. Taking shortcuts out of convenience or fear of implementation
effort ("sợ tốn sức") consistently produces code that fails production standards
and demands expensive subsequent rework.

## Solution

The forecast package now ingests model weights and provenance dynamically via
`services/go-server/internal/forecast/artifact.go` and embedded artifacts:

1. **Embedded & Dynamic Loading**: Default weights are embedded via Go's
   `//go:embed embedded/prototype_v1.json`, with runtime overrides supported
   via `FORECAST_MODEL_PATH` or functional options (`WithArtifact`, `WithModelPath`).
2. **Dynamic Cryptographic Verification**: Artifact digests are computed at load
   time via `crypto/sha256` directly over the raw JSON payload, eliminating
   manually transcribed checksum errors.
3. **Fail-Closed Security & Public Repository Hygiene**:
   - Because Project KV is a public repository, code and configuration are
     audited against automated scrapers and public vulnerability scans.
   - The loader enforces path validation, `.json` extension checks, and a strict
     10 MB maximum payload ceiling (`MaxArtifactSizeBytes`) to prevent memory
     exhaustion denial-of-service vectors.
   - Internal infrastructure names and sensitive hostnames are prohibited from
     tracked code and docs (`scripts/check-public-safe.sh`).

## Takeaways

- **Never fear implementation effort (Rule 2)**: The engineering cost of fixing
  a quick, sloppy shortcut is always significantly higher than doing it right
  the first time ("làm chậm mà chắc").
- **Contracts must be dynamic**: Never hardcode model coefficients, evaluation
  metrics, or cryptographic digests in executable source code. Decouple models
  into validated JSON artifacts with runtime integrity checks.
