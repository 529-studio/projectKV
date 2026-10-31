Close #<issue-number>

<!--
MANDATORY ISSUE LINKING (AGENTS.md Rule 9, amended by ADR-011 D6):
1. GitHub Issue: Use "Close #<issue-number>" format only (e.g., Close #297).
   Do NOT use informal phrasing like "Addresses Issue #297" or "Implement #297".
2. Sprint/task progress is tracked on the org GitHub Projects v2 board "ProjectKV"
   (https://github.com/orgs/529-studio/projects/2). The legacy 529 Studio Hub (Plane)
   Kanban format "closes [KV-<card-id>]" is RETIRED (Hub decommissioned 10/2026)
   and must NOT be used in new PRs.
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
- [ ] Updated affected `docs/*`, `README.md` and `README.vi.md` if applicable (Rule 6)
- [ ] Full automated test suite passes (`make test-go`, `npm test`)
