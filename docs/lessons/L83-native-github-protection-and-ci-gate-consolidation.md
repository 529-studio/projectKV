# Lesson 83: Native GitHub Protection & CI Gate Consolidation

## Navigation Aid & Quick Reference
- **Domain**: Public-repository CI, native GitHub security controls, branch protection,
  rulesets, and evidence integrity
- **Relevant Issues / Findings**: Issue #414; public-repository workflow consolidation
- **Key Files**:
  - `.github/workflows/evidence-integrity.yml` (unfiltered required evidence gate)
  - `.github/workflows/ci.yml` (evaluation and harvester tests without duplicate evidence work)
  - `scripts/check-public-safe.sh` (repository-specific public-material scan)
  - `docs/GO_BACKEND_ARCHITECTURE.md` and `docs/SYSTEM_DESIGN.md` (current PR-policy boundary)
  - `SECURITY.md` (public-material reporting and enforcement)
- **Core Invariants**:
  - Native secret scanning and push protection must both be verifiably enabled before Gitleaks
    is removed.
  - The public-material scan remains unfiltered and runs before the other evidence-gate checks.
  - Required-check migration preserves the existing GitHub Actions check identity and review
    policy.
  - Local hooks are conveniences; server-side branch protection and rulesets enforce PR policy.

---

## 1. Observed public-repository settings

Live API inspection before the migration established this baseline:

| Control | Observed state |
| --- | --- |
| Repository visibility | Public |
| Native secret scanning | `enabled` |
| Native secret-scanning push protection | `enabled` |
| `dev` required checks | `Scan for non-public material` and `Gitleaks Secret Detection`, both GitHub Actions app ID `15368` |
| `dev` up-to-date requirement | `strict: false` |
| `dev` reviews | One approving review; stale reviews dismissed |
| `dev` admin enforcement | Disabled before migration |
| `protect-main` ruleset | Active, ID `25944127`, targeting `refs/heads/main` |
| `main` rules | One approving review with stale-review dismissal, deletion protection, and non-fast-forward protection |
| `main` bypass | One `RepositoryRole` actor with `always` bypass before migration |

A workflow name in the repository did not prove that it was required, and a required-check API
response did not prove that native secret scanning was active. Both surfaces had to be inspected.

## 2. Status-check migration

The existing `Enforce evidence integrity & anti-spoofing` job already runs on every pull request
and on pushes to `main` and `dev` without path filters. It gains a named step running
`bash scripts/check-public-safe.sh` before `make test-ci` and `make evidence-lint`. The separate
public-safety workflow is therefore redundant, while the script remains a referenced repository
control rather than dead code.

The duplicate `make test-ci` and `make evidence-lint` invocations leave `verify-tools-dataset` in
`ci.yml`; its Pillow installation remains because the evaluation tooling still needs it. The
standalone Gitleaks workflow and configuration are removed only while both native secret controls
remain enabled. The branch-guard workflow is removed because native branch policy, not a
post-push workflow failure, is the enforcement mechanism.

After the consolidated context succeeds on the migration PR and every open `dev` PR has a
successful evidence-integrity result, native protection changes to:

- `dev` requires only `Enforce evidence integrity & anti-spoofing` from GitHub Actions app ID
  `15368`, with `strict: false` and the existing one-review/stale-dismissal policy unchanged.
- `dev` sets `enforce_admins.enabled` to `true`.
- `protect-main` remains active with its targeting, rules, and one-review policy unchanged, but
  its `bypass_actors` list becomes empty.
- Both settings and the native secret-scanning controls are read back after mutation. A material
  live-state mismatch blocks the migration rather than authorizing an overwrite of unrelated
  policy.

## 3. Resulting safety boundary

The local `.githooks/pre-push` hook remains a developer convenience. GitHub branch protection and
the active `protect-main` ruleset enforce the server-side PR policy; no workflow that runs after a
push is treated as a preventive access control.

On every PR and protected-branch push, the evidence gate checks repository-specific non-public
material, evidence-gate tests, and evidence integrity. Native secret scanning and push protection
provide the secret-detection layer. `dev` retains its approval requirement and gains admin
enforcement; `main` retains its active review/deletion/non-fast-forward rules without a bypass
actor.

Deleting the old workflows does not delete their historical incident or report references. Those
records describe what ran at that time; current-state documents describe the consolidated gate.

## 4. Takeaway

Inspect live repository security, branch protection, rulesets, check app IDs, and open-PR check
results before consolidation. Prove the replacement context, migrate required checks, and verify
the readback before allowing old workflow deletions to merge. Never replace a verified native
control with an assumed one, and never discard unrelated protection fields to simplify an API
update.
