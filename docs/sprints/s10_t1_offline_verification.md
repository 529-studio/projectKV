# S10-T1 (#372) Offline Verification Plan

Applies to [#372](https://github.com/529-studio/projectKV/issues/372) while the
remote infra worker node is offline. Companion to
[`s10_t1_forecast_contract_spec.md`](s10_t1_forecast_contract_spec.md), which remains
the contract authority. This file only narrows what may be claimed as evidence.

## 1. Constraint

The remote infra worker node is offline (power outage). Implementation happens on a local Mac.
Consequences that matter to the Definition of Done:

- No populated `hotset_verdict_history` for the 57 catalogued cameras, no harvester
  ticks, no staging instance, no compose stack.
- A `fit_source=live_history` artifact cannot be produced, so `200` with `availability=ok` and `validated=true` cannot be observed against real traffic. A hatch-open 200 is observable but is `experimental=true`, which is not the same claim.
- Everything fail-closed is fully observable locally, because the gate runs before any
  database read and the default configuration rejects the embedded artifact.

## 2. Evidence this machine can produce

Each row must be captured on a running local server or a `go test -race` run, with the
command and response body recorded in the PR description.

| ID | Scenario | Expected | Method |
|---|---|---|---|
| V1 | Route surface | `GET /api/v1/cameras/<id>/forecast` reaches the handler; `/forecast`, `/api/forecast`, `/api/cameras/<id>/forecast` all 404 | curl + route-tree test |
| V2 | Route tree intact | `/api/cameras/hotset/status`, `/api/cameras/<id>` and `/api/cameras/snapshot/<id>` still resolve after registration | route-tree test asserts all four paths |
| V3 | Default boot, embedded artifact | 503 `model_unvalidated`, `model.fit_source=synthetic_fixture`, no `current`/`horizons` keys present | curl |
| V4 | Zero DB work on the failing path | History reader called 0 times | stub reader with a call counter |
| V5 | Override boot | `FORECAST_ALLOW_SYNTHETIC=true` returns 200, `experimental=true`, `availability=ok` (or `stale`), `model.validated=false`, three horizons ordered | curl |
| V6 | Boot guard | `APP_ENV=staging FORECAST_ALLOW_SYNTHETIC=true` exits non-zero before listening | process exit assertion |
| V7 | Parameter failures | unknown camera 404, `?horizon=10` 400, `?model=lstm` 400, bad `timestamp` 400 | curl |
| V8 | Empty history with override | 422 `insufficient_history` and `requirements.observed_samples` present | curl + seeded local SQLite |
| V9 | Loader negatives | tampered `artifact_sha256`, missing model, `live_history` with 0 rows all fail at boot | unit test |
| V10 | Deployment hygiene | no staging/production compose file contains `FORECAST_ALLOW_SYNTHETIC`; `.env.example` carries it commented | grep |
| V11 | Swagger | exactly one forecast path, four schemas | `/swagger` inspection |
| V12 | Suite | `go test -race ./internal/forecast/... ./cmd/server/...` green; `make test-forecast` green | CI or local |

Note: `make test-forecast` runs only the Python toolchain under `tools/forecast`. The Go
package tests are a separate, mandatory command; treating `make test-forecast` as Go
evidence is a false green.

## 3. Evidence this machine cannot produce

Not claimable in the PR, and not replaceable by unit tests:

- `200` with `availability=ok` and `validated=true` against real traffic history.
- Staleness against a real clock/harvester gap (`200 stale` with real last-seen data).
- Any latency, throughput or memory statement about the endpoint on the infra node.
- Any staging soak evidence.

## 4. The forbidden shortcut

Do not manufacture a `live_history` artifact from fixture data, and do not hand-write a
`fit_data_sha256` to make the `ok` path green. An artifact that claims live provenance
without live provenance defeats the only reason this ticket exists, and a self-consistent
fixture proves nothing about the gate. Fixture artifacts in tests are labelled
`live_history` only inside `testdata/` and are never loadable by the server.

## 5. Revised closure gate for #372

`#372` is closable on V1-V12, with two explicit statements in the PR body:

1. The fail-closed gate is proven closed on a real local process, including zero DB reads
   on the rejection path.
2. The `ok` path is unproven against real data because the infra node is offline; that
   verification is transferred to `#375` (S10-T4), which owns refitting and publishing a
   `live_history` artifact.

This is a safe merge posture: the default runtime answer is 503, so nothing synthetic
becomes user-visible. It is strictly safer than the current `feat/s9-t6` branch, which
serves synthetic coefficients as if they were traffic.

If a reviewer insists on full Rule 10 proof before merge, the PR waits for the infra
node. It does not ship an unproven `ok` claim.

## 6. Handoff to #375 (S10-T4)

Add to `#375` acceptance:

- Refit on live history, publish the artifact, load it via `FORECAST_MODEL_PATH`.
- Record one `200 availability=ok` response and one `200 stale` response from the infra
  node into `docs/proofs/sprint10/`.
- Confirm `fit_source=live_history`, non-zero `fit_rows`, `validated=true`.

## 7. Downstream impact on #377 (S10-T6)

The timeline UI cannot render a real validated `ok` state locally. T6 must build against
a fixture server or mocked hook payload and mark that visual state as unverified until
the handoff in §6 produces evidence. The hatch-open state (`experimental=true`), the
`insufficient_history` and `model_unvalidated` disabled notices, and the dimmed `stale`
series are all verifiable now.

## 8. Estimate impact

No change to the 11.5 h implementation estimate. Roughly 1 h of the manual-verification
budget moves from #372 to #375; #372 gains the route-tree regression test, which roughly
offsets it. The delivery window (Nov 09-10) is unchanged for contract work; the `ok`-path
proof is bounded by infra availability, not by engineering effort.
