---
name: sprint-close
description: >-
  Close a ProjectKV sprint and hand the next one to a fresh conversation. Use
  when the sprint's tasks are merged, when the user asks to close or finish a
  sprint, write a handoff, or start the next sprint, and when a conversation has
  grown past roughly 1,500 steps.
---

# Sprint Close

A conversation is a sprint, not the project. A sprint is not closed until the
next conversation can start from files alone. Four outputs, all required, in
order.

## 1. Write the handoff

`docs/HANDOFF-s<N>.md`, from [templates/handoff.md](./templates/handoff.md).
English, 700 B - 12 KB (shorter means sections were skipped).

- Numbered sections, same order as the template. A fresh agent reads this once,
  end to end, so every line carries a fact.
- Facts only: branch, merged PRs, commit SHAs, issue numbers. Do not restate code
  the next agent can read.
- Decisions carry their ADR link, not the argument that produced them.
- Pitfalls: what failed, what it cost, what the next agent must not repeat.
- The next-sprint section is 5-10 tasks, one line each, each naming a file or
  area.
- Anything not verifiable goes under "Open Questions", never into "Merged
  Deliverables".

## 2. Verify before committing

```sh
tools/sprint/close.sh --sprint <N>   # validates structure, size, tracked+committed state
python3 tools/docs/docs_lint.py      # no NEW violations
```

The first command fails until the handoff is committed, by design: an untracked
handoff does not exist for the next conversation.

## 3. Commit with the sprint close

Its own commit on the sprint branch, or `docs/s<N>-handoff` if the sprint branch
is already merged. Rule 5 still applies: no direct push to `main` or `dev`.

## 4. Start the new conversation

```sh
tools/sprint/close.sh --sprint <N> --launch
```

Opens a new agy conversation in this repository, seeded from the handoff. Do not
type the bootstrap prompt by hand.

## Boundaries

- Do NOT start sprint N+1 work in the closing conversation, not even "one small
  task". The first task of the next sprint belongs to the next conversation.
- Do NOT resume the closed conversation for new work. It stays as a read-only
  reference for historical questions.
- If the user asks for new work and the handoff is missing, write the handoff
  first, then answer.
- If the sprint is only partly merged, stop: a handoff is written at the sprint
  boundary, not mid-sprint. Say which tasks remain and keep working.
