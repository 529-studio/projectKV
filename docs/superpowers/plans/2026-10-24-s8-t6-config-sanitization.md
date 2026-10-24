# S8-T6: Holistic Environment Configuration Sanitization & Fail-Fast Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate silent configuration parsing failures caused by whitespace, quotes, or invalid syntax across all 25+ environment variables in `services/go-server/internal/config/config.go`, replacing silent fallbacks with structured `log/slog` warnings and rigorous unit tests.

**Architecture:** Create reusable parsing helpers (`CleanEnv`, `parseEnvInt`, `parseEnvInt64`, `parseEnvFloat`, `parseEnvBool`, `parseEnvDuration`, `parseEnvString`) that systematically strip surrounding quotes (`"`, `'`) and whitespace, validate bounds/minimums, and log structured warnings on malformed inputs before falling back to defaults.

**Tech Stack:** Go 1.24, `log/slog`, `strconv`, `strings`, `time`.

**Spec:** Issue [#288](https://github.com/529-studio/projectKV/issues/288) ([KV-18]), [ADR-010](docs/ADR-010-sprint7-audit-and-sprint8-plan.md), [sprint8_execution_guide.md](docs/sprint8_execution_guide.md).

## Global Constraints
- Minimalist, monochromatic, zero-emoji in backend Go logs (AGENTS.md Rule 1).
- Rule 2 (Sound root-cause verification) & Rule 8 (Zero hardcoded secrets, fallbacks must strictly be safe defaults).
- Strict leaf modulith pattern: `internal/config` has zero internal dependencies.
- Rule 3 estimation: Active Effort 4 hours, Delivery Window 0.5 calendar day.

## Review Focus
1. Malformed quoted numeric variables (e.g. `HOTSET_INTERVAL_SECONDS="30"`) must parse accurately as `30`, not fall back to default.
2. Trailing spaces and tabs (e.g. `HARVESTER_ENABLED=" true \t "`) must parse cleanly without silent failure.
3. Invalid non-numeric input (e.g. `PCU_CAPACITY_FLOOR="abc"`) must log a structured warning via `slog.Warn` and fall back to `15.0`.
4. Values below minimum threshold (e.g. `HEX_MIN_CAMERAS="-1"`) must log a warning and fall back safely.
5. Both entrypoints `LoadConfig()` and `LoadFromEnv()` must share uniform sanitization behavior.

---

### Task 1: Reusable Configuration Parsing Helpers & Structured Warning Observability
**Files:**
- Modify: `services/go-server/internal/config/config.go`
- Test: `services/go-server/internal/config/config_test.go`

- [ ] Add `CleanEnv(key string) string` stripping surrounding whitespace and quotes.
- [ ] Add `parseEnvInt`, `parseEnvInt64`, `parseEnvFloat`, `parseEnvBool`, `parseEnvDuration`, `parseEnvString` with structured `slog.Warn`.
- [ ] Refactor `LoadFromEnv()` to use parsing helpers across all variables.
- [ ] Refactor `LoadConfig()` to use parsing helpers across all 25+ variables.

### Task 2: Comprehensive Unit Test Suite
**Files:**
- Modify: `services/go-server/internal/config/config_test.go`

- [ ] Write `TestLoadConfig_SanitizationAndQuotedVariables` testing quotes (`"30"`, `'15.0'`, `"true"`), tabs, trailing spaces.
- [ ] Test invalid inputs (`"not-a-number"`, `"xyz"`, negative values where prohibited) verifying fallback defaults.
- [ ] Test `LoadFromEnv` sanitization with quoted and spaced variables.
- [ ] Run `go test -race -v ./internal/config/...`.

### Task 3: Quality Gates & Verification
**Files:**
- Verify: Full Go backend test suite (`make test-go`)
- Verify: `gofmt -l .` and `go vet ./...` (Architecture Guardian)
- Verify: Run Go server with sample quoted `.env` variables and inspect startup logs.
- Update docs: `docs/project_status.md`, `docs/lesson_learn.md` (Lesson 55).
