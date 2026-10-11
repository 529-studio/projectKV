# AGENTS.md - Project Rules & Development Guidelines

This document defines the core principles, design standards, and mandatory technical rules for all AI Agents and developers contributing to this codebase.

1. **Design & UX Philosophy**: Minimalist, monochromatic, zero-emoji in user-facing UI and backend logs. Web UI supports bilingual i18n (`vi` default with full diacritics, `en` secondary). Codebase, documentation, commit messages, and tooling must be 100% English.

2. **Issue Estimation Mandate**: Before starting any issue or task, AI agents and developers MUST provide an explicit completion time estimate (active effort hours/days and expected delivery window) based on task scope.

3. **Sound Solutions Over Shortcuts**: Once an issue is estimated, never shy away from implementation effort or choose sub-optimal shortcuts out of convenience. Always select and build the most robust, rational, and maintainable solution for the project.

4. **End-to-End Bug Reproduction**: When investigating any bug or regression, you MUST simulate and reproduce the failure end-to-end from a real user/client perspective before attempting fixes. Passing unit tests alone is NEVER sufficient.

5. **Git Flow & Zero Direct Push**: Direct push to `main` and `dev` is strictly prohibited. Create feature/fix/doc branches (`feat/*`, `fix/*`, `docs/*`), push, and open PR targeting `dev`. AI agents must NEVER auto-merge PRs.

6. **Living Documentation Maintenance**: Whenever a PR is created or merged, developers/agents MUST update `docs/project_status.md` and `docs/lesson_learn.md`. Codebase, git history, and PR list are the sole source of truth; never fabricate claims.

7. **Architecture Standards**: Follow leaf modulith pattern in `services/go-server` (see `GO_BACKEND_ARCHITECTURE.md`). Hand-written SQL only (no ORM). Never fetch snapshots directly from frontend; use backend proxy cache. Run `make swagger` on API route changes.

8. **Zero Hardcoded Secrets & Mandatory Environment Configuration**: Hardcoding API keys, access secrets, private tokens, passwords, or credentials in any form (including offline test scripts, evaluation tools, benchmark harnesses, utility CLIs, or documentation files) is strictly forbidden. All secrets must be dynamically loaded from `.env` (which must remain strictly gitignored) or environment variables. When writing environment fallback functions or parameter defaults (e.g., `os.getenv("KEY", "")`, `config.Get("KEY", "")`), the fallback value MUST strictly be `""` (empty string) or trigger an immediate fail-fast termination (`os.Exit(1)` / panic / fatal log). Never place real, mock, or placeholder credential strings as fallback arguments in executable code. Example configuration templates in `docs/` and `.env.example` must use uppercase angle-bracket placeholders only (e.g., `R2_ACCESS_KEY_ID=<your-access-key-id>`).

9. **PR Issue Linking Format Mandate**: When creating a Pull Request that addresses or implements an issue, the PR description MUST strictly start with `Close #<issue-number>` (e.g. `Close #209`). AI agents and developers must NEVER use informal, non-linking phrasing such as `Addresses Issue #209`, `Addresses #209`, or `Implement #209` because GitHub's issue closing automation exclusively parses standard closing keywords (`Close #<number>`) to link PRs and auto-close issues upon merge. All PRs must adhere to `.github/pull_request_template.md`.

