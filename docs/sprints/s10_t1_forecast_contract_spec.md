# S10-T1 (#372) Forecast Contract Specification

Status: implementation-ready, awaiting T1 coding.
Owner: Technical Product Owner. Implementation owner: backend.
Governing: [`ADR-012`](../ADR-012-sprint-10-architecture.md) D2 and D5, [Rule 3](../../AGENTS.md) estimates, [L66](../lessons/L66-self-referential-tests-and-silent-fallbacks-hide-the-live-path.md), [L67](../lessons/L67-upsert-only-state-tables-cannot-feed-time-series-models.md).
Supersedes: `GET /api/forecast`, `GET /forecast`, `forecast.Response`, `forecast.Request.HorizonMinutes`.

## 1. Decisions taken in this spec

| # | Decision | Why |
|---|---|---|
| D1 | `availability` reports payload readiness, not model trust. Under `FORECAST_ALLOW_SYNTHETIC=true` the response is `200` with `availability` `ok` or `stale`, `experimental=true` and `model.validated=false`. | A hatch that returns a non-renderable 200 is unusable. Trust stays visible in `model.validated` and `experimental`, and production cannot set the hatch. |
| D2 | The gate covers all three models, not only the learned one. | All three report metrics from the same synthetic backtest. |
| D3 | Serving requires `MinServingSamples = 4` (current plus three lags); below that, `422 insufficient_history`. | `BuildFeatures` interpolates missing lags, so fewer samples silently fabricate confidence. |
| D4 | Provenance is per model. The default artifact is compiled in and labelled synthetic; `FORECAST_MODEL_PATH` loads a live artifact and fails boot if unverifiable. | ADR-012 D2 requires provenance; a configured path must never fall back silently (L66). |
| D5 | One route only. `/forecast`, `/api/forecast` and `/api/cameras/:id/forecast` return `404`. | Two routes were the F7 defect and no consumer exists yet. |
| D6 | `horizon` leaves the request and `horizons[]` enters the response; `?horizon=` returns `400`. | Asking for one horizon and receiving three was a silent redefinition. |

## 2. Route and parameter contract

| Item | Value |
|---|---|
| Route | `GET /api/v1/cameras/:id/forecast`, mounted as `rg.GET("/v1/cameras/:id/forecast", ...)` inside the `/api` group |
| `:id` | Camera ID from the catalog. Unknown → `404 not_found`. |
| `model` | Optional. One of `ridge_delta` (default), `persistence`, `historical_median`. Anything else → `400`. |
| `timestamp` | Optional RFC3339 / RFC3339Nano base time, default now (UTC). Invalid or more than 60 s in the future → `400`. |
| Removed | `camera_id`, `segment_id`, `node_id`, `edge_id` (now the path segment), `horizon`, `horizon_minutes` (now always all three horizons). |
| Retired routes | `GET /forecast`, `GET /api/forecast`, and the never-shipped `GET /api/cameras/:id/forecast` all return `404`. |

`horizon`/`horizon_minutes` are the only rejected parameters; other unknowns are ignored API-wide.

## 3. Exact Go types

Types live in `service.go` (transport) and `artifact.go` (provenance).

### 3.1 Enums

```go
type AvailabilityStatus string

const (
	AvailabilityOK                  AvailabilityStatus = "ok"
	AvailabilityInsufficientHistory AvailabilityStatus = "insufficient_history"
	AvailabilityModelUnvalidated    AvailabilityStatus = "model_unvalidated"
	AvailabilityStale               AvailabilityStatus = "stale"
)

type FitSource string

const (
	FitSourceLiveHistory      FitSource = "live_history"
	FitSourceSyntheticFixture FitSource = "synthetic_fixture"
)
```

`FitSource` has two members: a model with no fitted parameters is not a third case, because `persistence` still reports synthetic metrics.

