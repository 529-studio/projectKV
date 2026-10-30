# Agent Context Surface Audit — Documentation Bloat & Instruction-Set Fragmentation

- **Generated At**: `2026-10-30`
- **Scope**: `AGENTS.md`, `docs/**/*.md`, `README.md`, `.agents/skills/`, `.claude/skills/`, `services/go-server/internal/GEMINI.md`, plus the context-loading behavior of the agent CLIs in use (agy/Antigravity, Claude Code, Codex CLI, zcode/GLM)
- **Method**: byte-level measurement of every tracked markdown file; binary inspection of each installed CLI to determine which instruction files it auto-loads; cross-reference diff of the duplicated rule sets
- **Governing Rule**: `AGENTS.md` Rules 1, 2, 6 (living documentation), 7 (architecture standards)
- **Verdict**: **Bloated and fragmented.** The problem is not total repository size; it is (a) two monolithic documents that agents are mandated to read on every task, and (b) the same rule set duplicated across four locations that have already drifted apart.

---

## 1. Executive Summary

Tracked markdown on `dev` totals **502 KB (~125k tokens)** across 47 files. A single documentation commit currently in flight adds another **228 KB** — a **45% increase to the entire documentation corpus from one PR** — bringing the total to **730 KB (~182k tokens)**. An agent that follows Rule 6 literally ("audit and synchronize all related documentation") must load roughly **94k tokens before writing its first line of code**. That exceeds the effective context of GLM (128K), sits at the edge of Claude Code's 200K window, and consumes a large fraction of agy's quota on every task.

Two defects are more serious than the raw size, because they are correctness failures rather than cost failures:

1. **Claude Code currently loads zero project rules.** The repository has no `CLAUDE.md`. Claude Code 2.1.208 reads only `CLAUDE.md`; its binary references `AGENTS.md` solely inside the `/init` scaffolding prompt. Rules 1, 5, 8, 9 and 10 are therefore unenforced for that CLI.
2. **agy/Gemini loads a stale rule set.** `services/go-server/internal/GEMINI.md` is auto-applied to every file under `internal/` and claims to be the "exhaustive" import graph, but covers only 15 of the 20 packages that exist there.

The remediation below is **progressive disclosure plus single source of truth**, not deletion. The engineering lessons and ADRs are genuinely valuable; they simply must not be loaded wholesale on every turn.

---

## 2. Measured Context Surface

Token estimates use the `bytes / 4` heuristic for English prose. Vietnamese content tokenizes at roughly 1.5-2 characters per token, so its effective cost is about double the byte-derived figure.

Committed corpus on `dev`: **501,618 bytes / 47 files (~125k tokens)**. Rows marked *in-flight* are staged on `feat/s9-t5-notify-preview` and not yet on `dev`.

| File | Bytes | Est. tokens | Loaded by |
|---|---|---|---|
| `AGENTS.md` | 5,450 | 1.4k | Codex, agy (auto) |
| `docs/lesson_learn.md` (in-flight) | 142,821 | 35.7k | not auto; forced by Rule 6 |
| `docs/project_status.md` (in-flight) | 66,630 | 16.7k | not auto; declared "onboarding guide" |
| `README.md` (EN + VI duplicated) | 41,605 | 10.4k | on setup/debug tasks |
| `docs/ADR-004-route-check-and-snapshot-archive.md` | 36,665 | 9.2k | on architecture context |
| `docs/ADR-001-flood-alert-architecture.md` | 32,122 | 8.0k | on architecture context |
| `docs/ADR-009-zero-vlm-flood-segmentation.md` | 24,705 | 6.2k | on architecture context |
| `docs/ADR-009-self-hosted-flood-segmentation-and-directional-pcu.md` | 14,325 | 3.6k | on architecture context |
| `docs/sprint9_execution_guide.md` (in-flight) | 18,814 | 4.7k (VI, ~8.5k effective) | per sprint |
| `docs/GO_BACKEND_ARCHITECTURE.md` | 17,857 | 4.5k | referenced by Rule 7 |
| `.agents/skills/` (2 skills) | 10,477 | 2.6k | some CLIs |
| `services/go-server/internal/GEMINI.md` | 4,664 | 1.2k | agy (auto, nested) |
| `.claude/skills/go-modulith/SKILL.md` | 957 | 0.24k | Claude Code |

**Worst-case mandated read set** (lessons + status + 4 ADRs + architecture doc + README) = **377 KB ≈ 94k tokens**, before any source file is opened.

