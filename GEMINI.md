# GEMINI.md

Project rules are defined in `AGENTS.md` and are loaded automatically by agy
(Antigravity) and by Codex CLI, which both read `AGENTS.md` natively. This file exists
only for standalone Gemini CLI consumers, which use `GEMINI.md` as their context file.

- Do **not** `@`-import `AGENTS.md` from this file. agy already loads `AGENTS.md`, so an
  import would inject the same rule set into the context window twice — the exact
  duplication this repository is trying to remove.
- Standalone Gemini CLI: set `"contextFileName": "AGENTS.md"` in its `settings.json`
  instead of copying rule text here.
