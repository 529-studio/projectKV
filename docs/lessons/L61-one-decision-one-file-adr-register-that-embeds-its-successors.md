# L61 — One Decision, One File: ADR Registers That Embed Their Successors

## Context

`docs/ADR-001-flood-alert-architecture.md` was 32,122 B and contained four decision records. It opened with `# ADR-001`, and further down, at lines 114, 143 and 199, it opened again with `# ADR-002`, `# ADR-003` and `# ADR-004`. Only the first of those was the document's own subject.

The consequences had been accumulating for two months:

- Documentation cited `ADR-002` 7 times and `ADR-003` 5 times. Only four files matching `docs/ADR-*.md` existed, and neither number was one of them. The decisions were not missing, they were headings inside another decision.
- The embedded `# ADR-004` was a **stale copy**. The maintained `docs/ADR-004-route-check-and-snapshot-archive.md` had grown to 36,665 B with sections 6-15; the copy inside ADR-001 stopped at section 5, at 13,604 B. Two copies of one decision, and nothing marked which one was current.
- A reader who opened ADR-001 to learn about flood alerts read a superseded route-check plan on the way, with no signal that it was superseded.

The audit proposed writing stubs for the seven missing ADRs, marked `Superseded` or `Merged into`. Measurement rejected that: six of the seven had been fully written and were simply never merged, and the seventh (ADR-002, the Go migration) is current practice. A stub saying `Superseded` would have been a false statement in a document whose only job is to be authoritative.

## Root cause

**A decision's identity was a heading, not a file.** Every consumer of an ADR — a markdown link, a `grep`, a reader listing `docs/` — resolves a *number* to a *filename*. `ADR-002` as a `#` heading inside `ADR-001.md` is invisible to all three. The document existed and could not be found, which is indistinguishable from not existing.

The second defect follows from the first. Once two decisions share a file, copying that file to update one of them copies the other, and the copy is not marked. That is how the stale ADR-004 was created, and it is why the copy silently stopped at section 5: it was taken at a moment when section 5 was the end.

## Guardrail

- **One decision, one file, one number.** A new ADR is a new `docs/ADR-<NNN>-<slug>.md`. It is never appended to an existing ADR, however related the subject. The existing ADR may gain a `Related` link; it does not gain a second `# ADR-` heading.
- **A superseded decision is superseded by a number, not by an appended section.** The superseding ADR states `Supersedes: ADR-00N`, the superseded file states `Status: Superseded by ADR-00M`, and both files stay. A decision record that is edited in place cannot show what changed.
- **Before writing a stub, check whether the document exists under another name.** `git log --all --diff-filter=A -- '*ADR-005*'` answers in a second. Six of the seven "missing" ADRs were sitting in local branches; two more were inside ADR-001. Writing seven stubs would have destroyed the real text's only trace.
- **`python3 tools/docs/recover_adrs.py --check`** proves the recovery: the ADR-001 slices reassemble the original 32,122 B byte for byte, the dropped ADR-004 copy is a prefix of the maintained file, and each recovered file's SHA-256 matches the recorded source. The unique-ADR-number rule moves into `make docs-lint` under #346.