### 3.2 Prediction payload
```go
type ForecastMultiResponse struct {
	CameraID     string              `json:"camera_id"`
	RoadKey      string              `json:"road_key"`
	GeneratedAt  string              `json:"generated_at"`
	BaseTime     string              `json:"base_time"`
	Availability AvailabilityStatus  `json:"availability"`
	Experimental bool                `json:"experimental"`
	Current      CurrentState        `json:"current"`
	Horizons     []HorizonPrediction `json:"horizons"`
	Model        ModelProvenance     `json:"model"`
	SourceData   SourceData          `json:"source_data"`
}

type CurrentState struct {
	ObservedAt      string  `json:"observed_at"`
	SaturationIndex float64 `json:"saturation_index"`
	Severity        string  `json:"severity"`
	SpeedKmh        float64 `json:"speed_kmh"`
	Confidence      float64 `json:"confidence"`
}

type HorizonPrediction struct {
	HorizonMinutes           int                 `json:"horizon_minutes"`
	TargetTime               string              `json:"target_time"`
	PredictedSaturationIndex float64             `json:"predicted_saturation_index"`
	PredictedSeverity        string              `json:"predicted_severity"`
	PredictedSpeedKmh        float64             `json:"predicted_speed_kmh"`
	Confidence               float64             `json:"confidence"`
	ConfidenceIntervals      ConfidenceIntervals `json:"confidence_intervals"`
}
```

`ConfidenceIntervals` and `Interval` keep their current shape (`saturation_index` / `speed_kmh` with `lower_95` / `upper_95`).

Service-enforced invariants: exactly three horizons ordered `10, 20, 30`; `target_time = base_time + horizon_minutes`; SI `>= 0`; speed in `[3.0, 45.0]`; each 95 % interval brackets its point estimate. `confidence` is per horizon: `{1.00, 0.95, 0.90}` times observation confidence (the `0.99` is the upper clamp, not a multiplier), halved when stale, clamped to `[0.10, 0.99]`.

`segment_id` is removed: it was always a copy of `camera_id` and should return only with a real segment entity (L09).

### 3.3 Provenance

```go
type FitWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type HorizonMetric struct {
	HorizonMinutes int     `json:"horizon_minutes"`
	MAE            float64 `json:"mae"`
	RMSE           float64 `json:"rmse"`
	SampleCount    int     `json:"sample_count"`
}

type ModelProvenance struct {
	Name           string          `json:"name"`
	Version        string          `json:"version"`
	TargetMode     string          `json:"target_mode"`
	FitSource      FitSource       `json:"fit_source"`
	Validated      bool            `json:"validated"`
	FitRows        int             `json:"fit_rows"`
	FitCameras     int             `json:"fit_cameras"`
	FitWindow      *FitWindow      `json:"fit_window"`
	FitDataSHA256  string          `json:"fit_data_sha256"`
	ArtifactSHA256 string          `json:"artifact_sha256"`
	MAE            float64         `json:"mae"`
	Metrics        []HorizonMetric `json:"metrics"`
}
```

- `Validated` is derived, never stored: `FitSource == FitSourceLiveHistory`. It is the only input to the serving gate.
- `FitWindow` is a pointer so an artifact without a documented range serializes `null` instead of a fake epoch.
- `fit_data_sha256` hashes the fitted dataset. `artifact_sha256` hashes the coefficient and metric material: SHA-256 of the compact `encoding/json` serialization of the entry's `horizons` array.
- `mae` is the **maximum** MAE across served horizons, so one scalar cannot overstate quality; per-horizon values live in `metrics`.

### 3.4 Source data

```go
type SourceData struct {
	SampleCount    int     `json:"sample_count"`
	WindowMinutes  int     `json:"window_minutes"`
	LastObservedAt string  `json:"last_observed_at"`
	AgeMinutes     float64 `json:"age_minutes"`
	Stale          bool    `json:"stale"`
}
```

`AgeMinutes` is `base_time - last_observed_at`, rounded to one decimal and floored at `0`. `WindowMinutes` is the query window (`45`). `Stale` is `AgeMinutes > 15`, and it remains the authoritative staleness carrier even when `availability` reports a higher-precedence state (see §4).

