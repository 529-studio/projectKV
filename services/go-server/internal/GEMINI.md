# Go Modulith — Mandatory Rules for `services/go-server/internal/`

These rules apply automatically to **every** file under this directory.
They encode the binding architectural constraints from `docs/GO_BACKEND_ARCHITECTURE.md`.
Violation of any rule below must block the change — do not silently fix; report the violation clearly.

---

## Rule 1 — Allowed Import Graph (§2, GO_BACKEND_ARCHITECTURE.md)

The following table is the **exhaustive** list of permitted internal imports.
If a change requires an import not listed here, **stop and define an interface
in the consuming package** instead of importing the producing package directly
(dependency inversion — interface lives in the consumer, implementation in the
producer, wiring only in `cmd/api/main.go`).

```
cmd/api        → may import anything (composition root — wiring only, no business logic)
httpserver     → camera, proxycache, hotset, cvclient, subscription, notify, ws
camera         → (leaf — no internal imports)
proxycache     → camera
hotset         → camera
cvclient       → hotset, camera
notify         → i18n
subscription   → store, camera, notify
store          → camera
ws             → hotset
live           → hotset, camera
i18n           → (leaf)
config         → (leaf)
platform       → (leaf)
tilecache      → (leaf)
storage        → (leaf)
archiver       → storage, hotset
```

### How to apply this rule

1. Before adding any `import` of a sibling package under `internal/`, check the
   table above.
2. If the import is **not listed**, do **not** add it. Instead:
   - Define an `interface` in the package that needs the capability.
   - Implement that interface in the package that provides the capability.
   - Wire the concrete type in `cmd/api/main.go`.
3. If the import **is listed**, proceed normally.
4. Never create circular imports — the graph above is a DAG by design.

---

## Rule 2 — Banned Dependencies (§1, GO_BACKEND_ARCHITECTURE.md)

The following technologies / libraries are **explicitly banned** from this
codebase. Do not add them unless a human explicitly requests it with a stated
justification.

| Banned Item | Reason |
|---|---|
| **ORM** (GORM, ent, sqlc codegen) | Complexity must match the problem — a handful of tables does not justify an ORM. Hand-written SQL with `database/sql` + `modernc.org/sqlite` is sufficient. |
| **Viper / koanf** | Plain `os.Getenv` + a typed `Config` struct with defaults is enough at this scale. |
| **Cobra CLI framework** | Single binary with one command (`serve`). No subcommand routing needed. |
| **Message broker** (Kafka, NATS, RabbitMQ) | The system is a modular monolith on a single VPS. In-process function calls and goroutines replace what a broker would do at distributed scale. |
| **Kubernetes manifests** | Deployment target is a single VPS with Docker Compose at most. K8s would be premature infrastructure. |
| **Microservice split** beyond `go-server` + `cv-engine` | Two processes (Go control plane + Python ML plane) is the deliberate polyglot boundary. Further splitting adds network hops and operational cost without matching benefit. |

### How to apply this rule

- Before adding any new dependency to `go.mod`, check this table.
- If the dependency falls into a banned category, **reject the change** and
  explain why, referencing this rule.
- If a human explicitly asks for a banned dependency, require them to state
  the justification. Record their reasoning in a comment at the import site.

---

## Rule 3 — Approved Libraries (§1, GO_BACKEND_ARCHITECTURE.md)

Only these external dependencies are pre-approved:

| Concern | Approved Library |
|---|---|
| HTTP framework | `github.com/gin-gonic/gin` (v1.1x) |
| DB driver | `modernc.org/sqlite` (pure Go, no cgo) |
| WebSocket | `nhooyr.io/websocket` |
| Logging | `log/slog` (stdlib) |
| Linting | `golangci-lint` (govet, staticcheck, errcheck, gofmt) |

Adding a dependency **not in this list** requires explicit human approval.

---

## Rule 4 — Package = Module Boundary

- Everything lives under `internal/` (Go compiler enforces external import ban).
- Each top-level folder under `internal/` is one module.
- A module owns its own types. Do not reach into another module's unexported
  structs or use type assertions to access internal state.
- HTTP handler packages expose `RegisterRoutes(rg *gin.RouterGroup)`.
- Leaf packages have zero internal imports and can be compiled/tested in isolation.

---

## Rule 5 — Format & Vet Gate

Every change to Go files must pass:
1. `gofmt -l .` returns no output (all files formatted).
2. `go vet ./...` returns no errors.

Do not consider a change complete until both pass.
