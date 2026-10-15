# Sprint 7 — Hướng dẫn thực thi agy CLI theo từng Wave

> **Trạng thái**: Mọi ticket đã được gắn số issue chính thức trên GitHub 
> ([#246](https://github.com/529-studio/projectKV/issues/246) đến [#255](https://github.com/529-studio/projectKV/issues/255)), bám sát [ADR-008](ADR-008-sprint6-audit-and-sprint7-plan.md) (PR [#256](https://github.com/529-studio/projectKV/pull/256)). File sẵn sàng đưa vào vận hành.

Tài liệu hướng dẫn vận hành cho AI agents (`agy` CLI) và developers thực thi toàn bộ Sprint 7 Backlog theo quy chuẩn [ADR-008](ADR-008-sprint6-audit-and-sprint7-plan.md) (Autonomous Arterial Vision, Crowdsource Data Flywheel & Map Clutter Hygiene), [GO_BACKEND_ARCHITECTURE.md](GO_BACKEND_ARCHITECTURE.md) và [AGENTS.md](../AGENTS.md).

---

## 1. Issue Map & Epics Grouping

### Danh sách Epics Sprint 7

| Epic | Tên Epic | Issues con | Mục tiêu chính |
|---|---|---|---|
| **epic:dock-ux** | `[Epic] Dock, Map & Modal Usability Hardening` | S7-T2, S7-T3, S7-T6 | Ẩn 125 camera mất tín hiệu; thêm nút xúc giác "Báo cáo ngập tại đây"; tô màu thời gian thực cho 50 camera trục chính |
| **epic:infra-resilience** | `[Epic] Infrastructure Resilience & Staging Verification` | S7-T1 | Kiểm thử live bot Telegram trên môi trường staging Portainer Stack 14, bãi bỏ chế độ giả lập |
| **epic:flood-integrity** | `[Epic] Verified Real-Time Flood Ingestion & Alerting` | S7-T4, S7-T7 | Leaf package `internal/feedback` lưu snapshot R2; quét ngập tự động theo mưa bảo vệ quota VLM |
| **epic:vehicle-detection** | `[Epic] Autonomous Arterial Traffic Perception` | S7-T5 | Background worker quét tự động 50 camera trục chính bằng OpenVINO on-premise, không gọi cloud VLM |
| **epic:eval-tooling** | `[Epic] Multi-Lane Labeling Taxonomy & Toolchain` | S7-T8 | Nâng cấp schema nhãn phân luồng 2 chiều, trạng thái dừng đèn đỏ và đo độ ngập bánh xe máy |
| **rfc / architecture** | `[Epic] Zero-VLM Architecture & Governance` | S7-T9, S7-T10 | Đặc tả kỹ thuật ADR-009 phân đoạn ảnh mặt nước on-premise; nghiệm thu toàn diện Sprint 7 |

---

### Bảng phân bố Wave thực thi

| Wave | ID | GitHub Issue | Title | Epic | Labels | Phụ thuộc |
|:---:|:---:|:---:|---|---|---|:---:|
| **0** | **S7-T2** | [#247](https://github.com/529-studio/projectKV/issues/247) | `[web] Hide Offline / No-Signal Cameras by Default on Map (Map Hygiene)` | epic:dock-ux | `frontend` | None (Merged PR #257) |
| **0** | **S7-T1** | [#246](https://github.com/529-studio/projectKV/issues/246) | `[ci + ops] Live Staging Telegram Flood Alert Verification & Simulation Deprecation (Stack 14 homesrv)` | epic:infra-resilience | `tooling, cross-cutting` | Nice-to-have (Dời sau Sprint 7) |
| **1** | **S7-T3** | [#248](https://github.com/529-studio/projectKV/issues/248) | `[web] Human-in-the-Loop "Báo cáo ngập tại đây" Button & Tactile Modal Feedback` | epic:dock-ux | `frontend` | Hoàn thành (`feat/s7-t3-report-flood-button`) |
| **1** | **S7-T4** | [#249](https://github.com/529-studio/projectKV/issues/249) | `[go-server] internal/feedback Leaf Package & Cloudflare R2 Ground-Truth Archiver` | epic:flood-integrity | `backend, go` | [#248](https://github.com/529-studio/projectKV/issues/248) (S7-T3) |
| **2** | **S7-T5** | [#250](https://github.com/529-studio/projectKV/issues/250) | `[go-server + cv-engine] Autonomous 50 Hot Set Traffic Poller & Saturation Cache` | epic:vehicle-detection | `backend, go, epic:vehicle-detection` | None (Backend worker) |
| **2** | **S7-T6** | [#251](https://github.com/529-studio/projectKV/issues/251) | `[web] Real-Time Arterial Marker Saturation Coloring (Green / Yellow / Red Dots)` | epic:dock-ux | `frontend` | [#250](https://github.com/529-studio/projectKV/issues/250) (S7-T5) |
| **2** | **S7-T7** | [#252](https://github.com/529-studio/projectKV/issues/252) | `[go-server] Rain-Triggered Automated Hot Set Flood Poller (VLM Bridge)` | epic:flood-integrity | `backend, go, epic:flood-integrity` | [#250](https://github.com/529-studio/projectKV/issues/250) (S7-T5) |
| **3** | **S7-T8** | [#253](https://github.com/529-studio/projectKV/issues/253) | `[tooling] Multi-Lane Directional Labeling Taxonomy & Toolchain Upgrade` | epic:eval-tooling | `tooling, epic:eval-tooling` | None (Tooling độc lập) |
| **3** | **S7-T9** | [#254](https://github.com/529-studio/projectKV/issues/254) | `[docs + spike] ADR-009 Technical Design Spike: Self-Hosted Flood Segmentation (Zero-VLM)` | rfc | `documentation, rfc` | [#252](https://github.com/529-studio/projectKV/issues/252) (S7-T7) |
| **4** | **S7-T10** | [#255](https://github.com/529-studio/projectKV/issues/255) | `[docs] Holistic Repository Knowledge Synchronization & Sprint 7 Closeout` | rfc | `documentation` | S7-T1 đến S7-T9 |

---

## 2. Chuẩn bị môi trường

Trước khi thực thi bất kỳ ticket nào, đồng bộ nhánh `dev` mới nhất:

```bash
cd ~/repos/projectKV
git checkout dev
git pull origin dev
```

---

## Wave 0 — Vệ sinh Bản đồ & Kiểm thử Live Staging (S7-T2, S7-T1)

Hai ticket này chạy độc lập, hoàn thành nhanh để dọn sạch giao diện bản đồ và bảo đảm hạ tầng staging hoạt động chuẩn xác.

### Terminal 1 — S7-T2 (Ẩn camera mất tín hiệu trên bản đồ theo mặc định)

```bash
cd ~/repos/projectKV

agy "Implement issue #247: [web] S7-T2 — Hide Offline / No-Signal Cameras by Default on Map (Map Hygiene).

Read the full issue at https://github.com/529-studio/projectKV/issues/247 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- In apps/web/src/components/Map.tsx and CameraFilter.tsx:
  - Add boolean state 'hideOfflineCameras' defaulting to true.
  - Filter out cameras where status === 'NOT_IMAGE' or status === 'NO_SIGNAL'.
  - Provide a subtle, monochromatic filter toggle switch: '[Ẩn camera mất tín hiệu]' / '[Hiện camera mất tín hiệu]'.
  - Update camera counter badge on map header: 'Đang hiển thị <visible_count> / 837 camera'.
- Ensure marker clustering recomputes smoothly without layout jitter.
- Out of scope: Deleting offline cameras from cameras_enriched.json.
- Run tests: cd apps/web && npm test && npm run build.

Branch: feat/s7-t2-hide-offline-cameras
Open PR targeting dev starting with 'Close #247'. Do NOT merge."
```

### Terminal 2 — S7-T1 (Kiểm thử live bot Telegram trên staging Stack 14 homesrv — Nice-to-have, dời sau Sprint 7)

> **Lưu ý**: Ticket S7-T1 được đánh giá là nice-to-have và được dời sang các sprint sau để ưu tiên cho luồng dữ liệu ngập cộng đồng và thị giác máy tính.

```bash
cd ~/repos/projectKV

agy "Implement issue #246: [ci + ops] S7-T1 — Live Staging Telegram Flood Alert Verification & Simulation Deprecation (Stack 14 homesrv).

Read the full issue at https://github.com/529-studio/projectKV/issues/246 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create verification test script at scripts/verify_staging_telegram.sh.
- Author operational runbook at docs/runbooks/telegram_staging_verification.md:
  - Instructions to provision staging Telegram Bot token in Portainer Stack 14 environment.
  - Test alert dispatch procedure for camera 586e2995f9fab7001111b0b5 (Ung Văn Khiêm - D2).
  - Verify zero-emoji HTML formatting and bilingual template per Rule 1.
  - Verify sliding 30-minute debouncer: trigger second alert within 5 minutes and confirm suppression.
  - Verify internal/middleware/secretsanitizer.go scrubs bot tokens to '[REDACTED_BOT_TOKEN]'.

Branch: ops/s7-t1-verify-staging-telegram
Open PR targeting dev starting with 'Close #246'. Do NOT merge."
```

---

## Wave 1 — Vòng lặp Thu thập Dữ liệu Chủ động (S7-T3, S7-T4)

Thiết lập cơ chế Human-in-the-Loop: người dùng phản ánh nghi ngờ ngập nước trực tiếp trong modal $\rightarrow$ backend tự động chụp snapshot lưu về Cloudflare R2 có gắn thẻ `trust: unverified`.

### Terminal 1 — S7-T3 (Nút bấm "Báo cáo ngập tại đây" trong CameraModal)

```bash
cd ~/repos/projectKV

agy "Implement issue #248: [web] S7-T3 — Human-in-the-Loop 'Báo cáo ngập tại đây' Button & Tactile Modal Feedback.

Read the full issue at https://github.com/529-studio/projectKV/issues/248 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- In apps/web/src/components/CameraModal.tsx:
  - Add secondary tactile action button: '[Báo cáo ngập tại đây]' (Report flood here).
  - High-contrast monochromatic styling with zinc border: border-zinc-700 hover:bg-zinc-800 active:scale-[0.98] text-zinc-300.
  - Monochromatic SVG alert flag/triangle icon (zero emojis per Rule 1).
  - Enforce a 30-second client-side cooldown countdown: '[Đã báo cáo (30s)]' -> '(1s)'.
  - On click: dispatches POST /api/v1/cameras/:id/report-flood.
  - On success: renders ephemeral zinc chip '[Cảm ơn bạn đã hỗ trợ ghi nhận]' auto-dismissing in 3 seconds.
- Run tests: cd apps/web && npm test && npm run build.

Branch: feat/s7-t3-report-flood-button
Open PR targeting dev starting with 'Close #248'. Do NOT merge."
```

### Terminal 2 — S7-T4 (Package internal/feedback & Archiver lưu về Cloudflare R2)

```bash
cd ~/repos/projectKV

agy "Implement issue #249: [go-server] S7-T4 — internal/feedback Leaf Package & Cloudflare R2 Ground-Truth Archiver.

Read the full issue at https://github.com/529-studio/projectKV/issues/249 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create pure leaf package services/go-server/internal/feedback/ conforming to GO_BACKEND_ARCHITECTURE.md (zero sibling internal imports).
- Register HTTP route: POST /api/v1/cameras/:id/report-flood.
- Implement in-memory IP rate limiter: maximum 1 submission per client IP per camera per 5 minutes using sync.Map. Return HTTP 429 when throttled.
- Asynchronous R2 archiver:
  - Pull live frame from camera proxy.
  - Upload JPEG to Cloudflare R2: warded-proofs/feedback/<YYYY-MM-DD>/<camera_id>_<timestamp>.jpg.
  - Upload companion metadata JSON: warded-proofs/feedback/<YYYY-MM-DD>/<camera_id>_<timestamp>.json with:
    {
      'source': 'user_reported',
      'trust': 'unverified',
      'camera_id': '...',
      'timestamp': '...',
      'client_ip_hash': '<sha256>'
    }
- Verify compliance with architecture-guardian skill and make test-go (-race).

Branch: feat/s7-t4-feedback-r2-archiver
Open PR targeting dev starting with 'Close #249'. Do NOT merge."
```

---

## Wave 2 — Tự động hóa Thị giác Trục chính (S7-T5, S7-T6, S7-T7)

Nền móng Zero-VLM: Quét 50 camera trục chính bằng OpenVINO nội bộ trên `dev-box` $\rightarrow$ tô màu Xanh/Vàng/Đỏ trực tiếp trên bản đồ $\rightarrow$ tự động kích hoạt kiểm tra ngập khi trời mưa.

### Terminal 1 — S7-T5 (Worker quét tự động 50 camera Hot Set & Cache Saturation Index)

```bash
cd ~/repos/projectKV

agy "Implement issue #250: [go-server + cv-engine] S7-T5 — Autonomous 50 Hot Set Traffic Poller & Saturation Cache.

Read the full issue at https://github.com/529-studio/projectKV/issues/250 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Extend services/go-server/internal/hotset/scheduler.go:
  - 60-second ticker iterating across 50 priority arterial cameras (Võ Văn Kiệt, Mai Chí Thọ, Điện Biên Phủ, Cộng Hòa, Trường Chinh, QL1).
  - Fetches camera frames and dispatches inference requests to local OpenVINO cv-engine (POST /analyze).
  - Computes PCU Saturation Index (SI) using corridor capacities (S6-T2) and Schmitt trigger hysteresis (Lesson 33).
  - Persists latest SI and status (CLEAR, MODERATE, HEAVY, CONGESTED) in thread-safe memory store (hotset.Store).
- Expose cached endpoint: GET /api/v1/cameras/hotset/status (serving all 50 camera states with sub-5ms latency).
- Strict Rule: Zero Google Gemini VLM calls consumed for traffic counting.
- Run tests: make test-go.

Branch: feat/s7-t5-autonomous-hotset-poller
Open PR targeting dev starting with 'Close #250'. Do NOT merge."
```

### Terminal 2 — S7-T6 (Tô màu chấm camera thời gian thực trên bản đồ Web)

```bash
cd ~/repos/projectKV

agy "Implement issue #251: [web] S7-T6 — Real-Time Arterial Marker Saturation Coloring (Green / Yellow / Red Dots).

Read the full issue at https://github.com/529-studio/projectKV/issues/251 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- In apps/web/src/components/Map.tsx:
  - Poll GET /api/v1/cameras/hotset/status every 30 seconds.
  - Dynamically style markers for the 50 Hot Set cameras:
    - SI < 0.30 (CLEAR): Emerald green (#10b981) or subtle zinc ring.
    - 0.30 <= SI < 0.60 (MODERATE): Amber yellow (#f59e0b).
    - SI >= 0.60 (HEAVY / CONGESTED): Rose red (#ef4444).
  - Add subtle pulsating animation on CONGESTED markers.
  - Add monochromatic legend in map footer: '[Xanh: Thông thoáng] [Vàng: Đông xe] [Đỏ: Ùn ứ]'.
- Ensure smooth 60 FPS panning/zooming without memory leaks.
- Run tests: cd apps/web && npm test && npm run build.

Branch: feat/s7-t6-map-marker-saturation
Open PR targeting dev starting with 'Close #251'. Do NOT merge."
```

### Terminal 3 — S7-T7 (Tự động kiểm tra ngập khi có mưa theo Quota Governor)

```bash
cd ~/repos/projectKV

agy "Implement issue #252: [go-server] S7-T7 — Rain-Triggered Automated Hot Set Flood Poller (VLM Bridge).

Read the full issue at https://github.com/529-studio/projectKV/issues/252 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- In services/go-server/internal/flood/service.go:
  - Hook into Open-Meteo weather triggers: active rainfall >= 0.10 mm/h.
  - Automatically schedules Gemini VLM verification across known flood chokepoints (Ung Văn Khiêm, Hồ Học Lãm, Huỳnh Tấn Phát, Tô Ngọc Vân, Nguyễn Xí).
  - Strict Quota Governor: caps VLM dispatch to <= 2 requests per minute (strictly respecting 10 RPM / 300 RPD budget).
  - Updates in-memory flood state to FLOODED_SHALLOW or FLOODED_DEEP.
  - Dispatches Telegram alert via internal/notify with 30-minute debounce window.
  - Exposes status on camera endpoint so Web UI shows Blue badge '[NGẬP NƯỚC]'.
- Run tests: make test-go.

Branch: feat/s7-t7-rain-triggered-flood-poller
Open PR targeting dev starting with 'Close #252'. Do NOT merge."
```

---

## Wave 3 — Tái thiết Chuẩn Đánh nhãn & Thiết kế Zero-VLM (S7-T8, S7-T9)

Khắc phục triệt để lỗ hổng của ground-truth cũ (script đoán mò theo tên đường) và lập đặc tả kỹ thuật cho mô hình phân đoạn ảnh ngập nước on-premise (BiSeNet / YOLOv8-seg).

### Terminal 1 — S7-T8 (Chuẩn hóa Schema nhãn phân luồng 2 chiều & Toolchain)

```bash
cd ~/repos/projectKV

agy "Implement issue #253: [tooling] S7-T8 — Multi-Lane Directional Labeling Taxonomy & Toolchain Upgrade.

Read the full issue at https://github.com/529-studio/projectKV/issues/253 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Upgrade tools/eval/label_schema.json to JSON Schema Draft-07:
  - Multi-lane directional structure: direction ('inbound', 'outbound', 'bidirectional').
  - Traffic density: 'CLEAR', 'MODERATE', 'HEAVY', 'CONGESTED'.
  - Red-light queue tracking: 'is_waiting_red_light' (boolean).
  - Motorcycle wheel waterline depth indicators:
    - 'DRY'
    - 'WET_SURFACE' (wet road reflection only, no standing water)
    - 'TIRE_QUARTER_SUBMERGED' (puddle, 5-10cm)
    - 'TIRE_HALF_SUBMERGED' (hazardous flood, 15-20cm)
    - 'IMPASSABLE' (> 25cm)
- Upgrade tools/eval/labeler.html to support multi-lane tagging and waterline radio selectors.
- Build validation script tools/eval/validate_labels.py ensuring all exported JSON files adhere to schema.

Branch: tooling/s7-t8-multilane-labeling-taxonomy
Open PR targeting dev starting with 'Close #253'. Do NOT merge."
```

### Terminal 2 — S7-T9 (Đặc tả thiết kế ADR-009: Phân đoạn ảnh ngập nước On-Premise)

```bash
cd ~/repos/projectKV

agy "Implement issue #254: [docs + spike] S7-T9 — ADR-009 Technical Design Spike: Self-Hosted Flood Segmentation (Zero-VLM).

Read the full issue at https://github.com/529-studio/projectKV/issues/254 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Author docs/ADR-009-zero-vlm-flood-segmentation.md:
  - Model selection benchmarks: BiSeNet v2 vs SegFormer-B0 vs YOLOv8n-seg converted to OpenVINO FP16.
  - Compute latency target: < 80ms per frame on dev-box AMD Ryzen 7 6800HS.
  - Training dataset acquisition: protocol consuming Cloudflare R2 crowdsourced proofs (warded-proofs/feedback/) and harvested rain frames.
  - Physical Triangulation Logic: Dual-layer verification combining water puddle segmentation mask (> 15% road surface) with detected motorcycle wheel submergence depth (>= 20cm) per Lesson 37.
- Zero emojis, 100% English documentation per Rule 1.

Branch: docs/s7-t9-adr-009-zero-vlm-flood
Open PR targeting dev starting with 'Close #254'. Do NOT merge."
```

---

## Wave 4 — Nghiệm thu Toàn diện & Đồng bộ Tri thức (S7-T10)

Sau khi toàn bộ Wave 0–3 đã được review và merge vào `dev`, thực hiện nghiệm thu toàn cục.

### Terminal 1 — S7-T10 (Nghiệm thu Sprint 7 & Cập nhật Tài liệu Toàn dự án)

```bash
cd ~/repos/projectKV

agy "Implement issue #255: [docs] S7-T10 — Holistic Repository Knowledge Synchronization & Sprint 7 Closeout.

Read the full issue at https://github.com/529-studio/projectKV/issues/255 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Strictly adhere to AGENTS.md Rule 6 (holistic repository documentation synchronization):
  - Author docs/sprint7_report.md reconciling 100% of delivered tickets against git log and PR list.
  - Update docs/project_status.md (PR statistics, completed epics, operational gates).
  - Codify new lessons in docs/lesson_learn.md (Lesson 38: Heuristic Ground-Truth Pollution, Lesson 39: Autonomous Arterial Perception).
  - Update docs/GO_BACKEND_ARCHITECTURE.md with internal/feedback and hotset poller.
  - Update root README.md (both English and Vietnamese sections) documenting real-time traffic marker dots, crowdsource feedback button, and offline camera filtering.
- Run full test suites: make test-go, cd apps/web && npm test && npm run build, make test-model.

Branch: docs/sprint7-closeout-and-report
Open PR targeting dev starting with 'Close #255'. Do NOT merge."
```

---

## 3. Checklist Tuân thủ Quy tắc Dự án (AGENTS.md)

Trước khi mở bất kỳ Pull Request nào, kiểm tra nghiêm ngặt:
- [ ] **Rule 1 (Zero Emoji & Monochromatic)**: Tuyệt đối không dùng emoji trong code, log, commit và UI. Giao diện tiếng Việt chuẩn dấu; code và commit 100% tiếng Anh.
- [ ] **Rule 2 (Sound Solutions)**: Không dùng giải pháp chắp vá hoặc thủ thuật che giấu lỗi. Giải quyết triệt để tận gốc rễ.
- [ ] **Rule 3 (Định lượng công sức)**: Có ghi rõ thời gian ước lượng (active effort & delivery window) trong PR description.
- [ ] **Rule 4 (Tái hiện lỗi thực tế)**: Kiểm thử end-to-end từ góc nhìn người dùng thực trước khi hoàn thành.
- [ ] **Rule 5 (Không push trực tiếp dev/main)**: Mở PR từ nhánh tính năng nhắm vào `dev`. Không bao giờ tự ý auto-merge PR.
- [ ] **Rule 6 (Đồng bộ tài liệu)**: Cập nhật tài liệu liên quan trong `docs/` và `README.md` song ngữ nếu có thay đổi hành vi/endpoint.
- [ ] **Rule 7 (Kiến trúc leaf modulith & Raw SQL)**: Không dùng ORM, tuân thủ kiến trúc phân tầng trong Go backend. Chạy `make swagger` nếu sửa route API.
- [ ] **Rule 8 (Zero Hardcoded Secrets)**: Secret nạp từ `.env`, fallback là `""` hoặc dừng chương trình. Không để lộ token trong log hay error.
- [ ] **Rule 9 (Cú pháp link PR)**: Mô tả PR bắt buộc bắt đầu bằng `Close #<issue-number>` (ví dụ: `Close #247`).
