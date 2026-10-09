# Sprint 4 Technical Audit & Ground-Truth Verification Q&A

**Document:** `docs/sprint4_audit_qa.md`  
**Date:** 2026-10-10  
**Context:** Technical review and reality-check audit preceding Sprint 5 planning (ADR-006).  
**Source of Truth:** Git history, Pull Request list, Go backend codebase, evaluation datasets, and empirical reports in `docs/reports/`.  

---

## Group 1 — Core Decision T9 Verification (The Empirical Foundation)

### Question 1: Has T4 (real benchmark on 764 frames) been re-run after T5 calibrated PCU weights?
- **Answer:** **NO. It has NOT been re-run.**
- **Codebase & Git Evidence:**
  - In `docs/reports/eval_report_d6_real.md`, the timestamp is `2026-10-08T14:02:27Z`, corresponding to commit `ddeafa5` (PR [#200](https://github.com/529-studio/projectKV/pull/200) - Ticket T4), which reported **Traffic Macro F1 = 0.209**.
  - In PR [#202](https://github.com/529-studio/projectKV/pull/202) (Ticket T5 - PCU Calibration), the team updated `services/go-server/internal/cvclient/pcu.go`, `config.go`, and verified the mathematical logic via isolated mock unit tests (`pcu_test.go`).
  - Neither PR #202 nor any subsequent PR executed `python3 tools/eval/benchmark.py` against the 764 labeled real frames with the calibrated weights. No updated evaluation report was generated.
  - **Verdict:** Claude's diagnosis is **100% factually accurate**. Decision T9 was reached based on the theoretical derivation that `CapacityFloor = 15.0` would fix congestion recall, but without empirical re-measurement on real frames. This directly violated the project's own rule: *"Unit tests passing in isolation is never acceptable proof of correctness."* Re-running this benchmark must be Ticket #1 (T10 / S5-T1) of Sprint 5.

---

### Question 2: Detailed breakdown of smoke-test 2/10 — which 2 cameras passed (CLEAR vs. CONGESTED)?
- **Answer:** **Exactly 1 CLEAR camera passed, and exactly 1 CONGESTED camera passed.** 4 out of 5 CLEAR cameras failed, and 4 out of 5 CONGESTED cameras failed.
- **Empirical Evidence from [`docs/reports/smoke_eval_d6_real.md`](reports/smoke_eval_d6_real.md):**

| Camera ID | Name | Corridor | Expected | Actual | SI | Status | Root Cause & Anomaly |
|---|---|---|:---:|:---:|:---:|:---:|---|
| `56de42f611f398ec0c48129c` | Võ Văn Kiệt - Quốc Lộ 1 | `vo_van_kiet` | **CLEAR** | **CLEAR** | `0.00` | **PASS** | True off-peak zero queue. |
| `66b1c426779f74001867415e` | Điện Biên Phủ - Nguyễn Gia Trí | `dien_bien_phu` | **CONGESTED** | **CONGESTED** | `1.00` | **PASS** | True peak-hour bottleneck queue. |
| `5822b5ededeb6c0012a2d66a` | Mai Chí Thọ - Nguyễn Cơ Thạch 1 | `mai_chi_tho` | **CLEAR** | **HEAVY** | `0.70` | **FAIL** | False-positive congestion on wide boulevard. |
| `5a10c79f02eb490011a0b0dd` | Mai Chí Thọ - Tố Hữu (C6) | `mai_chi_tho` | **CLEAR** | **MODERATE** | `0.51` | **FAIL** | False-positive moderate congestion. |
| `56de42f611f398ec0c481287` | Võ Văn Kiệt - Ký Con | `vo_van_kiet` | **CLEAR** | **HEAVY** | `0.84` | **FAIL** | False-positive congestion near downtown. |
| `5deb576d1dc17d7c5515ad02` | Điện Biên Phủ - Pasteur | `dien_bien_phu` | **CLEAR** | **CONGESTED** | `1.00` | **FAIL** | False-positive maximum congestion. |
| `5deb576d1dc17d7c5515ad08` | Cộng Hòa - Tân Kỳ Tân Quý | `cong_hoa` | **CONGESTED** | **MODERATE** | `0.40` | **FAIL** | Under-reported bottleneck severity. |
| `586e1f18f9fab7001111b0a5` | Cộng Hòa - Trường Chinh | `cong_hoa` | **CONGESTED** | **CLEAR** | `0.00` | **FAIL** | Zero vehicle detections on critical bottleneck ($SI = 0.00$). |
| `5deb576d1dc17d7c5515acf2` | Điện Biên Phủ - Cách Mạng Tháng Tám | `dien_bien_phu` | **CONGESTED** | **CLEAR** | `0.32` | **FAIL** | Under-reported queue spillback. |
| `56de42f611f398ec0c481282` | Mai Chí Thọ - Đồng Văn Cống 2 | `mai_chi_tho` | **CONGESTED** | **CLEAR** | `0.00` | **FAIL** | Zero vehicle detections ($SI = 0.00$). |

- **Critical Mathematical & Perception Implications:**
  1. Claude's insight is **dead-on**: If the root cause had been *only* an oversized `CapacityFloor = 25.0`, the 5 `CLEAR` cameras (which have low vehicle counts) should have passed, yielding an expected pass rate of ~50% (5/10).
  2. Because $SI = \frac{PCU_{current}}{CapacityFloor}$, lowering `CapacityFloor` from 25.0 to 15.0 increases $SI$ by a factor of $1.67\times$. This means that for the 4 `CLEAR` cameras that already had $SI \ge 0.51$, **lowering the capacity floor will make false-positive congestion even worse!**
  3. Conversely, for the 2 `CONGESTED` cameras with $SI = 0.00$ (`Cộng Hòa - Trường Chinh` and `Mai Chí Thọ - Đồng Văn Cống 2`), no change in `CapacityFloor` or PCU weights can fix a zero: $0.00 / 15.0 = 0.00$.
  4. **True Diagnosis:** The issue is rooted in the **object detection layer itself**:
     - False-positive vehicle hallucination on complex backgrounds / night reflections.
     - Missed detections on occluded congestion queues.
     - Testing executed at `20:19` UTC+7, which violated the curated time windows (off-peak: 13:00–15:00, peak: 17:00–18:30).

---

### Question 3: Has the smoke test been re-run after T5 merged?
- **Answer:** **NO.**
- **Codebase Evidence:** `docs/reports/smoke_eval_d6_real.md` has only one commit (`ddeafa5` from PR #200). The 2/10 (20.0%) pass rate was recorded prior to T5. It has not been re-run on staging or dev post-T5.

---

## Group 2 — Dedicated Worker Architecture (`dev-box` Evaluation)

### Question 4: Is `dev-box` a dedicated 24/7 infrastructure server or a personal machine (laptop/mini-PC)?
- **Answer:** **It is a self-hosted physical workstation running KVM, not an enterprise dedicated cloud server.**
- **Hardware Ground Truth from [`cv_engine_bench_devbox.md`](reports/cv_engine_bench_devbox.md):**
  - Host CPU: **AMD Ryzen 7 6800HS Creator Edition** (Zen 3+, 8 cores / 16 threads, mobile/creator APU).
  - VM Environment: Isolated KVM guest on Tailscale IP `100.118.7.57`, allocated 8 vCPUs and 8 GB RAM, strictly cgroup-clamped for the container at `cpus: "2.00"` and `memory: "2048M"`.
  - While it is kept powered on as a headless homelab KVM hypervisor on a dedicated power line, it is physically a personal workstation/mini-PC host. It does not carry an enterprise cloud SLA (e.g. AWS/GCP 99.99%).

---

### Question 5: Does `dev-box` incur operational costs (power, hardware wear)?
- **Answer:**
  - **Third-Party API Cost:** Strictly **$0.00 / month** (zero token billing to Google Cloud or OpenAI).
  - **Infrastructure Cost:** Incurs physical electricity consumption (~35–65W continuous under load) and residential internet bandwidth. It is funded out-of-pocket by the team homelab rather than commercial cloud hosting bills. Claiming "$0 cost" in T9 was valid for SaaS/API operational budgets, but obscured physical hosting dependencies.

---

### Question 6: If Tailscale to `dev-box` disconnects, does the system degrade to `unknown` per S8, or does it crash/hang?
- **Answer:** **The Go codebase is fully protected and degrades gracefully to `SeverityUnknown`, BUT this failure mode has never been physically tested via network partition on Staging.**
- **Code Verification:**
  1. In `services/go-server/internal/cvclient/cvengine.go` (line 126–130):
     ```go
     predResp, err := a.client.Do(predReq)
     if err != nil {
         return unknownVerdict(cam.ID), fmt.Errorf("cv-engine: POST /predict: %w", err)
     }
     ```
     The call propagates `callCtx` with a strict `10 * time.Second` timeout (`hotset/scheduler.go:210`).
  2. In `services/go-server/internal/hotset/scheduler.go` (line 247–265):
     ```go
     verdict, err := s.analyzer.Analyze(callCtx, cam)
     if err != nil {
         if s.breaker != nil {
             s.breaker.RecordFailure(err)
         }
         s.log.Warn("analysis failed, degrading to unknown", "camera", cam.ID, "error", err)
         s.onVerdict(Verdict{
             CameraID:      cam.ID,
             Severity:      SeverityUnknown,
             Confidence:    0,
             Source:        "fallback",
         })
     }
     ```
  3. When repeated errors trip the `CircuitBreaker`, subsequent calls immediately return `Source: "circuit_breaker"` without waiting for the 10s timeout.
  4. **The Caveat:** This behavior is proven in unit tests (`cvengine_test.go`), but **no physical chaos test (e.g. `tailscale down` on `dev-box`) has been conducted on live staging**.

---

### Question 7: Does offloading to `dev-box` make the SPOF better or worse?
- **Honest Trade-off Analysis:**
  - **Improvements:**
    - **Eliminates Edge Compute Exhaustion:** The Celeron N2930 on `homesrv` had no AVX/AVX2 support, running at 90–100% CPU and 440ms/frame. Removing OpenVINO from `homesrv` prevents backend API lockups.
    - **Eliminates Edge Disk Contention:** No continuous frame processing load or disk caching on `homesrv`.
  - **New Vulnerabilities Introduced:**
    - **Network Tunnel Dependency (VPN SPOF):** Previously, backend and model were colocated on `homesrv` (Docker bridge, 0ms network latency, zero WAN dependency). Now, inference relies on **Tailscale peer-to-peer tunnels (`100.118.7.57`)**. If Tailscale DERP relays degrade or residential internet drops, inference stops.
    - **Multi-Node Dependency:** System availability now requires **two machines (`homesrv` AND `dev-box`)** to remain online simultaneously.
  - **Verdict:** It solved the CPU wall, but traded it for network topology coupling. A local fallback profile (`profiles: ["model-local"]` in `docker-compose.stg.yml`) must be maintained.

---

### Question 8: Does `T2b` (direct frame delivery to archiver) preserve `RawFrame` when calling `dev-box` over HTTP?
- **Answer:** **YES, 100% PRESERVED. The bytes are NEVER lost.**
- **Architectural Flow Verification in [`cvengine.go`](../services/go-server/internal/cvclient/cvengine.go#L93-L171):**
  1. `CVEngineAnalyzer.Analyze()` runs inside `warded-backend` on `homesrv`.
  2. `homesrv` downloads the snapshot bytes from DOT upstream (`io.ReadAll(snapResp.Body) -> jpegBytes`).
  3. `homesrv` keeps `jpegBytes` in memory and sends a POST request with `bytes.NewReader(jpegBytes)` across Tailscale to `http://100.118.7.57:8000/predict`.
  4. `dev-box` runs inference and returns a lightweight JSON payload (vehicle counts).
  5. `homesrv` receives the JSON, constructs the `hotset.Verdict`, and explicitly attaches:
     ```go
     RawFrame: jpegBytes, // line 170
     ```
  6. The `Verdict` containing `RawFrame` is passed directly to `archiver.OnVerdict()`.
  7. **Conclusion:** Switching `CV_ENGINE_URL` from local to remote HTTP did **not alter `RawFrame` delivery**. The caller (`homesrv`) already held the bytes before dispatching inference.

---

## Group 3 — Decoupled Hybrid Flood Plan: Status & Grounding

### Question 9: Is the "Decoupled Hybrid" VLM flood pipeline implemented, or only an architectural decision?
- **Answer:** **ONLY AN ARCHITECTURAL DECISION (ADR). Zero code has been implemented.**
- **Codebase Evidence:**
  - In `services/go-server/cmd/server/main.go` (lines 270–313), the server selects **exactly one** analyzer at startup based on `ANALYZER`:
    - `ANALYZER=mock`
    - `ANALYZER=vlm`
    - `ANALYZER=onnx`
    - `ANALYZER=none`
  - There is no hybrid analyzer, no event bus listening to weather radar precipitation, and no dynamic VLM dispatch on camera modal clicks.
  - Under `ANALYZER=onnx`, the backend sets `FloodSeverity: hotset.SeverityUnknown` permanently (`cvengine.go:165`).
  - Sprint 4 adhered to the rule *"No new features"*. Building the Decoupled Hybrid Flood Analyzer belongs strictly to **Sprint 5**.

---

### Question 10: Where does the "10 known flood chokepoints" list come from?
- **Answer:** **It was an illustrative estimation in the ADR-005 T9 document; NO formal list of 10 flood chokepoints exists in the codebase.**
- **Codebase Evidence:**
  - The author of `docs/decision-openvino-vs-vlm.md` conflated the **10 cameras in `smoke_test_cameras.json`** (which are 5 CLEAR and 5 CONGESTED traffic cameras) with hypothetical flood chokepoints to calculate daily API quota bounds ($10\text{ cams} \times \text{calls} < 500\text{ RPD}$).
  - While T3 labeled 120 flood frames across 206 cameras during the storm, a dedicated fixture of 10 persistent flood chokepoints (e.g. Nguyễn Văn Quá, Quốc Hương, Huỳnh Tấn Phát) has **not yet been curated**.
  - **Action for Sprint 5:** Formally curate `flood_chokepoint_cameras.json` based on authentic municipal flood maps.

---

### Question 11: Does the $\ge 2.0\text{ mm/h}$ rainfall threshold have an empirical basis, or is it a guess?
- **Answer:** **It is an unverified rule-of-thumb heuristic.**
- **Codebase Evidence:**
  - In `apps/web/src/utils/rainHexUtils.ts`, Open-Meteo precipitation is mapped using nominal cutoffs (`1.0 mm` = wet, `4.0 mm` = moderate, `15.0 mm` = deep).
  - Urban flooding in Ho Chi Minh City is driven by **30-minute cumulative rainfall ($\ge 30–50\text{ mm}$)** combined with **tidal river surges (triều cường)**, not instantaneous $2.0\text{ mm/h}$ rain rates.
  - No empirical correlation study linking Open-Meteo hourly precipitation rates to physical camera flood states has been conducted.

---

## Group 4 — Repository Metrics & Runtime Edge Cases

### Question 12: Verification of PR counts (`121 PR, 113 merged, 8 closed, 0 open`)
- **Answer:** **Re-verified via GitHub CLI (`gh pr list --limit 500`):**
  - As of PR #205 merge: Exactly 121 PRs (113 merged, 8 closed without merge, 0 open).
  - Current live count (after opening PR #206 for Sprint 4 report):
    - **Total PRs:** **123**
    - **Merged:** **114**
    - **Closed (unmerged):** **8**
    - **Open:** **1** ([PR #206](https://github.com/529-studio/projectKV/pull/206))
    - Mathematical verification: $114 + 8 + 1 = 123$. Clean and 100% reconciled.

---

### Question 13: If a Hot Set camera dies (Ghost), does $N$ drop below 57, or does it keep trying to analyze dead cameras?
- **Answer:** **$N$ DOES NOT DROP; the backend automatically pads with another living camera at startup, and suppresses dead feeds at runtime.**
- **Detailed Mechanism in [`services/go-server/cmd/server/main.go`](../services/go-server/cmd/server/main.go#L95-L172):**
  1. **Startup Filtering (`buildFixedHotSet`):**
     ```go
     eligible := make([]camera.Camera, 0, len(all))
     for _, c := range all {
         if !c.IsOnline || c.IsGhost || c.DuplicateOf != "" {
             continue
         }
         eligible = append(eligible, c)
     }
     ```
     Any camera classified as `isGhost` or `!isOnline` by the daily audit (`#178 / #179`) is excluded immediately.
  2. **Three-Pass Seeding:**
     - **Pass 1:** Selects intersection cameras (`len(road_keys) == 2`) on the 4 priority corridors. If 1 camera was marked ghost, Pass 1 yields 56 cameras.
     - **Pass 2 & Pass 3 (Automatic Padding):** If `len(critical) < maxCameras` (57), Pass 2 and Pass 3 pad with other alive, eligible cameras until reaching exactly $N = 57$.
     - **Result:** $N$ remains fixed at 57. Dead cameras are never seeded.
  3. **Runtime Mid-Day Failure (Between Server Restarts):**
     - If an active camera dies during the day, DOT upstream returns an empty body or placeholder PNG.
     - `proxycache` catches this and returns HTTP `503 GHOST_CAMERA`.
     - `CVEngineAnalyzer.Analyze()` returns an error, and `Scheduler` immediately marks the verdict as `SeverityUnknown` with `Source: "fallback"`.
     - It does not crash, and the UI immediately reflects the failure.

---

## Actionable Takeaway for Sprint 5 (ADR-006 Formulation)

1. **Ticket S5-T1 (Mandatory Gate):** Re-run D6 evaluation (`benchmark.py`) and smoke test (`verify_smoke_set.py`) on the 764 real frames with the calibrated PCU parameters. Record the actual Macro F1 and Pass Rate before implementing any new features.
2. **Ticket S5-T2 (Object Detection Audit):** Investigate why 4/5 CLEAR cameras failed in smoke testing (evaluating YOLOv8n vehicle hallucination vs. time-of-day sampling).
3. **Ticket S5-T3 (Decoupled Hybrid Flood Implementation):** Wire rain radar triggers and curate `flood_chokepoint_cameras.json` based on authentic municipal flood data.
4. **Ticket S5-T4 (Blob Storage Driver):** Implement `storage.BlobStorage` for `https://file.529studio.site` to decouple archiving from edge disk space.
5. **Ticket S5-T5 (Infrastructure Guarding):** Test Tailscale network disconnection handling and establish failover policies for `dev-box`.
