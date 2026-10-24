# Sprint 8 — Hướng dẫn thực thi agy CLI theo từng Wave

> **Trạng thái**: Mọi ticket đã được gắn số issue chính thức trên GitHub 
> ([#287](https://github.com/529-studio/projectKV/issues/287), [#288](https://github.com/529-studio/projectKV/issues/288), [#289](https://github.com/529-studio/projectKV/issues/289), [#290](https://github.com/529-studio/projectKV/issues/290), [#246](https://github.com/529-studio/projectKV/issues/246)), bám sát [ADR-010](ADR-010-sprint7-audit-and-sprint8-plan.md). Tài liệu sẵn sàng đưa vào vận hành.

Tài liệu hướng dẫn vận hành cho AI agents (`agy` CLI) và developers thực thi toàn bộ Sprint 8 Backlog theo quy chuẩn [ADR-010](ADR-010-sprint7-audit-and-sprint8-plan.md) (Ground-Truth Perception, Concurrency Resilience & Real-Time Alert Dispatch), [GO_BACKEND_ARCHITECTURE.md](GO_BACKEND_ARCHITECTURE.md) và [AGENTS.md](../AGENTS.md).

---

## 1. Issue Map & Epics Grouping

### Danh sách Epics Sprint 8

| Epic | Tên Epic | Issues con | Mục tiêu chính |
|---|---|---|---|
| **epic:infra-resilience** | `[Epic] Concurrency & Configuration Resilience` | S8-T5, S8-T6, S8-T9 | Sửa dứt điểm deadlock semaphore trong `pipeline.go`; chuẩn hóa toàn bộ 25+ biến môi trường trong `config.go`; kích hoạt token Telegram live trên Staging Stack 14 |
| **epic:flood-integrity** | `[Epic] Crowdsource Runtime Bridge & Live Alerting` | S8-T7 | Đấu nối `internal/feedback` vào `liveHub` và `FloodHexAggregator`, hiển thị marker hổ phách người dân báo ngập tức thì trên bản đồ |
| **epic:vehicle-detection & eval-tooling** | `[Epic] Road Surface Water Segmentation & Zero-VLM Inference` | S8-T8 | Xây dựng toolchain xuất dataset phân vùng mặt nước YOLO, pipeline huấn luyện off-box, và triển khai OpenVINO FP16 trên `dev-box` |

---

### Bảng phân bố Wave thực thi

| Wave | ID | Kanban (Plane) | GitHub Issue | Title | Epic | Labels | Ước lượng (Rule 3) | Phụ thuộc |
|:---:|:---:|:---:|:---:|---|---|---|:---:|:---:|
| **0** | **S8-T5** | **[KV-17]** | [#287](https://github.com/529-studio/projectKV/issues/287) | `[go-server] S8-T5: Harvester Semaphore Deadlock & Labeled Loop Break (internal/harvester)` | epic:infra-resilience | `backend, go, bug` | 4h (0.5d) | Bắt đầu ngay |
| **0** | **S8-T6** | **[KV-18]** | [#288](https://github.com/529-studio/projectKV/issues/288) | `[go-server] S8-T6: Holistic Environment Configuration Sanitization & Fail-Fast Validation (internal/config)` | epic:infra-resilience | `backend, go, enhancement` | 4h (0.5d) | Bắt đầu ngay |
| **1** | **S8-T7** | **[KV-19]** | [#289](https://github.com/529-studio/projectKV/issues/289) | `[go-server + web] S8-T7: Crowdsource Runtime Bridge — Wire internal/feedback to Live Map & Hex Aggregator` | epic:flood-integrity | `backend, frontend, cross-cutting` | 10h (1.5d) | S8-T5, S8-T6 (Hoàn thành / Đã verified) |
| **2** | **S8-T8** | **[KV-20]** | [#290](https://github.com/529-studio/projectKV/issues/290) | `[cv-engine + tooling] S8-T8: Road Surface Water Segmentation Dataset Toolchain & Off-Box Training Pipeline` | epic:eval-tooling | `tooling, epic:eval-tooling` | 16h (2.5d) | ADR-009, ADR-010 |
| **3** | **S8-T9** | **[KV-21]** | [#246](https://github.com/529-studio/projectKV/issues/246) | `[ci + ops] S8-T9: Live Staging Telegram Flood Alert Token Deployment (Stack 14 homesrv)` | epic:infra-resilience | `tooling, cross-cutting` | 4h (0.5d) | S8-T7 |

---

## 2. Chuẩn bị môi trường

Trước khi thực thi bất kỳ ticket nào, đồng bộ nhánh `dev` mới nhất:

```bash
cd ~/repos/projectKV
git checkout dev
git pull origin dev
```

---

## Wave 0 — Khẩn cấp: Ổn định Hạ tầng & Vá lỗi Bất đồng bộ (S8-T5, S8-T6)

Hai ticket này chạy độc lập, giải quyết các lỗi kiến trúc cốt lõi đã được chỉ ra trong đợt kiểm toán ADR-010.

### Terminal 1 — S8-T5 (Sửa lỗi Deadlock Semaphore & Labeled Loop Break trong Harvester)

```bash
cd ~/repos/projectKV

agy "Implement issue #287: [go-server] S8-T5: Harvester Semaphore Deadlock & Labeled Loop Break (internal/harvester).

Read the full issue at https://github.com/529-studio/projectKV/issues/287 for complete Technical Details and Definition of Done.

Key requirements:
1. In services/go-server/internal/harvester/pipeline.go lines 216-220:
   - Fix the critical concurrency bug where a bare 'break' in a select only breaks the select, not the enclosing for loop.
   - Use a labeled loop break:
     harvestLoop:
     for _, cam := range cameras {
         select {
         case sem <- struct{}{}:
         case <-ctx.Done():
             break harvestLoop
         }
         ...
     }
   - Ensure goroutines are NEVER spawned when ctx.Done() is received, preventing stolen semaphore tokens and container shutdown deadlocks.
2. In services/go-server/internal/harvester/pipeline.go and tools/eval/auto_harvest.py:
   - Deprecate DetermineWaterlineDepth weather-based lookup table (rainfall >= 5.0 mm -> SHALLOW).
   - In auto-harvested labels, set flood: 'UNKNOWN' and depth: 'UNKNOWN' unless verified by computer vision, eliminating synthetic answer key pollution per Lesson 39.
3. Add comprehensive unit test TestHarvester_ContextCancellationCleanShutdown in internal/harvester/harvester_test.go:
   - Cancel context immediately during a batch harvest with queued cameras.
   - Verify 0 goroutine leaks, 0 stolen tokens, and graceful exit in < 100ms.
4. Run verification:
   - go test -race -v ./internal/harvester/...
   - make test-go

Branch: fix/s8-t5-harvester-semaphore-deadlock
PR Title: fix(harvester): resolve semaphore deadlock with labeled break and deprecate heuristic weather depth
PR Description: Must strictly start with 'Close #287 closes [KV-17]' per AGENTS.md Rule 9. Do NOT merge."
```

---

### Terminal 2 — S8-T6 (Chuẩn hóa toàn diện biến môi trường & Cảnh báo log có cấu trúc)

```bash
cd ~/repos/projectKV

agy "Implement issue #288: [go-server] S8-T6: Holistic Environment Configuration Sanitization & Fail-Fast Validation (internal/config).

Read the full issue at https://github.com/529-studio/projectKV/issues/288 for complete Technical Details and Definition of Done.

Key requirements:
1. In services/go-server/internal/config/config.go:
   - Extend string, quote, and whitespace sanitization (strings.Trim(strings.TrimSpace(val), \"\\\"'\")) across ALL 25+ configuration variables:
     - SNAPSHOT_CACHE_TTL_SECONDS, HOTSET_INTERVAL_SECONDS, HOTSET_MAX_PARALLEL
     - ARCHIVE_SAMPLE_INTERVAL_SECONDS, HEX_RESOLUTION, HEX_MIN_CAMERAS
     - PCU_WEIGHT_*, PCU_CAPACITY_FLOOR, PCU_THRESHOLD_*
     - HARVESTER_*, DEV_ACCESS_TOKEN, TELEGRAM_*
2. Replace silently discarded errors (if v, err := strconv.Atoi(val); err == nil) with structured warning logs:
   - When parsing fails, log: slog.Warn('config: invalid numeric format, using fallback default', 'key', key, 'raw_value', val, 'default', fallback)
3. Add unit test suite TestLoadConfig_SanitizationAndQuotedVariables in services/go-server/internal/config/config_test.go:
   - Test quoted values (\"30\", '15.0', \"true\")
   - Test values with trailing spaces ('true   ', ' 60 ')
   - Test invalid non-numeric inputs and verify fallback defaults with warnings.
4. Run verification:
   - go test -race -v ./internal/config/...
   - make test-go

Branch: fix/s8-t6-config-sanitization-observability
PR Title: fix(config): holistic environment sanitization and fail-fast structured observability
PR Description: Must strictly start with 'Close #288 closes [KV-18]' per AGENTS.md Rule 9. Do NOT merge."
```

---

## Wave 1 — Đóng Hố Đen Phản Hồi Cộng Đồng (S8-T7)

Sau khi Wave 0 được merge, tiến hành đấu nối module tiếp nhận phản hồi người dân vào bản đồ trực tiếp.

### Terminal 1 — S8-T7 (Đấu nối phản hồi người dân vào Live Map & Hex Aggregator)

```bash
cd ~/repos/projectKV

agy "Implement issue #289: [go-server + web] S8-T7: Crowdsource Runtime Bridge — Wire internal/feedback to Live Map & Hex Aggregator.

Read the full issue at https://github.com/529-studio/projectKV/issues/289 for complete Technical Details and Definition of Done.

Key requirements:
1. In services/go-server/internal/feedback/:
   - Add observer callback: OnReportAccepted func(cameraID string, report Report) to Manager/Handler.
2. In services/go-server/cmd/server/main.go:
   - Wire OnReportAccepted to liveHub.BroadcastUserFloodReport and floodHexAggregator.OnUserReport.
3. In services/go-server/internal/live/hub.go:
   - Broadcast SSE / WebSocket event 'user_flood_report' containing camera_id, timestamp, and report_level.
4. In apps/web/src/components/Map.tsx and HexOverlay.tsx:
   - Listen for user flood reports.
   - Transition affected camera marker to a distinct high-contrast amber state: '[Báo cáo từ người dân: Có ngập]' with a 15-minute sliding TTL.
   - If >= 2 distinct IP hashes report flood at the same camera within 10 minutes, escalate visual prominence.
   - Strictly zero emojis per Rule 1.
5. Run verification:
   - make test-go
   - cd apps/web && npm test && npm run build

Branch: feat/s8-t7-crowdsource-runtime-bridge
PR Title: feat(feedback): wire crowdsourced flood reports to live map and spatial hex aggregator
PR Description: Must strictly start with 'Close #289 closes [KV-19]' per AGENTS.md Rule 9. Do NOT merge."
```

---

## Wave 2 — Thị Giác Phân Vùng Mặt Nước On-Premise (S8-T8) [COMPLETED]

Toolchain xuất dataset phân vùng mặt nước YOLO segmentation, pipeline huấn luyện off-box và logic Dual-Layer Physical Triangulation trong `services/cv-engine/server.py` đã hoàn thành và kiểm thử 100% (Issue #290, PR branch `feat/s8-t8-road-water-segmentation-pipeline`).

### Terminal 1 — S8-T8 (Toolchain xuất dataset phân vùng mặt nước & Pipeline huấn luyện Off-Box)

```bash
cd ~/repos/projectKV

agy "Implement issue #290: [cv-engine + tooling] S8-T8: Road Surface Water Segmentation Dataset Toolchain & Off-Box Training Pipeline.

Read the full issue at https://github.com/529-studio/projectKV/issues/290 for complete Technical Details and Definition of Done.

Key requirements:
1. Build dataset export CLI at tools/eval/export_yolo_seg.py:
   - Reads authentic R2 frames and ground-truth polygon annotations.
   - Exports Ultralytics YOLO segmentation directory format:
     data/yolo_seg/
       images/train/, images/val/
       labels/train/, labels/val/
       dataset.yaml (classes: roadway, water_puddle, motorcycle, car, bus, truck)
2. Author off-box training script tools/train/train_yolov8_seg.py:
   - Designed to run off-box (Google Colab / external GPU workstation) to respect dev-box 20GB disk and 2GB RAM limits.
   - Exports best checkpoint to OpenVINO FP16 IR format (yolov8n_seg.xml and yolov8n_seg.bin, ~6.5 MB).
3. Upgrade services/cv-engine/server.py:
   - Support loading OpenVINO segmentation model.
   - Parse mask prototype tensors (output1) alongside detection boxes (output0).
   - Implement dual-layer physical triangulation logic per ADR-009:
     - Layer 1: Road water ratio R_water = Area(water ∩ road) / Area(road) >= 0.15.
     - Layer 2: Motorcycle wheel submersion ratio delta_sub >= 0.18 (depth >= 20cm -> FLOODED_DEEP).
   - Return structured response: flood_severity, water_ratio, depth_estimated_cm.
4. Run verification:
   - make test-model
   - python3 -m unittest discover -s tools/eval -p '*_test.py'

Branch: feat/s8-t8-road-water-segmentation-pipeline
PR Title: feat(cv-engine): road surface water segmentation dataset toolchain and OpenVINO inference
PR Description: Must strictly start with 'Close #290 closes [KV-20]' per AGENTS.md Rule 9. Do NOT merge."
```

---

## Wave 3 — Nghiệm Thu Staging & Vận Hành Thực Tế (S8-T9) [COMPLETED]

Script verify ops (`scripts/verify_staging_telegram.sh`), runbook vận hành (`docs/runbooks/telegram_staging_verification.md`), regex scrub token Telegram trong response sanitizer, và bộ deterministic acceptance tests (debounce 5 phút, format contract, token redaction) đã hoàn thành và kiểm thử 100% (Issue #246, PR branch `ops/s8-t9-staging-verification`). Bước cấu hình token live trên Portainer Stack 14 do operator thực hiện theo runbook.

Triển khai token và kiểm thử cảnh báo live trên môi trường staging Portainer Stack 14.

### Terminal 1 — S8-T9 (Kiểm thử live bot Telegram trên Staging Stack 14 homesrv — Đóng Issue #246)

```bash
cd ~/repos/projectKV

agy "Implement issue #246: [ci + ops] S8-T9 — Live Staging Telegram Flood Alert Verification & Simulation Deprecation (Stack 14 homesrv).

Read the full issue at https://github.com/529-studio/projectKV/issues/246 for complete Technical Details and Definition of Done.

Key requirements:
1. Create verification test script at scripts/verify_staging_telegram.sh:
   - Simulates flood alert dispatch on camera 586e2995f9fab7001111b0b5 (Ung Văn Khiêm - D2).
   - Verifies HTTP 200 from Telegram Bot API.
2. Author operational runbook at docs/runbooks/telegram_staging_verification.md:
   - Instructions to configure TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID in Portainer Stack 14 environment.
   - Verify zero-emoji HTML formatting and bilingual template per Rule 1.
   - Verify 30-minute sliding debouncer: trigger second alert within 5 minutes and verify suppression.
   - Verify secretsanitizer.go and backend logs scrub the bot token to [REDACTED_BOT_TOKEN].
3. Run verification:
   - go test -race -v ./cmd/server/notify_integration_test.go
   - make test-go

Branch: ops/s8-t9-staging-telegram-verification
PR Title: ops(notify): live staging Telegram alert verification and operational runbook
PR Description: Must strictly start with 'Close #246 closes [KV-21]' per AGENTS.md Rule 9. Do NOT merge."
```

---

## 3. Checklist Tuân Thủ Bắt Buộc (Mandatory Rules 1-9)

Mọi PR trong Sprint 8 bắt buộc phải tự kiểm tra danh sách này trước khi yêu cầu review:

- [ ] **Rule 1 (Zero-Emoji & Đa Ngôn Ngữ)**: Tuyệt đối không có emoji trong UI, toast, modal, log server, hay thông báo Telegram.
- [ ] **Rule 2 (Giải pháp gốc rễ)**: Không dùng mẹo vá tạm (no monkey-patching). Phải sửa tận gốc nguyên nhân kỹ thuật.
- [ ] **Rule 3 (Ước lượng bắt buộc)**: Luôn ghi rõ thời gian thực thi (active hours) và thời hạn bàn giao (delivery window) trong PR description.
- [ ] **Rule 5 (Git Flow)**: Tạo nhánh riêng (`feat/*`, `fix/*`, `ops/*`), mở PR vào `dev`. Không bao giờ tự merge PR.
- [ ] **Rule 6 (Tài liệu sống)**: Cập nhật đồng thời `docs/lesson_learn.md`, `docs/project_status.md` và `README.md` (cả tiếng Anh và tiếng Việt) nếu tính năng hoặc kiến trúc thay đổi.
- [ ] **Rule 7 (Chuẩn Leaf Modulith)**: Không import chéo giữa các package con trong `services/go-server/internal/`. Chỉ dùng SQL thuần (không ORM).
- [ ] **Rule 8 (Bảo mật bí mật)**: Không hardcode token, API key. Giá trị fallback trong code bắt buộc là `""`.
- [ ] **Rule 9 (Cú pháp liên kết PR)**: Mô tả PR bắt buộc bắt đầu bằng `Close #<issue> closes [KV-<card>]`.
