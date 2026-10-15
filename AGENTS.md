# AGENTS.md - Project Rules & Development Guidelines

This document defines the core principles, design standards, and mandatory technical rules for all AI Agents and developers contributing to this codebase.

1. **Design & UX Philosophy**: Minimalist, monochromatic, zero-emoji in user-facing UI and backend logs. Web UI supports bilingual i18n (`vi` default with full diacritics, `en` secondary). Codebase, documentation, commit messages, and tooling must be 100% English.

2. **Sound Solutions & Root-Cause Verification (Never Fear Effort)**: When implementing any issue or task, never shy away from implementation effort or labor. Never choose sloppy, convenient shortcuts, superficial band-aids, or quick hacks out of convenience or fear of complexity. Always investigate and resolve problems at their absolute root cause, architect the soundest, most maintainable solution for the system, and verify exhaustively end-to-end before considering any task complete.

3. **Issue Estimation Mandate**: Before starting any issue or task, AI agents and developers MUST provide an explicit completion time estimate (active effort hours/days and expected delivery window) based on task scope. The estimate must honestly reflect the full effort required for sound, root-cause implementation per Rule 2, never anchoring to premature shortcuts.

4. **End-to-End Bug Reproduction**: When investigating any bug or regression, you MUST simulate and reproduce the failure end-to-end from a real user/client perspective before attempting fixes. Passing unit tests alone is NEVER sufficient.

5. **Git Flow & Zero Direct Push**: Direct push to `main` and `dev` is strictly prohibited. Create feature/fix/doc branches (`feat/*`, `fix/*`, `docs/*`), push, and open PR targeting `dev`. AI agents must NEVER auto-merge PRs.

6. **Living Documentation & Holistic Knowledge Maintenance**: Updating `docs/project_status.md` and `docs/lesson_learn.md` alone is NOT sufficient. Whenever any task or PR is implemented or merged, developers and AI agents MUST holistically audit and synchronize all related documentation across the repository:
   - Update any relevant topic, architecture, operational, sprint, or guideline files under `docs/` directly affected by the changes (e.g. execution guides, ADR references, data schemas, API catalogs).
   - Update root `README.md` (both English and Vietnamese sections) whenever public features, developer workflows, instructions, setup commands, or system architectures are affected or expanded.
   - Codebase, git history, and PR list remain the sole source of truth; never fabricate claims.

7. **Architecture Standards**: Follow leaf modulith pattern in `services/go-server` (see `GO_BACKEND_ARCHITECTURE.md`). Hand-written SQL only (no ORM). Never fetch snapshots directly from frontend; use backend proxy cache. Run `make swagger` on API route changes.

8. **Zero Hardcoded Secrets & Mandatory Environment Configuration**: Hardcoding API keys, access secrets, private tokens, passwords, or credentials in any form (including offline test scripts, evaluation tools, benchmark harnesses, utility CLIs, or documentation files) is strictly forbidden. All secrets must be dynamically loaded from `.env` (which must remain strictly gitignored) or environment variables. When writing environment fallback functions or parameter defaults (e.g., `os.getenv("KEY", "")`, `config.Get("KEY", "")`), the fallback value MUST strictly be `""` (empty string) or trigger an immediate fail-fast termination (`os.Exit(1)` / panic / fatal log). Never place real, mock, or placeholder credential strings as fallback arguments in executable code. Example configuration templates in `docs/` and `.env.example` must use uppercase angle-bracket placeholders only (e.g., `R2_ACCESS_KEY_ID=<your-access-key-id>`).

9. **PR Issue & Kanban Linking Format Mandate**: When creating a Pull Request that addresses or implements an issue or sprint task, the PR description MUST strictly start with `Close #<issue-number>` (e.g. `Close #248`) followed by the 529 Studio Hub (Plane) Kanban card identifier `closes [KV-<card-id>]` (e.g. `Close #248 closes [KV-2]`). AI agents and developers must NEVER use informal, non-linking phrasing such as `Addresses Issue #209`, `Addresses #209`, or `Implement #209` because GitHub's issue closing automation exclusively parses standard closing keywords (`Close #<number>`) to link PRs and auto-close issues upon merge, and the 529 Studio Hub n8n Gateway automation exclusively parses `closes [KV-X]` to automatically link PRs and move cards on the Kanban board (https://hub.529studio.site). All PRs must adhere to `.github/pull_request_template.md`.