### 3.5 Errors

```go
type ErrorKind string

const (
	KindInvalidParameter   ErrorKind = "invalid_parameter"
	KindNotFound           ErrorKind = "not_found"
	KindInsufficientHistory ErrorKind = "insufficient_history"
	KindModelUnvalidated   ErrorKind = "model_unvalidated"
	KindServiceUnavailable ErrorKind = "service_unavailable"
)

type Requirements struct {
	MinObservations       int `json:"min_observations"`
	ObservedSamples       int `json:"observed_samples"`
	RequiredWindowMinutes int `json:"required_window_minutes"`
}

type ErrorResponse struct {
	Error        ErrorKind          `json:"error"`
	Message      string             `json:"message"`
	Availability AvailabilityStatus `json:"availability,omitempty"`
	Requirements *Requirements      `json:"requirements,omitempty"`
	Model        *ModelProvenance   `json:"model,omitempty"`
}

type ForecastError struct {
	Kind         ErrorKind
	Message      string
	Requirements *Requirements
	Model        *ModelProvenance
	Err          error
}

func (e *ForecastError) Error() string
func (e *ForecastError) Unwrap() error
```

`availability` appears only for the two availability-shaped failures and MUST equal `error` for them. `model` accompanies `model_unvalidated` so an operator sees `fit_source` and the SHA; `requirements` accompanies `insufficient_history` so the UI can state the data requirement. `handler.go` maps `Kind` with one switch: `invalid_parameter` 400, `not_found` 404, `insufficient_history` 422, `model_unvalidated` and `service_unavailable` 503. An error that is not a `*ForecastError` maps to 500 `internal_error`, so a bug can never masquerade as a retryable outage. `experimental` is not part of the error envelope; it is a 200-only field.

The sentinel errors `ErrCameraNotFound`, `ErrInvalidHorizon`, `ErrInvalidModel` and `ErrInsufficientHistory` fold into `ForecastError`. `ErrInvalidCameraID` disappears: an empty path segment is not routable.

### 3.6 Artifact

```go
type ModelArtifact struct {
	SchemaVersion int          `json:"schema_version"`
	Models        []ModelEntry `json:"models"`
}

type ModelEntry struct {
	Name           string         `json:"name"`
	Version        string         `json:"version"`
	TargetMode     string         `json:"target_mode"`
	FitSource      FitSource      `json:"fit_source"`
	FitRows        int            `json:"fit_rows"`
	FitCameras     int            `json:"fit_cameras"`
	FitWindow      *FitWindow     `json:"fit_window"`
	FitDataSHA256  string         `json:"fit_data_sha256"`
	ArtifactSHA256 string         `json:"artifact_sha256"`
	GlobalMedian   float64        `json:"global_median,omitempty"`
	Horizons       []HorizonModel `json:"horizons"`
}

type HorizonModel struct {
	HorizonMetric
	Params *ModelParams `json:"params,omitempty"`
}
```

`ModelParams` gains JSON tags and remains the only representation of the coefficients; `service.go` stops duplicating the report's metrics. Loader contract in `artifact.go`:

```go
func EmbeddedSyntheticArtifact() ModelArtifact  // default, fit_source=synthetic_fixture
func LoadArtifact(path string) (ModelArtifact, error) // "" returns EmbeddedSyntheticArtifact()
func (a ModelArtifact) Validate() error          // exactly 3 models, 3 horizons each, SHA verified
func (a ModelArtifact) Entry(name string) (ModelEntry, bool)
func (e ModelEntry) Provenance() ModelProvenance
```

`Validate` rejects unknown model names, duplicate or out-of-range horizons, an `artifact_sha256` mismatch, a `live_history` entry with `fit_rows == 0` or a nil `fit_window`, and a synthetic entry claiming `validated`. These are boot failures, not request errors.

