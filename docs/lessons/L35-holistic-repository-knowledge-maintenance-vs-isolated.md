## Lesson 35: Holistic Repository Knowledge Maintenance vs. Isolated Status Logging

- **Incident & Tracking:** Rule 6 Enhancement in `AGENTS.md` (Issue #239, PR #240).
- **Symptom:**
  Engineers and AI agents dutifully appended status bullets in `docs/project_status.md` and lessons in `docs/lesson_learn.md`, but left accompanying guides in `docs/` (e.g. execution guides, ADRs, schema notes) and the root `README.md` stale. As a result, onboarding developers or external contributors encountered outdated setup commands, omitted newly supported endpoints, or missed critical architectural constraints documented only in isolated status files.
- **Root Cause:**
  "Checklist tunnel vision": Treating Rule 6 as a superficial two-file checklist (`project_status.md` + `lesson_learn.md`) rather than an active commitment to holistic repo-wide documentation integrity.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Strict Expansion of Rule 6**: Codified that updating `project_status.md` and `lesson_learn.md` alone is explicitly insufficient.
  2. **Active Blast-Radius Documentation Audit**: Every task completion requires surveying the blast radius of changes across `docs/` and root `README.md` (both English and Vietnamese sections). If CLI commands, environment variables, API endpoints, or workflows changed, `README.md` and related `docs/` must be updated in lockstep in the very same PR.
