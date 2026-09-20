---
name: go-scaffolder
description: >-
  Use this skill when creating a new module (package) under
  services/go-server/internal/. It generates boilerplate that follows the
  existing modulith patterns: leaf package structure, RegisterRoutes for HTTP
  handlers, hand-written SQL (no ORM), and proper dependency inversion.
---

# Go Scaffolder — New Module Boilerplate Generator

This skill generates correctly structured boilerplate for a new package under
`services/go-server/internal/`, following the established patterns from existing
modules.

---

## When to Activate

- When a user asks to "add a new module" or "create a new package" under
  `services/go-server/internal/`
- When implementing a feature that requires a new domain boundary

---

## Prerequisites

Before scaffolding, read:
- `services/go-server/internal/GEMINI.md` — mandatory import rules
- `docs/GO_BACKEND_ARCHITECTURE.md` §2–§3 — module boundary principles and
  directory structure

---

## Scaffolding Procedure

### Step 1 — Determine module type

Ask (or infer from context) which type of module is needed:

| Type | Characteristics | Examples |
|---|---|---|
| **Leaf** | No internal imports, pure domain logic or utility | `camera`, `i18n`, `config`, `platform` |
| **HTTP handler** | Exposes `RegisterRoutes(rg *gin.RouterGroup)`, may import leaf packages | `proxycache`, `subscription`, `hotset` (handler part) |
| **Infrastructure** | Implements interfaces defined by other packages, imported only by `cmd/api` | `store`, `cvclient` |

### Step 2 — Create the package directory

```
services/go-server/internal/<module_name>/
```

Use `snake_case` for multi-word names (Go convention: all lowercase, no
underscores preferred — but match existing repo style).

### Step 3 — Generate boilerplate files

#### For a Leaf package:

Create `<module_name>.go`:

```go
// Package <module_name> provides [one-line description].
// This is a leaf package — it MUST NOT import any other internal/ package.
package <module_name>

// [Domain types, constants, pure functions go here]
```

#### For an HTTP handler package:

Create `handler.go`:

```go
package <module_name>

import "github.com/gin-gonic/gin"

// RegisterRoutes mounts this module's HTTP endpoints onto the given router group.
func RegisterRoutes(rg *gin.RouterGroup /*, inject dependencies as params */) {
	// rg.GET("/<path>", handleGet)
}
```

Create `<module_name>.go` for domain types (if needed).

#### For an Infrastructure package:

Create `<module_name>.go` that implements an interface defined elsewhere:

```go
package <module_name>

// Ensure [ConcreteType] implements [consumer.InterfaceName].
var _ consumer.InterfaceName = (*ConcreteType)(nil)

type ConcreteType struct {
	// internal state
}

// [Implement interface methods here]
```

### Step 4 — Update the import graph

After creating the new package, determine its allowed imports:

1. Decide which existing packages it needs to consume.
2. For each dependency:
   - If the dependency is a **leaf package** and the import makes sense → add
     it to the allowed graph (document in a PR comment referencing §2).
   - If the dependency is a **non-leaf package** → define an **interface** in
     the new package and implement it in the dependency. Wire in `cmd/api/main.go`.
3. **Propose an update** to the import graph table in
   `docs/GO_BACKEND_ARCHITECTURE.md` §2 AND in
   `services/go-server/internal/GEMINI.md` Rule 1.

### Step 5 — Wire in the composition root

Add construction and dependency injection in `cmd/api/main.go`:

```go
// In main() or a setup function:
myModule := mymodule.New(/* injected dependencies */)
```

If the module has HTTP routes, register them in `internal/httpserver/router.go`:

```go
mymodule.RegisterRoutes(apiGroup, /* dependencies */)
```

### Step 6 — SQL (if the module needs persistence)

- Write raw SQL in a new migration file:
  `internal/store/migrations/NNNN_<description>.sql`
- Implement a repository in `internal/store/<module>_repo.go`
- The repo implements an interface defined in the consuming package
- **No ORM** — use `database/sql` with `modernc.org/sqlite` driver directly

### Step 7 — Verify

Run the architecture-guardian skill (or perform the checks manually):

```bash
cd services/go-server
gofmt -l .
go vet ./...
go build ./...
go test ./... -race
```

---

## Template: Minimal Leaf Package

```go
// Package example provides [description].
// Leaf package — no internal/ imports allowed.
package example

// Example is the primary domain type for this module.
type Example struct {
	ID   string
	Name string
}
```

## Template: Minimal HTTP Handler Package

```go
package example

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes mounts example endpoints.
func RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/examples", listExamples)
}

func listExamples(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"examples": []string{}})
}
```

---

## Checklist Before Completion

- [ ] Package compiles independently (`go build ./internal/<module>/...`)
- [ ] No unauthorized internal imports
- [ ] `RegisterRoutes` exposed if the package has HTTP handlers
- [ ] Import graph update proposed for architecture docs
- [ ] `gofmt` and `go vet` pass
- [ ] Interfaces defined in the consumer, not the producer
