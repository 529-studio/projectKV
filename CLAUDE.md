# CLAUDE.md

Entry point only. This file must never contain rule text — it imports the canonical
sources below, so any edit there takes effect here automatically and cannot drift.

@AGENTS.md
@docs/GO_BACKEND_ARCHITECTURE.md

## Skills

Canonical skills live in `.agents/skills/` (`architecture-guardian`, `go-modulith`,
`go-scaffolder`). `.claude/skills` is a symlink to that directory, so Claude Code,
agy and Codex all see the same three skills.
