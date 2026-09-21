---
name: go-modulith-conventions
description: Use whenever adding, modifying, or reviewing Go code under services/go-server. Enforces the modulith import rules and package responsibilities defined in docs/GO_BACKEND_ARCHITECTURE.md.
---

# Go Modulith Conventions for services/go-server

Before writing or modifying any file under `services/go-server/internal/`:
1. Re-check the allowed import graph in `docs/GO_BACKEND_ARCHITECTURE.md` §2. If your change requires an import not listed there, define an interface in the consuming package instead of importing the producing package directly.
2. Every package under `internal/` is a module boundary — do not reach into another package's unexported types.
3. Run `gofmt -w` and `go vet ./...` before considering a change complete.
4. Do not add new third-party dependencies without checking §1 of the architecture doc first (framework/library decisions are final unless a human explicitly revisits them).
