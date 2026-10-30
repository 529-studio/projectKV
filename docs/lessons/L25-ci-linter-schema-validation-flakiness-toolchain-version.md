## Lesson 25: CI Linter Schema Validation Flakiness & Toolchain Version Alignment

- **Incident & Tracking:** PR #216 (`feat/s5-t3-hybrid-flood-pipeline`), CI Run `38116173937`.
- **Symptom:**
  GitHub Actions workflow `CI - Code Verification` failed on step `Lint Go Backend` with an external network timeout error:
  ```text
  [.golangci.yml] validate: compile schema: failing loading "https://golangci-lint.run/jsonschema/golangci.v1.64.jsonschema.json": Get "https://golangci-lint.run/jsonschema/golangci.v1.64.jsonschema.json": context deadline exceeded (Client.Timeout exceeded while awaiting headers)
  ```
  Concurrently, the workflow emitted runner warnings: `! Restore cache failed: Dependencies file is not found in /home/runner/work/projectKV/projectKV. Supported file pattern: go.sum`.
- **Root Cause:**
  1. **External Schema Network Dependency**: In `golangci/golangci-lint-action@v6`, the action defaults to `verify: true`. If `.golangci.yml` exists, it invokes `golangci-lint config verify`, issuing an outbound HTTP GET to `https://golangci-lint.run`. Network latency or CDN rate limits on the external host abort the client deadline and fail the entire build, even when code is 100% clean and tests pass.
  2. **Monorepo Cache Path Blindness**: In `actions/setup-go@v5`, dependency caching searches for `go.sum` at the repository root by default. For monorepos with sub-packages (`services/go-server/go.sum`), caching fails unless `cache-dependency-path` is explicitly configured.
  3. **Toolchain Version Alignment**: When using precompiled binaries (`install-mode: "binary"`), binaries released on GitHub are compiled with older Go versions (e.g. Go 1.24). If the project specifies `go 1.26.0` in `go.mod`, `golangci-lint` fails with code 3 (`targeted Go version higher than build version`). Compiling via `install-mode: "goinstall"` ensures the linter binary matches the exact Go toolchain version installed by `actions/setup-go`.
- **Mandatory Takeaway:**
  1. **Disable Remote Schema Verification in CI**: Always configure `verify: false` in `golangci-lint-action` to eliminate brittle external HTTP dependencies during automated lint gates.
  2. **Codify Cache Dependency Paths in Monorepos**: Explicitly set `cache-dependency-path: 'services/go-server/go.sum'` in `actions/setup-go@v5` to enable dependency caching and prevent rebuild overhead.
  3. **Match Linter Build Toolchain with Target Go Version**: When projects adopt cutting-edge or custom Go toolchains, use `install-mode: "goinstall"` to compile `golangci-lint` against the runner's active Go runtime.
