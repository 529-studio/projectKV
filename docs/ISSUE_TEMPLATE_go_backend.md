# Issue Template — Go Backend Modulith Task

Copy this template for every new issue under `services/go-server`. Fill every
section — an issue without Technical Details or Definition of Done should not
be assigned to an agent session.

---

## Title
`[go-server] <module-name>: <one-line summary>`

## Context
Link back to the relevant section of `docs/GO_BACKEND_ARCHITECTURE.md` and,
if relevant, `docs/ADR-001-flood-alert-architecture.md`. One or two sentences
on why this module exists — do not make the agent re-derive intent from
scratch.

## Depends on
List issue numbers that must be merged to `dev` before this one starts.
Empty if this is a Tier-0 (leaf) issue.

## Technical Details
- Exact file paths to create/modify (copy from §3 directory tree).
- Exact interfaces/structs this module must define or implement, referencing
  the module spec section in `GO_BACKEND_ARCHITECTURE.md`.
- Which packages this module is **allowed** to import (copy the relevant row
  from §2's import graph table — do not let the agent infer this).
- Any config/env vars this module reads (copy from §6 table).

## Out of Scope
Explicitly list what NOT to touch — other modules, other files, no new
dependencies beyond what's already approved in §1. This section exists
specifically to stop scope creep in agent-driven work.

## Definition of Done
- [ ] All listed files created at the exact specified paths
- [ ] `go build ./...` succeeds for this package in isolation
- [ ] `gofmt -l .` reports no files
- [ ] `go vet ./...` clean
- [ ] Only imports packages allowed by the §2 import graph for this module
- [ ] No dependency added outside the §1 approved list
- [ ] Unit tests included for non-trivial logic (state changes, error paths)
- [ ] `architecture-guardian` sub-agent review run and passed (or violations
      listed and justified)
- [ ] Real production verification evidence attached to PR before merge: For any task claiming to serve real/live data (not mock), attach real operational proof (actual API response snippet, live container log output, or screenshot from the live deployment). Passing unit tests alone is strictly insufficient.
- [ ] Feature branch pushed, PR opened against `dev` per `AGENTS.md` workflow
      (no direct push — the existing pre-push hook enforces this regardless)

---

## Worked Example — Issue #3: `internal/camera`

### Context
Leaf package (§2 import graph: no internal dependencies). Loads the existing
`cameras_all.json` catalog and exposes it to every other module that needs
camera metadata (proxycache, hotset, cvclient, store, subscription).
Reference: `GO_BACKEND_ARCHITECTURE.md` §3, `camera/catalog.go` and
`camera/handler.go` rows.

### Depends on
None (Tier 0).

### Technical Details
- Create `internal/camera/catalog.go`:
  - `type Camera struct` — match the exact key casing already used in
    `cameras_all.json` at the repo root (inspect the file, do not guess field
    names).
  - `type Catalog struct` holding the loaded cameras in memory (slice + a
    map keyed by ID for O(1) lookup).
  - `func LoadCatalog(path string) (*Catalog, error)` — reads and parses the
    JSON file once at startup.
  - `func (c *Catalog) ByID(id string) (Camera, bool)`
  - `func (c *Catalog) FilterByZone(zone string) []Camera`
  - `func (c *Catalog) Zones() []string` — distinct zone list, used by
    `GET /api/zones` for subscription validation later.
- Create `internal/camera/handler.go`:
  - `func RegisterRoutes(rg *gin.RouterGroup, catalog *Catalog)`
  - `GET /api/cameras` — supports `?zone=` and `?corridor=` query params
  - `GET /api/cameras/:id`
  - `GET /api/zones`
- JSON response keys: `snake_case`, matching `cameras_all.json` convention
  (see §4 of the architecture doc).

### Out of Scope
- Do not touch `proxycache`, `hotset`, or any other module.
- Do not add a database-backed camera store — the catalog is static JSON,
  loaded once, in-memory only, per §3.
- Do not add caching/TTL logic here — that belongs to `proxycache`.

### Definition of Done
- [ ] `internal/camera/catalog.go` and `internal/camera/handler.go` created
- [ ] `go build ./internal/camera/...` succeeds standalone
- [ ] `gofmt -l internal/camera` empty
- [ ] `go vet ./internal/camera/...` clean
- [ ] No imports outside stdlib + `gin` for this package
- [ ] Unit test: `LoadCatalog` on a fixture JSON returns expected count and
      a known camera by ID; `FilterByZone` returns only matching entries
- [ ] `architecture-guardian` review passed
- [ ] PR opened against `dev`
