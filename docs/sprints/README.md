# Sprint Guides

Per-sprint execution guides: the issue map, the wave schedule, and the agent
prompts used to drive each ticket.

## What belongs here

A guide lives here only while its sprint is open. It is an operational artifact,
not permanent documentation: it restates issue metadata that GitHub already owns,
and its prompts are written for the agent CLI in use at the time. Keeping it in a
permanent directory made readers pay for a document with a shelf life.

| Sprint | Guide | State |
|---|---|---|
| 9 | [`sprint9_execution_guide.md`](sprint9_execution_guide.md) | Closed — retrospective in [`docs/reports/sprint9_report.md`](../reports/sprint9_report.md) |
| 10 | [`sprint10_issue_commands.md`](sprint10_issue_commands.md) | Closed — #372–#379 closed; retrospective in [`docs/reports/sprint10_report.md`](../reports/sprint10_report.md) |
| 10 | [`s10_t1_forecast_contract_spec.md`](s10_t1_forecast_contract_spec.md) | Contract spec for #372 (closed) |
| 10 | [`s10_t1_offline_verification.md`](s10_t1_offline_verification.md) | #372 evidence split (closed) |
| 11 | [`sprint11_issue_commands.md`](sprint11_issue_commands.md) | Closed — retrospective in [`docs/reports/sprint11_report.md`](../reports/sprint11_report.md) |
| 12 | [`s12_t1_issue.md`](s12_t1_issue.md) | Implemented & Verified — Close #413, PR in review |
| 12 | [`s12_t2_spec.md`](s12_t2_spec.md) | Implemented & Verified — Close #400 |
| 12 | [`s12_t3_spec.md`](s12_t3_spec.md) | Implemented; Rule-10 evidence **blocked** on S12-T5 trained segmentation head — [evidence](../reports/s12_t3_manual_evidence.md) |

## Lifecycle

1. **Open** — the guide is written once the sprint plan (the ADR) is approved, and
   updated as tickets land.
2. **Closed** — when every issue in the sprint is closed, the guide is either
   deleted or reduced to a short retrospective under `docs/reports/`. Nothing is
   left here to be read by accident months later.
3. **Never archived wholesale** — the durable record of a sprint is the ADR that
   planned it, the issues that executed it, and the lessons that came out of it.
   The guide is scaffolding, not the building.

Sprint 8's guide was never merged to `dev` and is not recovered here: the sprint is
closed, and this policy deletes closed-sprint scaffolding rather than archiving it.
Its decision record ([ADR-011](../ADR-011-sprint8-audit-and-sprint9-plan.md)) and its
completion report ([`docs/reports/sprint8_report.md`](../reports/sprint8_report.md))
carry everything durable from that sprint.
