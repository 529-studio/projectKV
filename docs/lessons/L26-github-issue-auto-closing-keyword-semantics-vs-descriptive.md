## Lesson 26: GitHub Issue Auto-Closing Keyword Semantics vs. Descriptive Prose

- **Incident & Tracking:** Issue #209, Pull Request #216 (`feat(flood): decoupled hybrid multimodal flood pipeline`).
- **Symptom:**
  PR #216 completed and verified all deliverables for Issue #209 and was approved and merged into `dev`. However, Issue #209 remained `OPEN` in the GitHub repository issue tracker, requiring manual closure.
- **Root Cause:**
  The PR description used informal explanatory language:
  ```markdown
  ## Summary & Context
  Addresses **Issue #209: [go-server + web] decoupled hybrid flood pipeline (Sprint 5 Task S5-T3)**.
  ```
  GitHub's issue linking and auto-closing automation exclusively recognizes strict action keywords (`close`, `closes`, `closed`, `fix`, `fixes`, `fixed`, `resolve`, `resolves`, `resolved` immediately followed by `#<issue-number>`). Descriptive or decorative phrasing such as `Addresses Issue #209`, `Addresses #209`, or `Implement #209` is parsed as arbitrary markdown prose and fails to create the native GitHub event hook link.
- **Mandatory Takeaway:**
  1. **Strict Format at PR Top**: Every PR resolving an issue MUST begin directly with `Close #<issue-number>` (e.g. `Close #209`). Never use `Addresses Issue #...` or `Implement #...`.
  2. **Codified PR Template**: Maintain a permanent `.github/pull_request_template.md` that embeds this format by default.
  3. **Non-Negotiable Rule**: Codified as Rule 9 in `AGENTS.md` and `docs/project_status.md` Section 5.
