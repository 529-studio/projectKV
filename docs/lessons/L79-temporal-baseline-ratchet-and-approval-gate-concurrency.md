# Lesson 79: Temporal Baseline Ratchet Expiry & Manual Approval Gate Concurrency

## Navigation Aid & Quick Reference
- **Domain**: CI/CD Pipelines, Evidence Integrity Ratchet, GitHub Actions Workflow Concurrency, Environment Approval Gates, Artifact Lifecycle Invalidation
- **Relevant Issues / Runs**: Run 42756398440 (`dev` CI evidence gate failure), Run 40511857235 (deployment freeze), Run 42756398574 (starved queue), ADR-013 F2
- **Key Files**:
  - `tools/ci/check_evidence_integrity.py` (Prohibits clock-dependent baseline entries; validates `SUPERSEDED` artifact lifecycle)
  - `tools/ci/check_evidence_integrity_test.py` (Unit tests for temporal baseline rejection and artifact superseding)
  - `.evidence-integrity-baseline.txt` (Ratchet baseline file, purged of dynamic assertions and obsolete simulation entries)
  - `docs/proofs/sprint10/harvester_observation.json` (Migrated to `SUPERSEDED` status pointing to authentic S11-T2 soak proof)
  - `.github/workflows/docker-publish.yml` (Decoupled root workflow concurrency; scoped concurrency to `staging-deployment`)
  - `.github/workflows/ci.yml` (Added daily scheduled cron to verify temporal invariants proactively)
- **Core Invariants**:
  - **Static ratchet invariants**: Baseline ratchet files must only grandfather static, time-invariant structural defects. Any entry containing dynamic clock-dependent conditions (`is in the future`, `current_time`) is strictly prohibited.
  - **Explicit artifact invalidation**: Obsolete or prototype evidence artifacts superseded by authentic live runs must be declared via structured schema (`status: "SUPERSEDED"`, `superseded_by`, `invalidation_reason`), bypassing live temporal assertions without baseline debt.
  - **Scoped deployment concurrency**: Pipelines containing manual environment approval gates must never use top-level workflow concurrency with `cancel-in-progress: true`. Concurrency must be isolated strictly to the terminal deployment job with `cancel-in-progress: false`.

---

## 1. Context & The Incidents
On 2026-11-10, two independent CI/CD deadlocks halted repository operations on the `dev` branch:

1. **Evidence Integrity Gate Failure (Run 42756398440)**:
   - Line 23 of `.evidence-integrity-baseline.txt` grandfathered a Sprint 10 soak proof:
     `gate2_temporal  docs/proofs/sprint10/harvester_observation.json  window.end (2026-11-10T06:00:00+07:00) is in the future (> current_time + 300s)`
   - When the clock passed 06:00:00 ICT on 2026-11-10, the violation naturally stopped triggering during `check_evidence_integrity.py --verify-all`.
   - The ratchet algorithm (`stale_baseline = [v for v in relevant_baseline if v not in curr_set]`) detected the line as stale and failed CI, preventing any new PR or dev commit from merging.
   - Line 22 also grandfathered an impossible artifact assertion (`generated_at < window.end`). Sprint 11 ADR-013 F2 had documented that this prototype simulation was superseded by authentic soak in S11-T2 (`docs/proofs/sprint11/harvester_soak_status.json`), yet the obsolete file lingered in the baseline.

2. **Docker Publish & Staging Deployment Freeze (Run 40511857235 & 42756398574)**:
   - Run 40511857235 was waiting for manual approval to deploy to the `staging` environment.
   - `.github/workflows/docker-publish.yml` configured workflow-level concurrency:
     `concurrency: group: ${{ github.workflow }}-${{ github.ref }}, cancel-in-progress: true`
   - GitHub Actions cannot cancel runs that are paused waiting for an environment manual approval gate.
   - As a result, the entire workflow queue froze: subsequent run 42756398574 was starved in pending state for over 35 minutes, blocking all container builds and test publishing.

---

## 2. Root Cause Analysis

### A. Temporal Baseline Ratchet Expiry
A ratchet baseline file (such as `.evidence-integrity-baseline.txt` or `.docs-lint-baseline.txt`) functions on the assumption that recorded violations are **static defects** that remain until an engineer explicitly resolves them in code or documentation.

When a baseline entry contains a dynamic, clock-dependent assertion (`is in the future (> current_time + 300s)`):
- The violation cures itself purely due to the passage of wall-clock time, without any human code modification.
- Once the target timestamp enters the past, the gate stops generating the violation.
- The ratchet logic observes that a baseline entry is no longer emitted by the scanner and flags it as a "stale baseline entry", intentionally failing CI to force developers to keep baselines tight.
- Thus, grandfathering clock-dependent assertions creates a ticking time bomb that inevitably breaks CI at a predetermined future moment.
- Additionally, grandfathering invalid prototype artifacts indefinitely masks technical debt rather than providing an auditable lifecycle transition.

### B. Manual Approval Gate Concurrency Deadlock
GitHub Actions concurrency behaves differently depending on job status:
- When a new run arrives with `cancel-in-progress: true`, GitHub cancels active runs that are currently executing normal job steps.
- When an active run enters a job protected by an Environment with required reviewers, GitHub pauses execution waiting for manual approval. GitHub Actions **cannot automatically cancel a run paused at an approval gate**.
- When concurrency is declared at the top level of the workflow, the concurrency lock covers all jobs (lint, unit tests, image builds, package publishing, and deployment).
- The paused approval gate holds the workflow lock indefinitely until an operator approves or rejects it (or the environment timeout expires, which defaults to 30 days). All subsequent workflow runs are queued indefinitely and never start.

