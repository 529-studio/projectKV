# Sprint 6 — Hướng dẫn chạy agy CLI theo từng Wave

> **Trạng thái**: Mọi placeholder đã được gắn số issue thật trên GitHub
> (S6-T1=[#221](https://github.com/529-studio/projectKV/issues/221), S6-T2=[#222](https://github.com/529-studio/projectKV/issues/222), S6-T3=[#223](https://github.com/529-studio/projectKV/issues/223), S6-T4=[#224](https://github.com/529-studio/projectKV/issues/224), S6-T5=[#225](https://github.com/529-studio/projectKV/issues/225)). File sẵn sàng sử dụng.

Tài liệu hướng dẫn vận hành cho AI agents (`agy` CLI) và developers thực thi toàn
bộ Sprint 6 Backlog theo quy chuẩn [ADR-007](ADR-007-sprint5-audit-and-sprint6-plan.md)
(Resolving the Perception Layer Gate & Dispatching Verified Flood Alerts),
[ADR-006](ADR-006-sprint4-audit-and-sprint5-plan.md), và [AGENTS.md](../AGENTS.md).

---

## 1. Issue Map & Epics Grouping

### Danh sách Epics

| Epic | Tên Epic | Issues con | Mục tiêu chính |
|---|---|---|---|
| **epic:eval-tooling** | `[Epic] Evaluation & Object Detection Integrity` | S6-T1 | Code NMS, letterboxing, nâng 416px vào `server.py` và đo lại F1 trên toàn bộ 764 frame (**Hard Gate**) |
| **epic:infra-resilience** | `[Epic] Infrastructure Resilience & Security Guardrail` | S6-T4 | Tự động hóa quét secret trong GitHub Actions CI bằng Gitleaks, triệt tiêu nguy cơ lộ credential |
| **epic:vehicle-detection** | `[Epic] Physical Traffic Calibration & State Persistence` | S6-T2 | Hiệu chỉnh capacity theo số làn đường cho từng camera; lưu đỉnh dung lượng bền vững vào SQLite |
| **epic:dock-ux** | `[Epic] Dock & Modal Usability Hardening` | S6-T3 | Tách biệt affordance nút bấm xác minh ngập và badge thụ động; thêm cooldown timer 15s client-side |
| **epic:flood-integrity** | `[Epic] Verified Real-Time Flood Alerting` | S6-T5 | Xây dựng package `internal/notify`, kênh gửi Telegram Bot, quản lý subscription SQLite và debounce 30 phút |

---

### Bảng phân bố Wave thực thi

| Wave | ID | GitHub Issue | Title | Epic | Labels | Phụ thuộc |
|:---:|:---:|:---:|---|---|---|:---:|
| **0** | **S6-T1** | [#221](https://github.com/529-studio/projectKV/issues/221) | `[cv-engine + tooling] Implement NMS, Letterbox, 416px Scaling & Re-run 764-Frame D6 Benchmark` (**Completed / Merged in PR #232**) | epic:eval-tooling | `tooling, epic:eval-tooling, backend` | None (**Hard Gate**) |
| **0** | **S6-T4** | [#224](https://github.com/529-studio/projectKV/issues/224) | `[ci + ops] Automated Secret Scanning in GitHub Actions CI (Gitleaks)` (**Completed / Merged in PR #243**) | epic:infra-resilience | `tooling, cross-cutting` | None (Độc lập CI) |
| **1** | **S6-T2** | [#222](https://github.com/529-studio/projectKV/issues/222) | `[go-server] Corridor-Specific Visual Capacity Calibration & Persistent Peak Storage` (**Completed / Merged in PR #233**) | epic:vehicle-detection | `go, backend, epic:vehicle-detection` | [#221](https://github.com/529-studio/projectKV/issues/221) (S6-T1) |
| **1** | **S6-T3** | [#223](https://github.com/529-studio/projectKV/issues/223) | `[web] CameraModal UI/UX Redesign (Tactile Affordance) & 15s Client-Side Cooldown` (**Completed / Merged in PR #236**) | epic:dock-ux | `frontend, epic:dock-ux` | None (Độc lập UI) |
| **2** | **S6-T5** | [#225](https://github.com/529-studio/projectKV/issues/225) | `[go-server] Telegram Flood Alert Dispatcher (internal/notify, Subscription Store & Debounce)` (**Completed / Merged in PR #241**) | epic:flood-integrity | `go, backend, epic:flood-integrity` | S5-T3 ([#209](https://github.com/529-studio/projectKV/issues/209)) |

> **Quy tắc cổng chặn (Hard Gate) & Ghi chú hoàn thành**:
> 1. **S6-T1 là Hard Gate của Sprint 6**: Đã nghiệm thu và đo lại toàn bộ 764 frame thực tế, Macro F1 tăng +18.9% (tuân thủ nghiêm ngặt **Lesson 19** & **Lesson 29**).
> 2. **S6-T4 hoàn thành ở Wave 0**: Đã tích hợp workflow Gitleaks tập trung của Organization `529-studio` (`.github/workflows/secret-scan.yml` + `.gitleaks.toml`), bảo vệ toàn bộ commit tiếp theo khỏi rò rỉ secret (PR #243).
> 3. **S6-T3 hoàn thành ở Wave 1**: Đã phân tách affordance nút bấm, chặn spam 15s, che giấu lỗi VLM và gắn middleware `SecretSanitizer`.
> 4. **S6-T5 hoàn thành ở Wave 2**: Đã xây dựng package `internal/notify`, `internal/subscription`, debounce 30 phút, hoàn thành tích hợp composition root và đã merge vào `dev` (PR #241).
> 5. **Báo cáo tổng kết Sprint 6**: Xem chi tiết tại [`docs/sprint6_report.md`](sprint6_report.md).

---

## 2. Chuẩn bị môi trường

```bash
cd ~/repos/projectKV
git checkout dev
git pull origin dev
```

---

## Wave 0 — Nền tảng Nhận diện & Bảo vệ CI (S6-T1, S6-T4)

Hai ticket này có thể mở 2 terminal độc lập. Trong đó **S6-T1 là Hard Gate** bắt buộc nghiệm thu trước khi bắt đầu S6-T2.

### Terminal 1 — S6-T1 (Code NMS, Letterbox, 416px vào cv-engine & Đo lại 764 frame D6)

```bash
cd ~/repos/projectKV

agy "Implement issue #221: [cv-engine + tooling] S6-T1 — Implement NMS, Letterbox, 416px Scaling & Re-run 764-Frame D6 Benchmark.

Read the full issue at https://github.com/529-studio/projectKV/issues/221 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Hard Gate: Highest priority in Sprint 6, blocks downstream traffic calibration (S6-T2) per Lesson 19.
- Implement Non-Maximum Suppression (NMS) in services/cv-engine/server.py:
  Replace raw 'np.sum(data[class_idx, :] > 0.45)' with standard NMS (confidence threshold 0.25, IoU threshold 0.45) to collapse multi-anchor activations.
- Implement Aspect-Preserving Letterboxing in services/cv-engine/server.py:
  Replace 'image.resize((320, 320))' with 16:9 letterboxing padded with neutral gray (114, 114, 114) to maintain geometric priors.
- Upgrade model input resolution to 416x416:
  Configure model loading and preprocessing for [1, 3, 416, 416] input dimensions on dev-box worker.
- Re-run full 764-frame controlled benchmark:
  python3 tools/eval/benchmark.py --analyzer cv-engine --labels-file tools/eval/dataset/labels_d6_real.json --output-report docs/reports/eval_report_d6_sprint6.md
- Document before/after Macro F1, Weighted F1, and per-class confusion metrics in docs/reports/eval_report_d6_sprint6.md.
- Ensure all model unit tests pass: make test-model.

Branch: tooling/s6-t1-cv-nms-letterbox-benchmark
Open PR targeting dev starting with 'Close #221'. Do NOT merge."
```

### Terminal 2 — S6-T4 (Tự động hóa quét secret trong GitHub Actions CI bằng Gitleaks)

```bash
cd ~/repos/projectKV

agy "Implement issue #224: [ci + ops] S6-T4 — Automated Secret Scanning in GitHub Actions CI (Gitleaks).

Read the full issue at https://github.com/529-studio/projectKV/issues/224 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Create GitHub Actions workflow at .github/workflows/secret-scan.yml.
- Use official gitleaks/gitleaks-action to scan all pull requests and pushes to dev and main.
- Create .gitleaks.toml configuration file:
  - Add detection rules for Cloudflare R2 tokens, Telegram Bot tokens, Gemini API keys, and internal dev access tokens.
  - Configure explicit allowlists for test fixture mocks, mock keys in unit tests, and documentation placeholders ('<placeholder>').
- Ensure the CI workflow fails fast and blocks merging if high-entropy credentials are committed.
- Verify the workflow locally or via dry-run drill.

Branch: ci/s6-t4-gitleaks-secret-scanning
Open PR targeting dev starting with 'Close #224'. Do NOT merge."
```

---

## Wave 1 — Hiệu chỉnh Giao thông & Nâng cấp UI/UX (S6-T2, S6-T3)

Chạy Wave 1 sau khi S6-T1 đã hoàn thành và F1 thực tế đã được kiểm định.

### Terminal 1 — S6-T2 (Hiệu chỉnh Capacity theo số làn & Lưu trữ Đỉnh vào SQLite)

```bash
cd ~/repos/projectKV

agy "Implement issue #222: [go-server] S6-T2 — Corridor-Specific Visual Capacity Calibration & Persistent Peak Storage.

Read the full issue at https://github.com/529-studio/projectKV/issues/222 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Depends on #221 (S6-T1): Run only after perception layer fixes are verified.
- Address false-positive congestion on wide boulevards (Võ Văn Kiệt - Ký Con, 10-12 lanes):
  1. In cameras_enriched.json, annotate cameras with 'base_capacity' based on physical lane count:
     - Expressway/major boulevards (10-12 lanes): 40.0 - 50.0 PCU
     - Major arterials (4-6 lanes): 25.0 - 30.0 PCU
     - Narrow/residential streets (2 lanes): 15.0 PCU (default fallback)
  2. In services/go-server/internal/hotset/pcu.go, consume per-camera 'base_capacity' instead of uniform hardcoded 15.0 constant.
  3. Create SQLite table 'camera_capacities (camera_id TEXT PRIMARY KEY, learned_max REAL, updated_at TIMESTAMP)' to persist peak capacities across container redeployments.
- Verify all Go tests pass under race detector: make test-go.
- Provide real operational simulation showing Võ Văn Kiệt - Ký Con no longer misclassifies free-flowing traffic as [ĐÔNG XE].

Branch: feat/s6-t2-corridor-capacity-sqlite
Open PR targeting dev starting with 'Close #222'. Do NOT merge."
```

### Terminal 2 — S6-T3 (Tái thiết kế UI/UX Modal & 15s Cooldown Client-side)

```bash
cd ~/repos/projectKV

agy "Implement issue #223: [web] S6-T3 — CameraModal UI/UX Redesign (Tactile Affordance) & 15s Client-Side Cooldown.

Read the full issue at https://github.com/529-studio/projectKV/issues/223 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Fix Affordance Collision in apps/web/src/components/CameraModal.tsx:
  1. Redesign '[Xác minh ngập nước]' from passive chip styling to a distinct, tactile monochromatic action button (inverted background, sharp border, active hover/press state).
  2. Remove square brackets '[...]' from action buttons (reserve brackets strictly for passive status badges).
  3. Add a crisp monochromatic SVG radar/scan icon ('⌖').
  4. Separate the action button row from the passive verdict badge row for clear visual hierarchy.
- Implement 15-second client-side cooldown timer:
  1. After click, disable button and display countdown text (e.g. '[Chờ xác minh (15s)]' -> '[Chờ xác minh (14s)]').
  2. Prevent any duplicate network requests during cooldown.
- Maintain Rule 1 zero-emoji policy and bilingual support (vi default, en secondary).
- Verify all web unit tests pass: cd apps/web && npm test.

Branch: feat/s6-t3-modal-affordance-cooldown
Open PR targeting dev starting with 'Close #223'. Do NOT merge."
```

---

## Wave 2 — Thông báo Ngập Thời gian thực (S6-T5)

### Terminal 1 — S6-T5 (Hệ thống Cảnh báo Ngập Telegram & Debounce 30 phút)

```bash
cd ~/repos/projectKV

agy "Implement issue #225: [go-server] S6-T5 — Telegram Flood Alert Dispatcher (internal/notify, Subscription Store & Debounce).

Read the full issue at https://github.com/529-studio/projectKV/issues/225 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Build notification subsystem in services/go-server/internal/notify/ (leaf modulith pattern per GO_BACKEND_ARCHITECTURE.md):
  1. Define Channel interface: Send(ctx context.Context, alert FloodAlert) error.
  2. Implement TelegramChannel using Telegram Bot API (sendMessage with clean HTML, zero emojis, bilingual vi/en).
  3. Load bot token via TELEGRAM_BOT_TOKEN with fail-fast empty default per Rule 8.
- Implement subscription persistence in services/go-server/internal/subscription:
  SQLite store for user chat IDs mapped to target zones, wards, or specific flood chokepoints.
- Wire flood event emission:
  Connect flood.Service verified flood verdicts (from S5-T3 Gemini VLM) into notify.Manager.
- Implement 30-minute anti-spam sliding debounce window:
  Ensure identical flooded chokepoints do NOT trigger repeat notifications within a 30-minute quiet period.
- Write full unit tests covering channel dispatch, SQLite store, and debounce logic.
- Verify all Go tests pass under race detector: make test-go.

Branch: feat/s6-t5-telegram-flood-alert
Open PR targeting dev starting with 'Close #225'. Do NOT merge."
```

---

## 3. Nguyên tắc Nghiệm thu & Definition of Done

1. **Tuân thủ quy chuẩn Rule 1 (Zero-Emoji & Monochromatic UI)**:
   Mọi giao diện web, thông báo Telegram, và log hệ thống tuyệt đối không dùng emoji. Giao diện tuân thủ bảng màu đơn sắc tương phản cao, hỗ trợ song ngữ (`vi` mặc định, `en` phụ).
2. **Giải pháp tận gốc & verify triệt để (Rule 2)**:
   Không sợ tốn sức; không đi tắt cẩu thả; luôn giải quyết tận gốc và verify triệt để trước khi hoàn thành task.
3. **Khai báo dự toán thời gian bắt buộc (Rule 3)**:
   Mọi PR phải khai báo rõ thời gian dự toán và thực tế thực hiện dựa trên giải pháp đúng đắn theo Rule 2.
4. **Bằng chứng thực nghiệm Staging (Rule 4 & Lesson 3)**:
   Mọi PR tuyên bố mô hình cải thiện, UI thay đổi, hoặc tính năng thông báo hoạt động MUST đính kèm log terminal thật, curl output, hoặc ảnh chụp màn hình từ Staging/môi trường chạy thật.
5. **Quy trình Git & Zero Auto-Merge (Rule 5)**:
   Tạo nhánh tính năng (`tooling/*`, `feat/*`, `ci/*`), push lên origin, và mở PR hướng về `dev`. AI agents tuyệt đối không được tự động merge PR.
6. **Đồng bộ tài liệu toàn diện & README (Rule 6)**:
   Cập nhật `docs/project_status.md` và `docs/lesson_learn.md` là chưa đủ. Bắt buộc rà soát và cập nhật mọi file `docs/` liên quan và `README.md` (cả EN & VI) nếu thay đổi ảnh hưởng đến quy trình, lệnh chạy, cấu hình hay tính năng.
7. **Không lộ secret & Fail-Fast rỗng (Rule 8)**:
   Không hardcode API key/token ở bất kỳ file nào. Fallback trong code phải là chuỗi rỗng `""` hoặc dừng chương trình lập tức.
8. **Bắt buộc cú pháp liên kết issue (Rule 9)**:
   Mô tả PR bắt buộc phải bắt đầu bằng `Close #<issue-number>` (ví dụ: `Close #221`). Không dùng các từ ngữ mô tả tự do như `Addresses #...` hay `Implement #...`.
9. **Không tự ý hack tham số (Lesson 19)**:
   Không được sửa các ngưỡng `CapacityFloor` hay trọng số PCU để che giấu lỗi bỏ sót/nhận nhầm của detector. Lỗi tầng nhận diện phải được xử lý và kiểm chứng bằng 764 frame ground truth.
