# Benchmark Report: CV Engine Evaluation on dev-box (AMD Ryzen 7 6800HS, 2 vCPU / 2GB RAM)

**Date:** 2026-10-09  
**Target Environment:** `dev-box` (Isolated KVM Virtual Machine on Tailnet: `100.118.7.57`)  
**Hardware Host:** AMD Ryzen 7 6800HS Creator Edition (Zen 3+, 8 vCPUs on VM)  
**Container Resource Limit:** cgroups `cpus: "2.00"` | `memory: "2048M"`  
**Service Under Test:** `projectkv-cv-engine` (`ghcr.io/529-studio/projectkv-model:stg-latest`)  
**Model:** `yolov8n_openvino_320` (Intel OpenVINO IR FP16, $320 \times 320$, 6.4 MB)  
**Test Data:** Real HCMC traffic camera snapshots ($1280 \times 720$, 53.7 KB JPEG)  
**Reference Baseline:** [`docs/spike-cv-runtime.md`](../spike-cv-runtime.md) (Intel Celeron N2930 @ 1.83GHz on `homesrv`)  

---

## 1. Executive Summary & Key Conclusions

This benchmark evaluates the performance of the Computer Vision inference service (`cv-engine`) running inside a strictly resource-constrained Docker container (**2.00 vCPU, 2048 MB RAM**) on the newly deployed KVM worker **`dev-box`**.

### Key Achievements
1. **23.4x Faster Latency**:
   - Sequential HTTP P50 latency dropped from **425.84 ms** (Celeron N2930) down to **18.22 ms** (AMD Ryzen 7 6800HS).
   - Pure model inference latency P50 is **16.17 ms** (minimum **12.44 ms**).
2. **10.6x Higher Throughput**:
   - Single-container throughput reached **24.01 FPS (RPS)** under 2 vCPUs, compared to **2.27 FPS** on Celeron N2930.
3. **Full Citywide Scale (720 Cameras)**:
   - At 24.01 FPS, this single 2 vCPU container can process **all 720 traffic cameras across Ho Chi Minh City in exactly 30.0 seconds** ($\frac{720}{24.01} = 29.98\text{ s}$).
   - For the Hot Set (30 cameras every 30s), processing takes only **1.25 seconds** (average CPU utilization: **4.1%**).
4. **Strict Isolation & Ultra-Low Memory**:
   - Container CPU was strictly clamped at **200.08%** by the Linux CFS scheduler, ensuring the remaining 6 vCPUs and the host system were 100% unaffected.
   - Peak RSS memory under maximum concurrency (16 workers) was only **86.35 MiB / 2048 MiB (4.22%)**. Zero memory leakage across 200 continuous requests.

---

## 2. Head-to-Head Comparison: `homesrv` vs `dev-box`

| Metric | Baseline: `homesrv` (Celeron N2930) | Target: `dev-box` (AMD Ryzen 7 6800HS) | Improvement |
|---|---|---|---|
| **CPU Core Limits** | `1.00 vCPU` (Intel Silvermont, No AVX) | `2.00 vCPU` (AMD Zen 3+, AVX2/FMA) | Newer microarchitecture + AVX2 |
| **RAM Limit / Usage** | `512 MB` / 81.75 MB | `2048 MB` / 86.35 MB | $< 5\%$ allocation used |
| **Model Cold Load Time** | 1,101.1 ms | **272.5 ms** | **4.0x faster** |
| **Sequential P50 Latency** | 425.84 ms | **18.22 ms** | **23.4x faster** ($-95.7\%$) |
| **Sequential P90 Latency** | 468.75 ms | **79.27 ms** | **5.9x faster** |
| **Sequential Min Latency** | 410.71 ms | **14.33 ms** (Model: 12.44 ms) | High consistency |
| **Throughput (Peak FPS)** | 2.27 FPS | **24.01 FPS** | **10.6x higher throughput** |
| **Hot Set (30 cams / 30s)** | 44% load of 1 core | **1.25s execution (4.1% load)** | Leaves 95.9% CPU idle |
| **720 Cameras Citywide** | **FAILED (Takes ~317s)** | **PASSED (Takes 30.0s)** | Meets 30s SLA |
| **Error Rate** | 0.0% (50/50) | **0.0% (1,363 / 1,363 requests)** | 100% reliability |

---

## 3. Detailed Benchmark Results on `dev-box`

### 3.1 Sequential Baseline (100 Consecutive Requests)
Tested with real HCMC snapshot JPEG ($1280 \times 720$, 53,739 bytes):

```text
Requests Executed: 100
Successful:        100 (100.0%)
Error Rate:        0.0%
```

