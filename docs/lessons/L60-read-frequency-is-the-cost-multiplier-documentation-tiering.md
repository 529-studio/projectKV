# L60 — Read Frequency Is the Cost Multiplier: Tiering Documentation by How Often It Loads

## Context

By 2026-10-30 the repository's documentation cost an agent roughly 94k tokens before it wrote a line of code. The remediation that followed is recorded in `docs/reports/agent-context-bloat-audit-2026-10-30.md`: split the 143 KB lesson register into 58 files plus an index, reduce `project_status.md` (66 KB) to a 3.5 KB onboarding guide, split the 41 KB bilingual README, move sprint guides out of the permanent doc set. Measured result: the always-read set fell to 26,422 B, about 6.6k tokens.

Every one of those fixes followed the same pattern, and it took a while to name it.

## The mistake a single size ceiling makes

The obvious rule is "no document over N KB". It is wrong in both directions, and the numbers show it (measured 2026-11-02, before this rule added ~1.4 KB to `AGENTS.md`):

| File | Size | Loaded | Cost per day |
|---|--:|---|---|
| `AGENTS.md` | 6,330 B | every task | paid dozens of times |
| `README.md` | 24,254 B | when someone sets up or looks up a command | paid rarely |
| `docs/lessons/INDEX.md` | 15,524 B | when a lesson might be relevant | paid often |

A 25 KB ceiling passes all three. It would have blessed the 143 KB register for four more months, because the register was under 25 KB for most of its life — it only became a problem once it crossed a threshold nobody had defined, and by then it was 143 KB.

A 12 KB ceiling fails `README.md` and the index, both of which are correct as they are. Enforcing it would have meant deleting genuine content to satisfy a number, which is the kind of rule people route around.

The variable that actually predicts cost is not size. It is **size multiplied by how many times the file is loaded**. `AGENTS.md` at 6 KB costs more per month than a 24 KB README nobody opens. That is why the fix had to be two tiers rather than one ceiling:

- **Always-loaded set** (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `docs/ONBOARDING.md`, `docs/lessons/INDEX.md`): **32 KB in total, 16 KB for any single file**. These are paid on every turn, so a byte here is worth roughly an order of magnitude more than a byte in a report.
- **On-demand files** (`README.md`, ADRs, reports, runbooks, sprint guides): **25 KB each**, and read in ranges. A large file is fine if entering it is cheap.

## What already breaks it, and why the ceiling is a ratchet

Seven tracked markdown files were already over 25,600 B when the rule landed: the 60,837 B status scorecard, ADR-004 (36,665 B), three D6 evaluation reports (33,551 / 30,780 / 30,759 B), ADR-001 (32,122 B) and the audit report itself (27,355 B). None is over 100 KB.

Deleting or truncating them would destroy evidence that other documents cite, which is the mistake this audit exists to prevent. So they are exceptions: recorded in the lint configuration, forbidden to grow, and removable from that list only by actually shrinking them. That is what a ratchet means — the number is enforced on new work from day one, and the backlog is paid down deliberately rather than waved through with a big-bang cleanup nobody schedules.

The same logic sets the always-loaded numbers. A flat 12 KB per entry file was the first draft and it failed on measurement: `docs/lessons/INDEX.md` is 15,886 B, and the 3.6 KB it would have had to shed is exactly the guardrail column that makes on-demand reading possible. A ceiling that forces you to delete the mechanism the ceiling exists to protect is the wrong ceiling. Capping the set as a whole (32 KB) and letting the index be the one large file (16 KB) bounds the same recurring cost without gutting it — and the index now sits close enough to its 16 KB ceiling that the next lesson to land has to shorten an existing guardrail or split the index by topic rather than simply appending.

## The second half: a large document must be enterable

A ceiling alone produces files that pass the check and are still unusable, because a 24 KB wall of prose with no headings cannot be read partially — you either load all of it or none. `docs/lessons/INDEX.md` is the counter-example that became the model: one row per lesson with a guardrail, so an agent decides whether to open a lesson at all before paying for it.

So the rule pairs the ceiling with a shape requirement: over 25 KB means a table of contents or a companion INDEX in the first 40 lines, and reading happens by `grep -n` to locate then `sed -n 'A,Bp'` to read. The navigation aid is what makes the ceiling survivable rather than a content diet.

## Guardrail

`AGENTS.md` Rule 11 fixes the numbers (always-loaded set ≤ 32 KB with a 16 KB per-file cap, on-demand files ≤ 25 KB, 100 KB is a defect) and the partial-reading discipline; `make docs-lint` is the gate that will enforce the numbers once it lands, and review enforces the behavior today. When a document grows, ask first which tier it is in — the same edit is cheap in one and expensive in the other.

## Why this is worth a rule and not a cleanup

The 143 KB register and the 41 KB bilingual README were not written by careless people. Both were written by appending to the obvious place, one reasonable paragraph at a time, and both were cheap to write and expensive to read. Nothing in the rules made the accumulating cost visible, so nobody saw it until an audit measured it. The ceiling makes the cost visible at the moment of the edit, which is the only moment it is cheap to avoid.
