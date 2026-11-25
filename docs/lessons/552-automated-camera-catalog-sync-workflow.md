# Lesson 552: Automated Daily Camera Catalog Synchronization Workflow

## Navigation Aid & Quick Reference
- **Domain**: GitHub Actions automation, camera catalog synchronization, corridor-floor culling gate, CodeQL Actions security.
- **Relevant Issues**: Issue #552 (S19-T4), Issue #531 (S18-T8 Dead Camera Culling Tool).
- **Key Files**:
  - `.github/workflows/camera-catalog-sync.yml` (Scheduled 02:00 UTC workflow running `cull_dead_cameras.py` and raising PRs on drift)
  - `tools/eval/cull_dead_cameras.py` (Feed health auditor enforcing 14-day threshold and 50% corridor coverage floor)
  - `tools/eval/test_cull_dead_cameras.py` (Unit test suite executed as pre-flight safeguard in CI)
- **Core Invariants**:
  - **Pre-Flight Safeguard Gate**: `test_cull_dead_cameras.py` must pass before any catalog mutation executes in CI.
  - **Zero Direct Push**: Following AGENTS.md Rule 5, the scheduled action always branches and opens a PR targeting `dev` without auto-merging.
  - **Least-Privilege Actions & Injection Safety**: Top-level permissions default to `contents: read`; write permissions are scoped strictly to the job; all GitHub context variables (`github.run_number`, `inputs.dry_run`) pass via `env:` bindings rather than inline shell interpolation.

---

## 1. Context & Automation Need

Issue #531 introduced `tools/eval/cull_dead_cameras.py` with arterial corridor protection (`>= 50%`), while `.github/workflows/camera-health-audit.yml` probed upstream liveness. However, the 14-day culling pass was still invoked manually via `make cull-dead-cameras`. Automating this in `.github/workflows/camera-catalog-sync.yml` ensures stale cameras are safely retired daily without manual intervention while guaranteeing corridor visibility.

---

## 2. Verification & Empirical Proof

1. **YAML & Syntax Validation**:
   - Verified clean YAML parsing via Python `yaml.safe_load`.
2. **Local Dry-Run Execution**:
   - Executed `python3 -m unittest tools/eval/test_cull_dead_cameras.py` (5/5 passing).
   - Executed `python3 tools/eval/cull_dead_cameras.py --catalog cameras_enriched.json --threshold-days 14 --min-corridor-coverage 0.50 --dry-run --output-report /tmp/camera_cull_audit.json`.
3. **CodeQL Actions Check**:
   - Verified zero script-injection patterns and minimal job-scoped permissions.
