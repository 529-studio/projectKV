# AGENTS.md - Project Rules & Development Guidelines

This document defines the core principles, design standards, and mandatory technical rules for all AI Agents and developers contributing to this codebase.

---

## 1. Design Philosophy

> **"Minimalist • Monochromatic • Pragmatic • UX-First"**

*   **Monochromatic & High-Contrast**:
    *   Primary palette: Black (`#000000`), Dark Gray (`#121212`, `#1e1e1e`), White (`#ffffff`), and Light Gray (`#888888`).
    *   Functional accent colors are used strictly for status indicators: Red (Congested / Closed), Green (Clear / Active), Yellow (Slow).
    *   **Strictly prohibited**: Glossy effects, colorful gradients, heavy glow/shadows, and bubbly rounded elements.
*   **Language Standards & Internationalization (i18n)**:
    *   **Codebase, Documentation, and Tooling**: Must be **100% English** (comments, logs, documentation, git hooks, Makefile, package.json, and CI/CD workflows).
    *   **Web Client (UI)**: Must support internationalization (`i18n`) with **Vietnamese (`vi`)** as the primary/default locale and **English (`en`)** as the secondary locale.
    *   **Vietnamese Typography**: In Vietnamese mode, proper diacritics (dấu tiếng Việt đầy đủ) are strictly mandatory. Never use unaccented Vietnamese in user-facing UI labels, titles, buttons, or notifications.
*   **Zero-Click AI Automation**:
    *   AI Vision features must operate completely automatically upon opening/selecting a camera, or run in the background with memory caching.
    *   **Never require the user to manually click an "Analyze with AI" button**.
*   **Zero Bloat Features**:
    *   Do not integrate third-party sharing buttons (Zalo, Waze redirect, etc.) unless specifically requested.
    *   Focus 100% on camera browsing, rapid filtering, and automated scene evaluation.

---

## 2. Map & Spatial Rules

*   **Clean Tile Layer without Watermarks**:
    *   Do not use unauthenticated commercial tile servers (to avoid *"API KEY REQUIRED"* watermark errors).
    *   Use standard OpenStreetMap (`https://tile.openstreetmap.org/{z}/{x}/{y}.png`) with neutral styling.
*   **Territorial Integrity**:
    *   Strictly enforce `maxBounds` locking the viewport within the Southeast mainland of Vietnam (`[10.15, 106.35]` to `[11.35, 107.45]`), eliminating any territorial ambiguity.
*   **Clutter Prevention & Dynamic Marker Scaling**:
    *   Do not clutter the map with dense, oversized markers.
    *   Default cameras: Render as discrete dots (`circleMarker` radius 3.5px to 12px depending on zoom).
    *   Pinned/Monitored cameras: Render as prominent numbered dots (18px to 30px, bold border, brought to front above other layers, compact numeric label `#1`, `#2` inside). Never use bulky text callouts that occlude map routes.
*   **No Fixed / Default Routes**:
    *   Never force any hardcoded default routes on application startup.
    *   The application focuses 100% on free camera exploration with optimized low latency to check live traffic conditions before traveling.

---

## 3. Engineering & Performance Rules

*   **Modern Frontend Stack**:
    *   Use **Next.js (App Router)**, **Tailwind CSS**, and **TypeScript** for all web frontend development.
    *   Animations must be sleek, minimalist, and implemented via **Framer Motion** or Tailwind utilities.
    *   Ensure components are properly split (e.g., Client Components for Leaflet/interactive hooks, Server Components for static shells).
*   **API Protection & Rate Limiting Prevention**:
    *   All snapshot images from upstream traffic portals must pass through the backend proxy (`server.js`) with an in-memory cache of at least 10–15 seconds.
    *   Never allow the frontend client to fetch bulk snapshot images directly from `giaothong.hochiminhcity.gov.vn`.
*   **Intelligent Analysis Output**:
    *   Future Computer Vision / AI modules must deliver concise, structured verdicts regarding:
        1. Vehicle density on the corridor.
        2. Congestion status and roadway bottlenecks.
        3. Actionable travel recommendations.

---

## 4. Git, Branching & CI/CD Rules

*   **100% English for GitHub Actions & CI/CD**:
    *   All configurations in `.github/workflows/` (workflow names, job names, step names, scripts, log outputs, automated commit messages, issue titles/bodies) **MUST BE 100% ENGLISH**.
*   **Branching Model**:
    *   `dev`: Default integration and development branch.
    *   `main`: Production branch for verified releases.
*   **Zero Direct Push Policy**:
    *   Direct pushes to `main` and `dev` are strictly forbidden for both human developers and AI agents.
    *   All features and bugfixes must follow the Pull Request workflow:
        1. Create feature branch: `git checkout -b feat/<feature-name>` or `fix/<fix-name>`.
        2. Push feature branch: `git push origin feat/<feature-name>`.
        3. Open a Pull Request targeting `dev` for review and automated CI verification.
    *   **No Auto-Merge by AI Agents**: AI agents must NEVER merge or squash Pull Requests. All PRs must remain open in "In review" state for direct human review and approval.
    *   For emergency administrative maintenance, the commit message must include the flag `[skip-pr-check]`.
*   **Two-Tier Protection Guard**:
    *   *Tier 1 (Client-Side Guard)*: `.githooks/pre-push` rejects direct push attempts to `main` or `dev` locally. Activated automatically via `make install` or `make setup-hooks`.
    *   *Tier 2 (Server-Side Guard)*: GitHub Action `branch-guard.yml` verifies commit provenance on GitHub. Commits not originating from a valid Pull Request immediately **FAIL ❌** and trigger an automated violation issue.
