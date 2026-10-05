# Engineering Lessons Learned & Post-Mortem Register

**Project:** ProjectKV (Ho Chi Minh City Real-Time Urban Traffic & Flood Monitoring)  
**Document Version:** 1.0.0  
**Last Updated:** 2026-10-05  
**Governing Rule:** Codebase, git history, and PR list are the sole sources of truth. All lessons recorded here represent verified technical failures, root causes, and mandatory operational guardrails.

---

## Lesson 1: Passing Unit Tests is Never Sufficient (Upstream WAF & Header Blackholing)

- **Incident & Tracking:** Issue #161, Pull Request #162 (`fix/cvengine-snapshot-user-agent`).
- **Symptom:**
  During live staging audit (`local-homesrv` with `ANALYZER=onnx`), `warded-backend` logged recurring timeout warnings:
  ```
  WARN analysis failed, degrading to unknown camera=... error="cv-engine: fetch snapshot: Get \"https://giaothong.hochiminhcity.gov.vn/render/ImageHandler.ashx?id=...\": context deadline exceeded"
  ```
  Meanwhile, the Go backend unit test suite (`go test -race ./...`) passed 100% with zero failures.
- **Root Cause:**
  In Go standard library `net/http`, when `http.NewRequestWithContext()` is called without explicitly setting a `User-Agent` header, Go's transport layer defaults to sending `User-Agent: Go-http-client/1.1`. The upstream traffic portal (`https://giaothong.hochiminhcity.gov.vn`) is protected by a WAF that silently drops inbound TCP packets containing `Go-http-client/*` without sending a TCP RST or HTTP error response. The connection hung until the 10-second client timeout expired.  
  The unit test suite passed because `httptest.NewServer` in `cvengine_test.go` mocked `/snapshot/` without asserting the presence of a `User-Agent` header, creating a false sense of security.
- **Mandatory Takeaway:**
  1. **End-to-End Simulation**: When investigating any bug or regression, you MUST simulate and reproduce the failure end-to-end against the real network/user interface before writing fixes.
  2. **Mock Contract Enforcement**: Unit test mocks must strictly enforce upstream contract constraints (e.g. rejecting requests that lack required headers) to prevent blind tests.

---

## Lesson 2: Sequence Inversion Risk & Invisible Downstream Divergence

- **Incident & Tracking:** Issue #136 (S2) deferred past Issue #137 (S3), #138 (S4), #140 (S5b); resolved via PR #159.
- **Symptom:**
  S2 (Hot Set camera curation) was scheduled in Wave 1 between S1 (capacity spike) and S3 (OpenVINO analyzer). S3 bypassed S2 by hardcoding numerical capacity targets (`HotsetFixedMaxCameras = 57`, `HotsetIntervalSeconds = 30`) in `config.go`.
- **Root Cause:**
  While decoupling allowed rapid development of S3, S4, and S5b, the Go backend's temporary fallback seeder in `main.go` selected cameras via crude keyword matching (`"điện biên phủ"`, `"võ văn kiệt"`) without verifying `len(road_keys) == 2`. Consequently, the OpenVINO model was running inference on straight-road mid-block cameras and arbitrary padding rather than genuine intersection choke points.
- **Mandatory Takeaway:**
  1. **Document Sequence Inversions**: If a dependency sequence is inverted to unblock velocity, the architectural divergence and its downstream implications must be explicitly disclosed in sprint documentation.
  2. **Catalog Reconciliation**: Verify that hardcoded assumptions exactly match empirical catalog data as soon as the deferred task is completed (PR #159 reconciled exactly 57 intersection cameras).

---

## Lesson 3: Completion Claims vs. Production Reality Gap

- **Incident & Tracking:** ADR-004 §8 (Sprint 2 A4 carryover omission), ADR-005 (Sprint 3 production audit).
- **Symptom:**
  Sprint completion reports declared milestones as "100% completed" and "production-grade," yet live staging inspections showed pending deployment tasks (e.g., physical model weights had not yet been ingested into the `warded-model-weights` volume on `local-homesrv`, and the D6 evaluation accuracy gate had never been run against real labeled camera frames).
- **Root Cause:**
  Equating local development completion, test passes, or PR merges with live production deployment and verified operational readiness.
- **Mandatory Takeaway:**
  1. **Definition of Done Hardening (ADR-005 T8)**: Any issue claiming that a feature delivers real (non-mock) data to users must include a verified live production check (an actual API response or screenshot from the real deployment host, not just a passing unit test) before it can be closed.
  2. **Truthful Reporting**: Never describe unverified or undeployed code as production-ready in project documentation.

---

## Lesson 4: Tooling & Environment Discrepancies on Host Platforms

- **Incident & Tracking:** Local Colima & Docker Compose onboarding (`make dev-model`).
- **Symptom:**
  Executing `make dev-model` (`docker compose -f docker-compose.local.yml up -d`) failed on developer macOS machines with:
  ```
  unknown shorthand flag: 'f' in -f
  Usage: docker [OPTIONS] COMMAND [ARG...]
  make: *** [dev-model] Error 125
  ```
- **Root Cause:**
  On macOS systems using Homebrew without Docker Desktop, `docker-compose` is installed as a standalone binary in `/opt/homebrew/bin/docker-compose`, rather than as a Docker CLI plugin in `~/.docker/cli-plugins/docker-compose`. As a result, the Docker CLI did not recognize the subcommand `compose` (with space) and attempted to parse `-f` as a root flag.
- **Mandatory Takeaway:**
  1. **Multi-Environment Resilience**: Tooling scripts and Makefiles should support both standalone binaries and CLI plugins, or document exact plugin symlinking:
     ```bash
     mkdir -p ~/.docker/cli-plugins && ln -sfn $(which docker-compose) ~/.docker/cli-plugins/docker-compose
     ```

---

## Lesson 5: Zero-Emoji Standard & Telemetry Stream Corruption

- **Incident & Tracking:** PR #152 (`fix(web): replace hourglass emoji with svg and enforce zero-emoji rule`).
- **Symptom:**
  Unicode emojis (such as hourglass, pin, warning, and rain characters) were originally used in loading states, map chips, and backend logging.
- **Root Cause:**
  Emojis rendered inconsistently across operating systems (iOS vs Android vs Windows vs macOS), violated the monochromatic high-contrast design philosophy, caused multi-byte UTF-8 encoding corruption in headless environments, and broke regex parsing in CI/CD telemetry aggregators (e.g. Loki, Datadog).
- **Mandatory Takeaway:**
  1. **Strictly Zero Emoji**: All UI controls and indicators MUST use monochromatic, scalable vector SVGs (`fill="none" stroke="currentColor"`).
  2. **Structured Logging**: Backend logs MUST strictly use standardized uppercase prefix tags (`[INIT]`, `[INFO]`, `[WARN]`, `[ERROR]`, `[FATAL]`) and key-value attributes.