**In-flight addition at audit time**: `docs/lesson_learn.md`, `docs/project_status.md` and `docs/sprint9_execution_guide.md` were staged on `feat/s9-t5-notify-preview` — 1,805 added lines, **228,265 bytes**. Merging that PR grows the documentation corpus by 45% and adds ~57k tokens of potential read cost.

---

## 3. Per-CLI Load Behavior and Impact

Determined from the installed binaries (`strings` inspection) and each tool's documented defaults.

| CLI / Model | Instruction files actually loaded | Impact |
|---|---|---|
| **Codex CLI** `0.154.0` | `AGENTS.md` at repo root plus nested `AGENTS.md` files, hard-capped by `project_doc_max_bytes` (default **32,768**). Does **not** read `GEMINI.md` or `.claude/skills`. | Safe today (5.4 KB). Latent risk: once `AGENTS.md` exceeds 32 KB the tail is truncated **silently**, dropping Rules 8-10 without any error. Codex receives no modulith guardrail unless `AGENTS.md` links to it. |
| **agy CLI** `1.4.10` (Gemini 3.8 Flash, Medium) | Binary contains 57 references to `AGENTS.md` and 51 to `GEMINI.md`; loads root `AGENTS.md`, root `GEMINI.md` (absent), and the nested `services/go-server/internal/GEMINI.md`. | 1M window means no overflow, but three costs: stale import graph is enforced (see D1), quota burn on oversized reads, and attention dilution across 90k+ tokens of history. |
| **Claude Code** `2.1.208` (1M context disabled via env) | Only `CLAUDE.md`. No `CLAUDE.md` exists in the repo. | **Zero project rules loaded.** Highest-severity finding: branch protection, secret handling, PR linking and manual-test mandates are all unenforced. |
| **zcode (GLM)** | Follows the `AGENTS.md` convention; session artifacts land in `.zcode/` (gitignored). The repo does not pin a context file for it. | 128-200K window. A 90k-token documentation read triggers compaction mid-task. Vietnamese documents cost roughly twice as many tokens as English for the same content. |

---

## 4. Confirmed Structural Defects

### D1 — The modulith rule set exists in four places and has already drifted

The same import graph appears in:

1. `docs/GO_BACKEND_ARCHITECTURE.md` §2 (16 packages + `cmd/notify-preview`)
2. `services/go-server/internal/GEMINI.md` Rule 1 — self-described as "exhaustive", **covers 15 of the 20 existing packages**, omitting `feedback`, `flood`, `middleware`, `notify` and `subscription`
3. `.claude/skills/go-modulith/SKILL.md`
4. `docs/GO_BACKEND_ARCHITECTURE.md` §8, which embeds the skill text verbatim as a code block

Because (2) is the copy that agy applies automatically to every file under `internal/`, the stale graph is the one actually enforced. The `cmd/notify-preview` entry being added to (1) on `feat/s9-t5-notify-preview` is not mirrored into (2).

### D2 — Skills are not synchronized between CLIs

`.agents/skills/` contains `architecture-guardian` and `go-scaffolder`; `.claude/skills/` contains only `go-modulith`. Different CLIs are therefore held to different standards on the same repository.

### D3 — No entry point for Claude Code

No `CLAUDE.md` exists at any level. See §3.

### D4 — Dangling ADR references

Only four ADR files exist: `ADR-001`, `ADR-004`, `ADR-009`, and a second `ADR-009` (number collision). Documentation references **ADR-002, 003, 005, 006, 008, 010 and 011** more than 50 times in total, including 15 references to `ADR-011` and 3 to `ADR-010`. Readers and agents are sent to files that do not exist.

### D5 — Repository hygiene

- `services/go-server/notify-preview` is a **17 MB binary**, untracked and **not gitignored** — one `git add -A` away from entering a public repository.
- `.zcode/` is declared twice in `.gitignore` (lines 20 and 79).
- `docs/proofs/` (2.9 MB of images) and `tools/geo-enrich/data/phuong_boundaries.geojson` (19 MB) are tracked.
- `docs/lesson_learn.md` numbering is broken: Lesson 56 precedes Lesson 55, and Lesson 54 does not exist (57 headings for 58 lessons).

### D6 — Language rule violation

`docs/sprint9_execution_guide.md` is written in Vietnamese. Rule 1 requires documentation to be 100% English. It is also a per-sprint, transient artifact containing tool-specific terminal prompts, which does not belong in a permanent documentation directory.

---

## 5. Remediation Plan

### P0 — Compliance fixes (approximately 2 hours, do before the in-flight documentation PR merges)

