---
name: architecture-guardian
description: >-
  Use this skill to review any Go code change under services/go-server/ before
  considering it complete. It checks the modulith import graph, banned
  dependencies, and code formatting. Activate this skill after writing or
  modifying Go backend code to perform a final architecture compliance review.
---

# Architecture Guardian — Go Backend Review Skill

This skill performs an architecture compliance review of Go code changes in
`services/go-server/`. It **reports violations clearly** rather than silently
fixing them, so the developer or calling agent can make an informed decision.

---

## When to Activate

- After any code change touching `services/go-server/internal/**`
- Before marking a Go backend task as complete
- When reviewing a PR that modifies Go backend code

---

## Review Checklist

Execute each check below **in order**. If any check fails, stop and report
**all** violations found so far — do not auto-fix silently.

### 1. Import Graph Compliance

Reference: `services/go-server/internal/GEMINI.md` Rule 1 (or
`docs/GO_BACKEND_ARCHITECTURE.md` §2).

For every modified `.go` file under `internal/`:

1. Identify which package the file belongs to (e.g., `internal/hotset`).
2. List all imports of sibling packages under `internal/`.
3. Cross-reference each import against the allowed import graph:
   ```
   cmd/api        → may import anything
   httpserver     → camera, proxycache, hotset, cvclient, subscription, notify, ws
   camera         → (leaf)
   proxycache     → camera
   hotset         → camera
   cvclient       → hotset, camera
   notify         → i18n
   subscription   → store, camera, notify
   store          → camera
   ws             → hotset
   i18n           → (leaf)
   config         → (leaf)
   platform       → (leaf)
   ```
4. **Violation**: Any import not in the table above. Report:
   - File path
   - The unauthorized import
   - Suggested fix: "Define an interface in package X and implement it in
     package Y; wire in cmd/api/main.go"

### 2. Banned Dependency Check

Reference: `services/go-server/internal/GEMINI.md` Rule 2.

Scan `go.mod` and all new import statements for:
- ORM libraries (GORM, ent, sqlc)
- Viper, koanf
- Cobra
- Message brokers (Kafka, NATS, RabbitMQ client libraries)
- Kubernetes client-go or related libraries

**Violation**: Any newly added banned dependency. Report:
- The dependency
- Why it is banned (quote the reason from the rules)

### 3. Approved Library Verification

If a **new** external dependency was added to `go.mod`, verify it is in the
approved list:
- `github.com/gin-gonic/gin`
- `modernc.org/sqlite`
- `nhooyr.io/websocket`
- Standard library (`log/slog`, `net/http`, `database/sql`, etc.)

Any dependency not on this list requires explicit human approval. Flag it.

### 4. Format & Vet

Run the following commands (or instruct the calling agent to run them):

```bash
cd services/go-server
gofmt -l .
go vet ./...
```

**Violation**: Any file listed by `gofmt -l` or any error from `go vet`.
Report each issue with the file path and error message.

### 5. Package Boundary Integrity

For each modified package, verify:
- No unexported types from sibling packages are accessed via type assertions.
- HTTP handler packages expose `RegisterRoutes(rg *gin.RouterGroup)`.
- Leaf packages (`camera`, `i18n`, `config`, `platform`) have **zero** imports
  of other `internal/` packages.

### 6. Swagger API Documentation Synchronization

Reference: `AGENTS.md` Section 3 (Mandatory Swagger Synchronization for Backend APIs).

If any HTTP endpoint was added, modified, or removed under `services/go-server/`:
1. Verify that the handler function has accurate godoc Swagger annotations (`@Summary`, `@Description`, `@Tags`, `@Produce`, `@Success`, `@Router`, etc.).
2. Verify that `make swagger` was executed and generated artifacts under `services/go-server/docs/` (`docs.go`, `swagger.json`, `swagger.yaml`) are committed.

**Violation**: Any API route added, modified, or removed without corresponding Swagger annotations or regenerated Swagger artifacts. Report:
- Affected route and handler
- Suggested fix: "Add Swagger godoc comments to handler and run `make swagger`"

---

## Output Format

Produce a structured report:

```
## Architecture Guardian Report

### ✅ Passed Checks
- [ list of checks that passed ]

### ❌ Violations Found
1. **[Check Name]**: [description of violation]
   - File: `path/to/file.go`
   - Issue: [specific issue]
   - Suggested Fix: [how to resolve]

### ⚠️ Warnings
- [ any non-blocking observations ]
```

If all checks pass, output:
```
## Architecture Guardian Report
✅ All checks passed. Change is architecture-compliant.
```

---

## Important

- **Never auto-fix violations silently.** The purpose of this skill is to
  surface problems for human or agent review, not to mask them.
- If a violation is intentional (e.g., a human explicitly approved a banned
  dependency), note it but do not block — just ensure the justification is
  recorded in a code comment.