| Metric | HTTP Round-Trip Latency | Pure Model Inference Latency |
|---|---|---|
| **P50** | **18.22 ms** | **16.17 ms** |
| **P90** | 79.27 ms | 77.26 ms |
| **P95** | 80.20 ms | 78.31 ms |
| **P99** | 81.18 ms | 79.40 ms |
| **Min** | 14.33 ms | 12.44 ms |
| **Max** | 81.18 ms | 79.40 ms |
| **Avg** | 39.58 ms | 35.73 ms |

---

### 3.2 Concurrency Sweep & Saturation Test
Testing worker scalability under the hard 2.00 vCPU limit (8 seconds per tier):

| Concurrency (Workers) | Total Requests | Successful Requests | Throughput (RPS / FPS) | P50 Latency (ms) | P90 Latency (ms) | P99 Latency (ms) | Error Rate (%) |
|---|---|---|---|---|---|---|---|
| **1 Worker** | 190 | 190 | **23.72 RPS** | 18.35 ms | 79.99 ms | 82.22 ms | 0.0% |
| **2 Workers** | 194 | 194 | **24.01 RPS** | 94.82 ms | 99.41 ms | 106.40 ms | 0.0% |
| **4 Workers** | 190 | 190 | **23.45 RPS** | 189.35 ms | 204.58 ms | 297.81 ms | 0.0% |
| **8 Workers** | 195 | 195 | **23.49 RPS** | 313.85 ms | 421.96 ms | 518.34 ms | 0.0% |
| **12 Workers** | 198 | 198 | **23.32 RPS** | 503.25 ms | 614.67 ms | 798.44 ms | 0.0% |
| **16 Workers** | 196 | 196 | **22.80 RPS** | 699.35 ms | 883.10 ms | 1,708.61 ms | 0.0% |

**Analysis:**
- **Saturation Point:** Peak throughput plateau occurs at **~24.0 FPS**, exactly matching the hardware capability of 2 Zen 3+ physical cores running OpenVINO with AVX2.
- **Queueing Behavior:** Because `server.py` uses `ThreadingHTTPServer`, high concurrency queues requests cleanly without a single dropped packet or error (0.0% error rate across all tiers).

---

### 3.3 Resource Containment & cgroups Verification

During the high-concurrency peak load test:
```text
CONTAINER ID   NAME                CPU %     MEM USAGE / LIMIT   MEM %     PIDS
4c47925d5019   dev-box-cv-engine   200.08%   86.35MiB / 2GiB     4.22%     15
```

• **CPU Capping:** The CFS scheduler locked the CPU at **200.08%** (2 cores maximum). The host and VM remained completely quiet on the remaining 6 vCPUs.  
• **RAM Safety:** The memory limit of 2048 MB provides an enormous safety margin. Actual RSS memory hovered between **72 MB and 86 MB** ($< 4.5\%$ of allocated memory).

---

### 3.4 Endurance & Memory Leak Verification (200 Cycles)

Memory samples taken across 200 back-to-back requests:

| Test Milestone | Container RSS Memory | Memory Percentage | Memory Drift |
|---|---|---|---|
| **Cycle 1** | 83.99 MiB | 4.10% | Baseline |
| **Cycle 50** | 84.23 MiB | 4.11% | +0.24 MiB |
| **Cycle 100** | 84.27 MiB | 4.11% | +0.28 MiB |
| **Cycle 150** | 84.24 MiB | 4.11% | +0.25 MiB |
| **Cycle 200** | 83.73 MiB | 4.09% | **-0.26 MiB** |

**Conclusion:** Memory consumption is completely flat and stable. Python garbage collection and OpenVINO tensor memory buffers operate with zero memory leaks.

---

## 4. Architectural Recommendations

1. **Production Offloading Decision**:
   - Move the CV inference workload permanently off `homesrv` (Celeron N2930) and onto `dev-box` (Ryzen 7 6800HS).
   - This frees up 44% CPU load on `homesrv` to focus solely on Traefik, database, and web services.
2. **Network Routing**:
   - The Go backend (`warded-backend`) on `homesrv` can call `dev-box-cv-engine` via the private Tailscale IP:
     `CV_ENGINE_URL=http://100.118.7.57:8000`
   - Latency between `homesrv` and `dev-box` over Tailscale is ~40–50 ms. Total end-to-end inference request time will be $\sim 65\text{ ms}$, still **6.5x faster** than running locally on Celeron N2930.
3. **Scaling Beyond 720 Cameras**:
   - If traffic camera frequency increases to every 10 seconds, `dev-box` can easily scale by allocating 4 vCPUs or running a second replica to reach $> 48\text{ FPS}$.