Embedded artifact values, from [`forecast_prototype_v1.json`](../reports/forecast_prototype_v1.json): `version` `2026.11.05-synthetic`, `fit_source` `synthetic_fixture`, `fit_rows` `1152`, `fit_cameras` `2`, `fit_window` `2026-10-28T00:00:00Z` → `2026-10-29T23:55:00Z`, `fit_data_sha256` `1a54fd12319e0f936f1b67dcf61025d73ee8d88aecae131327b637674ea4d840`.

Per-horizon metrics for the three entries (MAE / RMSE):

| Horizon | `ridge_delta` | `persistence` | `historical_median` |
|---:|---|---|---|
| 10 | 0.00440486 / 0.00508631 | 0.00917440 / 0.01110912 | 0.02354196 / 0.02766057 |
| 20 | 0.01016163 / 0.01172547 | 0.01837405 / 0.02220334 | 0.02359264 / 0.02774466 |
| 30 | 0.01724040 / 0.01989593 | 0.02747778 / 0.03316360 | 0.02345770 / 0.02757248 |

All three entries carry `sample_count: 342` and `fit_source: synthetic_fixture`.
### 3.7 Service surface

```go
const (
	HistoryWindow     = 45 * time.Minute
	MaxHistoryRows    = 50
	MinServingSamples = 4
	StaleAfterDefault = 15 * time.Minute
)

type Request struct {
	CameraID string
	BaseTime time.Time
	Model    string
}

type Options struct {
	Artifact       ModelArtifact
	AllowSynthetic bool
	StaleAfter     time.Duration
}

func NewService(history HistoryReader, catalog CameraCatalog, opts Options) (*Service, error)
func (s *Service) PredictMulti(ctx context.Context, req Request) (*ForecastMultiResponse, error)
```

`NewService` errors on an empty artifact, a missing model, `StaleAfter <= 0` or a nil history; no constructor default substitutes an artifact (L66). One serving path remains.

## 4. Availability state machine

Evaluation order in `PredictMulti`:

| # | Condition | HTTP | `availability` | `experimental` | Extra body |
|---:|---|---:|---|---|---|
| 1 | Invalid `model` / `timestamp` / retired `horizon` | 400 | absent | false | `error=invalid_parameter` |
| 2 | Camera not in catalog | 404 | absent | false | `error=not_found` |
| 3 | `entry.Validated == false` and `AllowSynthetic == false` | 503 | `model_unvalidated` | false | `model` provenance |
| 4 | `entry.Validated == false` and `AllowSynthetic == true` | 200 | `ok`, or `stale` per #6 | true | full payload, `model.validated=false` |
| 5 | Fewer than 4 observations in 45 m | 422 | `insufficient_history` | absent (error envelope) | `requirements` |
| 6 | Last observation older than `StaleAfter` | 200 | `stale` | as #4 | full payload, confidence halved |
| 7 | Otherwise | 200 | `ok` | as #4 | full payload |

Notes that an implementer must not "fix" later:

- The gate runs before the history query: an outage cannot masquerade as `insufficient_history` and no DB work happens while the endpoint fails closed.
- `availability` answers "may this payload be rendered", not "is the model trusted". A hatch-open 200 is renderable with `experimental=true` and `model.validated=false`; there is no 200 `model_unvalidated` state. `stale` still wins over the hatch because it describes the data being drawn.
- `insufficient_history` never coexists with `model_unvalidated`.

Client contract for T6: render predictions when `availability` is `ok` or `stale`; show the experimental badge when `experimental=true`; treat `insufficient_history` and any 503 as non-renderable. `stale` dims the series and uses the halved confidence, per ADR-012 D5.

## 5. Fail-closed synthetic gate

```go
prov := entry.Provenance()
if !prov.Validated {
	if !s.allowSynthetic {
		return nil, &ForecastError{Kind: KindModelUnvalidated,
			Message: "forecast model was not fitted on live traffic history", Model: &prov}
	}
	experimental, availability = true, AvailabilityModelUnvalidated
}
```

Configuration in `internal/config`:

