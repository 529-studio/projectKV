# Sprint 9 — Hướng dẫn thực thi agy CLI theo từng Wave

> **Trạng thái**: Toàn bộ 6 ticket đã có issue chính thức trên GitHub
> ([#297](https://github.com/529-studio/projectKV/issues/297),
> [#298](https://github.com/529-studio/projectKV/issues/298),
> [#299](https://github.com/529-studio/projectKV/issues/299),
> [#300](https://github.com/529-studio/projectKV/issues/300),
> [#301](https://github.com/529-studio/projectKV/issues/301),
> [#302](https://github.com/529-studio/projectKV/issues/302)),
> bám sát [ADR-011](ADR-011-sprint8-audit-and-sprint9-plan.md) và kết quả audit
> [Sprint 8 Report](sprint8_report.md). Tài liệu sẵn sàng đưa vào vận hành.
> Theo dõi tiến độ trên GitHub Projects v2 board **ProjectKV** (ADR-011 D6; Plane Hub đã decommission).

Tài liệu hướng dẫn vận hành cho AI agents (`agy` CLI) và developers thực thi toàn bộ Sprint 9 Backlog theo quy chuẩn [ADR-011](ADR-011-sprint8-audit-and-sprint9-plan.md) (Ground-Truth Segmentation Flywheel & Live Model Retraining), [GO_BACKEND_ARCHITECTURE.md](GO_BACKEND_ARCHITECTURE.md) và [AGENTS.md](../AGENTS.md).

---

## 1. Issue Map & Epics Grouping

### Danh sách Epics Sprint 9

| Epic | Tên Epic | Issues con | Mục tiêu chính |
|---|---|---|---|
| **epic:eval-tooling** | `[Epic] Ground-Truth Segmentation Flywheel` | S9-T1, S9-T2, S9-T4 | Polygon annotation contract + canvas editor, SAM-assisted pre-labeling trên dev-box, huấn luyện segmentation lần đầu với mAP gate và deploy weights |
| **epic:infra-resilience** | `[Epic] Autonomous Staging & Alert Hygiene` | S9-T3, S9-T5 | Harvester chạy tự chủ theo giờ cố định trên staging + observability không cần terminal; vá parity bug script Telegram |
| **(mở rộng)** | `[Epic] Forecast Foundation` (stretch) | S9-T6 | Prototype dự báo tắc đường 10-30 phút từ chuỗi PCU, vượt baseline persistence |

---

### Bảng phân bố Wave thực thi

| Wave | ID | GitHub Issue | Title | Epic | Labels | Ước lượng (Rule 3) | Phụ thuộc |
|:---:|:---:|:---:|---|---|---|:---:|:---:|
| **0** | **S9-T1** | [#297](https://github.com/529-studio/projectKV/issues/297) | `[go-server + web + tooling] S9-T1: Segmentation Polygon Annotation Contract & Canvas Editor in /labeler` | epic:eval-tooling | `backend, frontend, cross-cutting` | 16-20h (3d) | Bắt đầu ngay (backend contract đã dở dang trên branch `feat/polygon-labeler-annotation-contract`) |
| **1** | **S9-T2** | [#298](https://github.com/529-studio/projectKV/issues/298) | `[tooling] S9-T2: SAM-Assisted Polygon Pre-Labeling Batch Toolchain (dev-box offline)` | epic:eval-tooling | `tooling` | 8-10h (1.5d) | S9-T1 |
| **1** | **S9-T3** | [#299](https://github.com/529-studio/projectKV/issues/299) | `[ops + go-server] S9-T3: Staging Autonomous Data Flywheel — Scheduled Harvester, Auto-Label & Status Observability` | epic:infra-resilience | `tooling, backend, cross-cutting` | 6-8h (1.5d) | Bắt đầu ngay (chạy song song Wave 0) |
| **2** | **S9-T4** | [#300](https://github.com/529-studio/projectKV/issues/300) | `[tooling + cv-engine] S9-T4: First Off-Box YOLOv8-Seg Training, mAP Benchmark Gate & Staging Weights Deployment` | epic:eval-tooling | `tooling, cross-cutting` | 10-12h (2d) | S9-T1, S9-T2, S9-T3 |
| **3** | **S9-T5** | [#301](https://github.com/529-studio/projectKV/issues/301) | `[go-server + ops] S9-T5: Telegram Verification Script Single-Source Parity Rework (notify-preview)` | epic:infra-resilience | `backend, tooling` | 4h (0.5d) | Bắt đầu ngay |
| **3** | **S9-T6** | [#302](https://github.com/529-studio/projectKV/issues/302) | `[go-server] S9-T6: Short-Horizon Congestion Forecast Prototype (PCU Time-Series, 10-30 min) — stretch` | (mở rộng) | `backend, enhancement` | 12h (2d) | Chỉ khởi động khi Wave 0-2 không chặn |

---

## 2. Chuẩn bị môi trường

Trước khi thực thi bất kỳ ticket nào, đồng bộ nhánh `dev` mới nhất:

```bash
cd ~/repos/projectKV
git checkout dev
git pull origin dev
```

Sự thật phần cứng cần nhớ (ADR-011 §1.6, Outline `[Arch]` docs):
- **dev-box** (Ryzen 7 6800HS, AVX2): mọi batch inference (SAM pre-label, benchmark) chạy ở đây, ngoài giờ cao điểm.
- **home-srv** (Celeron N2930, non-AVX): chỉ ingress + fallback; cấm chạy PyTorch/ONNX prebuilt và SAM.

---

## Wave 0 — Critical Path: Polygon Contract & Canvas Editor (S9-T1)

### Terminal 1 — S9-T1 (Contract polygon + Canvas editor trong /labeler — Đóng Issue #297)

```bash
cd ~/repos/projectKV

agy "Implement issue #297: [go-server + web + tooling] S9-T1: Segmentation Polygon Annotation Contract & Canvas Editor in /labeler.

Read the full issue at https://github.com/529-studio/projectKV/issues/297 for complete Technical Details and Definition of Done.

Key requirements:
1. Backend (internal/archiver): PolygonAnnotation{class, polygon normalized [0,1], source} on LabelPayload;
   SaveLabel canonicalizes alias classes and validates (6-class taxonomy, 3-64 vertices, <=32 annotations,
   reject pixel coordinates). Note: an uncommitted draft exists on branch feat/polygon-labeler-annotation-contract —
   rebase onto it, do not duplicate.
2. tools/eval/export_yolo_seg.py: synthesis becomes opt-in via --allow-synthetic; default skips labels without
   explicit annotations and reports skipped_no_annotations (Lesson 39 extended to geometry).
3. internal/harvester/schema.go + tools/eval/label_schema.json: optional annotations property.
4. apps/web: PolygonCanvas.tsx (SVG overlay, zero new dependencies; click-add vertices, close, drag, delete;
   6-class selector, monochromatic tones; normalized coords); labeler/page.tsx segmentation mode toggle +
   merge annotations into POST payload + provenance badge [Tu dong: SAM]; labelerUtils polygon helpers + tests.
5. Run verification:
   - make test-go
   - cd apps/web && npm test && npm run build
   - python3 -m unittest discover -s tools/eval -p '*_test.py'
   - make swagger
   - Browser E2E: draw a real polygon on a local snapshot -> save -> GET labels shows geometry ->
     export produces YOLO txt with the real polygon and zero synthesized masks (screenshots as evidence).

Branch: feat/s9-t1-polygon-annotation-contract (continue from feat/polygon-labeler-annotation-contract if unmerged)
PR Title: feat(labeler): segmentation polygon annotation contract and canvas editor
PR Description: Must strictly start with 'Close #297' per AGENTS.md Rule 9 (GitHub-native linking; KV format retired by ADR-011 D6). Do NOT merge."
```

---

## Wave 1 — Tăng tốc dữ liệu & Flywheel tự chủ (S9-T2, S9-T3)

Hai ticket chạy song song: T2 phụ thuộc T1, T3 độc lập (ops).

### Terminal 1 — S9-T2 (SAM-assisted pre-labeling offline trên dev-box)

```bash
cd ~/repos/projectKV

agy "Implement issue #298: [tooling] S9-T2: SAM-Assisted Polygon Pre-Labeling Batch Toolchain (dev-box offline).

Read the full issue at https://github.com/529-studio/projectKV/issues/298 for complete Technical Details and Definition of Done.

Key requirements:
1. tools/seg/prelabel_sam.py following the auto_harvest.py / batch_auto_label.py pattern: stream R2 frames via boto3,
   run EfficientViT-SAM/MobileSAM class model (ONNX/OpenVINO; weights path from environment per Rule 8),
   emit candidate labels with annotations (ADR-011 D1 contract) and annotator=sam_prelabel to R2.
2. Roadway proposed via largest stable central region; water_puddle proposal-only; vehicles via existing detector bboxes.
3. Guardrails: token bucket, exponential backoff, --dry-run, --limit, --date; host guard refusing non-AVX machines.
4. Runbook docs/runbooks/sam_prelabeling.md with measured throughput on dev-box (>= 50 real frames).
5. Run verification: python3 -m unittest discover -s tools/seg -p '*_test.py'

Branch: feat/s9-t2-sam-prelabel-toolchain
PR Title: feat(tooling): SAM-assisted polygon pre-labeling batch toolchain
PR Description: Must strictly start with 'Close #298'. Do NOT merge."
```

### Terminal 2 — S9-T3 (Harvester tự chủ trên staging + observability)

```bash
cd ~/repos/projectKV

agy "Implement issue #299: [ops + go-server] S9-T3: Staging Autonomous Data Flywheel — Scheduled Harvester, Auto-Label & Status Observability.

Read the full issue at https://github.com/529-studio/projectKV/issues/299 for complete Technical Details and Definition of Done.

Key requirements:
1. Baseline first: run make dev-all locally, capture harvester behavior from the Go backend (localhost:3000) and
   web app (localhost:3001) logs BEFORE touching staging.
2. Go backend: add GET /api/harvester/status (behind dev auth): last/next slot, cameras harvested, frames/labels
   uploaded, recent errors. Unit tested.
3. Portainer Stack 14: HARVESTER_ENABLED=true, HARVESTER_SCHEDULE_HOURS=7,12,17, HARVESTER_CONCURRENCY=15;
   confirm CV_ENGINE_URL routing to dev-box engine with internal fallback.
4. Verify one full day: 3 scheduled slots in logs + R2 object counts per slot; one rain-trigger path with
   60-minute cooldown respected; docker stats evidence of no regression on home-srv.
5. Runbook docs/runbooks/staging_harvester_flywheel.md incl. rollback (HARVESTER_ENABLED=false).
6. Run verification: make test-go

Branch: ops/s9-t3-staging-data-flywheel
PR Title: ops(harvester): autonomous staging data flywheel enablement and status observability
PR Description: Must strictly start with 'Close #299'. Do NOT merge."
```

---

## Wave 2 — Huấn luyện lần đầu & Deploy (S9-T4)

Sau khi Wave 0 + 1 merge và staging flywheel đã chạy (có dữ liệu mask thật).

### Terminal 1 — S9-T4 (Train off-box, mAP gate, deploy weights)

```bash
cd ~/repos/projectKV

agy "Implement issue #300: [tooling + cv-engine] S9-T4: First Off-Box YOLOv8-Seg Training, mAP Benchmark Gate & Staging Weights Deployment.

Read the full issue at https://github.com/529-studio/projectKV/issues/300 for complete Technical Details and Definition of Done.

Key requirements:
1. Dataset v1 via export_yolo_seg.py (post S9-T1): >= 500 masked frames target, stratified rain/dry + day/night;
   commit manifest only (images stay on R2/local).
2. Off-box training (Colab/GPU) via tools/train/train_yolov8_seg.py; document hyperparameters and checkpoint lineage.
3. Evaluation: mask mAP50-95 + per-class IoU on held-out gold split; report docs/reports/seg_eval_v1.md.
   Deployment gate: water_puddle IoU must beat null baseline.
4. Latency p95 at 416px on dev-box vs 30s/57-camera budget (fall back to 320px if exceeded).
5. Deploy yolov8n_seg weights into dev-box model volume per DEVOPS_S3_OPENVINO_DEPLOYMENT.md; verify /healthz
   model_loaded, /predict segmentation fields, POST /api/flood/verify segmentation-sourced verdict; rollback documented.
6. Run verification: make test-model; python3 -m unittest discover -s tools/eval -p '*_test.py'

Branch: feat/s9-t4-first-seg-training-and-deploy
PR Title: feat(cv-engine): first segmentation training cycle, mAP benchmark gate and weights deployment
PR Description: Must strictly start with 'Close #300'. Do NOT merge."
```

---

## Wave 3 — Dọn nợ & Stretch (S9-T5, S9-T6)

### Terminal 1 — S9-T5 (Vá parity script Telegram bằng notify-preview)

```bash
cd ~/repos/projectKV

agy "Implement issue #301: [go-server + ops] S9-T5: Telegram Verification Script Single-Source Parity Rework (notify-preview).

Read the full issue at https://github.com/529-studio/projectKV/issues/301 for complete Technical Details and Definition of Done.

Key requirements:
1. Extract the dispatchFloodAlert mapping precedence (DisplayName->Title->CameraID; district=cam.Zone; ward=cam.PhuongName;
   chokepoint depth) into an exported pure helper in internal/notify (leaf-safe, primitives only) + unit tests;
   cmd/server/main.go consumes it (behavior unchanged).
2. New CLI services/go-server/cmd/notify-preview: -camera-id/-severity/-observed-at; loads camera catalog
   (config.ResolvePath, CAMERAS_FILE_PATH) + chokepoints fixture; prints FormatTelegramMessage output; fail-fast on unknown camera.
3. scripts/verify_staging_telegram.sh Check 3 generates the message via go run ./services/go-server/cmd/notify-preview
   (fail fast when Go toolchain missing); keep zero-emoji/bilingual assertions.
4. Update TestFormatTelegramMessage_StagingChokepointFixture to true production values
   (Area: Thạnh Mỹ Tây, Khu vực Nội thành TP.HCM); correct the runbook wording and Go prerequisite.
5. Run verification: make test-go; script 5/5 against a local mock Bot API.

Branch: fix/s9-t5-notify-preview-parity
PR Title: fix(notify): single-source staging verification message via notify-preview
PR Description: Must strictly start with 'Close #301'. Do NOT merge."
```

### Terminal 2 — S9-T6 (Prototype forecast — stretch, chỉ chạy khi Wave 0-2 xong)

```bash
cd ~/repos/projectKV

agy "Implement issue #302: [go-server] S9-T6: Short-Horizon Congestion Forecast Prototype (PCU Time-Series, 10-30 min) — stretch.

Read the full issue at https://github.com/529-studio/projectKV/issues/302 for complete Technical Details and Definition of Done.

Key requirements:
1. Feature store from SQLite hotset verdict history (PCU/SI per camera, 5-10 min bins); documented schema.
2. Backtest harness: horizons 10/20/30 min; baselines persistence + historical median; candidate = CPU-light
   gradient boosting / regularized regression; report docs/reports/forecast_prototype_v1.md.
3. GET /api/v1/cameras/hotset/forecast?horizon_minutes=30 with staleness guard (no fabricated values on stale data).
4. No UI exposure until the model beats persistence on >= 1 horizon.
5. Run verification: make test-go

Branch: feat/s9-t6-congestion-forecast-prototype
PR Title: feat(hotset): short-horizon congestion forecast prototype with backtest gate
PR Description: Must strictly start with 'Close #302'. Do NOT merge."
```

---

## 3. Checklist Tuân Thủ Bắt Buộc (Mandatory Rules 1-9)

Mọi PR trong Sprint 9 bắt buộc phải tự kiểm tra danh sách này trước khi yêu cầu review:

- [ ] **Rule 1 (Zero-Emoji & Đa Ngôn Ngữ)**: Tuyệt đối không có emoji trong UI, toast, modal, log server, hay thông báo Telegram.
- [ ] **Rule 2 (Giải pháp gốc rễ)**: Không dùng mẹo vá tạm (no monkey-patching). Phải sửa tận gốc nguyên nhân kỹ thuật.
- [ ] **Rule 3 (Ước lượng bắt buộc)**: Luôn ghi rõ thời gian thực thi (active hours) và thời hạn bàn giao (delivery window) trong PR description.
- [ ] **Rule 5 (Git Flow)**: Tạo nhánh riêng (`feat/*`, `fix/*`, `ops/*`), mở PR vào `dev`. Không bao giờ tự merge PR.
- [ ] **Rule 6 (Tài liệu sống)**: Cập nhật đồng thời `docs/project_status.md`, `docs/lesson_learn.md` và `README.md` (cả tiếng Anh và tiếng Việt) nếu tính năng hoặc kiến trúc thay đổi.
- [ ] **Rule 7 (Chuẩn Leaf Modulith)**: Không import chéo giữa các package con trong `services/go-server/internal/`. Chỉ dùng SQL thuần (không ORM).
- [ ] **Rule 8 (Bảo mật bí mật)**: Không hardcode token, API key, kể cả đường dẫn weights mang tính credential. Giá trị fallback trong code bắt buộc là `""`.
- [ ] **Rule 9 (Cú pháp liên kết PR — cập nhật theo ADR-011 D6)**: Mô tả PR bắt buộc bắt đầu bằng `Close #<issue-number>`. Format Kanban `closes [KV-<card-id>]` đã retire vì Plane Hub decommission (10/2026); tiến độ theo dõi trên GitHub Projects v2 board ProjectKV.
