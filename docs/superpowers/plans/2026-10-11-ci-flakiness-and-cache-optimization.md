# CI Flakiness and Cache Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate external schema validation network timeouts and enable proper Go module caching in GitHub Actions CI pipeline.

**Architecture:** Harden `.github/workflows/ci.yml` by setting `verify: false` on `golangci/golangci-lint-action@v6` to avoid external HTTP requests to `golangci-lint.run`, configure `cache-dependency-path: 'services/go-server/go.sum'` on `actions/setup-go@v5` for repository subdirectory awareness, and pin `version: v1.64.5` with precompiled binary installation to eliminate ~96s of unnecessary compilation compute overhead.

**Tech Stack:** GitHub Actions, YAML, Go 1.24, golangci-lint v1.64.5.

**Spec:** PR #216 CI failure diagnosis from run `38116173937` (`context deadline exceeded` loading remote JSONSchema).

## Effort & Delivery Window Estimate
- **Active Effort:** 0.5 hours (30 minutes).
- **Expected Delivery Window:** Same session (~45 minutes including CI push and verification).

## Global Constraints
- Minimalist, monochromatic, zero-emoji in user-facing UI and backend logs.
- Codebase, documentation, commit messages, and tooling must be 100% English.
- No direct push to `main` or `dev`.
- Changes must not degrade any existing Go backend tests, linting coverage, or frontend verification.

## Review Focus
1. `golangci-lint` must still execute all linters (`govet`, `staticcheck`, `errcheck`, `gofmt`) without skipping lint analysis.
2. Go module cache in `actions/setup-go@v5` must resolve `services/go-server/go.sum` without root-level path errors.
3. Binary install mode must work reliably on `ubuntu-latest` without runner architecture mismatch.
4. Concurrency cancel-in-progress and paths filter triggers must remain intact.
5. All CI checks must remain 100% green on PR #216.

---

### Task 1: Harden golangci-lint Step Against Remote Network Timeouts

**Files:**
- Modify: `.github/workflows/ci.yml:73-78`

**Interfaces:**
- Consumes: `services/go-server/.golangci.yml`
- Produces: Resilient CI lint execution independent of `https://golangci-lint.run` availability

- [ ] **Step 1: Inspect existing Lint Go Backend step configuration**

Current configuration in `.github/workflows/ci.yml`:
```yaml
      - name: Lint Go Backend
        uses: golangci/golangci-lint-action@v6
        with:
          working-directory: services/go-server
          install-mode: "goinstall"
```

- [ ] **Step 2: Add `verify: false`, specify `version: v1.64.5`, and switch `install-mode: "binary"`**

Configure `ci.yml` to:
1. Set `verify: false`: Skips the flaky external HTTP request to `https://golangci-lint.run/jsonschema/golangci.v1.64.jsonschema.json`.
2. Set `version: v1.64.5`: Pin known stable version matching Go 1.24.
3. Set `install-mode: "binary"`: Eliminates the 96-second `go install` compilation from source.

```yaml
      - name: Lint Go Backend
        uses: golangci/golangci-lint-action@v6
        with:
          version: v1.64.5
          install-mode: "binary"
          working-directory: services/go-server
          verify: false
```

- [ ] **Step 3: Verify YAML syntax locally**

Run: `node -e "const yaml = require('yaml'); const fs = require('fs'); yaml.parse(fs.readFileSync('.github/workflows/ci.yml', 'utf8')); console.log('YAML syntax valid');"`
Expected: PASS with "YAML syntax valid".

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(backend): harden golangci-lint against remote schema timeout (#209)"
```

---

### Task 2: Configure Go Module Cache Dependency Path

**Files:**
- Modify: `.github/workflows/ci.yml:68-72`

**Interfaces:**
- Consumes: `services/go-server/go.sum`
- Produces: Functional Go module cache restore and save for `services/go-server`

- [ ] **Step 1: Check existing Setup Go step in `ci.yml`**

Current configuration:
```yaml
      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'services/go-server/go.mod'
```
Note: In CI run annotations, this produced:
`! Restore cache failed: Dependencies file is not found in /home/runner/work/projectKV/projectKV. Supported file pattern: go.sum`

- [ ] **Step 2: Add `cache-dependency-path: 'services/go-server/go.sum'`**

Update `Setup Go` step to specify:
```yaml
      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'services/go-server/go.mod'
          cache-dependency-path: 'services/go-server/go.sum'
```

- [ ] **Step 3: Verify YAML syntax and check that `services/go-server/go.sum` exists**

Run: `test -f services/go-server/go.sum && node -e "const yaml = require('yaml'); const fs = require('fs'); yaml.parse(fs.readFileSync('.github/workflows/ci.yml', 'utf8')); console.log('Cache path verified');"`
Expected: PASS with "Cache path verified".

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(backend): configure cache-dependency-path for go.sum in monorepo (#209)"
```

---

### Task 3: Push to PR #216 and Verify End-to-End CI Run

**Files:**
- No code changes (push branch `feat/s5-t3-hybrid-flood-pipeline`)

**Interfaces:**
- Consumes: Pushed commits on GitHub origin
- Produces: Green GitHub Actions run with reduced execution time and zero schema timeout warnings

- [ ] **Step 1: Push commits to `feat/s5-t3-hybrid-flood-pipeline`**

Run: `git push origin feat/s5-t3-hybrid-flood-pipeline`
Expected: Push succeeds and triggers GitHub Actions CI.

- [ ] **Step 2: Monitor new GitHub Actions run**

Run: `gh pr checks 216`
Expected: All checks pass, with `Verify Go Backend` completing significantly faster without schema verification errors.