| Variable | Type | Default | Rule |
|---|---|---|---|
| `FORECAST_ALLOW_SYNTHETIC` | bool | `false` | `parseEnvBool`. Development escape hatch only. |
| `FORECAST_MODEL_PATH` | string | `""` | Empty means the embedded synthetic artifact. A non-empty value that cannot be read or verified is fatal at boot. |
| `FORECAST_STALE_TTL_SECONDS` | int | `900` | Three 5-minute feature bins. Not `VERDICT_STALE_TTL_SECONDS`, which measures 30 s verdict freshness. |

Boot guard in `main.go`: `log.Fatalf` when `AppEnv` is `staging` or `production` and `ForecastAllowSynthetic` is true — the hatch is not a deployment option. In `dev` it logs one `[WARN]` naming the variable. No compose file gains it; `.env.example` gets a commented `# FORECAST_ALLOW_SYNTHETIC=false`.

## 6. Worked payloads

### 6.1 `200 ok`

Base `2026-11-06T08:30:00Z`; observations at `-1/-6/-11/-16` m with SI `0.45/0.42/0.40/0.38`; `live_history` artifact; `ridge_delta`:

```json
{
  "camera_id": "cam-abc123",
  "road_key": "vo_van_kiet",
  "generated_at": "2026-11-06T08:30:00.412Z",
  "base_time": "2026-11-06T08:30:00Z",
  "availability": "ok",
  "experimental": false,
  "current": {"observed_at": "2026-11-06T08:29:00Z", "saturation_index": 0.45, "severity": "MODERATE", "speed_kmh": 29.0, "confidence": 0.9},
  "horizons": [
    {"horizon_minutes": 10, "target_time": "2026-11-06T08:40:00Z", "predicted_saturation_index": 0.4742, "predicted_severity": "MODERATE", "predicted_speed_kmh": 27.5, "confidence": 0.9, "confidence_intervals": {"saturation_index": {"lower_95": 0.4643, "upper_95": 0.4842}, "speed_kmh": {"lower_95": 26.9, "upper_95": 28.1}}},
    {"horizon_minutes": 20, "target_time": "2026-11-06T08:50:00Z", "predicted_saturation_index": 0.4968, "predicted_severity": "MODERATE", "predicted_speed_kmh": 26.2, "confidence": 0.86, "confidence_intervals": {"saturation_index": {"lower_95": 0.4738, "upper_95": 0.5197}, "speed_kmh": {"lower_95": 24.8, "upper_95": 27.6}}},
    {"horizon_minutes": 30, "target_time": "2026-11-06T09:00:00Z", "predicted_saturation_index": 0.5161, "predicted_severity": "MODERATE", "predicted_speed_kmh": 25.0, "confidence": 0.81, "confidence_intervals": {"saturation_index": {"lower_95": 0.4771, "upper_95": 0.5551}, "speed_kmh": {"lower_95": 22.7, "upper_95": 27.4}}}
  ],
  "model": {
    "name": "ridge_delta", "version": "2026.11.05-live", "target_mode": "delta",
    "fit_source": "live_history", "validated": true, "fit_rows": 8640, "fit_cameras": 57,
    "fit_window": {"start": "2026-11-01T00:00:00Z", "end": "2026-11-04T00:00:00Z"},
    "fit_data_sha256": "<sha256>", "artifact_sha256": "<sha256>", "mae": 0.0172404,
    "metrics": ["…three HorizonMetric objects, values in §3.6…"]
  },
  "source_data": {"sample_count": 4, "window_minutes": 45, "last_observed_at": "2026-11-06T08:29:00Z", "age_minutes": 1.0, "stale": false}
}
```

These are real outputs of the current coefficients; use `1e-4` tolerance, not byte equality after a JSON round trip.

### 6.2 `200 stale`

Same inputs, base time +20 m: `availability: "stale"`, `source_data.stale: true`, `age_minutes: 21.0`, confidence `0.45 / 0.43 / 0.41`. Predictions and intervals are unchanged.

### 6.3 `503 model_unvalidated` (default config, embedded artifact)

