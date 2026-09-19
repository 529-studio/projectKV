# TÀI LIỆU THIẾT KẾ HỆ THỐNG & ĐỊNH HƯỚNG PHÁT TRIỂN (CAMERA VIEW)

Tài liệu này tổng hợp toàn bộ ý tưởng kiến trúc, lộ trình phát triển và các giải pháp kỹ thuật cho dự án **Camera View**, từ giai đoạn hiện tại (Monorepo tinh gọn) đến giai đoạn mở rộng (Backend Golang hiệu năng cao & Computer Vision phân tích tắc đường).

---

## 1. Tầm Nhìn Dự Án (Project Vision)

*   **Tên Ứng Dụng**: **CAMERA VIEW**
*   **Sứ Mệnh**: Hệ thống giám sát, tra cứu và theo dõi hiện trường giao thông thời gian thực với độ trễ thấp, giao diện đơn sắc tối ưu chuẩn phòng điều độ (Control Room UX), hỗ trợ lọc phân cấp thông minh và ghim theo dõi đa camera đồng thời.
*   **Mục Tiêu Mở Rộng**: 
    1. Xây dựng tầng Backend chịu tải cao, proxy & cache thông minh hàng ngàn luồng camera.
    2. Tích hợp thị giác máy tính (Computer Vision) để tự động hóa hoàn toàn việc phát hiện ùn tắc, đếm xe, đo chiều dài hàng xe nối đuôi và cảnh báo sự cố.

---

## 2. Kiến Trúc Tổng Thể (Monorepo)

Hệ thống được tổ chức theo mô hình Monorepo tinh gọn, phân tách độc lập giữa ứng dụng người dùng, dịch vụ backend và công cụ phụ trợ:

```
hcm-traffic-cameras/
│
├── apps/                                  # TẦNG ỨNG DỤNG (CLIENTS)
│   └── web/                               # Web App thuần (HTML/CSS/JS + Leaflet + TailwindCDN)
│       ├── index.html                     # Giao diện điều độ đơn sắc
│       ├── styles.css                     # CSS responsive desktop/tablet/mobile
│       ├── app.js                         # Leaflet map, Chained Deep Filter, Docking logic
│       └── package.json                   # Package @camera-view/web
│
├── services/                              # TẦNG DỊCH VỤ BACKEND
│   ├── node-server/                       # [HIỆN HÀNH] Express.js server tinh gọn (Port 3000)
│   │   └── server.js                      # API /api/cameras & Snapshot Proxy Cache 10s
│   │
│   └── cv-engine/                         # [ĐỊNH HƯỚNG TƯƠNG LAI] Dịch vụ Computer Vision
│       ├── models/                        # Trọng số YOLO / Detection models
│       ├── worker.py                      # Worker phân tích ảnh snapshot định kỳ
│       └── api.py                         # API trả về kết quả phân tích tắc đường
│
├── tools/                                 # CÔNG CỤ NỘI BỘ & SCRIPTS
│   └── crawler/                           # Thu thập danh mục camera và tọa độ GPS mới
│       └── get_all_cameras.js
│
├── docs/                                  # TÀI LIỆU DỰ ÁN & SYSTEM DESIGN
│   └── SYSTEM_DESIGN.md                   # File này
│
├── cameras_all.json                       # Cơ sở dữ liệu 837 camera
├── LICENSE                                # Bản quyền đóng, chỉ dành cho nghiên cứu tham khảo
├── Makefile                               # Quản lý lệnh chạy tập trung
└── README.md                              # Hướng dẫn chi tiết
```

---

## 3. Đánh Giá Backend & Chiến Lược Caching

### 3.1. Đánh Giá Kiến Trúc: Node.js Hiện Tại vs Golang Khi Scale Lớn
*   **Node.js Express (`services/node-server`) - Hiện hành (Khuyến nghị giai đoạn 1)**:
    *   *Ưu điểm*: Tối giản, không phụ thuộc trình biên dịch bên ngoài, chia sẻ cú pháp JS đồng nhất với Frontend.
    *   *Hiệu năng hiện tại*: Đủ sức gánh hàng trăm người dùng đồng thời nhờ cơ chế In-memory Buffer Cache (giới hạn tối đa 1 fetch tới cổng GTVT mỗi 10s/camera).
    *   *Đánh giá*: Phù hợp nhất cho giai đoạn tinh gọn, kiểm thử và vận hành ban đầu.
