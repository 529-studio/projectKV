# Implementation Plan: Automated Camera Synchronization & Dead Feed Health Culling

- [ ] 1. Create `tools/eval/cull_dead_cameras.py`:
  - [ ] Implement camera catalog loader with coordinate and road metadata verification.
  - [ ] Implement culling logic with configurable blackout threshold (`--threshold-days`, default: 14) and arterial corridor coverage floors (`--min-corridor-coverage`, default: 0.50).
  - [ ] Support `--catalog`, `--history`, `--threshold-days`, `--min-corridor-coverage`, `--dry-run`, and `--output-report`.
  - [ ] Provide clear CLI terminal reporting (table of culled cameras, preserved corridor cameras, and summary metrics).
- [ ] 2. Create `tools/eval/test_cull_dead_cameras.py`:
  - [ ] Unit tests for 14-day persistent failure detection.
  - [ ] Unit tests for arterial corridor coverage protection (ensuring cameras are preserved if culling drops active cameras below the 50% floor).
  - [ ] Unit tests for dry-run non-mutation.
  - [ ] Unit tests for JSON report output structure.
- [ ] 3. Update `Makefile`:
  - [ ] Add `cull-dead-cameras` target: `python3 tools/eval/cull_dead_cameras.py --dry-run`.
- [ ] 4. Create Runbook `docs/runbooks/camera_catalog_maintenance.md`:
  - [ ] Document audit process, culling safeguards, corridor floor rules, and recovery procedures.
- [ ] 5. Run full verification suite (`python3 -m unittest tools/eval/test_cull_dead_cameras.py`, `make cull-dead-cameras`, `make test-go`, `make docs-lint`, `make evidence-lint`, `make test-model`).
- [ ] 6. Document lesson in `docs/lessons/531-daily-camera-sync-and-culling.md`.
- [ ] 7. Commit, push branch `feat/531-dead-camera-culling`, create PR targeting `dev`, watch CI until 100% green.
- [ ] 8. Merge PR and close Sprint 18 using `sprint-close` skill.
