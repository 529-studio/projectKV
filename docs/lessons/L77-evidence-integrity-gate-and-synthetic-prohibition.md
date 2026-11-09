# Lesson 77: Evidence Integrity CI Gate, Anti-Spoofing, and Synthetic Prohibition

## Navigation Aid (Table of Contents)
- [Context](#context)
- [Problem & Root Cause](#problem--root-cause)
- [Solution](#solution)
- [Guardrails & Invariants](#guardrails--invariants)
- [Evidence & Verification](#evidence--verification)

---

## Context
During the Sprint 10 independent reality-check audit (coordinated with an external OpenCode CLI auditor), a profound chasm was discovered between green CI test runs and the empirical truth of the system:
1. The forecast model ablation claimed 19.5%–25.1% accuracy improvement when adding precipitation, but the backtest ran against a synthetic sine-wave fixture (`synthetic_congestion_v2_5m.csv`) where `0.03 * precip` was injected directly into the jam target.
2. The 72-hour flywheel soak log (`harvester_observation.json`) was committed on 2026-11-06, yet contained timestamps for 2026-11-07 through 2026-11-10.
3. The committed segmentation model (`models/yolov8n_seg_openvino_416/yolov8n_seg.bin`) was a 64 KB stub of repeating null bytes, passing CI because the retraining gate read from hardcoded default metrics.
4. Co-tenancy benchmarks declared `measurement_source: "reference-profile"` while the test assertions treated that string as a pass condition.

In Sprint 11 Wave 0 (Issue #395, ADR-013 §D1), we built an automated, fail-closed CI gate (`tools/ci/check_evidence_integrity.py`) to systematically prevent synthetic or spoofed evidence from clearing release gates.

---

## Problem & Root Cause
1. **Verifiers that generated what they verified**: `verify_flywheel_soak.py` defaulted to running a simulated generator and then asserting on the generator's own output, creating a self-referential closed loop.
2. **Missing temporal invariants**: Release gates never asserted that observation timestamps must predate or equal the execution instant, or that `generated_at >= window.end`.
3. **Absence of binary weight inspection**: CI checked that model files existed, but never inspected file size, entropy, byte distribution, or compression ratios. A 64 KB repeating byte stub passed cleanly.
4. **Docs and models excluded from CI**: `.github/workflows/ci.yml` had `paths-ignore: ['docs/**', '**.md']`, meaning any PR modifying solely reports or proof artifacts bypassed all CI validation.
5. **No defense against static HTML mocks**: Proof screenshots could be forged by rendering static HTML pages rather than running containers.

---

## Solution
1. **Standalone Production Gate (`tools/ci/check_evidence_integrity.py`)**:
   - **Gate 1 (Synthetic Prohibition)**: Rejects `source`, `fit_source`, or `measurement_source` equal to `"synthetic"`, `"simulated"`, `"mock"`, `"dummy"`, `"reference-profile"`, or `"placeholder"`. Enforces `fit_source == "live_history"` for forecast and training release artifacts.
   - **Gate 2 (Temporal Invariants)**: Asserts `generated_at >= window.end`, `generated_at <= now + 300s`, `window.end <= now + 300s`, and rejects naive local timestamps lacking explicit UTC offsets.
   - **Gate 3 (Model Weight Integrity)**: Asserts binary weights > 1 MB, Shannon entropy $H \ge 3.0$ bits/byte, $\ge 32$ unique bytes, gzip compressibility ratio $\ge 0.60$, rejects periodic pattern repetitions $\le 4096$ bytes, rejects zero-padded tails, and checks companion IR XML layer counts.
   - **Gate 4 (Empirical Proof Integrity)**: Asserts magic bytes (JPEG/PNG), resolution $\ge 320 \times 240$, and detects degenerate solid-color placeholder screenshots.
2. **Ratchet Engine**: Modeled after `docs_lint.py`, grandfathering historical Sprint 10 violations in `.evidence-integrity-baseline.txt`. New violations fail CI immediately; stale baseline lines trigger CI failure.
3. **Dedicated GitHub Workflow (`.github/workflows/evidence-integrity.yml`)**: Runs on all pull requests and pushes without `paths-ignore`, ensuring report or proof changes are scrutinized.
4. **Definition of Done Hardening (`AGENTS.md` Rule 10)**: Mandates that all release evidence artifacts satisfy the integrity gate.

---

## Guardrails & Invariants
- **Never trust self-declared strings alone**: Provenance fields must be verified against file structure and mathematical properties (entropy, duration spans, byte counts).
- **No release gate may accept synthetic fixtures**: `fit_source` must strictly be `live_history`.
- **A verifier must never generate its own test data**: Generators belong in offline development tools; verifiers must be strictly read-only.
- **Model stubs are prohibited**: Any neural network artifact under `models/` must be a genuine binary weight file with $H \ge 3.0$ and $> 1\text{ MB}$.

---

## Evidence & Verification
- Unit test suite: `python3 -m unittest discover -s tools/ci -p "*_test.py"` runs 43 unit tests covering all gates and adversarial vectors in $< 0.5$s.
- Repository linter: `make evidence-lint` scans 32 tracked and local evidence artifacts.
- Adversarial review: OpenCode CLI auditor designed 14 adversarial vectors (`scratch/evidence_gate_prototype.py`); all 14 vectors fail closed while authentic control passes.
