Close #<issue-number> closes [KV-<card-id>]

<!--
MANDATORY ISSUE & KANBAN LINKING (AGENTS.md Rule 9):
1. GitHub Issue: Use "Close #<issue-number>" format only (e.g., Close #248).
   Do NOT use informal phrasing like "Addresses Issue #248" or "Implement #248".
2. Kanban Board (529 Studio Hub / Plane): Include "closes [KV-<card-id>]" (e.g., closes [KV-2]).
   The n8n Gateway automation exclusively parses "closes [KV-X]" to automatically link the PR
   and transition the card on the Kanban board (https://hub.529studio.site).
-->

## Summary & Rationale
<!-- Brief 2-3 sentence explanation of what this PR does and why -->

## Key Changes
<!-- Concise bullet points of code, schema, config, or documentation changes -->

## Verification & Operational Proof (Rule 4)
<!-- Paste real operational simulation logs, command output, and test pass evidence -->

## Checklist
- [ ] Sound solutions & root-cause verification over shortcuts (Rule 2)
- [ ] Explicit time estimate provided before work started (Rule 3)
- [ ] End-to-end bug reproduction/simulation verified before fix (Rule 4)
- [ ] Zero hardcoded secrets / credentials in code and configs (Rule 8)
- [ ] Monochromatic, zero-emoji UI and structured backend logs (Rule 1)
- [ ] Updated docs/project_status.md, docs/lesson_learn.md, affected docs/*, and README.md if applicable (Rule 6)
- [ ] Full automated test suite passes (`make test-go`, `npm test`)
