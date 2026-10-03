# Sprint 3 — Hướng dẫn chạy agy CLI theo từng Wave

> **Trạng thái**: Mọi placeholder `#<TBD-Sx>` đã được thay bằng số issue thật
> (S1=#135, S2=#136, S3=#137, S4=#138, S5=#139, S5b=#140, S6=#141, S7=#142,
> S8=#143, S9=#144). File sẵn sàng sử dụng.

Tài liệu hướng dẫn vận hành cho AI agents (`agy` CLI) và developers thực thi toàn
bộ Sprint 3 Backlog theo quy chuẩn [ADR-004](docs/ADR-004-route-check-and-snapshot-archive.md)
(§10, §13, §14, §15), [ADR-001](docs/ADR-001-flood-alert-architecture.md), và
[GO_BACKEND_ARCHITECTURE.md](docs/GO_BACKEND_ARCHITECTURE.md).

---

## 1. Issue Map & Epics Grouping

### Danh sách Epics

| Epic | Tên Epic | Issues con | Mục tiêu chính |
|---|---|---|---|
| **epic:vehicle-detection** | `[Epic] Self-Hosted Vehicle Detection Pipeline` | S1, S2, S3, S4 | Chuyển phân tích xe cộ từ VLM sang self-hosted OpenVINO, có PCU scoring |
| **epic:dock-ux** | `[Epic] Dock UX Completion (A4 Carryover)` | S5, S5b | Hoàn thiện A4 còn nợ từ Sprint 2: kéo thả, cỡ tile, severity badge |
| **epic:eval-tooling** | `[Epic] Evaluation & Labeling Tooling` | S6 | Tăng tốc gán nhãn frame ngập phục vụ Accuracy Gate (D6) |
| **epic:infra-resilience** | `[Epic] Infrastructure Resilience & Observability` | S7, S8 | Giám sát I/O đĩa, xử lý SPOF `local-homesrv` đã xác nhận |
| **epic:flood-integrity** | `[Epic] Flood Signal Integrity` | S9 | Tách lớp ngập xác thực-camera khỏi lớp radar dự báo |

---

### Bảng phân bố Wave thực thi

| Wave | ID | Title | Epic | Labels | GitHub URL |
|:---:|:---:|---|---|---|---|
| **0** | S1 | `[spike] vehicle-analysis capacity decision: corridor scope & cycle interval` | epic:vehicle-detection | spike, backend | `#135` |
| **0** | S5 | `[web] dock rework: drag-to-reorder, size presets, pin cap (A4 carryover)` | epic:dock-ux | frontend | `#139` |
| **0** | S6 | `[tooling] flood-frame hotkey labeler tool` | epic:eval-tooling | tooling | `#141` |
| **0** | S7 | `[infra] disk I/O baseline + storm-burst simulation on local-homesrv` | epic:infra-resilience | infra | `#142` |
| **0** | S8 | `[infra + go-server] local-homesrv <-> warded-stg resilience and fallback behavior` | epic:infra-resilience | infra, backend | `#143` |
| **0** | S9 | `[web] separate flood-hex (camera-verified) from weather radar (forecast)` | epic:flood-integrity | frontend | `#144` |
| **1** | S2 | `[tooling] curate Hot Set vehicle-detection camera list via A6 road_keys filter` | epic:vehicle-detection | tooling | `#136` |
| **2** | S3 | `[go-server + cv-engine] self-hosted vehicle analyzer (OpenVINO)` | epic:vehicle-detection | backend | `#137` |
| **3** | S4 | `[go-server] PCU-weighted Saturation Index traffic severity mapping` | epic:vehicle-detection | backend | `#138` |
| **3** | S5b | `[web] wire real severity badge into pinned dock` | epic:dock-ux | frontend | `#140` |

> S5, S6, S7, S8, S9 không phụ thuộc gì — nằm ở Wave 0 cùng S1 chỉ để tận dụng
> chạy song song, không phải vì chúng cần chờ S1. S2 bắt buộc chờ S1 (cần quyết
> định phạm vi hành lang/chu kỳ trước khi lọc danh sách camera). S3 chờ S2. S4 và
> S5b chờ S3.

---

## 2. Chuẩn bị môi trường

```bash
cd ~/repos/projectKV
git checkout dev
git pull origin dev
```

---

## Wave 0 — Nền tảng Độc lập (S1, S5, S6, S7, S8, S9)

Sáu ticket này độc lập với nhau, mở 6 terminal chạy song song.

### Terminal 1 — S1 (Quyết định phạm vi hành lang & chu kỳ phân tích)

```bash
cd ~/repos/projectKV

agy "Implement issue #135: [spike] vehicle-analysis capacity decision:
corridor scope & cycle interval under current CPU limit.

Read the full issue at https://github.com/529-studio/projectKV/issues/135 for complete Technical Details, Out of Scope,
and Definition of Done.

Key requirements:
- Do NOT raise warded-model's 1.0 CPU / 512MB limit in this issue — ADR-004
  Finding 2 confirms local-homesrv is a single point of failure for the entire
  public warded-stg service; do not add load to it here.
- Evaluate two options quantitatively, reusing spike #104's measured 2.27 FPS /
  440ms-per-frame single-core throughput:
  - Option 1: narrow to 4-5 priority corridors (~55 cameras), 30s cycle.
  - Option 3: all curated intersection cameras across 16 corridors (~245-286),
    cycle widened to 45-60s.
- Document findings and a recommendation in docs/spike-vehicle-capacity.md —
  do NOT silently pick one and let it cascade into S2; flag for explicit human
  confirmation before S2 starts (same human-gate convention as other waves).
- No code changes to services/go-server or apps/web in this issue.

Branch: spike/s1-vehicle-capacity
Open PR against dev when done. Do NOT merge."
```

### Terminal 2 — S5 (Dock rework: kéo thả, cỡ tile, pin cap)

```bash
cd ~/repos/projectKV

agy "Implement issue #139: [web] dock rework: drag-to-reorder, size
presets, pin cap (A4 carryover).

Read the full issue at https://github.com/529-studio/projectKV/issues/139 for complete Technical Details, Out of Scope,
and Definition of Done.

Key requirements:
- Drag-to-reorder pinned cameras using Pointer Events (no library), with
  touch fallback (◀ ▶ buttons) and keyboard support
- 3 tile size presets (S/M/L) and a full-width grid view
- Pin cap (12), pin list + order persisted in localStorage
- Each tile shows name, image age, online/stale state
- Severity: show an explicit 'no analysis yet' state for now — do NOT block
  on S3/S4, the real badge is wired separately in S5b
- No new npm dependency added

Branch: feat/s5-dock-rework
Open PR against dev when done. Do NOT merge."
```

### Terminal 3 — S6 (Công cụ gán nhãn frame ngập)

```bash
cd ~/repos/projectKV

agy "Implement issue #141: [tooling] flood-frame hotkey labeler tool.

Read the full issue at https://github.com/529-studio/projectKV/issues/141 for complete Technical Details, Out of Scope,
and Definition of Done.

Key requirements:
- tools/eval/labeler.py (or a local HTML/JS page) reading frames from
  data/proofs/
- Single-keypress labeling (1=dry, 2=shallow, 3=deep) against the existing
  tools/eval/label_schema.json
- No new heavy dependency (no CVAT, no Label Studio) — keep it minimal
- Document usage in tools/eval/README.md

Branch: feat/s6-flood-labeler
Open PR against dev when done. Do NOT merge."
```

### Terminal 4 — S7 (Giám sát I/O đĩa trên local-homesrv)

```bash
cd ~/repos/projectKV

agy "Implement issue #142: [infra] disk I/O baseline + storm-burst
simulation on local-homesrv.

Read the full issue at https://github.com/529-studio/projectKV/issues/142 for complete Technical Details, Out of Scope,
and Definition of Done.

Key requirements:
- Capture baseline disk I/O wait/throughput on local-homesrv under normal load
- Simulate a storm-burst scenario: concurrent archiver writes + SQLite writes +
  Valhalla tile reads happening together
- Document a concrete threshold for when to move to SSD or split services
  across disks
- Output as a checked-in report (docs/) — no code change required unless a
  monitoring script is the chosen deliverable

Branch: feat/s7-disk-io-monitoring
Open PR against dev when done. Do NOT merge."
```

### Terminal 5 — S8 (Khả năng chịu lỗi local-homesrv ↔ warded-stg)

```bash
cd ~/repos/projectKV

agy "Implement issue #143: [infra + go-server] local-homesrv <->
warded-stg resilience and fallback behavior.

Read the full issue at https://github.com/529-studio/projectKV/issues/143 for complete Technical Details, Out of Scope,
and Definition of Done.

Key requirements:
- ADR-004 Finding 2 (confirmed, not hypothetical): warded-backend,
  warded-frontend, and warded-model all run on the single local-homesrv box,
  WiFi-only uplink — this is a real SPOF for the entire public service
- Define and implement degrade-to-'unknown' behavior (reusing the existing
  daily-budget circuit breaker pattern from internal/hotset/budget.go) if an
  internal dependency (warded-model, Valhalla) becomes unreachable — never
  hard-fail the whole request and never freeze on a stale last-known state
- Document the SPOF explicitly in docs/ even if full mitigation (e.g. a
  second host) is out of scope for this issue — acknowledging and guarding
  against it is the deliverable here, not eliminating it

Branch: feat/s8-resilience-fallback
Open PR against dev when done. Do NOT merge."
```

### Terminal 6 — S9 (Tách lớp ngập camera khỏi radar dự báo)

```bash
cd ~/repos/projectKV

agy "Implement issue #144: [web] separate flood-hex (camera-verified)
from weather radar (forecast) as distinguishable layers.

Read the full issue at https://github.com/529-studio/projectKV/issues/144 for complete Technical Details, Out of Scope,
and Definition of Done.

Key requirements:
- ADR-004 Finding 3 (confirmed): the single 'flood' mode toggle currently
  drives both RainRadarLayer (forecast) and the camera-verified flood hex
  simultaneously; with floodHexCells empty today, users only ever see radar
  clouds with no indication it is forecast, not a camera-confirmed observation
- Two independently labeled layers/controls, both can be enabled together
- Camera-verified flood hex must render and stay visible even when the radar
  forecast layer is also enabled — never hidden behind it
- i18n labels in vi and en make the observed-vs-forecast distinction explicit
  (e.g. 'Dự báo radar' vs 'Camera xác nhận')

Branch: feat/s9-flood-radar-separation
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 0 xong
1. Review PRs: S1, S5, S6, S7, S8, S9
2. Merge vào `dev`
3. **Riêng S1**: xác nhận quyết định (Option 1 hay Option 3) trước khi mở Wave 1 — đây là điểm cần quyết định của người, không để agent tự chọn.
4. Cập nhật local: `git checkout dev && git pull origin dev`

---

## Wave 1 — Chốt danh sách camera (S2)

### Terminal 1 — S2 (Lọc camera giao lộ theo A6 + quyết định của S1)

```bash
cd ~/repos/projectKV

agy "Implement issue #136: [tooling] curate Hot Set vehicle-detection
camera list via A6 road_keys intersection filter.

Read the full issue at https://github.com/529-studio/projectKV/issues/136 for complete Technical Details, Out of Scope,
and Definition of Done. Also read docs/spike-vehicle-capacity.md (merged from
S1) for the confirmed corridor scope.

Key requirements:
- Filter cameras_enriched.json: road_keys length == 2 AND at least one key
  matches the corridor set confirmed in S1 — reuse A6's existing road_keys
  field, do not add new classification logic
- Exclude offline (status != 'UP') and duplicate cameras
- Output must be reproducible: a script, not a hand-picked list
- Document the final N and cross-check it against S1's target range

Branch: feat/s2-curate-hotset
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 1 xong
1. Review PR: S2
2. Merge vào `dev`
3. Cập nhật local: `git checkout dev && git pull origin dev`

---

## Wave 2 — Vehicle Analyzer Self-Hosted (S3)

### Terminal 1 — S3 (OpenVINO analyzer, sửa standby response trước)

```bash
cd ~/repos/projectKV

agy "Implement issue #137: [go-server + cv-engine] self-hosted vehicle
analyzer (OpenVINO).

Read the full issue at https://github.com/529-studio/projectKV/issues/137 for complete Technical Details, Out of Scope,
and Definition of Done.

Key requirements:
- FIRST, before anything else: fix warded-model's standby response (ADR-004
  Finding 1) — POST /predict currently returns HTTP 200 with a fabricated
  {\"traffic_severity\": \"CLEAR\", \"confidence\": 0.95, \"mode\":
  \"standby_ready\"} when no model is loaded. Change this to a non-200 status,
  or at minimum an explicit \"source\": \"standby_dummy\" with confidence 0.0.
  Do this before wiring any Go client to CV_ENGINE_URL.
- Benchmark stock COCO-pretrained YOLOv8n/v11n vs. DatapolisX's best.pt (MIT
  licensed, Vietnamese-vehicle-specific) via tools/eval (B4 harness) on
  Warded's own camera frames — not DatapolisX's. Record the decision with
  numbers in the PR description.
- Export the chosen model to OpenVINO IR; mount into warded-model per
  docker-compose.stg.yml
- Implement a Go-side analyzer in internal/cvclient/ implementing
  hotset.Analyzer, calling CV_ENGINE_URL
- Set Verdict.Source accordingly (e.g. 'onnx') — never leave it indistinguishable from 'mock' or 'vlm'
- Unit tests with mocked HTTP responses for both the real-prediction and
  standby-rejection paths
- Run architecture-guardian skill review before completing

Branch: feat/s3-vehicle-analyzer
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 2 xong
1. Review PR: S3
2. Merge vào `dev`
3. Cập nhật local: `git checkout dev && git pull origin dev`

---

## Wave 3 — PCU Scoring & Severity Badge (S4, S5b)

### Terminal 1 — S4 (PCU-weighted Saturation Index)

```bash
cd ~/repos/projectKV

agy "Implement issue #138: [go-server] PCU-weighted Saturation Index
traffic severity mapping.

Read the full issue at https://github.com/529-studio/projectKV/issues/138 for complete Technical Details, Out of Scope,
and Definition of Done.

Key requirements:
- Per-class vehicle counts (motorcycle/car/bus/truck/container) from S3's
  analyzer output -> PCU-weighted score -> CLEAR/MODERATE/HEAVY/CONGESTED
- Starting weights (motorcycle 0.2, car 1.0, bus/truck 2.0, container 3.0) are
  a configurable starting point, not hardcoded as final — document as
  calibratable against the B4 eval set once real labeled data exists
- Unit tests on the bucket boundaries

Branch: feat/s4-pcu-saturation-index
Open PR against dev when done. Do NOT merge."
```

### Terminal 2 — S5b (Nối severity badge thật vào dock)

```bash
cd ~/repos/projectKV

agy "Implement issue #140: [web] wire real severity badge into pinned
dock.

Read the full issue at https://github.com/529-studio/projectKV/issues/140 for complete Technical Details, Out of Scope,
and Definition of Done.

Key requirements:
- Replace S5's placeholder 'no analysis yet' state with the real severity
  badge, now that S4's CLEAR/MODERATE/HEAVY/CONGESTED mapping exists
- Reuse S5's existing tile layout — no restructuring
- Badge reflects source/staleness rules already established (A5): gray for
  stale, explicit state for source=mock outside dev

Branch: feat/s5b-dock-severity-badge
Open PR against dev when done. Do NOT merge."
```

### ⏸️ Chờ Wave 3 xong
1. Review PRs: S4, S5b
2. Merge vào `dev`
3. ⛔ Sprint 3 Review

---

## 3. Tổng kết quy trình Workflow

```
Wave 0:  T1(S1) ┃ T2(S5) ┃ T3(S6) ┃ T4(S7) ┃ T5(S8) ┃ T6(S9)
         ─────────────────────────────────────────────────────
                ⏸️ Review & Merge to dev
                ⏸️ Người xác nhận quyết định S1 (Option 1 / Option 3)
         ─────────────────────────────────────────────────────
Wave 1:  T1(S2)
         ─────────────────────────────────────────────────────
                ⏸️ Review & Merge to dev
         ─────────────────────────────────────────────────────
Wave 2:  T1(S3)
         ─────────────────────────────────────────────────────
                ⏸️ Review & Merge to dev
         ─────────────────────────────────────────────────────
Wave 3:  T1(S4) ┃ T2(S5b)
         ─────────────────────────────────────────────────────
                ⛔ Sprint 3 Review
```

> **IMPORTANT**: Giữa mỗi wave, bắt buộc merge PR vào `dev` và `git pull origin
> dev` trước khi mở wave tiếp theo.
>
> **CAUTION**: Tuân thủ nghiêm `AGENTS.md`: AI agents tuyệt đối không tự ý merge
> Pull Request — mọi PR phải qua Human review trước.
>
> **CAUTION riêng cho S1 → S2**: khác với các wave khác, cổng giữa Wave 0 và
> Wave 1 không chỉ là review code — còn cần người xác nhận rõ ràng chọn Option 1
> hay Option 3 trước khi S2 chạy, vì đây là đánh đổi sản phẩm (độ phủ vs tần suất
> cập nhật), không phải quyết định kỹ thuật thuần agent tự quyết được.
