# Agent Context Surface Audit — Documentation Bloat & Instruction-Set Fragmentation

- **Generated At**: `2026-10-30`
- **Status Last Verified**: `2026-11-01` — P0 and all of P1 except the P1.2c translation are merged (`dev` at `4a51363`). Still open: #341 (translate the sprint guide), #342 (the stranded Sprint 8 artifacts) and all of P2. See §7 for the per-item status, the measured effect, and the six corrections this audit had to make to itself.
- **Baseline Commit**: `010ee50` — the `dev` tip the audit measured (47 tracked markdown files, 501,618 bytes). Every figure in §1-§6 describes that commit and is **not** the current state of the tree; §7 carries the current numbers.
- **Scope**: `AGENTS.md`, `docs/**/*.md`, `README.md`, `.agents/skills/`, `.claude/skills/`, `services/go-server/internal/GEMINI.md`, plus the context-loading behavior of the agent CLIs in use (agy/Antigravity, Claude Code, Codex CLI, zcode/GLM)
- **Method**: byte-level measurement of every tracked markdown file; binary inspection of each installed CLI to determine which instruction files it auto-loads; cross-reference diff of the duplicated rule sets
- **Governing Rule**: `AGENTS.md` Rules 1, 2, 6 (living documentation), 7 (architecture standards)
- **Verdict**: **Bloated and fragmented.** The problem is not total repository size; it is (a) two monolithic documents that agents are mandated to read on every task, and (b) the same rule set duplicated across four locations that have already drifted apart.

---

## 1. Executive Summary

