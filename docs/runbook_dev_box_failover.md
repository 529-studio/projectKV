# Disaster Recovery Runbook: Worker Node Failover & Network Resilience (`dev-box` ↔ `local-homesrv`)

**Document Version:** 1.0.0  
**Effective Date:** 2026-10-10  
**Task Reference:** Issue [#210](https://github.com/529-studio/projectKV/issues/210) (Sprint 5 Task S5-T4)  
**Applicable Scope:** Staging (`warded-stg.529studio.site`) & Distributed Production Cluster  
**Governing Documents:** [ADR-004 §D9, §S8](ADR-004-route-check-and-snapshot-archive.md), [ADR-006 Finding 4](ADR-006-sprint4-audit-and-sprint5-plan.md), [topology-resilience-and-fallback.md](topology-resilience-and-fallback.md)

---

## 1. System Architecture & Fault Domain

The Computer Vision pipeline runs in a decoupled distributed architecture:

```
  ┌────────────────────────────────────────────────────────────────────────┐
  │                           PHYSICAL BOUNDARY                            │
  │                                                                        │
  │   Coordinator Host: local-homesrv (Portainer Endpoint ID: 3)          │
  │   Chassis: ZOTAC CI320NANO (Fanless Celeron N2930, 4 cores, 8GB RAM)   │
  │   Stack 14: warded-stg (Traefik, warded-backend, warded-frontend)      │
  │   Standby: warded-model (Profile: model-local, port 8000 internal)    │
  └───────────────────────────────────┬────────────────────────────────────┘
                                      │
                                      ▼ Tailscale Mesh VPN (100.118.7.57)
                                      │ ⚠️ Network Single Point of Failure (SPOF)
                                      ▼
  ┌────────────────────────────────────────────────────────────────────────┐
  │   Compute Worker Host: dev-box (Portainer Endpoint ID: 4)              │
  │   Chassis: AMD Ryzen 7 6800HS APU (8 vCPU, 8GB RAM allocated)          │
  │   Stack 17: warded-stg-cv (warded-cv-engine OpenVINO, port 8000)       │
  │   Throughput: 24.01 FPS (P50 = 18ms)                                   │
  └────────────────────────────────────────────────────────────────────────┘
```

### Risk Classification: VPN SPOF
Worker host `dev-box` is an isolated hypervisor connected over residential Tailscale. A VPN split-brain, ISP dropout, or host reboot breaks the inference bridge.

---

## 2. Automated Circuit Breaker & Degradation Contract

The Go backend hotset scheduler ([`internal/hotset/scheduler.go`](../services/go-server/internal/hotset/scheduler.go)) enforces strict graceful degradation:

1. **Per-Camera Context Timeout (`10s`):**  
   Each inference call is bounded by `context.WithTimeout(ctx, 10*time.Second)`. A network blackhole cannot block scheduler workers or leak goroutines.
2. **Initial Failure Degradation (`source: "fallback"`):**  
   On timeout or connection refusal, the scheduler logs `WARN analysis failed, degrading to unknown` and emits `Verdict{Severity: UNKNOWN, FloodSeverity: UNKNOWN, Confidence: 0, Source: "fallback", Stale: true}`.
3. **Circuit Breaker Tripping (`source: "circuit_breaker"`):**  
   After **3 consecutive failures**, `CircuitBreaker` enters `StateOpen`. Subsequent calls immediately short-circuit without attempting network I/O, emitting `Source: "circuit_breaker"`.
4. **Zero Frozen Stale States:**  
   The UI receives degraded `UNKNOWN` states instantly via SSE (`/api/live/stream`) and renders neutral gray badges. Stale green/red verdicts are NEVER frozen.
5. **Auto-Recovery (`StateHalfOpen` $\rightarrow$ `StateClosed`):**  
   After a 15-second cooldown, the breaker allows a single probe request. Upon HTTP 200, it automatically resets to `StateClosed` and resumes real ONNX telemetry.

---

## 3. Incident Triage & Health Diagnostics

When alerts fire or telemetry degrades to `UNKNOWN`:

### Step 1: Probe Worker Health & Tailscale Connectivity
Run from coordinator node or any tailnet machine:
```bash
# 1. Probe dev-box CV engine HTTP endpoint
curl -s -m 5 http://100.118.7.57:8000/healthz

# Expected healthy output:
# {"status": "healthy", "service": "cv-engine", "openvino_available": true, "model_loaded": true, "timestamp": ...}

# 2. Check Tailscale ping to dev-box
tailscale ping 100.118.7.57
```

### Step 2: Check Staging Backend Health & Verdict State
```bash
# 1. Check backend liveness and readiness
curl -s https://warded-stg.529studio.site/healthz

# 2. Inspect latest verdicts provenance
curl -s https://warded-stg.529studio.site/api/traffic-status | jq '.verdicts[0:3]'
```

### Triage Matrix

| Symptom | Root Cause | Action Required |
|---|---|---|
| `source: "budget_breaker"` | Daily analysis cap hit (`ANALYZER_DAILY_BUDGET`) | Ensure `ANALYZER_DAILY_BUDGET=500000` is set in Stack 14 environment. |
| `source: "circuit_breaker"` + `dev-box 8000 unreachable` | `dev-box` down or Tailscale partition | Follow **Procedure A** (transient) or **Procedure B** (prolonged). |
| `source: "circuit_breaker"` + `dev-box 8000 healthy` | Transient network hiccup during cooldown | Wait 15s for automatic breaker reset; inspect backend logs. |

---

## 4. Disaster Recovery Procedures

### Procedure A: Transient Outage (< 15 Minutes)
**Zero Intervention Required.**  
The backend will isolate failures, protect system stability, and automatically recover when Tailscale or `dev-box` restores. Monitor logs to confirm recovery:
```bash
# On homesrv:
docker logs -f --tail 20 warded-backend
# Observe automatic transition: snapshot archived successfully reason=state_change
```

---

### Procedure B: Prolonged Outage (> 15 Minutes) — Fallback to Local Model

If `dev-box` hardware fails or Tailscale has a prolonged regional partition, restore inference on coordinator node `homesrv`:

#### 1. Verify Local Standby Container
Container `warded-model` is colocated on `homesrv` within `core-network`.
```bash
# Verify container is running:
docker ps --filter "name=warded-model"

# If stopped, launch using profile model-local:
docker compose -f docker-compose.stg.yml --profile model-local up -d warded-model

# Probe local healthz:
curl -s http://127.0.0.1:8000/healthz || docker exec warded-backend wget -qO- http://warded-model:8000/healthz
```

#### 2. Switch Backend Inference Target
In Portainer (Stack 14) or staging compose environment:
1. Update environment variable:
   ```env
   CV_ENGINE_URL=http://warded-model:8000
   ```
2. Restart `warded-backend`:
   ```bash
   docker restart warded-backend
   ```
3. Confirm telemetry is restored:
   ```bash
   curl -s https://warded-stg.529studio.site/api/traffic-status | jq '.verdicts[0].source'
   # Expected: "onnx"
   ```

> [!NOTE]
> `local-homesrv` runs on a 7.5W fanless Celeron N2930. Local inference runs at ~2.27 FPS (vs 24 FPS on `dev-box`). This is intended for emergency fallback only.

---

### Procedure C: Restoring Primary Worker `dev-box`

Once `dev-box` is back online:

1. Confirm `dev-box` container is healthy:
   ```bash
   curl -s http://100.118.7.57:8000/healthz
   ```
2. In Portainer (Stack 14) or compose environment:
   ```env
   CV_ENGINE_URL=http://100.118.7.57:8000
   ```
3. Restart `warded-backend`:
   ```bash
   docker restart warded-backend
   ```
4. Optional: Stop local `warded-model` container on `homesrv` to free CPU cycles:
   ```bash
   docker stop warded-model
   ```

---

## 5. Post-Drill Verification Proof Reference

Live staging chaos drill executed and verified on 2026-10-10 under Issue #210. Full timestamped terminal logs and verification proof can be found at:  
[`docs/reports/tailscale_failover_drill_report.md`](reports/tailscale_failover_drill_report.md).
