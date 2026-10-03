# S3 Self-Hosted Vehicle Analyzer (OpenVINO) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the `warded-model` OpenVINO inference container as the primary `ANALYZER=onnx` backend, replacing the stub `CVEngineAnalyzer` with a production-grade HTTP client in `internal/cvclient/cvengine.go`, after first hardening the standby response in `services/cv-engine/server.py`.

**Architecture:** Three sequential changes: (1) fix Python standby response to return HTTP 503, (2) implement real Go HTTP client posting snapshot JPEG bytes to `POST /predict` and parsing the response, (3) wire the new `"onnx"` case into `cmd/server/main.go`. The existing `hotset.Analyzer` interface, `Verdict` struct, and `CircuitBreaker` are unchanged — this plan only replaces the stub body of `CVEngineAnalyzer.Analyze`.

**Tech Stack:** Python 3.11 (cv-engine/server.py), Go stdlib `net/http`, `encoding/json`, `fmt`, `io`; no new Go dependencies.

**Spec:** https://github.com/529-studio/projectKV/issues/137

## Global Constraints

- `ANALYZER=onnx` valid only when `CV_ENGINE_URL` is set and non-empty; `Fatalf` otherwise.
- `Verdict.Source` must be `"onnx"` — never `"mock"` or `"vlm"`.
- No new Go dependencies beyond stdlib (approved list: gin, modernc/sqlite, nhooyr/websocket).
- `gofmt -l .` and `go vet ./...` must be clean before final commit.
- Zero emoji in all log statements — use `[INIT]`, `[WARN]`, `[ERROR]`, `[FATAL]` prefixes.
- All log output: English only (codebase standard).
- S1 capacity decision: Option 1b — `HOTSET_FIXED_MAX_CAMERAS=57`, `HOTSET_INTERVAL_SECONDS=30` defaults for `ANALYZER=onnx`.
- Branch: `feat/s3-vehicle-analyzer`; PR targets `dev`; do NOT merge.

## Review Focus

1. **Standby path still returns 200**: If `compiled_model is None` returns HTTP 200, Go client treats it as a real verdict. Test: Go client must return an error (not a verdict) when server responds `source: standby_dummy`.
2. **`CV_ENGINE_URL` not set with `ANALYZER=onnx`**: Server must `Fatalf` at startup, not panic at first call. Test: config validation in `main.go` `case "onnx"`.
3. **Non-200 from cv-engine on inference error**: Go client must propagate as error, not silently degrade. Test: mock 500 response → `Analyze` returns error.
4. **Snapshot fetch failure**: If proxy returns non-200 fetching the camera snapshot, `Analyze` must return `(SeverityUnknown verdict, error)`. Test: mock snapshot 404.
5. **Per-camera 10s context timeout**: The scheduler already passes a 10s context; the Go client must respect it. Test: mock slow server, context cancel before response → `Analyze` returns error.

---

## Task 1: Fix cv-engine standby response (Python)

**Files:**
- Modify: `services/cv-engine/server.py:161-171`

**Interfaces:**
- Produces: `POST /predict` returns HTTP 503 + `{"source":"standby_dummy","confidence":0.0,"mode":"standby_ready"}` when `compiled_model is None`.

- [ ] **Step 1:** In `server.py` `do_POST` standby block (lines ~161-171), change `self._send_json(200, ...)` to `self._send_json(503, {..., "source": "standby_dummy", "confidence": 0.0})`. Remove the fabricated `"traffic_severity": "CLEAR"` and `"confidence": 0.95` fields from the standby response.

- [ ] **Step 2:** Verify the /healthz response still includes `"model_loaded": false` when no model is present — no change needed, just confirm the health endpoint is unaffected.

- [ ] **Step 3:** Commit:
```bash
git add services/cv-engine/server.py
git commit -m "fix(cv-engine): return HTTP 503 on standby — no model loaded (ADR-004 Finding 1)"
```

---

## Task 2: Implement real `CVEngineAnalyzer.Analyze` in Go

**Files:**
- Modify: `services/go-server/internal/cvclient/cvengine.go` (replace stub body)
- Create: `services/go-server/internal/cvclient/cvengine_test.go`

