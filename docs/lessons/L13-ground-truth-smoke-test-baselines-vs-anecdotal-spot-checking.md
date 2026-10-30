## Lesson 13: Ground-Truth Smoke-Test Baselines vs. Anecdotal Spot-Checking

- **Incident & Tracking:** Issue #187, ADR-005 Finding B (`feat/187-known-state-smoke-set`).
- **Symptom:**
  Historically in ProjectKV, model accuracy verification on staging relied on opportunistic spot-checks of web UI maps or ad-hoc screenshots. Without fixed reference cameras or synchronized observation windows, regressions (such as completely clear expressways flagged as `CONGESTED` due to sensitive PCU floor boundaries, or chronic bottlenecks flagged as `CLEAR`) repeatedly slipped into staging unnoticed until manual user audits.
- **Root Cause:**
  Lack of a standardized, deterministic test harness. Traffic density varies dynamically by time and weather, making unanchored observations subjective and non-reproducible.
- **Mandatory Takeaway:**
  1. **Curate Deterministic Operational Extremes**: Isolate cameras with mathematically extreme, predictable behaviors (e.g., 5 wide 12-lane express sections that are reliably `CLEAR` during midday off-peak with $SI < 0.35$, and 5 chronic convergence bottlenecks that are reliably `CONGESTED` during peak rush hour with $SI \ge 0.90$).
  2. **Codify Ground-Truth Baselines as Versioned Fixtures**: Never keep baseline lists in developer notes or ad-hoc chats. Store them in versioned JSON fixtures (`tools/eval/fixtures/smoke_test_cameras.json`) with documented validation windows and road geometry rationale.
  3. **Automate Discrepancy Gating**: Provide automated verification scripts (`tools/eval/verify_smoke_set.py`) that can test mock verdicts, stored predictions, or live backend APIs before declaring model changes ready.