*   **Golang - Lựa chọn nâng cấp khi scale lớn (High Traffic)**:
    *   *Khi nào cần Go*: Khi số lượng camera mở rộng lên hàng nghìn trên toàn quốc hoặc lượng người dùng đồng thời đạt hàng chục nghìn kết nối. Goroutines và Memory footprint của Go (< 30MB RAM) sẽ phát huy ưu thế vượt trội so với V8 engine.
    *   *Quyết định hiện tại*: Đã loại bỏ code Golang thừa (`services/api-go`) để tập trung vào Node.js, chỉ tái khởi động module Go khi có nhu cầu tải thực tế.

### 3.2. Chiến Lược Caching Đa Tầng (Multi-Tier Caching)
1. **Lớp 1 - Client Browser**: Cache 10s qua header `Cache-Control: public, max-age=10`.
2. **Lớp 2 - Backend Memory Cache (Go RAM / Node Buffer)**:
   - Lưu trữ trực tiếp Buffer ảnh của 837 camera.
   - Khi có 100 người dùng cùng xem 1 camera, chỉ có **1 request** duy nhất được gửi tới cổng GTVT mỗi 10 giây.
3. **Lớp 3 - Edge Cache (Cloudflare Worker)**:
   - Sử dụng Cloudflare Cache API tại biên với TTL 15s cho người dùng toàn cầu.

### 3.3. Quyết Định Lưu Trữ & Quản Lý Trạng Thái (Storage Strategy)
*   **Giai đoạn hiện tại (Quyết định chính thức)**:
    - Sử dụng 100% **In-memory RAM Cache** (`sync.RWMutex` trong Golang / `Map` trong Node.js) kết hợp tệp dữ liệu tĩnh `cameras_all.json`.
    - **Tuyệt đối không dùng Database ngoài** (không cài PostgreSQL, Redis, MongoDB). Duy trì triết lý Zero-Bloat: máy tính không cần cài thêm bất kỳ service chạy ngầm nào, clone code về là chạy ngay.
*   **Giai đoạn tương lai (Khi tích hợp Computer Vision)**:
    - Khi mô hình CV cần lưu chuỗi thời gian (time-series) lịch sử kẹt xe hoặc báo cáo thống kê theo giờ, sẽ xem xét SQLite nhúng (`data/traffic.db`) hoặc Redis.

---

## 4. Lộ Trình Tích Hợp Computer Vision (CV Engine)

### 4.1. Mục Tiêu Nghiệp Vụ Của Module CV
*   **Đo mật độ phương tiện (Vehicle Density Estimation)**: Phân loại mật độ Thấp (Clear), Trung bình (Moderate), Đông đúc (Heavy), Kẹt cứng (Congested).
*   **Phát hiện hàng xe nối đuôi (Queue Length Detection)**: Đặc biệt nhận diện xe container và xe tải nặng nối đuôi nhau chờ vào cảng Cát Lái hoặc qua ngã tư.
*   **Cảnh báo sự cố / Ngập nước**: Nhận diện lòng đường bóng nước sau mưa bão hoặc xe gặp sự cố chắn ngang làn đường.

### 4.2. Kiến Trúc Triển Khai CV Engine
```
 [Cổng GTVT TP.HCM]
         │ (Tải ảnh định kỳ 30s-60s)
         ▼
 [CV Worker (Python/YOLO)] ────> [Inference / Model Detection]
         │                                      │
         ▼                                      ▼
 [SQLite / Redis / PostGIS] <────── [Lưu kết quả phân tích: Điểm nghẽn, % kẹt xe]
         │
         ▼
 [Backend Golang API] ─────────────> [Camera View Frontend Web]
 (Endpoint: /api/traffic-status)     (Đổi màu marker: Đỏ = Kẹt, Vàng = Đông, Xanh = Thoáng)
```

*   **Công nghệ dự kiến**:
    *   Model: YOLOv8-nano / YOLOv11-nano (tối ưu tốc độ, chạy mượt trên CPU máy chủ giá rẻ).
    *   Ngôn ngữ: Python (FastAPI / Celery) hoặc triển khai Go thông qua ONNX Runtime (`onnxruntime-go`).
    *   Cơ chế: Chạy định kỳ nền (Background Worker) quét qua các camera trọng điểm, không chạy realtime trên từng request để tránh quá tải CPU/GPU.

### 4.3. Kiến Trúc Mở Rộng Cho Nhiều Người Dùng (Multi-User Scalability)
Khi hệ thống có từ hàng trăm đến hàng chục ngàn người dùng đồng thời, nếu để mỗi người dùng kích hoạt 1 lần tải ảnh và 1 lần chạy AI thì hệ thống sẽ sập ngay lập tức. Giải pháp tối ưu theo chuẩn thế giới là: **"Tách rời Tải người dùng khỏi Tải tính toán" (Decoupled Compute via Hot Set Pool)**:

