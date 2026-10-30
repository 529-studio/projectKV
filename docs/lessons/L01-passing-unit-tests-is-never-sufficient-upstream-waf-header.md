## Lesson 1: Passing Unit Tests is Never Sufficient (Upstream WAF & Header Blackholing)

- **Incident & Tracking:** Issue #161, Pull Request #162 (`fix/cvengine-snapshot-user-agent`).
- **Symptom:**
  During live staging audit (`edge host` with `ANALYZER=onnx`), `warded-backend` logged recurring timeout warnings:
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