At the audit baseline (`010ee50`), tracked markdown on `dev` totalled **502 KB (~125k tokens)** across 47 files. A single documentation commit then in flight (#327) added another **228 KB** — a **45% increase to the entire documentation corpus from one PR** — bringing the total to **730 KB (~182k tokens)**. An agent following Rule 6 literally ("audit and synchronize all related documentation") had to load roughly **94k tokens before writing its first line of code**. That exceeds the effective context of GLM (128K), sits at the edge of Claude Code's 200K window, and consumes a large fraction of agy's quota on every task.

Two defects were more serious than the raw size, because they were correctness failures rather than cost failures:

1. **Claude Code loaded zero project rules.** The repository had no `CLAUDE.md`. Claude Code 2.1.208 reads only `CLAUDE.md`; its binary references `AGENTS.md` solely inside the `/init` scaffolding prompt. Rules 1, 5, 8, 9 and 10 were therefore unenforced for that CLI.
2. **agy/Gemini loaded a stale rule set.** `services/go-server/internal/GEMINI.md` was auto-applied to every file under `internal/` and claimed to be the "exhaustive" import graph, but covered only 15 of the 20 packages that existed there.

The remediation below is **progressive disclosure plus single source of truth**, not deletion. The engineering lessons and ADRs are genuinely valuable; they simply must not be loaded wholesale on every turn.

Both correctness defects are now closed, and the two monoliths have been split. The always-read set dropped from **~94k tokens worst case to 26,422 bytes (~6.6k tokens)** at `4a51363`, a **93% reduction**, while the historical content was preserved rather than deleted — which is why the corpus total barely moved. §7 records the status of every item below with its measured effect.

---

## 2. Measured Context Surface

Token estimates use the `bytes / 4` heuristic for English prose. Vietnamese content tokenizes at roughly 1.5-2 characters per token, so its effective cost is about double the byte-derived figure.

Committed corpus on `dev` at `010ee50`: **501,618 bytes / 47 files (~125k tokens)**. Rows marked *in-flight* were staged on `feat/s9-t5-notify-preview` and not yet on `dev` when the audit ran; that branch merged as #327 on `2026-10-30`. The table records audit-time sizes so the baseline stays reproducible — current sizes are in §7.

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

**In-flight addition at audit time**: `docs/lesson_learn.md`, `docs/project_status.md` and `docs/sprint9_execution_guide.md` were staged on `feat/s9-t5-notify-preview` — 1,805 added lines, **228,265 bytes**. The prediction held: #327 merged the next day and took the corpus from 514,264 to **745,613 bytes (+45%, +231,349)**. The two documents it introduced were then split by #334 and #336, which is why the corpus total never came back down: the content was moved, not deleted.

---

## 3. Per-CLI Load Behavior and Impact

Determined from the installed binaries (`strings` inspection) and each tool's documented defaults.

| CLI / Model | Instruction files actually loaded | Impact |
|---|---|---|
| **Codex CLI** `0.154.0` | `AGENTS.md` at repo root plus nested `AGENTS.md` files, hard-capped by `project_doc_max_bytes` (default **32,768**). Does **not** read `GEMINI.md` or `.claude/skills`. | Safe today (5.4 KB). Latent risk: once `AGENTS.md` exceeds 32 KB the tail is truncated **silently**, dropping Rules 8-10 without any error. Codex receives no modulith guardrail unless `AGENTS.md` links to it. |
| **agy CLI** `1.4.10` (Gemini 3.8 Flash, Medium) | Binary contains 57 references to `AGENTS.md` and 51 to `GEMINI.md`; loads root `AGENTS.md`, root `GEMINI.md` (absent), and the nested `services/go-server/internal/GEMINI.md`. | 1M window means no overflow, but three costs: stale import graph is enforced (see D1), quota burn on oversized reads, and attention dilution across 90k+ tokens of history. |
| **Claude Code** `2.1.208` (1M context disabled via env) | Only `CLAUDE.md`. No `CLAUDE.md` exists in the repo. | **Zero project rules loaded.** Highest-severity finding: branch protection, secret handling, PR linking and manual-test mandates are all unenforced. |
| **zcode (GLM)** | Follows the `AGENTS.md` convention; session artifacts land in `.zcode/` (gitignored). The repo does not pin a context file for it. | 128-200K window. A 90k-token documentation read triggers compaction mid-task. Vietnamese documents cost roughly twice as many tokens as English for the same content. |

This table describes the audit-time state. `CLAUDE.md`, the root `GEMINI.md` and the `internal/GEMINI.md` pointer all landed in #332; the one step that remains unverified is whether Claude Code actually surfaces them, because `claude -p` produces no output in this environment (§7, open question O1).

---

## 4. Confirmed Structural Defects

Each defect below is recorded as it stood at the audit baseline (`010ee50`). Current status is tracked per defect in §7.1 — D1, D2 and D3 are closed, D5 is partly closed, and D4 and D6 remain open.

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

The **Status** column was added on `2026-10-31`; the Action and Rationale columns are unchanged from the audit so the original reasoning stays auditable.

### P0 — Compliance fixes (approximately 2 hours, do before the in-flight documentation PR merges)

| # | Action | Rationale | Status |
|---|---|---|---|
| 1 | Add root `CLAUDE.md` containing only `@AGENTS.md` and `@docs/GO_BACKEND_ARCHITECTURE.md` | Restores all 10 rules for Claude Code without duplicating text | Done — #332 / #331. 451 B |
| 2 | Add root `GEMINI.md` pointing at `AGENTS.md` | Makes the load set explicit instead of relying on tool defaults | Done with a deviation — #332. 611 B **non-importing** note; an `@`-import would duplicate a rule set agy already loads |
| 3 | Replace the body of `services/go-server/internal/GEMINI.md` with a 3-line pointer to `docs/GO_BACKEND_ARCHITECTURE.md` §1-§3 | Eliminates D1 permanently; the architecture doc becomes the only source | Done — #332. 4,664 B → 972 B, no copy of the graph remains |
| 4 | Make `.agents/skills/` canonical; symlink or move `.claude/skills/` to it | Eliminates D2 | Done — #332. `.claude/skills` is a symlink to `../.agents/skills` |
| 5 | Gitignore `services/go-server/notify-preview`; remove the duplicate `.zcode/` entry | Eliminates the 17 MB commit hazard | Done — #332. `.gitignore:56`; one `.zcode/` entry left |

### P1 — Remove the actual bloat (approximately 6-8 hours)

| # | Action | Rationale | Status |
|---|---|---|---|
| 6 | Split `lesson_learn.md` into `docs/lessons/L01-*.md` … `L58-*.md` plus a `docs/lessons/INDEX.md` holding `number \| title \| one-line guardrail \| link` (~2 KB) | 35.7k tokens of always-read context becomes ~500 tokens; lessons open on demand. Also fixes the numbering defects | Done — #334 / #333. 142,821 B → 15,255 B INDEX + 57 files. The "~2 KB" estimate was wrong: the index is **15 KB** |
| 7 | Rewrite Rule 6: append to the specific lesson file and add one INDEX row; explicitly forbid appending to a monolithic file | Removes the mechanism that caused the growth | Done — #334. New bullet in `AGENTS.md` Rule 6 |
| 8 | Reduce `project_status.md` to `docs/ONBOARDING.md` (≤150 lines): purpose, service/port map, quickstart, links. Move metrics and scorecards to `docs/reports/` | 16.7k tokens of duplication with `AGENTS.md` and `README.md` | Done — #336 / #335. 66,630 B → 3,122 B onboarding + 60,680 B scorecard moved verbatim |
| 9 | Move sprint guides to `docs/sprints/`, translate to English, archive completed sprints | Satisfies Rule 1; keeps transient artifacts out of the permanent doc set | **Moved, not translated** — P1.2c (#343). `docs/sprints/sprint9_execution_guide.md` is 18,829 B / 284 lines with 406 diacritics across 49 lines. The move is byte-verified by `tools/docs/verify_doc_move.py`: +15 B, all of it `../` prefixes on four link targets. Translation is #341; archiving waits for Sprint 9 to close (#298, #300, #302, #309 still open) |
| 10 | Split `README.md` into `README.md` (English, canonical) and `README.vi.md` | Removes the EN/VI duplication from every setup read | **Done** — P1.2b (#339). `README.md` 41,717 B → 24,254 B; `README.vi.md` is 2,183 B. The Vietnamese file shipped as a full translation and was cut to a quickstart before merge, because that translation had drifted four days after being written; `tools/docs/check_readme_vi_parity.py` now enforces the four facts that must stay in sync |

### P2 — Prevent recurrence (approximately 8 hours)

| # | Action | Rationale | Status |
|---|---|---|---|
| 11 | Add `make docs-lint` and a CI gate enforcing: entry files ≤ 12 KB; each ADR ≤ 25 KB; unique ADR numbers; internal links resolve; no tracked file > 1 MB; `docs/**` English-only except `README.vi.md` | Catches D4, D5 and D6 automatically from now on | **Open** — #346. Must land after #341, otherwise the English-only rule fails immediately on the Vietnamese sprint guide |
| 12 | Add a "Context Budget" rule to `AGENTS.md`: never read a file over 25 KB wholesale — use `grep` / `sed -n` ranges; every long document must ship with an INDEX | Prevents the next monolith | **Open** — #347, cheapest remaining item; best bundled with item 11 |
| 13 | Reconcile ADRs: create stubs for 002/003/005/006/008/010/011 with status `Superseded` or `Merged into`, or rewrite the references; renumber the second `ADR-009` to `ADR-009b` | Removes 50+ dangling references | **Open** — re-measured on `2026-11-01`: **72 references across 31 files** (this report excluded, since it cites the defect), and two files still share the `ADR-009` number. #342 covers the four artifacts that would recover ADR-010 and ADR-011 |

### Estimate (per `AGENTS.md` Rule 3)

| Package | Content | Active effort | Delivery window |
|---|---|---|---|
| P0 | 5 compliance and hygiene fixes | ~2 h | same day |
| P1 | Lesson split, onboarding reduction, README split, sprint archive | 6-8 h | 1 working day |
| P2 | docs-lint CI gate, context budget rule, ADR reconciliation | ~8 h | 1 working day |
| **Total** | | **~2 active days** | 2-3 days |

Actuals so far: P0 **~2 h** as estimated. P1.1 cost **~1.5 h** instead of the 6-8 h budgeted for the whole P1 package, because the split was performed by a deterministic script (`tools/docs/split_lessons.py`) rather than by retyping 143 KB of prose. P1.2a cost **~1.5 h**.

Expected outcome: baseline context cost falls from **~94k tokens to ~3-4k tokens** (`AGENTS.md` plus the lesson INDEX), and all four CLIs read **the same, correct rule set**. Measured: the always-read set is **26,422 bytes ≈ 6.6k tokens**, above the 3-4k target because the lesson index carries titles plus guardrails rather than titles alone. The 93% reduction against the 94k baseline stands.

---

## 6. Reproduction

All figures in §1-§4 are reproducible from the audit baseline. The commands must run **against that commit**, not against `dev` — the tree has since changed:

```bash
git switch --detach 010ee50    # the dev tip this audit measured

# Per-file size and token estimate
for f in $(git ls-files '*.md'); do
  printf "%8d %6d %s\n" $(wc -c < "$f") $(( $(wc -c < "$f") / 4 )) "$f"
done | sort -rn | head -25

# Total tracked markdown at the baseline
git ls-files -z '*.md' | xargs -0 cat | wc -c      # 501,618 bytes / 47 files

# The in-flight documentation that #327 later merged
git ls-tree -r --name-only ae97b26 | grep '\.md$' | while read f; do
  git cat-file -s ae97b26:$f
done | paste -sd+ - | bc                          # 745,613 bytes / 52 files

# Which instruction files a CLI actually loads
strings "$(readlink -f "$(command -v claude)")" | grep -c "AGENTS.md"
strings "$(readlink -f "$(command -v agy)")"    | grep -o "AGENTS\.md\|GEMINI\.md" | sort | uniq -c

# Import-graph drift: packages present vs packages listed in internal/GEMINI.md
ls services/go-server/internal
sed -n '/^```$/,/^```$/p' services/go-server/internal/GEMINI.md | grep -E "→|->"
```

Current state, for §7 (`2026-11-01`, `dev` at `4a51363`):

```bash
# Always-read set: the files a task actually loads before touching source
wc -c AGENTS.md CLAUDE.md GEMINI.md docs/ONBOARDING.md docs/lessons/INDEX.md
#  6330 + 451 + 611 + 3506 + 15524 = 26,422 bytes ~= 6.6k tokens   (at 4a51363)

# Corpus total (moved content, not deleted)
git ls-files -z '*.md' | xargs -0 cat | wc -c      # 757,470 bytes / 117 files at 4a51363
#  this refresh grows this report by ~4 KB, so a run after merge reports a slightly larger total

# Item 9: moved, not translated (P1.2c)
wc -l -c docs/sprints/sprint9_execution_guide.md   # 284 lines / 18,829 bytes
python3 -c "
import pathlib
vi=set('ăâđêôơưàảãáạằẳẵắặầẩẫấậèẻẽéẹềểễếệìỉĩíịòỏõóọồổỗốộờởỡớợùủũúụừửữứựỳỷỹýỵ')
t=pathlib.Path('docs/sprints/sprint9_execution_guide.md').read_text().lower()
print(sum(t.count(c) for c in vi), 'diacritics on', sum(1 for l in t.splitlines() if any(c in vi for c in l)), 'lines')"
#  406 diacritics on 49 lines   (grep -c '[àáảãạăâđêôơư]' undercounts: 45, the class is incomplete)

# Item 10: split, and the Vietnamese half is a quickstart (P1.2b)
wc -c README.md README.vi.md                       # 24,254 / 2,183
python3 tools/docs/check_readme_vi_parity.py       # 6 checks, exit 0

# Item 13 still open: 72 occurrences across 31 files, excluding this report
python3 -c "
import re,subprocess,pathlib
d=['ADR-002','ADR-003','ADR-005','ADR-006','ADR-008','ADR-010','ADR-011']
n=0;s=set()
for f in subprocess.run(['git','ls-files','*.md'],capture_output=True,text=True).stdout.split():
    if 'agent-context-bloat-audit' in f: continue
    c=sum(len(re.findall(x,pathlib.Path(f).read_text(errors='replace'))) for x in d)
    if c: n+=c;s.add(f)
print(n,'occurrences across',len(s),'files')"
ls docs/ADR-*.md
```

---

## 7. Remediation Status

Verified on `2026-11-01` against `dev` at `4a51363`.

### 7.1 Defects

| Defect | Status | Evidence |
|---|---|---|
| **D1** — modulith rule set duplicated and drifted | **Closed** | `services/go-server/internal/GEMINI.md` is now a 972-byte pointer to `docs/GO_BACKEND_ARCHITECTURE.md` §1-§3 and holds no copy of the import graph. `.claude/skills` is a symlink to `.agents/skills`, so the skill copy is single-source too. |
| **D2** — skills not synchronized between CLIs | **Closed** | `.claude/skills -> ../.agents/skills`; both CLIs see `architecture-guardian`, `go-modulith` and `go-scaffolder`. |
| **D3** — no entry point for Claude Code | **Closed in the repository, unverified at runtime** | `CLAUDE.md` (451 B) imports `AGENTS.md` and `docs/GO_BACKEND_ARCHITECTURE.md`; root `GEMINI.md` (611 B) makes the agy load set explicit. The runtime check is still outstanding — see O1. |
| **D4** — dangling ADR references | **Open, root cause identified** | **72 occurrences across 31 files** to ADR-002/003/005/006/008/010/011, none of which exist. This report is excluded from its own count, because it cites the defect repeatedly and would otherwise inflate it. The figure rose from 68/29 at `a2d578b` because two new documents describe the defect and therefore cite it (L59 +5, `docs/sprints/README.md` +1), while retiring the Vietnamese half of `README.md` removed two. #344 traced the cause: `ADR-010` and `ADR-011` were committed with the Sprint 8 report on a branch that was never pushed (`a2a248d`), and the sync that reached `dev` (`dcd2555`) carried only the sprint guide. Tracked in #342. Two files still share the `ADR-009` number. |
| **D5** — repository hygiene | **Partly closed** | `notify-preview` gitignored (`.gitignore:56`); `.zcode/` declared once. Lesson numbers are preserved exactly and never renumbered, because other documents cite them by number; `docs/lessons/INDEX.md` documents the missing Lesson 54, and the source order (56 before 55) is kept as-is rather than silently reordered. Still tracked: 11 files / 3,313,492 B under `docs/proofs/` and `tools/geo-enrich/data/phuong_boundaries.geojson` at 18,953,890 B. |
| **D6** — Vietnamese sprint guide | **Moved, not translated** | `docs/sprints/sprint9_execution_guide.md` is 18,829 B / 284 lines with 406 diacritics across 49 lines — unchanged prose, four `../` prefixes added by the move. Out of the permanent doc set, still a Rule 1 violation until #341 translates it. |

### 7.2 Measured effect on the always-read set

This is the metric that matters, and it is the one that moved. Sizes are from `4a51363`:

| File | Bytes | Loaded |
|---|---|---|
| `AGENTS.md` | 6,330 | every task |
| `docs/lessons/INDEX.md` | 15,524 | when a lesson is relevant |
| `docs/ONBOARDING.md` | 3,506 | onboarding and setup tasks |
| `GEMINI.md` | 611 | agy |
| `CLAUDE.md` | 451 | Claude Code |
| **Total** | **26,422** | **≈ 6.6k tokens** |

The total is 1,327 B larger than at `a2d578b`, and all of it is deliberate: `AGENTS.md` +674 B (Rule 1 and Rule 6 rewritten to describe the quickstart instead of a translation), `docs/ONBOARDING.md` +384 B (this report's link, the `README.vi.md` pointer, the `docs/sprints/` index), `docs/lessons/INDEX.md` +269 B (one new lesson, L59). Paying 1.3 KB of rule text to stop a 143 KB register from regrowing is the trade this audit exists to make.

The figure is pinned to a commit on purpose: any edit to these five files moves it, and a number with no commit attached is a number nobody can check.

Against the 377 KB / ~94k-token worst case measured in §2, that is a **93% reduction**.

The corpus total did **not** fall: 47 files / 501,618 bytes at the baseline, **117 files / 757,470 bytes** now. That is expected and is not a regression — #327 added 231,349 bytes of genuinely new content, and #334/#336 moved roughly 208 KB of it out of the always-read set into on-demand files rather than deleting it. A corpus-size metric would have reported failure for a change that succeeded; the always-read set is the metric that actually moved.

Two files shrank to redirect stubs so that pre-existing links keep resolving: `docs/lesson_learn.md` (142,821 B → 582 B) and `docs/project_status.md` (66,630 B → 711 B).

### 7.3 Corrections to this audit's own estimates and measurements

Recorded because an audit that hides its own misses is not auditable:

1. **Lesson index size.** Item 6 predicted `INDEX.md` at "~2 KB" and "~500 tokens". It is **15,524 bytes (~3.9k tokens)**, because the index carries a guardrail column extracted from each lesson, not just titles. The guardrails are what let an agent decide whether to open a lesson at all, so the extra 13 KB buys real work — but the prediction was optimistic by roughly 7x.
2. **Target context cost.** §5 predicted a floor of "~3-4k tokens". The measured floor is **6.6k tokens**. The 93% reduction stands; the absolute target did not.
3. **P1 effort.** Budgeted at 6-8 h for the entire package. P1.1 came in at ~1.5 h, P1.2a at ~1.5 h, P1.2c at ~45 min. The gap there is methodological, not a windfall: the split and the move were done by deterministic scripts, so no model read or rewrote 143 KB of prose. **P1.2b is the exception at ~3 h**, and the extra time was not spent on the split — it went on noticing that the split preserved a document that had already drifted, and on building the check that keeps it from drifting again. Purely mechanical items are cheap; the items where the correct shape has to be decided are not.
4. **`GEMINI.md`.** Item 2 specified a file that imports `AGENTS.md`. The shipped file is a non-importing note, because agy already loads `AGENTS.md` and an `@`-import would have loaded the same 10 rules twice.
5. **P1.2b scope.** Item 10 read as "split the file", and the split was mechanical and correct. What it did not anticipate is that keeping the Vietnamese half intact would preserve a document that had already drifted four days after being written (`b4ad721` → `dcd2555`), and that Rule 6 as first drafted would then have made that drift a permanent parity obligation. The fix landed before merge (`47b3d3b`), but it was a scope change found by reviewing the result, not by the estimate. Estimating a documentation change by its file operations misses the question that decides whether the result is maintainable.
6. **Vietnamese diacritic count.** D6 and item 9 reported "45 lines with Vietnamese diacritics", measured with `grep -c '[àáảãạăâđêôơư]'`. That character class is incomplete — it has no `ằẳẵắặ`, `ầẩẫấậ`, `ềểễếệ`, `ồổỗốộ`, `ờởỡớợ`, `ừửữứự` — so it undercounted. The real figure is **406 diacritics across 49 lines**. The conclusion was unaffected (the guide was Vietnamese either way), but the number in the audit was wrong, and a wrong number that nobody re-runs is how an audit stops being an audit. The reproduction block now carries the command that produces the stated figure.

### 7.4 Open questions and residual risk

- **O1 — Does Claude Code actually surface `CLAUDE.md`?** `claude -p` returns no output in this environment (reproduced at baseline, with an explicit model and stdin redirected; the gateway answers 200), so the import has never been observed working. One manual `claude` session running `/memory` settles it. Until then, D3 is closed on paper only.
- **O2 — Binary bloat is untouched.** `docs/proofs/` and the 19 MB GeoJSON are still tracked. P0 blocked one binary; it did not shrink the repository.
- **O3 — Ordering constraint for item 11.** A `docs-lint` gate that enforces English-only `docs/**` will fail on `docs/sprints/sprint9_execution_guide.md` until #341 lands. The placement half is resolved — the guide is out of the permanent doc set, in `docs/sprints/` with a lifecycle index — but the language half is live: item 11 must follow #341, or the two must ship together.
- **O4 — The stubs are load-bearing.** `docs/lesson_learn.md` and `docs/project_status.md` are redirects, not empty files, because sprint reports and ADRs still link to them. Deleting them requires a link sweep first; a `docs-lint` link check (item 11) is the prerequisite.