```json
{"error": "model_unvalidated", "message": "forecast model was not fitted on live traffic history",
 "availability": "model_unvalidated",
 "model": {"name": "ridge_delta", "version": "2026.11.05-synthetic", "fit_source": "synthetic_fixture",
   "validated": false, "fit_rows": 1152, "fit_cameras": 2, "mae": 0.0172404, "metrics": ["…three entries…"]}}
```

`requirements` is absent. With `FORECAST_ALLOW_SYNTHETIC=true` the same request returns `200` with the §6.1 body, `availability: "ok"` (or `"stale"`), `experimental: true` and `model.validated: false`.

### 6.4 `422 insufficient_history`

```json
{"error": "insufficient_history", "message": "fewer than 4 observations in the trailing 45 minutes",
 "availability": "insufficient_history",
 "requirements": {"min_observations": 4, "observed_samples": 2, "required_window_minutes": 45}}
```

## 7. File-by-file changes

### 7.1 `internal/forecast/handler.go`

- `RegisterRoutes`: `rg.GET("/v1/cameras/:id/forecast", h.GetCameraForecast)`. Delete `RegisterRootRoutes`.
- Rename `GetForecast`; read `id := c.Param("id")`; delete the four query aliases. Reject `horizon`/`horizon_minutes` with `400` (D6). Keep `model` and `timestamp` parsing.
- Replace the `errors.Is` switch with `errors.As(err, &fe)` on `*ForecastError`, mapping `Kind` as in §3.5. Every failure path, including handler-raised 400s, serializes `ErrorResponse`. Set `Cache-Control: no-store` on every response.
- Swagger: path parameter `id`, `@Success 200 {object} forecast.ForecastMultiResponse`, all failures `{object} forecast.ErrorResponse`, `@Router /api/v1/cameras/{id}/forecast [get]`.

### 7.2 `internal/forecast/service.go`

- Delete `Response`, `ModelMetadata`, `Request.HorizonMinutes`, `Predict`, and the per-horizon MAE/RMSE literals in the persistence and median branches. Add the §3.2-§3.5 types, `ErrorKind` and `ForecastError`; `NewService` takes `Options` and applies the §3.7 validations.
- `PredictMulti` order: validate model → camera lookup → synthetic gate → history query (`GetRecentByCameraBefore`, 45 m, 50 rows) → sample floor → staleness → one `HorizonModel` per horizon ascending → assemble `SourceData` and `ModelProvenance` → one log line with `camera_id`, `availability`, `fit_source`, `validated`, `experimental`, horizon SIs. A nil reader or query error becomes `KindServiceUnavailable`; it must not fall through to the 422 path.
- Coefficients and metrics come only from `s.artifact`. `CalibratedRidgeParams` moves to `artifact.go`, unexported and reachable only through `EmbeddedSyntheticArtifact`; nothing else may rebuild it.
- Rounding: SI 4 dp, speed 1 dp, confidence 2 dp, MAE 6 dp, age 1 dp.

### 7.3 `internal/forecast/artifact.go` (new)

- The §3.6 types plus `FitSource`, `EmbeddedSyntheticArtifact`, `LoadArtifact`, `Validate`, `Entry`, `Provenance`.
- Loader: read → `json.Unmarshal` → `Validate` → recompute each `ArtifactSHA256` from `json.Marshal(entry.Horizons)` and compare. An empty path returns the embedded artifact through the same validation path (L66).
- The embedded metrics come from the §3.6 table, transcribed from the prototype report, not from `service.go`.

### 7.4 `internal/config/config.go`

- Add `ForecastAllowSynthetic bool`, `ForecastModelPath string`, `ForecastStaleTTLSeconds int` to both config structs and their constructors; parse with `parseEnvBool("FORECAST_ALLOW_SYNTHETIC", false)`, `parseEnvString("FORECAST_MODEL_PATH", "")`, `parseEnvInt("FORECAST_STALE_TTL_SECONDS", 900, 60)`. Cover defaults and quoted values in table-driven tests.

### 7.5 `cmd/server/main.go`

