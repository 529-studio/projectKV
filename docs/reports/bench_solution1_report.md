# Báo cáo Đánh giá Hiệu năng & Tải: Solution 1 (ProjectKV CV Engine)

**Thời gian thực hiện:** 2026-10-09  
**Dịch vụ thử nghiệm:** `services/cv-engine` (Standalone Inference Microservice)  
**Mô hình:** YOLOv8n OpenVINO IR 320x320 FP16 (`yolov8n_openvino_320`)  
**Cấu hình phân bổ (Solution 1):**  
• **RAM:** 2,048 MB (2 GB)  
• **CPU:** 150% (1.5 vCore)  
• **Storage:** 2,048 MB (2 GB)  
• **Botkeep Workload ID:** `b9f0b592-72d8-435d-9ecb-6f85596a4396` (trạng thái: `queued` trên cloud)  

---

## 1. Tóm tắt kết quả cốt lõi (Executive Summary)

Đã thực hiện benchmark toàn diện qua **3,383 requests** kiểm thử với dữ liệu ảnh thực tế từ camera giao thông TP.HCM (`fixture_01_clear_dry_day.jpg`).

| Chỉ số | Kết quả đo đạc | Đánh giá so với hệ thống |
|---|:---:|---|
| **Độ trễ P50 (Sequential)** | **10.2 ms** | Siêu nhanh (thời gian tính toán model thuần túy ~9.7 ms) |
| **Độ trễ P90 (Sequential)** | **12.0 ms** | Độ ổn định rất cao, biên độ dao động cực thấp |
| **Thông lượng tối đa (Max RPS)** | **113.2 req/s** | Đạt trần bão hòa ở mức ~113 ảnh/giây |
| **Tỷ lệ lỗi (Error Rate)** | **0.00%** | 0 lỗi / 3,383 requests (không rớt kết nối, không timeout) |
| **Tiêu thụ RAM thực tế** | **~98 MB – 105 MB** | Chiếm < 5.2% giới hạn RAM 2,048 MB |

---

## 2. Chi tiết kết quả kiểm thử tải (Load & Concurrency Test)

### 2.1 Baseline đơn luồng (Sequential - 50 requests)
• **Min:** 8.08 ms  
• **P50 (Median):** 10.23 ms  
• **P90:** 12.02 ms  
• **P95:** 12.60 ms  
• **P99 / Max:** 30.68 ms  
• **Model Inference thuần túy:** 9.71 ms  

### 2.2 Khả năng chịu tải đồng thời (Concurrency Sweep)
Kiểm thử tăng dần số lượng worker đồng thời gửi request liên tục trong các phiên:

| Concurrency (VUs) | Tổng requests | Throughput (RPS) | P50 Latency | P90 Latency | P99 Latency | Error Rate |
|:---:|:---:|:---:|:---:|:---:|:---:|:---:|
| **1 worker** | 490 | **98.0 RPS** | 9.59 ms | 12.04 ms | 21.50 ms | **0.0%** |
| **2 workers** | 561 | **111.9 RPS** | 16.78 ms | 20.14 ms | 36.60 ms | **0.0%** |
| **5 workers** | 570 | **113.2 RPS** | 43.11 ms | 57.64 ms | 76.44 ms | **0.0%** |
| **10 workers** | 573 | **112.9 RPS** | 85.20 ms | 131.29 ms | 158.67 ms | **0.0%** |
| **15 workers** | 562 | **109.5 RPS** | 135.02 ms | 193.83 ms | 227.63 ms | **0.0%** |
| **20 workers** | 577 | **111.9 RPS** | 175.60 ms | 265.25 ms | 327.36 ms | **0.0%** |

---

## 3. Khả năng đáp ứng bài toán thực tế của ProjectKV

### Kịch bản 1: Hotset hiện tại (57 camera trọng điểm, chu kỳ 30 giây)
• Tải yêu cầu: $\frac{57\text{ cameras}}{30\text{ giây}} = \mathbf{1.90\text{ RPS}}$  
• Năng lực cung cấp: **113.2 RPS**  
• **Hệ số an toàn (Safety Margin):** $\mathbf{59.5\times}$ (Dư thừa gần 60 lần công suất cần thiết).  
• Thời gian tiêu tốn cho 1 chu kỳ 57 camera: Chỉ mất **~0.5 giây** để xử lý xong toàn bộ 57 camera!

### Kịch bản 2: Toàn bộ 712 camera online TP.HCM (chu kỳ 30 giây)
• Tải yêu cầu: $\frac{712\text{ cameras}}{30\text{ giây}} = \mathbf{23.73\text{ RPS}}$  
• Năng lực cung cấp: **113.2 RPS**  
• **Hệ số an toàn:** $\mathbf{4.77\times}$  
• **Kết luận:** Cấu hình Solution 1 **thừa sức gánh toàn bộ camera thành phố** mà không sợ nghẽn cổ chai.

---

## 4. Hành vi xử lý & Điểm nghẽn hệ thống (Bottleneck Analysis)

1. **Cơ chế đa luồng:** `cv-engine` sử dụng `ThreadingHTTPServer` của Python kết hợp OpenVINO CPU Threading. Khi concurrency tăng từ 1 lên 5, throughput tăng từ 98 lên 113 RPS.
2. **Ngưỡng bão hòa (Plateau):** Từ 5 đến 20 workers, thông lượng giữ nguyên ở mức **~112–113 RPS**, trong khi độ trễ tăng tuyến tính từ 43ms lên 175ms (hàng đợi xử lý tuần tự). Không có hiện tượng crash hoặc drop request.
3. **Bộ nhớ:** Không có rò rỉ RAM (Zero memory leak), ổn định ở mức ~100 MB RSS sau hàng nghìn requests liên tục.

---

## 5. Tình trạng triển khai trên Botkeep Cloud
• Đã tạo thành công Workload `projectkv-cv-engine` (`b9f0b592-72d8-435d-9ecb-6f85596a4396`) qua Developer API.  
• Hiện tại workload đang ở hàng đợi khởi tạo (`lifecycleState: queued`) của hạ tầng cluster Botkeep.  
• File weights (`yolov8n.xml` & `yolov8n.bin`) đã được xuất chuẩn hóa sẵn sàng đẩy lên container ngay khi cluster hoàn tất cấp phát đĩa.
