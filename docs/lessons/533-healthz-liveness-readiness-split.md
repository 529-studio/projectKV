# 533 - Liveness and readiness are separate contracts

## Trigger

On 2026-11-22 `desk-srv-02` was powered off for several hours. `warded-backend` on `desk-srv-01` answered `503` on `/health`, `/healthz` and `/api/health` because the cv-engine probe (`CV_ENGINE_URL=http://100.64.107.57:8000`) could not connect. `Dockerfile` runs its `HEALTHCHECK` against `/healthz`, so the container went `(unhealthy)` even though the Go process, SQLite and every non-inference route were serving normally. The only way to keep the web, archiver and API surfaces up was a manual `ANALYZER=none` override on Stack 14.

## Lesson

A health endpoint that mixes liveness with a remote dependency makes the container runtime report a failure it cannot fix. Docker's `HEALTHCHECK` decides whether to keep, restart or stop a container; it must answer "is this process able to serve?" and nothing else. A dependency on another host belongs to readiness, and readiness must be a separate path that a caller can choose.

The split that landed:

| Path | Contract | Consumer |
| :--- | :--- | :--- |
| `/healthz` | Liveness: `200` while the process answers. The `cv_engine` block is informational | Docker `HEALTHCHECK`, node-local supervisors |
| `/health`, `/api/health` | Readiness: `503` when the wired cv-engine is unhealthy | Load balancers, the external dead-man switch, the web client |

Both paths run the same probe and report the same body shape; only the overall status and HTTP code differ, so an operator still sees the engine failure from either one.

## Rules extracted

1. A container `HEALTHCHECK` must point at a liveness probe. Anything that can fail because of a process on another host belongs in readiness.
2. Readiness is where a dependency outage is allowed to surface as `503`; liveness must never inherit that code.
3. When a temporary environment override exists only to compensate for a health contract, treat the override as a bug report against the contract, not as configuration to keep.