---

## 3. Architecture & Solution

### A. Ratchet Hardening & Dynamic Baseline Prohibition
1. **Dynamic Assertion Ban (`tools/ci/check_evidence_integrity.py`)**:
   - Added validation during baseline loading (`validate_baseline_entry` and `is_clock_dependent_violation`).
   - If any entry in `.evidence-integrity-baseline.txt` matches `"is in the future"` or `"current_time"`, the checker immediately raises a `ValueError` during initialization:
     `Dynamic clock-dependent baseline entry prohibited: ...`
   - Prohibits future time-dependent assertions from ever entering the ratchet baseline file.

2. **First-Class Artifact Invalidation Lifecycle (`SUPERSEDED`)**:
   - Instead of grandfathering superseded historical artifacts in the baseline, `tools/ci/check_evidence_integrity.py` introduces structured lifecycle metadata in evidence JSON files:
     ```json
     {
       "status": "SUPERSEDED",
       "superseded_by": "docs/proofs/sprint11/harvester_soak_status.json",
       "invalidation_reason": "Sprint 10 prototype simulation superseded by authentic S11-T2 unattended soak (ADR-013 F2 / Lesson 78)"
     }
     ```
   - Gate 2 validates that `status == "SUPERSEDED"` requires:
     - A non-empty `superseded_by` path pointing to an authentic target file that actually exists on disk.
     - A non-empty `invalidation_reason` string documenting provenance.
   - When valid, Gate 2 bypasses live temporal checks (`generated_at < window.end`), preserving historical artifacts for auditability while eliminating baseline entries.
   - Updated `docs/proofs/sprint10/harvester_observation.json` and deleted line 22 from `.evidence-integrity-baseline.txt`.

3. **Scheduled Proactive CI (`.github/workflows/ci.yml`)**:
   - Added scheduled daily cron (`schedule: - cron: '0 1 * * *'`) and `workflow_dispatch`.
   - Ensures temporal boundaries and baseline health are verified continuously on `dev`, rather than only when a PR is opened.

### B. Decoupled Pipeline & Scoped Deployment Concurrency
1. **Remove Top-Level Workflow Concurrency**:
   - Removed `concurrency: group: ${{ github.workflow }}-${{ github.ref }}` from `.github/workflows/docker-publish.yml`.
   - Upstream jobs (`detect-changes`, `build-backend`, `build-frontend`, `build-model`) now execute freely per push, generating images and artifacts without starvation.

2. **Scope Concurrency Exclusively to Deployment Job**:
   - Attached concurrency strictly to the terminal `deploy` job with `cancel-in-progress: false`:
     ```yaml
     deploy:
       name: Deploy to Staging
       runs-on: ubuntu-latest
       environment: staging
       concurrency:
         group: staging-deployment
         cancel-in-progress: false
     ```
   - Deployment runs are strictly serialized, ensuring that only one staging deployment executes at a time.
   - Because upstream build jobs are not part of the `staging-deployment` group, new commits build and publish images immediately, even if a previous run is awaiting staging approval.

---

## 4. Key Takeaways & Prevention Rules

1. **Ratchet baselines must be time-invariant**: Never grandfather clock-dependent violations (e.g. `is in the future`, `now + delta`, TTL expirations). Ratchet systems assume defects require human intervention to fix; clock-dependent entries will silently become stale and detonate CI.
2. **Supersede artifacts explicitly in data, not in baseline suppressions**: When an earlier proof or report is invalidated by subsequent empirical runs, mark the artifact with `status: "SUPERSEDED"`, `superseded_by`, and `invalidation_reason`. Keep baselines reserved for genuine unresolved defects.
3. **Never apply `cancel-in-progress: true` workflow-wide when using environment gates**: If any job in a workflow waits for manual reviewer approval, top-level concurrency will cause catastrophic queue starvation. Concurrency must be scoped to the deployment job with `cancel-in-progress: false`.
4. **Decouple build-publish from environment deployment**: Image building and artifact compilation are idempotent operations that should complete per push. Only the environment state transition requires serialization.
5. **Continuous scheduled runs for temporal invariants**: Any CI system that verifies temporal windows or freshness contracts should run on a periodic schedule (e.g. daily cron) to catch boundary shifts before active development is blocked.

---

## 5. Verification & Evidence
- **Dynamic Baseline Rejection**: `tools/ci/check_evidence_integrity_test.py` (`TestBaselineClockDependencyCheck`) verifies that entries with `is in the future` raise `ValueError`.
- **Superseded Artifact Handling**: `TestSupersededArtifactHandling` verifies that valid `SUPERSEDED` artifacts bypass temporal checks, while missing fields or invalid targets fail closed.
- **Evidence Integrity Verification**: `make evidence-lint` and `python3 tools/ci/check_evidence_integrity.py --repo-root . --mode gate` pass cleanly across 33 artifacts.
- **Workflow Decoupling**: `.github/workflows/docker-publish.yml` deploys with job-level concurrency `staging-deployment`, eliminating approval gate deadlocks.
