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
| 9 | [`sprint9_execution_guide.md`](sprint9_execution_guide.md) | In flight — #298, #300, #302, #309 open; #297, #299, #301 closed |

## Lifecycle

1. **Open** — the guide is written once the sprint plan (the ADR) is approved, and
   updated as tickets land.
2. **Closed** — when every issue in the sprint is closed, the guide is either
   deleted or reduced to a short retrospective under `docs/reports/`. Nothing is
   left here to be read by accident months later.
3. **Never archived wholesale** — the durable record of a sprint is the ADR that
   planned it, the issues that executed it, and the lessons that came out of it.
   The guide is scaffolding, not the building.

## Known gaps

- **Not in English yet.** Rule 1 requires English documentation. This guide is
  still Vietnamese, which costs roughly twice the tokens of the same content in
  English. Translation is tracked in #341; until it lands the violation is
  recorded here rather than left unnoticed.
- **Two dead links.** The guide links `../ADR-011-sprint8-audit-and-sprint9-plan.md`
  and `../sprint8_report.md`. Neither file exists on `dev`: they were committed
  together with this guide on the local branch `docs/sprint8-report-and-sprint9-plan`
  (`a2a248d`), and the sync that reached `dev` (`dcd2555`) took the guide alone.
  Tracked in #342.
