# Lesson 573: Lore Ontology Drift Against the Canonical Extractor Predicate Set

## Navigation Aid & Quick Reference
- **Domain**: Agent tooling, Verifiable memory, Ontology/cardinality configuration.
- **Relevant Issues**: Issue #573.
- **Key Files**:
  - `lore.ontology.yml` (predicate declarations: cardinality, domain, optional TTL)
  - `docs/lessons/463-lore-verifiable-memory-setup-opencode-agy.md` (original Lore bootstrap)
  - `.lore.yml` (repository id and hub remote; no predicates)
- **Core Invariants**:
  - **Ontology mirrors the toolchain, not history**: `lore.ontology.yml` must declare exactly the predicate set the installed lore extractors emit. A predicate absent from the file cannot hold claims, so extractor output silently disappears instead of failing loudly.
  - **The template is the contract**: `/Users/gdq2k2/projects/lore/templates/code/lore.ontology.yml` ships with the lore release. Upgrading lore without re-syncing this file is drift, and `lore doctor` is the only detector.
  - **Doctor is the acceptance gate**: `lore doctor` prints `[OK] ontology defines all 28 extractor predicates` only when every extractor predicate is declared; the other doctor rows (MCP configs, conflicts) are unrelated to this check.

---

## 1. Context & Problem Statement

The repository was bootstrapped at lore 0.6.0 with a 15-predicate ontology copied from an older template. The canonical code template declares 28. The 13 undeclared predicates were:

`section_title`, `title`, `status`, `provides_command`, `requires_runtime`, `has_keyword`, `detects`, `enforces_invariant`, `defines_rule`, `decides`, `has_guardrail`, `has_constraint`, `describes_issue`.

Each one is emitted by a deterministic extractor or by the memory-write path (doc titles and section titles, concept status, package commands/runtimes/keywords, rule detection, file invariants, ADRs, guardrails and constraints, issue descriptions). With them absent, those claims could not be represented in repository memory at all, and no error was raised at ingest time — the ledger simply never contained them.

---

## 2. Implementation

1. Replaced the `predicates:` block in `lore.ontology.yml` with the canonical 28-predicate block, preserving this repository's four header comment lines (meaning of `cardinality: one` / `many` and `ttl_days`). Predicate order follows the template so future diffs against upstream stay minimal.
2. Verified with `lore doctor` (the only command that validates the ontology against the extractor set).
3. Re-ran the memory pipeline: `lore ingest`, `lore promote`, `lore push`, `lore status`.

---

## 3. Verification Evidence

```
lore doctor
  [OK]   store.path        .../.lore/lore.db is readable (5451776 bytes)
  [OK]   store.schema      schema current (22 tables, required columns present)
  [OK]   parity            main holds 3182 claims
  [OK]   ontology          ontology defines all 28 extractor predicates
  [WARN] health.conflicts  1 unresolved conflicts (0 merge, 1 claim)
doctor: 10 OK, 5 WARN, 0 ERR

lore ingest      -> + 0 new claims (3152 candidates scanned) / memory already up to date
lore promote     -> error: agent/wt_8da38b1554 has no commits yet   (no-op, see §4)
lore push        -> [OK] pushed main k_9e8b943a06 (imported 0 remote row(s), merge up-to-date)
lore status      -> claims 3538 total 3538 active 0 draft ... remote http ... (in sync)
```

The 5 remaining `WARN` rows are pre-existing and out of scope: four unconfigured MCP clients (`.mcp.json`, `.codex`, `.cursor`, `.gemini`) and one unresolved tier-1 conflict.

---

## 4. Lessons

- **`lore promote` fails when the session branch has no commits.** Declaring predicates is configuration, not a claim write, so an ontology-only change produces a clean `ingest` (`0 new claims`) and an empty branch. `promote` then exits non-zero with `agent/<branch> has no commits yet`; this is not an error condition for the ontology fix, and `lore doctor --fix` does not change it. The memory effect only appears once an extractor or an agent writes a claim using one of the newly declared predicates.
- **Ontology drift is invisible without `lore doctor`.** Ingest reports success ("memory already up to date") whether or not the declared set is complete, so the predicate count must be checked against the installed template after any lore upgrade.
- **Configuration order matters for reviews**: predicate order and header comments were kept byte-stable against upstream so the diff is a pure insertion of 13 lines plus 13 reordered neighbours, not a rewrite.
