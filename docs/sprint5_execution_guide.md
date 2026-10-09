# Sprint 5 — Hướng dẫn chạy agy CLI theo từng Wave

> **Trạng thái**: Mọi placeholder `#<TBD-Sx>` đã được thay bằng số issue thật
> (S5-T1=#207, S5-T2=#208, S5-T3=#209, S5-T4=#210). File sẵn sàng sử dụng.

Tài liệu hướng dẫn vận hành cho AI agents (`agy` CLI) và developers thực thi toàn
bộ Sprint 5 Backlog theo quy chuẩn [ADR-006](ADR-006-sprint4-audit-and-sprint5-plan.md)
(The Ground-Truth Reckoning), [ADR-004](ADR-004-route-check-and-snapshot-archive.md),
và [AGENTS.md](../AGENTS.md).

---

## 1. Issue Map & Epics Grouping

### Danh sách Epics

| Epic | Tên Epic | Issues con | Mục tiêu chính |
|---|---|---|---|
| **epic:eval-tooling** | `[Epic] Evaluation & Object Detection Integrity` | S5-T1, S5-T2 | Đo lại F1 thực tế sau hiệu chỉnh T5; audit lỗi bỏ sót/nhận nhầm xe của YOLOv8n |
| **epic:flood-integrity** | `[Epic] Decoupled Multimodal Flood Pipeline` | S5-T3 | Curate danh sách ngập chuẩn `flood_chokepoints.json`; kích hoạt VLM theo 2 trigger |
| **epic:infra-resilience** | `[Epic] Infrastructure Resilience & Chaos Testing` | S5-T4 | Chaos-test ngắt Tailscale trên `dev-box`; xác nhận hạ cấp xuống `unknown` không sập |

---

### Bảng phân bố Wave thực thi

| Wave | ID | GitHub Issue | Title | Epic | Labels | Phụ thuộc |
|:---:|:---:|:---:|---|---|---|:---:|
| **0** | **S5-T1** | [#207](https://github.com/529-studio/projectKV/issues/207) | `[tooling] re-run D6 benchmark and smoke test with post-T5 weights` | epic:eval-tooling | `tooling, epic:eval-tooling` | None (**Hard Gate**) |
| **0** | **S5-T4** | [#210](https://github.com/529-studio/projectKV/issues/210) | `[infra] physical Tailscale disconnection test for dev-box failover` | epic:infra-resilience | `backend, epic:infra-resilience` | None (Độc lập) |
| **1** | **S5-T2** | [#208](https://github.com/529-studio/projectKV/issues/208) | `[cv-engine] object detection audit for false-positive/missed detections` | epic:eval-tooling | `tooling, epic:eval-tooling` | #207 (S5-T1) |
| **1** | **S5-T3** | [#209](https://github.com/529-studio/projectKV/issues/209) | `[go-server + web] decoupled hybrid flood pipeline` | epic:flood-integrity | `backend, frontend, epic:flood-integrity` | Khuyến nghị chờ #207 |

> **Quy tắc cổng chặn (Hard Gate)**: S5-T1 chặn mọi phân tích mô hình tiếp theo. S5-T4 độc lập có thể chạy song song ở Wave 0. S5-T2 bắt buộc chờ kết quả phân tích từng camera mới từ S5-T1. S5-T3 nên chờ S5-T1 để giữ kỷ luật không phân tán luồng phát triển.

---

## 2. Chuẩn bị môi trường

```bash
cd ~/repos/projectKV
git checkout dev
git pull origin dev
```

---

## Wave 0 — Nền tảng Độc lập & Hard Gate (S5-T1, S5-T4)

Hai ticket này có thể mở 2 terminal độc lập. Trong đó **S5-T1 là Hard Gate** bắt buộc nghiệm thu trước khi bắt đầu Wave 1.

### Terminal 1 — S5-T1 (Đo lại D6 Benchmark & Smoke Test với trọng số T5)

```bash
cd ~/repos/projectKV

agy "Implement issue #207: [tooling] re-run D6 benchmark and smoke test with post-T5 weights.

Read the full issue at https://github.com/529-studio/projectKV/issues/207 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Hard Gate: Highest priority in Sprint 5, blocks all further modeling tickets.
- Do NOT modify any Go backend code or Python CV model code.
- Do NOT alter any ground-truth labels in tools/eval/dataset/labels_d6_real.json.
- Rename historical baseline reports before overwriting:
  - docs/reports/eval_report_d6_real.md -> docs/reports/eval_report_d6_pre_t5.md
  - docs/reports/smoke_eval_d6_real.md -> docs/reports/smoke_eval_d6_pre_t5.md
- Run benchmark verbatim against the 764 labeled real frames using post-T5 weights:
  python3 tools/eval/benchmark.py --analyzer cv-engine --labels-file tools/eval/dataset/labels_d6_real.json --output-report docs/reports/eval_report_d6_real.md
- Run known-state smoke test:
  python3 tools/eval/verify_smoke_set.py --verdicts predictions_real.json --output docs/reports/smoke_eval_d6_real.md
- Directly compare new metrics against pre-T5 baseline and ADR-006 Section 2 mathematical predictions (specifically whether the 4 CLEAR cameras with false positives worsened due to SI x 1.67).

Branch: tooling/s5-t1-rerun-d6-benchmark
Open PR targeting dev when done. Do NOT merge."
```

### Terminal 2 — S5-T4 (Chaos-Test ngắt Tailscale trên dev-box)

```bash
cd ~/repos/projectKV

agy "Implement issue #210: [infra] physical Tailscale disconnection test for dev-box failover.

Read the full issue at https://github.com/529-studio/projectKV/issues/210 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Execute a real physical network partition drill on Staging (homesrv Stack 14):
  1. While hotset scheduler actively polls cameras, run 'tailscale down' on worker host dev-box.
  2. Verify backend logs warning, times out within 10s, and degrades cleanly to SeverityUnknown (Source: 'fallback') without crashing.
  3. Verify web UI reflects unknown status cleanly.
  4. Test fallback capability of docker-compose.stg.yml profile 'model-local' on homesrv.
  5. Bring Tailscale back up ('tailscale up') on dev-box and confirm automatic telemetry recovery.
- Document operational disaster recovery runbook in docs/runbook_dev_box_failover.md.
- Attach 1 real empirical proof (timestamped terminal logs, API responses, or screenshots) demonstrating graceful degradation and recovery per Rule 4.

Branch: infra/s5-t4-tailscale-failover-drill
Open PR targeting dev when done. Do NOT merge."
```

---

## Wave 1 — Chẩn đoán Tầng Nhận diện & Hybrid Flood Pipeline (S5-T2, S5-T3)

Chạy Wave 1 sau khi S5-T1 đã hoàn thành và có số liệu F1 thực tế.

### Terminal 1 — S5-T2 (Audit lỗi tầng Object Detection)

```bash
cd ~/repos/projectKV

agy "Implement issue #208: [cv-engine] object detection audit for false-positive/missed detections.

Read the full issue at https://github.com/529-studio/projectKV/issues/208 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Depends on #207 (S5-T1): Use the camera-by-camera breakdown generated by S5-T1.
- Investigate two failure modes in YOLOv8n:
  (a) 2 CONGESTED cameras reporting SI = 0.00 (Cộng Hòa - Trường Chinh, Mai Chí Thọ - Đồng Văn Cống 2): extract bounding boxes, confidence scores, and determine why vehicles were missed.
  (b) 4 CLEAR cameras falsely reporting congestion: inspect bounding boxes and determine if road markings, headlight glare, tree shadows, or nighttime noise triggered false detections.
- Do NOT arbitrarily adjust confidence thresholds or model weights without diagnosis.
- Document root causes and visual bounding-box evidence in docs/reports/cv_detection_error_audit.md.
- Formulate concrete resolution proposals (resolution upgrade to 416x416, threshold calibration).

Branch: tooling/s5-t2-cv-detection-audit
Open PR targeting dev when done. Do NOT merge."
```

### Terminal 2 — S5-T3 (Decoupled Hybrid Flood Pipeline)

```bash
cd ~/repos/projectKV

agy "Implement issue #209: [go-server + web] decoupled hybrid flood pipeline.

Read the full issue at https://github.com/529-studio/projectKV/issues/209 for complete Technical Details, Out of Scope, and Definition of Done.

Key requirements:
- Curate authentic tools/eval/fixtures/flood_chokepoints.json from real HCMC municipal flood data (Ung Văn Khiêm, Nguyễn Xí Cầu Đỏ, Huỳnh Tấn Phát, Hồ Học Lãm, Tô Ngọc Vân, Quốc lộ 13...), citing data sources. Do NOT reuse the 10 traffic smoke cameras.
- Reconcile rain threshold divergence between apps/web/src/utils/rainHexUtils.ts (1.0/4.0/15.0 mm) and backend/ADR documents (2.0 mm/h heuristic). Correct the 2 documentation discrepancies cited in ADR-006 Finding 3.
- Implement dual-trigger VLM dispatch in services/go-server:
  1. Trigger 1 (Environmental): Background polling of flood_chokepoints.json ONLY during rain events exceeding threshold.
  2. Trigger 2 (User On-Demand): Modal click in apps/web.
- Enforce strict 10 RPM / 300 RPD daily quota limit on free tier.
- Update web client modal to display flood verification badge.
- Attach real operational proof on staging to PR per Rule 4.

Branch: feat/s5-t3-hybrid-flood-pipeline
Open PR targeting dev when done. Do NOT merge."
```

---

## 3. Nguyên tắc Nghiệm thu & Definition of Done

1. **Bằng chứng thực nghiệm Staging (Rule 4 & Lesson 3)**:
   Mọi PR tuyên bố mô hình cải thiện hoặc hạ tầng chịu lỗi MUST đính kèm log terminal thật hoặc ảnh chụp màn hình từ Staging. Unit test vượt qua cục bộ không bao giờ là bằng chứng đủ.
2. **Không tự ý hack tham số (Lesson 19)**:
   Không được sửa các ngưỡng `CapacityFloor` hay trọng số PCU để che giấu lỗi bỏ sót/nhận nhầm của detector. Lỗi tầng nhận diện phải được xử lý ở tầng nhận diện.
3. **Quy trình Git & Zero Auto-Merge (Rule 5)**:
   Tạo nhánh (`tooling/*`, `feat/*`, `infra/*`), push lên origin, và mở PR hướng về `dev`. AI agents tuyệt đối không được tự động merge PR.
