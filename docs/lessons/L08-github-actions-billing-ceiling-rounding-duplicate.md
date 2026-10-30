## Lesson 8: GitHub Actions Billing Ceiling Rounding & Duplicate Verification Jobs

- **Incident & Tracking:** Issue #173 (GitHub Actions compute audit, 2026-10-01 to 2026-10-06).
- **Symptom:**
  Within the first 6 days of October 2026 the `529-studio` organization consumed 971 of its 2,000 monthly GitHub Free minutes (48.55%). Actual runner execution time across 563 active jobs was only 556m 50s; the remaining 414 minutes (74.5%) were pure billing overhead.
- **Root Cause:**
  1. **Per-job 1-minute ceiling rounding**: GitHub bills every job rounded up to the nearest whole minute. Micro-jobs (`Auto Request Reviewers`, `Branch Protection`) ran 5–10s each but were billed a full minute, turning ~8 minutes of real work into 87 billable minutes.
  2. **Duplicate component verification**: `go-ci.yml` and `ci.yml`'s `verify-backend` job both triggered on `pull_request` for the same `services/go-server/**` paths and both executed `go build`/`go vet`/`go test`, doubling backend verification minutes.
  3. **Redundant push re-verification**: `ci.yml` also carried a `push` trigger, re-running the exact verification already performed on the (mandatory, branch-guard-enforced) pull request after every merge into `dev`/`main`.
  4. **No concurrency cancellation**: rapid push iterations spawned parallel pipeline runs on superseded commits (245 runs over 2026-10-01 to 2026-10-04).
- **Mandatory Takeaway:**
  1. **Steps are free, jobs are not**: whenever logic can be folded into an already-running job as a step, do that instead of creating a new job — each additional job adds a 1-minute minimum.
  2. **One component, one verification workflow/event**: never define two workflows that verify the same component on the same trigger; consolidate to a single job to avoid paying twice.
  3. **Verify on `pull_request`, build/deploy on `push`**: since direct pushes to protected branches are forbidden, post-merge verification re-runs are pure waste.
  4. **Always set `concurrency: { group, cancel-in-progress: true }`** on PR/push workflows so superseded runs are cancelled instead of consuming quota.
  5. **Free-tier private repos have no branch protection**: `dev`/`main` report `protected: false` and the branch-protection API returns 403, so custom enforcement (`branch-guard.yml`) is required and an aggregation check like `ci-gate` has no gating power.