**Interfaces:**
- Consumes: `hotset.Analyzer` interface — `Analyze(ctx context.Context, cam hotset.Camera) (hotset.Verdict, error)`
- Consumes: `cam.SnapshotURL` to fetch JPEG bytes; `a.url` as base URL for cv-engine
- Produces: `NewCVEngineAnalyzer(url string, httpClient *http.Client) *CVEngineAnalyzer` (add optional `httpClient` param for testability; if nil, use `SharedClient`)
- Produces: On success: `hotset.Verdict{Source: "onnx", Severity: ..., Confidence: ..., CameraID: cam.ID, Timestamp: time.Now()}`
- Produces: On standby (HTTP 503 OR `source == "standby_dummy"`): return `(hotset.Verdict{Severity: hotset.SeverityUnknown, ...}, error("cv-engine standby: no model loaded"))`
- Produces: On non-200 from cv-engine: return error wrapping status code

**cv-engine predict response schema (success):**
```json
{"vehicles_detected": N, "traffic_severity": "CLEAR|MODERATE|HEAVY|CONGESTED", "confidence": 0.88, "latency_ms": 12.3, "model": "yolov8n_openvino_320"}
```

**Severity mapping from response:** use `traffic_severity` field directly (already matches `hotset.Severity*` constants).

- [ ] **Step 1: Write failing tests** in `cvengine_test.go`:

```go
// TestCVEngineAnalyzer_Analyze_Success: mock http server returns 200 with
// valid predict response → verdict has Source="onnx", Severity="MODERATE", Confidence=0.88, CameraID="cam-1", no error.

// TestCVEngineAnalyzer_Analyze_StandbyRejected: mock returns 503 →
// Analyze returns non-nil error; verdict.Severity == hotset.SeverityUnknown.

// TestCVEngineAnalyzer_Analyze_SnapshotFetchFails: snapshot endpoint returns 404 →
// Analyze returns non-nil error; verdict.Severity == hotset.SeverityUnknown.

// TestCVEngineAnalyzer_Analyze_CVEngineNon200: cv-engine returns 500 →
// Analyze returns non-nil error containing "500".

// TestCVEngineAnalyzer_Analyze_ContextCancelled: context already cancelled →
// Analyze returns non-nil error; verdict.Severity == hotset.SeverityUnknown.
```

- [ ] **Step 2: Run tests to confirm they fail**
```bash
cd services/go-server && go test ./internal/cvclient/... -run TestCVEngineAnalyzer -v
```
Expected: compilation error or FAIL.

- [ ] **Step 3: Implement `CVEngineAnalyzer` in `cvengine.go`**

Replace the stub with real logic:
1. Add `client *http.Client` field to `CVEngineAnalyzer`; `NewCVEngineAnalyzer(url string, client *http.Client)` — if `client == nil`, use `SharedClient`.
2. `Analyze`:  
   a. Build snapshot URL from `cam.SnapshotURL` (already a full URL — use as-is).  
   b. `GET cam.SnapshotURL` with `ctx`; if non-200 or error → return `SeverityUnknown, error`.  
   c. Read body as `[]byte` (JPEG frame).  
   d. `POST a.url+"/predict"` with `Content-Type: application/octet-stream` body = JPEG bytes, with `ctx`.  
   e. If HTTP 503 OR parsed `source == "standby_dummy"` → return `SeverityUnknown, error("cv-engine standby: no model loaded")`.  
   f. If non-200 → return `SeverityUnknown, fmt.Errorf("cv-engine /predict: status %d", statusCode)`.  
   g. Parse JSON response into local struct `cvPredictResponse{TrafficSeverity, Confidence, VehiclesDetected, LatencyMs, Model string/float/int}`.  
   h. Build and return `hotset.Verdict{CameraID: cam.ID, Severity: resp.TrafficSeverity, Confidence: resp.Confidence, Source: "onnx", Timestamp: time.Now()}`.

- [ ] **Step 4: Run tests to confirm they pass**
```bash
cd services/go-server && go test ./internal/cvclient/... -v -race
```
Expected: all pass.

- [ ] **Step 5: Format and vet**
```bash
cd services/go-server && gofmt -l . && go vet ./internal/cvclient/...
```
Expected: no output.

- [ ] **Step 6: Commit**
```bash
git add services/go-server/internal/cvclient/cvengine.go services/go-server/internal/cvclient/cvengine_test.go
git commit -m "feat(cvclient): implement real CVEngineAnalyzer HTTP client for OpenVINO backend"
```

---

## Task 3: Wire `ANALYZER=onnx` in `cmd/server/main.go` + config defaults

**Files:**
- Modify: `services/go-server/internal/config/config.go` — add `onnx` defaults for `HotsetIntervalSeconds=30`, `HotsetMaxParallel=10`, `HotsetFixedMaxCameras=57`
- Modify: `services/go-server/cmd/server/main.go` — add `case "onnx":` branch

