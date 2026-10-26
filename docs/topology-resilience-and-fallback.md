# Infrastructure Topology, Resilience & Graceful Fallback (`local edge host` ↔ `warded-stg`)

**Task ID:** [#143](https://github.com/529-studio/projectKV/issues/143) (S8)  
**Branch:** `feat/s8-resilience-fallback`  
**Execution Date:** 2026-10-03  
**Author:** Giang Đàm (`gdq2k2`)  
**Status:** Completed & Validated  
**Related Documents:** [ADR-004](ADR-004-route-check-and-snapshot-archive.md) (§9 D14, §10 S8, §14 Finding 2, §15), infra-disk-io-baseline.md

---

## 1. Executive Summary

This architecture document formally addresses **ADR-004 §10 Item S8** and **§14 Finding 2**:
> **Finding 2 (§14)**: All three staging containers (`warded-backend`, `warded-frontend`, `warded-model`) run on the same physical box, `local edge host`, with **no separate VPS and no wired ethernet** (WiFi-only uplink, `enp1s0` is down). A home power or WiFi outage takes down the entire public `warded-stg` service, not just a compute auxiliary. This elevates S8 from "worth doing" to a **real operational risk**.

To guarantee public safety integrity and system reliability, this document establishes:
1. **Full Topology Mapping**: Complete documentation of the current deployment architecture, container network, and single points of failure (SPOF).
2. **Graceful Degradation Architecture**: A strict degrade-to-`UNKNOWN` circuit breaker pattern (reusing D9) preventing hard HTTP 500 crashes and eliminating the hazard of frozen stale data (preventing recurrence of the H0 incident pattern).
3. **Real-Time Client Propagation**: Immediate broadcasting of fallback events via Server-Sent Events (SSE `camera_update`) to ensure map clients instantly visualize degraded cameras rather than stale green/red states.
4. **Thermal Monitoring Specification**: Temperature threshold and frequency monitoring for the 7.5W TDP fanless ZOTAC CI320NANO chassis.
5. **Component Health Probing**: Enhanced `/health` and `/healthz` endpoints distinguishing between full availability and graceful degradation.

---

## 2. Deployment Topology & Confirmed Single Points of Failure (SPOF)

### 2.1 Current Physical & Network Topology

```
             ┌─────────────────────────────────────────────────────────┐
             │                     Public Internet                     │
             │           (Users accessing warded-stg.529studio.site)    │
             └────────────────────────────┬────────────────────────────┘
                                          │ HTTPS (Cloudflare Proxy / DNS)
                                          ▼
             ┌─────────────────────────────────────────────────────────┐
             │                  Home WiFi Router (2.4/5GHz)             │
             │    ⚠️ SPOF 1: Residential power & wireless link           │
             └────────────────────────────┬────────────────────────────┘
                                          │ 802.11ac WiFi (wlp2s0)
                                          │ [Wired enp1s0 is DOWN]
                                          ▼
  ═══════════════════════════════════════════════════════════════════════════
   PHYSICAL HOST: local edge host (ZOTAC ZBOX-CI320NANO, Debian 13)
   ⚠️ SPOF 2: Single node, no VPS, fanless 7.5W TDP chassis, 5400RPM HDD
  ═══════════════════════════════════════════════════════════════════════════
     │
     ├── Traefik Edge Router (Port 80/443, Cloudflare SSL Resolver)
     │     │
     │     ├── [core-network (Docker Bridge)] ────────────────────────┐
     │     │                                                         │
     │     ├─► warded-frontend (Next.js Standalone, 0.25 CPU / 64M)  │
     │     │                                                         │
     │     ├─► warded-backend (Go Gin Server, 0.5 CPU / 256M)        │
     │     │     │                                                   │
     │     │     ├── Hot Set Pool Scheduler                          │
     │     │     ├── SQLite State Store (/app/data/camera-view.db)   │
     │     │     ├── In-Memory Proxy Cache (10s TTL)                 │
     │     │     └── SSE Hub (/api/live/stream)                      │
     │     │           │                                             │
     │     │           ▼ (HTTP internal call: CV_ENGINE_URL)          │
     │     └─► warded-model (OpenVINO / Python, 1.0 CPU / 512M) ─────┘
     │           [Internal only: traefik.enable=false, port 8000]
     │
     └── Shared Disk: 250GB 5400RPM SATA HDD (⚠️ SPOF 3: Disk I/O contention)
```

### 2.2 Confirmed SPOF Breakdown

| Component | Failure Mode | Blast Radius | Mitigation Status |
|---|---|---|---|
| **Residential Power / UPS** | Home power outage | **Total outage**: Entire `warded-stg` stack goes offline | Documented operational risk; future UPS or dual-homed VPS |
| **WiFi Uplink (`wlp2s0`)** | WiFi interference or disconnect (`enp1s0` down) | **Total network unreachable**: Inbound user traffic drops | Graceful degradation at Cloudflare layer; future wired LAN |
| **`warded-model` Container** | OOM kill, unhandled exception, or model crash | Backend Hot Set inference stalls | **Mitigated by S8**: Circuit breaker trips $\to$ degrades to `UNKNOWN` |
| **Upstream DOT Portal** | `giaothong.hochiminhcity.gov.vn` rate limit or 503 | Snapshot proxy misses; model receives no frames | **Mitigated by S8**: Circuit breaker fallback $\to$ `UNKNOWN` verdict |
| **Disk I/O Thrashing** | Concurrent storm writes (Archiver + SQLite + Valhalla) | High `%iowait`, latency spike | Monitored by S7 (#142); threshold matrix established |
| **Thermal Throttling** | 4 cores sustained load on 7.5W fanless chassis | Clock throttles from 1.83GHz to <500MHz | **Mitigated by S8**: Thermal monitor tool & warning thresholds |

---

## 3. Fallback & Graceful Degradation Architecture

### 3.1 Principles: Zero Hard Crashes & Zero Frozen Stale States
In public safety and navigation software, **serving frozen last-known state as fresh is more dangerous than reporting unknown state** (The H0 Incident Pattern). For example, if a camera observed "CLEAR" 2 hours ago and the analyzer subsequently crashed, showing green dots during a sudden storm causes drivers to plunge into flooded corridors.

Therefore:
1. **Never return HTTP 500**: Downstream model failures must never propagate as 500 internal server errors to clients.
2. **Never freeze stale verdicts**: If an analyzer invocation fails, the system must immediately issue a degraded fallback verdict.
3. **Immediate SSE notification**: Degraded verdicts must be broadcast across SSE streams to update client UI badges instantly.

### 3.2 Circuit Breaker & Fallback Flowchart

```
                 [Hot Set Scheduled Analysis Tick]
                                │
                                ▼
               Is Daily Budget Exhausted (D9)?
                     ├── YES ──► Emit Verdict{Severity: UNKNOWN, Source: "budget_breaker"}
                     │
                     └── NO
                          │
                          ▼
             Is Analyzer Circuit Breaker Tripped?
                     ├── YES ──► Emit Verdict{Severity: UNKNOWN, Source: "circuit_breaker", Stale: true}
                     │
                     └── NO (Closed / Half-Open)
                          │
                          ▼
                 Call Analyzer.Analyze(ctx, cam)
                   (Timeout: 10s per camera)
                          │
             ┌────────────┴────────────┐
             ▼                         ▼
         [SUCCESS]                  [ERROR]
   Breaker.RecordSuccess()    Breaker.RecordFailure()
   Emit Real Verdict          Emit Fallback Verdict:
   (Severity: CLEAR/HEAVY...)  - Severity: UNKNOWN
                              - FloodSeverity: UNKNOWN
                              - Confidence: 0.0
                              - Source: "fallback"
                              - Stale: true
                              - ObservedAt: time.Now()
                                       │
                                       ▼
                   ┌───────────────────┴───────────────────┐
                   ▼                                       ▼
        Broadcast via SSE (/api/live)          Persist to SQLite Store
        Event: camera_update                   Hydrates /api/traffic-status
        (Client renders gray/stale badge)      (API serves UNKNOWN state)
```

### 3.3 Degraded Fallback Verdict Contract
When a camera analysis fails or trips the breaker:
```json
{
  "camera_id": "cam_0123",
  "severity": "UNKNOWN",
  "flood_severity": "UNKNOWN",
  "confidence": 0.0,
  "source": "fallback",
  "observed_at": "2026-10-03T18:40:00Z",
  "stale": true,
  "timestamp": "2026-10-03T18:40:00Z"
}
```
* **Frontend Reaction**: `useTraffic` and `PinnedDock` inspect `stale: true` and `severity: "UNKNOWN"`, rendering an explicit *"Chưa có dữ liệu"* / *"Mất kết nối"* indicator with neutral gray styling per `AGENTS.md`.

---

## 4. Enhanced Health Check Architecture

To provide operational observability without breaking Traefik load balancing, the server exposes two complementary health endpoints:

1. **`GET /healthz` (Liveness Probe)**:
   - Ultra-lightweight endpoint returning `{"status": "ok"}` with HTTP 200.
   - Used by container orchestrators to confirm the process is alive.

2. **`GET /health` (Readiness & Component Probe)**:
   - Deep inspection of subsystem health:
     - `catalog`: Camera store loaded count.
     - `database`: SQLite write/read verification.
     - `analyzer`: Circuit breaker state (`CLOSED`, `HALF_OPEN`, `OPEN`).
   - Returns HTTP 200 even when degraded, indicating operational status:
     ```json
     {
       "status": "degraded",
       "hostname": "local edge host",
       "cameras_loaded": 1234,
       "online_cameras": 1050,
       "components": {
         "catalog": "ok",
         "database": "ok",
         "analyzer": "degraded (circuit breaker open: connection refused to warded-model:8000)"
       }
     }
     ```

---

## 5. Fanless Chassis Thermal Throttling Monitoring

### 5.1 Thermal Profile (Intel Celeron N2930 / Silvermont)
- **TDP**: 7.5W (fanless, passive heatsink).
- **TjMax (Maximum Junction Temperature)**: 100°C.
- **Hardware Throttling Mechanism**: Intel Digital Thermal Sensor (DTS) activates clock modulation when package temperature reaches 85°C–90°C.
- **Throttling Impact**: Core frequencies drop from 1.83GHz to 499MHz (a 73% compute penalty), causing timeouts across HTTP and inference pipelines.

### 5.2 Thermal Monitoring Tool (`tools/infra/thermal_monitor.py`)
Provides continuous or one-shot inspection of kernel thermal zones (`/sys/class/thermal/thermal_zone*/temp`) and core frequency scaling (`/sys/devices/system/cpu/cpu*/cpufreq/scaling_cur_freq`):

| Temperature Zone | Operating State | Action / Alert Level |
|---|---|---|
| **< 60.0°C** | Normal (Nominal) | Nominal operating envelope |
| **60.0°C – 75.0°C** | Elevated (Warm) | Normal under sustained multi-tenant load |
| **75.1°C – 82.0°C** | Warning (High) | Log warning; monitor ambient temperature |
| **> 82.0°C** | Critical (Throttling) | Thermal throttling active; throttle Hot Set analysis interval |

---

## 6. Long-Term Redundancy Roadmap (Post-Sprint 3)

1. **Wired Ethernet Uplink**: Re-enable or repair `enp1s0` on `local edge host` to replace WiFi with gigabit Ethernet.
2. **Dual-Homed Staging Gateway**: Deploy a lightweight VPS reverse proxy (e.g. Hetzner / DigitalOcean, $4/mo) holding static frontend assets, proxying to `local edge host` via a private mesh VPN. When `local edge host` drops off private mesh VPN, the public gateway serves a maintenance banner rather than dropping DNS connections.
3. **Hardware Storage Upgrade**: Replace 5400RPM HDD with SATA SSD per S7 report recommendations.
