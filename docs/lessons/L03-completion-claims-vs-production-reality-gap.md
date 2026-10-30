## Lesson 3: Completion Claims vs. Production Reality Gap

- **Incident & Tracking:** ADR-004 §8 (Sprint 2 A4 carryover omission), ADR-005 (Sprint 3 production audit).
- **Symptom:**
  Sprint completion reports declared milestones as "100% completed" and "production-grade," yet live staging inspections showed pending deployment tasks (e.g., physical model weights had not yet been ingested into the `warded-model-weights` volume on `edge host`, and the D6 evaluation accuracy gate had never been run against real labeled camera frames).
- **Root Cause:**
  Equating local development completion, test passes, or PR merges with live production deployment and verified operational readiness.
- **Mandatory Takeaway:**
  1. **Definition of Done Hardening (ADR-005 T8)**: Any issue claiming that a feature delivers real (non-mock) data to users must include a verified live production check (an actual API response or screenshot from the real deployment host, not just a passing unit test) before it can be closed.
  2. **Truthful Reporting**: Never describe unverified or undeployed code as production-ready in project documentation.
