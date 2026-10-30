## Lesson 34: Architectural & Psychological Priority: Sound Solutions Precede Task Estimation to Prevent Time-Anchoring

- **Incident & Tracking:** Core Governance Update (`AGENTS.md`, `.github/pull_request_template.md`, `docs/sprint6_execution_guide.md`, `docs/project_status.md`).
- **Symptom:**
  When engineering rules enforce task estimation prior to mandating sound, root-cause solutions, developers and AI agents experience subconscious cognitive bias ("time anchoring"). Under pressure to provide brief estimates or deliver against aggressive initial timelines, implementers are tempted to adopt superficial band-aids, temporary hacks, or parameter tweaking (e.g. silently nudging thresholds instead of fixing perception pipelines) rather than doing the hard engineering work required to resolve root causes.
- **Root Cause:**
  Structural flaw in guideline ordering: prioritizing "how fast" (Estimation Mandate) before "how right" (Sound Solutions). If time estimation is anchored first, subsequent architectural choices become bounded by that premature estimate.
- **Mandatory Takeaway & Engineering Resolution:**
  1. **Strict Priority Inversion (Rule 2 Precedes Rule 3)**:
     - **Rule 2**: *Sound Solutions & Root-Cause Verification (Never Fear Effort)*: Mandates that developers and AI agents must never shy away from implementation effort or labor, never choose sloppy band-aids out of convenience or fear of complexity, and always resolve problems at root cause with thorough end-to-end verification.
     - **Rule 3**: *Issue Estimation Mandate*: Estimation must follow after the sound architecture is designed, honestly reflecting the full effort required for root-cause resolution rather than justifying shortcuts.
  2. **Backward Compatibility Preservation**:
     Rules 4 through 9 retain their exact historical numbering across all repository ADRs, PR templates, and operational documentation.