| # | Action | Rationale |
|---|---|---|
| 1 | Add root `CLAUDE.md` containing only `@AGENTS.md` and `@docs/GO_BACKEND_ARCHITECTURE.md` | Restores all 10 rules for Claude Code without duplicating text |
| 2 | Add root `GEMINI.md` pointing at `AGENTS.md` | Makes the load set explicit instead of relying on tool defaults |
| 3 | Replace the body of `services/go-server/internal/GEMINI.md` with a 3-line pointer to `docs/GO_BACKEND_ARCHITECTURE.md` §1-§3 | Eliminates D1 permanently; the architecture doc becomes the only source |
| 4 | Make `.agents/skills/` canonical; symlink or move `.claude/skills/` to it | Eliminates D2 |
| 5 | Gitignore `services/go-server/notify-preview`; remove the duplicate `.zcode/` entry | Eliminates the 17 MB commit hazard |

### P1 — Remove the actual bloat (approximately 6-8 hours)

| # | Action | Rationale |
|---|---|---|
| 6 | Split `lesson_learn.md` into `docs/lessons/L01-*.md` … `L58-*.md` plus a `docs/lessons/INDEX.md` holding `number \| title \| one-line guardrail \| link` (~2 KB) | 35.7k tokens of always-read context becomes ~500 tokens; lessons open on demand. Also fixes the numbering defects |
| 7 | Rewrite Rule 6: append to the specific lesson file and add one INDEX row; explicitly forbid appending to a monolithic file | Removes the mechanism that caused the growth |
| 8 | Reduce `project_status.md` to `docs/ONBOARDING.md` (≤150 lines): purpose, service/port map, quickstart, links. Move metrics and scorecards to `docs/reports/` | 16.7k tokens of duplication with `AGENTS.md` and `README.md` |
| 9 | Move sprint guides to `docs/sprints/`, translate to English, archive completed sprints | Satisfies Rule 1; keeps transient artifacts out of the permanent doc set |
| 10 | Split `README.md` into `README.md` (English, canonical) and `README.vi.md` | Removes the EN/VI duplication from every setup read |

### P2 — Prevent recurrence (approximately 8 hours)

| # | Action | Rationale |
|---|---|---|
| 11 | Add `make docs-lint` and a CI gate enforcing: entry files ≤ 12 KB; each ADR ≤ 25 KB; unique ADR numbers; internal links resolve; no tracked file > 1 MB; `docs/**` English-only except `README.vi.md` | Catches D4, D5 and D6 automatically from now on |
| 12 | Add a "Context Budget" rule to `AGENTS.md`: never read a file over 25 KB wholesale — use `grep` / `sed -n` ranges; every long document must ship with an INDEX | Prevents the next monolith |
| 13 | Reconcile ADRs: create stubs for 002/003/005/006/008/010/011 with status `Superseded` or `Merged into`, or rewrite the references; renumber the second `ADR-009` to `ADR-009b` | Removes 50+ dangling references |

### Estimate (per `AGENTS.md` Rule 3)

| Package | Content | Active effort | Delivery window |
|---|---|---|---|
| P0 | 5 compliance and hygiene fixes | ~2 h | same day |
| P1 | Lesson split, onboarding reduction, README split, sprint archive | 6-8 h | 1 working day |
| P2 | docs-lint CI gate, context budget rule, ADR reconciliation | ~8 h | 1 working day |
| **Total** | | **~2 active days** | 2-3 days |

Expected outcome: baseline context cost falls from **~94k tokens to ~3-4k tokens** (`AGENTS.md` plus the lesson INDEX), and all four CLIs read **the same, correct rule set**.

---

## 6. Reproduction

All figures above are reproducible from a clean checkout:

```bash
# Per-file size and token estimate
for f in $(git ls-files '*.md'); do
  printf "%8d %6d %s\n" $(wc -c < "$f") $(( $(wc -c < "$f") / 4 )) "$f"
done | sort -rn | head -25

# Total tracked markdown on dev (committed baseline)
git ls-files -z '*.md' | xargs -0 cat | wc -c      # 501,618 bytes / 47 files

# In-flight documentation staged on feat/s9-t5-notify-preview
wc -c docs/lesson_learn.md docs/project_status.md docs/sprint9_execution_guide.md

# Which instruction files a CLI actually loads
strings "$(readlink -f "$(command -v claude)")" | grep -c "AGENTS.md"
strings "$(readlink -f "$(command -v agy)")"    | grep -o "AGENTS\.md\|GEMINI\.md" | sort | uniq -c

# Import-graph drift: packages present vs packages listed in internal/GEMINI.md
ls services/go-server/internal
sed -n '/^```$/,/^```$/p' services/go-server/internal/GEMINI.md | grep -E "→|->"
```
