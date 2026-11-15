# Lesson 463: Lore Verifiable Memory Integration for OpenCode and Antigravity (agy)

## Navigation Aid & Quick Reference
- **Domain**: Agent tooling, Verifiable memory, MCP server integration, Knowledge persistence.
- **Relevant Issues**: Issue #463.
- **Key Files**:
  - `lore.ontology.yml` (Predicates, domains, and cardinality constraints)
  - `.agents/mcp_config.json` (Antigravity MCP server configuration)
  - `opencode.json` (OpenCode MCP server configuration)
  - `.gitignore` (Ignores local `.lore/` database)
- **Core Invariants**:
  - **Single Source of Truth**: Agent memory is deterministic and verifiable against the underlying repository state using git-like branch promotion (`main` vs `agent/<session>`).
  - **Zero Database Tracking**: Local SQLite stores (`.lore/lore.db`) must remain gitignored to prevent database merge conflicts across developer machines and parallel agent sessions.
  - **Shared Memory Plane**: Both OpenCode and Antigravity connect via MCP stdio (`lore mcp`) to access and write claims with session isolation (`LORE_SESSION: opencode` and `LORE_SESSION: antigravity`).

---

## 1. Context & Problem Statement

As autonomous agents (Antigravity CLI / agy and OpenCode) work across sprints, knowledge of repository conventions, file structures, routes, and operational quirks risk fragmentation across conversation transcripts. Lore provides a git-native verifiable memory layer that ingests deterministic facts from code and allows agents to search and record claims backed by evidence.

---

## 2. Implementation & Architecture

1. **Repository Memory Initialization**:
   - Initialized `.lore/` local database and committed `lore.ontology.yml`.
   - Updated `.gitignore` to keep `.lore/` local and untracked.
2. **MCP Integration for Antigravity (agy)**:
   - Configured `.agents/mcp_config.json` to expose `lore mcp` under session `antigravity`.
3. **MCP Integration for OpenCode**:
   - Configured `opencode.json` to mount `lore mcp` under session `opencode`.
4. **Deterministic Ingest & Branch Promotion**:
   - Ingested 2,125 deterministic claims from packages, imports, routes, ports, and markdown documentation.
   - Promoted the active claims into Lore's `main` branch so new sessions inherit project facts automatically.