1. **Khái niệm "Hot Set" (Tập Camera Đang Được Quan Tâm)**:
   - Hệ thống đếm số người đang xem hoặc ghim từng camera.
   - Bất kỳ camera nào đang có ít nhất 1 người xem (hoặc nằm trong ~30 camera cửa ngõ huyết mạch như Cát Lái, Mỹ Thủy, An Sương) sẽ tự động lọt vào danh sách **Hot Set**.
2. **Worker chạy nền cố định (Fixed-Rate Worker)**:
   - Worker CV chỉ quét danh sách Hot Set mỗi 30 – 45 giây/lần.
   - Dù có **1 người** hay **100.000 người** cùng mở xem camera Cát Lái, CPU/GPU của bạn **chỉ chạy đúng 1 lần inference** mỗi 30 giây!
3. **Phục vụ kết quả tức thì (< 2ms)**:
   - Kết quả phân tích được lưu đệm trong RAM Go (`sync.RWMutex`).
   - Mọi người dùng gửi request `GET /api/traffic-status` đều nhận ngay kết quả JSON tức thì từ RAM mà không phải chờ AI tính toán.
4. **Chi phí hạ tầng cực thấp**:
   - Chỉ cần 1 VPS giá rẻ ($10 – $20/tháng) chạy ONNX Runtime trên CPU đã có thể phân tích 50–100 camera Hot Set liên tục mà không cần GPU đắt đỏ.

### 4.4. Định Dạng Kết Quả Báo Cáo Tắc Đường (Traffic Status API Schema)
```json
{
  "updatedAt": 1726750000000,
  "hotCount": 35,
  "status": {
    "662b86c41afb9c00172dd31c": {
      "level": "HEAVY",
      "levelVi": "Đông đúc / Chậm",
      "vehicleCount": 46,
      "containerCount": 18,
      "queueLengthM": 140,
      "isFlooded": false,
      "recommendation": "Xe tải nên cân nhắc lộ trình vòng qua cầu Phú Mỹ"
    }
  }
}
```

### 4.5. Chiến Lược Module Hóa CV Theo Độ Khả Thi Từng Giai Đoạn (Pluggable Adapter)
Thay vì khóa cứng vào một công nghệ AI duy nhất ngay từ đầu, hệ thống thiết kế theo mô hình **Adapter Pattern (Cắm & Rút linh hoạt)**:

```
┌─────────────────────────────────────────────────────────────┐
│               GIAO DIỆN CHUẨN (CV INTERFACE)                 │
│        AnalyzeTraffic(ImageBuffer) -> TrafficVerdict        │
└─────────────────────────────────────────────────────────────┘
                               │
       ┌───────────────────────┼───────────────────────┐
       ▼                       ▼                       ▼
  [GIAI ĐOẠN 1]           [GIAI ĐOẠN 2]           [GIAI ĐOẠN 3]
  Mock / Heuristic      Cloud Vision API        Self-Hosted YOLO/ONNX
  (Test UI, 0đ chi phí) (Gemini / OpenAI API)   (Python FastAPI / Go ONNX)
  - Phục vụ dev & dựng   - Thử nghiệm nhanh,    - Triển khai Production khi
    giao diện marker      chính xác, không cần   đông người dùng, tối ưu
    đổi màu trước         huấn luyện model       tốc độ & chi phí cực rẻ
```

*   **Lợi ích lớn nhất**: Toàn bộ Frontend (`apps/web`) và cấu trúc Backend API (`GET /api/traffic-status`) hoàn toàn giữ nguyên 100%. Tùy vào thời điểm, ngân sách và độ khả thi của từng bài toán nghiệp vụ, chúng ta chỉ việc chuyển đổi Adapter bên dưới mà không làm vỡ hệ thống.

---

## 5. Trạng Thái & Các Bước Thực Hiện Tiếp Theo (Action Items)

1. [x] Di dời Frontend vào `apps/web/`.
2. [x] Đổi tên ứng dụng thành **CAMERA VIEW**.
3. [x] Dọn dẹp root: Chuyển backend cũ vào `services/node-server/` và crawler vào `tools/crawler/`.
4. [x] Chuẩn hóa tập lệnh `Makefile` ở thư mục gốc (`make dev-node`, `make dev-go`, `make crawl`, `make test-go`).
5. [x] Dựng sẵn khung Backend Golang trong `services/api-go/` (đã test chạy thành công).
6. [x] Lưu trữ quyết định: In-memory RAM Cache + static JSON (Zero-Bloat).
7. [ ] Xây dựng thư viện mô phỏng dữ liệu tắc đường `/api/traffic-status` (Mock Data) để Frontend sẵn sàng đổi màu marker hiển thị trước.
8. [ ] Chuẩn bị khung dịch vụ Computer Vision trong `services/cv-engine/`.