**Interfaces:**
- Consumes: `cvclient.NewCVEngineAnalyzer(cfg.CVEngineURL, nil)` (Task 2)
- Consumes: `cfg.CVEngineURL` (already in `ServerConfig` at `config.go:22`)
- Produces: `ANALYZER=onnx` starts the scheduler with `CVEngineAnalyzer`

- [ ] **Step 1:** In `config.go` `LoadConfig()`, in the `hotsetIntervalSeconds/hotsetMaxParallel/hotsetFixedMaxCameras` sections, add `else if analyzer == "onnx"` branches:
  - `hotsetIntervalSeconds = 30` (S1 Option 1b: 25.1s per cycle, 16% headroom)
  - `hotsetMaxParallel = 10`
  - `hotsetFixedMaxCameras = 57` (S1 Option 1b: 4 corridors = 57 cameras)

- [ ] **Step 2:** In `main.go` switch on `cfg.Analyzer`, add after `case "vlm":`:
```go
case "onnx":
    if cfg.CVEngineURL == "" {
        log.Fatalf("[FATAL] Refusing to start: ANALYZER=onnx requires CV_ENGINE_URL to be set.")
    }
    analyzer = cvclient.NewCVEngineAnalyzer(cfg.CVEngineURL, nil)
    log.Printf("[INIT] ANALYZER=onnx active (url=%s, interval=%ds, maxParallel=%d, fixedCameras=%d). Hot Set scheduler will run OpenVINO inference.",
        cfg.CVEngineURL, cfg.HotsetIntervalSeconds, cfg.HotsetMaxParallel, cfg.HotsetFixedMaxCameras)
```

- [ ] **Step 3:** Update the existing fatal log for unknown analyzer to include `onnx`:
```go
log.Fatalf("[FATAL] Unknown ANALYZER=%q (valid options: none, mock, vlm, onnx)", cfg.Analyzer)
```

- [ ] **Step 4:** Build to confirm compilation succeeds:
```bash
cd services/go-server && go build ./...
```

- [ ] **Step 5:** Run full test suite with race detector:
```bash
cd services/go-server && go test ./... -race
```
Expected: all pass.

- [ ] **Step 6:** Format and vet:
```bash
cd services/go-server && gofmt -l . && go vet ./...
```
Expected: no output.

- [ ] **Step 7:** Commit:
```bash
git add services/go-server/internal/config/config.go services/go-server/cmd/server/main.go
git commit -m "feat(go-server): wire ANALYZER=onnx with CVEngineAnalyzer and S1-1b capacity defaults"
```

---

## Task 4: Update docker-compose.stg.yml — mount model volume

**Files:**
- Modify: `docker-compose.stg.yml` — add named volume mount for model weights into `warded-model`

**Interfaces:**
- Produces: `warded-model` service reads model from `/app/models/yolov8n_openvino_320/` (already `MODEL_DIR` env default)

- [ ] **Step 1:** Add a named volume `warded-model-weights` to `docker-compose.stg.yml` under `volumes:` section.

- [ ] **Step 2:** Mount it in `warded-model` service:
```yaml
volumes:
  - warded-model-weights:/app/models
```

- [ ] **Step 3:** Document in a comment: model IR files (`yolov8n.xml` + `yolov8n.bin`) must be pre-populated into the volume before first run. The volume name and mount path must match `MODEL_DIR=/app/models/yolov8n_openvino_320`.

- [ ] **Step 4:** Commit:
```bash
git add docker-compose.stg.yml
git commit -m "feat(compose): add named volume for OpenVINO model weights in warded-model"
```

---

## Task 5: Architecture Guardian Review + PR

**Files:**
- No code change — verification and PR creation only.

- [ ] **Step 1:** Run architecture guardian checklist:
  - Import graph: `cvclient` imports `hotset` and `camera` (allowed per graph). No new imports introduced.
  - Banned deps: `go.mod` unchanged — no new deps.
  - Approved libs: stdlib only in new code.
  - `gofmt -l .` → clean.
  - `go vet ./...` → clean.
  - No new HTTP endpoints added → Swagger sync not required.

- [ ] **Step 2:** Run full test suite one final time:
```bash
cd services/go-server && go test ./... -race
```
Expected: all pass.

- [ ] **Step 3:** Push branch and open PR:
```bash
git push -u origin feat/s3-vehicle-analyzer
gh pr create --base dev --head feat/s3-vehicle-analyzer \
  --title "feat(go-server + cv-engine): S3 self-hosted vehicle analyzer (OpenVINO) — closes #137" \
  --body "..."
```

- [ ] **Step 4:** Do NOT merge. Leave PR open for human review.