- Delete the root-alias block (`forecastHandler.RegisterRootRoutes(router)`, around line 1147) and its comment. `RegisterRoutes(api)` now mounts `/api/v1/cameras/:id/forecast`.
- Build the handler whenever `camCatalog != nil`, not only when `verdictHistoryRepo != nil`. A missing SQLite repository must answer 503 `service_unavailable`; skipping registration turns an infrastructure fault into a 404 that looks like a removed route.
- Inside that block: load and validate the artifact, apply the staging/production guard, build the service with `Options`, fatal on any error, and log one `[INIT]` line with `version`, `fit_source`, `validated` and the first 12 hex characters of `artifact_sha256`.

### 7.6 Tests

- Rewrite `handler_test.go` around `NewService` with an injected live artifact; add `testdata/live_artifact.json` whose hashes the loader produces, never hand-written. Remove assertions on `segment_id`, top-level `confidence` and `?horizon=`, and add 404 cases for all three retired paths. In `cmd/server/server_test.go`, assert the canonical route is registered and the legacy paths are not.

### 7.7 Regeneration and docs

- `make swagger` and commit the three generated files. `README.md`: replace "No endpoint or UI is exposed" with the canonical route, the four availability states and the dev-only caveat. `docs/reports/forecast_prototype_v1.md`: one line stating that `artifact.go` carries this report's provenance and is gated.

## 8. Test matrix and Definition of Done

| ID | Scenario | Expected |
|---|---|---|
| G1 | Live artifact, 4 fresh observations, `?model=ridge_delta` | 200, `ok`, three horizons ordered, intervals bracket the point estimate, `validated=true` |
| G2 | Same, base time +20 m | 200, `stale`, `source_data.stale=true`, confidence halved |
| G3 | Embedded artifact, default config | 503, `model_unvalidated`, `model.fit_source=synthetic_fixture` |
| G4 | Embedded artifact, `AllowSynthetic=true` | 200, `experimental=true`, `availability=ok`, `model.validated=false` |
| G5 | Two observations | 422, `requirements.observed_samples=2` |
| G6 | Zero observations / repository error | 422 / 503 with the matching kind |
| G7 | Unknown camera | 404 `not_found` |
| G8 | `?horizon=10`, `?model=lstm`, bad `timestamp` | 400 `invalid_parameter` |
| G9 | `GET /forecast`, `GET /api/forecast` | 404 |
| G10 | Tampered `artifact_sha256`, missing model, `live_history` with 0 rows | `Validate`/`LoadArtifact` error |
| G11 | Golden JSON schema test | Exact key set, types, enum literals; numeric values within tolerance |
| G12 | `go test -race ./...` and `make test-forecast` | Green |

Manual verification (Rule 10) is split: V1-V12 in [`s10_t1_offline_verification.md`](s10_t1_offline_verification.md) are provable locally; `200 ok` against real history is transferred to `#375`. Never claim the `ok` path from a fixture artifact.

## 9. Out of scope

- The batch `GET /api/v1/cameras/hotset/forecast` route (T6).
- The 60 m / 80 % coverage gate, history integrity and boot schema assertion (T4); `MinServingSamples` is the temporary floor and the seam T4 replaces.
- Refitting and publishing a `live_history` artifact, including the Docker `COPY` for `FORECAST_MODEL_PATH` (T4).

## 10. Estimate reconciliation

The issue's 8-10 h did not price D4 or the golden test. Breakdown: `artifact.go` loader and hash verification 2.5 h; `service.go` rewrite 2.5 h; `handler.go` and envelope 1.5 h; config and main 1.0 h; tests G1-G12 plus fixture 2.5 h; Swagger, docs and manual scenarios 1.5 h. **Total 11.5 h**, recommended as 10-12 h (2 working days), Nov 09-10.

If the deadline is immovable, descope the loader (about 2 h) and defer `FORECAST_MODEL_PATH` plus hash verification to T4. Never descope the gate, enum, route deletion or golden contract test.
