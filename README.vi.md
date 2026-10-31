# CAMERA VIEW

<div align="center">

**Ho Chi Minh City Traffic Camera Monitor & Extensible Analytics**  
*Ứng Dụng Xem Camera Giao Thông TP. Hồ Chí Minh & Tính Năng Mở Rộng*

<p align="center">
  <a href="https://github.com/529-studio/projectKV/actions"><img src="https://img.shields.io/badge/CI-passing-2ea44f?logo=github&logoColor=white" alt="CI"></a>
  <a href="https://nextjs.org/"><img src="https://img.shields.io/badge/Next.js-15.5+-000000?logo=nextdotjs&logoColor=white" alt="Next.js"></a>
  <a href="https://www.typescriptlang.org/"><img src="https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript&logoColor=white" alt="TypeScript"></a>
  <a href="https://tailwindcss.com/"><img src="https://img.shields.io/badge/Tailwind_CSS-3.4+-06B6D4?logo=tailwindcss&logoColor=white" alt="Tailwind CSS"></a>
  <a href="https://www.framer.com/motion/"><img src="https://img.shields.io/badge/Framer_Motion-11+-0055FF?logo=framer&logoColor=white" alt="Framer Motion"></a>
  <a href="https://leafletjs.com/"><img src="https://img.shields.io/badge/Leaflet-1.9+-199900?logo=leaflet&logoColor=white" alt="Leaflet"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="https://sqlite.org/"><img src="https://img.shields.io/badge/SQLite-3-003B57?logo=sqlite&logoColor=white" alt="SQLite"></a>
  <a href="docs/ADR-001-flood-alert-architecture.md#adr-003-sprint-1-scope--live-updates-sse-hot-set-scoped-congestion-colors-phường-based-filter"><img src="https://img.shields.io/badge/SSE-Live_Stream-FFA500?logo=html5&logoColor=white" alt="SSE"></a>
  <a href="https://open-meteo.com/"><img src="https://img.shields.io/badge/Open--Meteo-Weather_API-FF8C00?logo=weatherdotcom&logoColor=white" alt="Open-Meteo"></a>
  <a href="https://www.rainviewer.com/api.html"><img src="https://img.shields.io/badge/RainViewer-Radar_API-0284C7?logo=radar&logoColor=white" alt="RainViewer"></a>
  <a href="https://www.cloudflare.com/developer-platform/r2/"><img src="https://img.shields.io/badge/Cloudflare_R2-Object_Storage-F38020?logo=cloudflare&logoColor=white" alt="Cloudflare R2"></a>
  <a href="https://python.org/"><img src="https://img.shields.io/badge/Python-3.11+-3776AB?logo=python&logoColor=white" alt="Python"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-AGPL--3.0-blue" alt="License"></a>
</p>

[English](README.md#english) &nbsp;•&nbsp; [Tiếng Việt](README.vi.md#tiếng-việt)

<br />
<br />

<p align="center">
  <img src="apps/web/public/warded-dockUI.jpg" alt="Warded Traffic Camera Monitor - Multi-Camera Corridor Dock UI" width="100%">
</p>
<p align="center">
  <em>Multi-Camera Corridor Tracking Dock & Map Explorer</em>
</p>

<br />

<p align="center">
  <img src="apps/web/public/warded-camera.jpg" alt="Warded Traffic Camera Monitor - Live Camera Inspection View" width="100%">
</p>
<p align="center">
  <em>Live Camera Snapshot Modal with Traffic Verdict & Pin Control</em>
</p>

</div>

---

# Tiếng Việt

> **Ứng dụng tra cứu và theo dõi camera giao thông Thành phố Hồ Chí Minh với độ trễ thấp tối ưu, giao diện tối giản trực quan, hỗ trợ bộ lọc phân cấp liên hoàn, dock ghim theo dõi nhiều camera đồng thời, tích hợp đa ngôn ngữ i18n cùng các tính năng phân tích mở rộng — hướng tới xử lý thị giác máy tính thời gian thực và cảnh báo ngập lụt.**

Hệ thống được tổ chức theo mô hình monorepo, tách biệt Web Client (`apps/web`) khỏi backend Go (`services/go-server`), với một service Python riêng (`services/cv-engine`) chuyên trách suy luận mô hình (model inference).

## Điểm nổi bật chính

*   **Giao diện tối giản & Trực quan**: Thiết kế đơn sắc tương phản cao với gam màu đen, xám và trắng.
*   **Hỗ trợ đa ngôn ngữ i18n**: Chuyển đổi tức thì giữa Tiếng Việt (`vi`, mặc định) và Tiếng Anh (`en`), tự động ghi nhớ tùy chọn.
*   **Tối ưu trực quan bản đồ & Lọc camera mất tín hiệu**: Mặc định ẩn 135 camera mất tín hiệu hoặc lỗi nguồn (`NOT_IMAGE` / `NO_SIGNAL`), hiển thị huy hiệu đếm camera trực quan (`Đang hiển thị 702 / 837 camera`) kèm nút chuyển đổi tinh tế.
*   **Bộ lọc sâu liên hoàn (Chained Filter)**: Lọc đa tầng theo *Phường / Khu vực* ➔ *Tuyến đường / Hành lang* ➔ *Giao lộ*.
*   **Màu sắc Điểm nghẽn Trục chính Thời gian thực**: Tự động tô màu 50 camera Hot Set trục chính theo chu kỳ 30 giây dựa trên chỉ số bão hòa PCU từ OpenVINO: Xanh lục (`#10b981`) khi Thông thoáng ($SI < 0.30$), Vàng hổ phách (`#f59e0b`) khi Đông xe ($0.30 \le SI < 0.60$), và Đỏ hồng (`#ef4444`) khi Ùn ứ ($SI \ge 0.60$) với hiệu ứng xung nhịp tinh tế, tooltip hiển thị phần trăm bão hòa và PCU, cùng thanh chú giải đơn sắc ở chân bản đồ `[Xanh: Thông thoáng] [Vàng: Đông xe] [Đỏ: Ùn ứ]`.
*   **Đại tu Đường ống Radar Doppler Khí tượng (S8-T1)**: Chiếu tile radar RainViewer ở độ phân giải gốc Zoom 8 (`0_0.png`) không nội suy mờ nhòe, giải mã phản hồi RGB theo ngưỡng phản xạ thực tế dBZ (18–29 dBZ Mưa nhỏ, 30–39 dBZ Mưa vừa, $\ge 40$ dBZ Mưa to), lấy mẫu 7 điểm co rút hình học với cơ chế đồng thuận quorum $\ge 3/7$, kết hợp cổng đối soát thực địa với camera mặt đường giúp triệt tiêu hoàn toàn cảnh báo ngập giả vào ngày nắng ráo.
*   **Dock theo dõi nhiều camera đồng thời**: Ghim tối đa 12 camera quan trọng vào thanh dock bên dưới với chu kỳ tự động cập nhật snapshot đồng bộ.
*   **Báo cáo ngập nước cộng đồng (Human-in-the-Loop)**: Cho phép người dân báo cáo ngập thực địa ngay trong modal camera kèm bộ đếm ngược chống spam 30 giây và phản hồi xúc giác tối giản.
*   **Phân loại đa làn hai chiều & Mốc ngập thực tế**: Phân loại mật độ giao thông độc lập theo hướng (Hướng A xe tiến vs Hướng B xe lùi) xử lý bất đối xứng trên đại lộ phân cách, đồng thời neo mức độ ngập lụt chuẩn hóa theo vạch vành bánh xe máy.
*   **Backend Go hiệu năng cao**: File thực thi tĩnh duy nhất, lưu trữ SQLite WAL, điều phối Hot Set theo chu kỳ 30s.
*   **Lưu trữ Cloudflare R2**: Lưu trữ ảnh snapshot bằng chứng giao thông chuẩn hóa thời gian ICT (UTC+7), phân phối qua Edge CDN (`https://file.529studio.site`).

## Khởi chạy nhanh

### Cách 1: Khởi chạy cơ bản (Không cần Docker model)
Phù hợp để phát triển giao diện hoặc kiểm thử nhanh mà không cần chạy mô hình nhận diện AI:

```bash
# Cài đặt dependencies (lần đầu tiên)
make install

# Khởi chạy Go Backend (:3000) và Next.js Frontend (:3001) đồng thời
make dev-all

# Hoặc chỉ chạy Go Backend (cổng 3000) - Phục vụ cả API và Web tĩnh
make dev-go
```

### Cách 2: Khởi chạy Full-Stack kèm Mô hình OpenVINO cục bộ (Khuyên dùng)
Để chạy toàn bộ hệ thống thực tế với mô hình YOLOv8n OpenVINO nhận diện xe và tính toán chỉ số bão hòa PCU:

```bash
# Bước 1: Khởi động container model ngầm (cổng 8000)
make dev-model
# (Tương đương: docker compose -f docker-compose.local.yml up -d)

# Bước 2: Đảm bảo file .env đã cấu hình kết nối model:
# ANALYZER=onnx
# CV_ENGINE_URL=http://localhost:8000

# Bước 3: Chạy song song Go Backend (:3000) và Next.js Frontend (:3001)
make dev-all
```

#### Bảng Cổng Dịch Vụ & Địa Chỉ Truy Cập Cục Bộ

| Dịch vụ | Cổng | Địa chỉ truy cập | Mục đích kiểm tra |
|---|:---:|---|---|
| **Giao diện Bản đồ** | `3001` | http://localhost:3001 | Bản đồ camera, dock theo dõi hành lang |
| **Gán nhãn Labeler** | `3001` | http://localhost:3001/labeler | Công cụ dán nhãn tập trung với bộ lọc duyệt nhãn tự động, bộ nhớ đệm phân vùng, phím tắt duyệt nhanh Shift+Enter và chế độ phân vùng vẽ polygon ground truth 6 lớp (ADR-011 D1) |
| **Go Backend API** | `3000` | http://localhost:3000 | Máy chủ API Gin |
| **API Đăng ký Cảnh báo** | `3000` | http://localhost:3000/api/subscriptions | `POST`, `GET`, `DELETE` đăng ký cảnh báo ngập |
| **Tài liệu Swagger** | `3000` | http://localhost:3000/swagger/index.html | Tài liệu API tương tác |
| **Kiểm tra Sức khỏe BE** | `3000` | http://localhost:3000/health | Phản hồi `{"status":"ok"}` |
| **Mô hình CV Engine** | `8000` | http://localhost:8000/healthz | Phản hồi trạng thái container `warded-model` |

> [!NOTE]
> `make dev-all` khởi chạy Go Backend và Next.js Dev Server. Lệnh này giả định container Docker `warded-model` đã được khởi động từ trước khi chọn `ANALYZER=onnx`.

*   **Truy cập trình duyệt**:
    *   Toàn bộ ứng dụng (Backend + Web tĩnh): `http://localhost:3000`
    *   Giao diện Frontend Dev (Hot Reload): `http://localhost:3001`

## Hạ tầng & Phân bổ Microservices

| Dịch vụ | Công nghệ | Cổng | Node & Máy chủ | Vai trò chính |
|---|---|:---:|---|---|
| **Web Client** (`apps/web`) | Next.js 15, React 19, Tailwind CSS, Leaflet | `3001` | `edge host` | Trực quan hóa bản đồ, điểm tròn camera, H3 hex overlay, dock ghim camera, giao diện song ngữ (`vi` / `en`). |
| **Backend API** (`services/go-server`) | Go 1.26, Gin, SQLite (WAL mode) | `3000` | `edge host` | Proxy cache snapshot (15s TTL), điều phối Hot Set (chu kỳ 30s), lưu trữ và cầu nối phản ánh ngập cộng đồng thời gian thực (`internal/feedback` sang SSE & Hex Aggregator), tính chỉ số bão hòa PCU, luồng SSE, tích hợp lưu trữ Cloudflare R2. |
| **CV Engine** (`services/cv-engine`) | Python 3.11, ThreadingHTTPServer, OpenVINO IR, YOLOv8n-seg FP16 | `8000` | `inference worker` | Nhận diện phương tiện thông lượng cao và phân đoạn ngập nước mặt đường (`POST /predict`), phân tầng vật lý 2 lớp (Layer 1 tỷ lệ nước mặt đường, Layer 2 độ ngập bánh xe máy), kiểm tra sức khỏe `GET /healthz` (< 38.5ms latency). |
| **Lưu trữ & Edge CDN** | Cloudflare R2, AWS Signature V4 | `443` | Cloudflare Edge (`file.529studio.site`) | Lưu trữ ảnh chụp bằng chứng giao thông (`warded-proofs`), phân cấp ngày theo giờ Việt Nam ICT (`UTC+7`, `YYYY-MM-DD/<camera_id>/<unix_ts>_<reason>.jpg`). |

## Công nghệ cốt lõi (Tech Stack)

| Phân tầng | Công nghệ & Huy hiệu | Vai trò |
|---|---|---|
| **Frontend** | [![Next.js](https://img.shields.io/badge/Next.js-15.5+-000000?logo=nextdotjs&logoColor=white)](https://nextjs.org/) [![TypeScript](https://img.shields.io/badge/TypeScript-5.0+-3178C6?logo=typescript&logoColor=white)](https://www.typescriptlang.org/) [![Tailwind CSS](https://img.shields.io/badge/Tailwind_CSS-3.4+-06B6D4?logo=tailwindcss&logoColor=white)](https://tailwindcss.com/) [![Framer Motion](https://img.shields.io/badge/Framer_Motion-11+-0055FF?logo=framer&logoColor=white)](https://www.framer.com/motion/) [![Leaflet](https://img.shields.io/badge/Leaflet-1.9+-199900?logo=leaflet&logoColor=white)](https://leafletjs.com/) | Next.js App Router, TypeScript, Tailwind CSS, animation Framer Motion, bản đồ tương tác Leaflet |
| **Backend** | [![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev/) [![Gin](https://img.shields.io/badge/Gin-1.10+-008ECF?logo=go&logoColor=white)](https://gin-gonic.com/) [![SQLite](https://img.shields.io/badge/SQLite-3-003B57?logo=sqlite&logoColor=white)](https://sqlite.org/) [![SSE](https://img.shields.io/badge/SSE-Live_Stream-FFA500?logo=html5&logoColor=white)](docs/ADR-001-flood-alert-architecture.md#adr-003-sprint-1-scope--live-updates-sse-hot-set-scoped-congestion-colors-phường-based-filter) | Single static binary (Go), Gin router, lưu trữ SQLite, in-memory TTL cache, phát luồng Server-Sent Events (SSE) trực tiếp |
| **Lưu trữ đám mây & CDN** | [![Cloudflare R2](https://img.shields.io/badge/Cloudflare_R2-Object_Storage-F38020?logo=cloudflare&logoColor=white)](https://www.cloudflare.com/developer-platform/r2/) [![AWS SDK v2](https://img.shields.io/badge/AWS_SDK_v2-Go-FF9900?logo=amazonwebservices&logoColor=white)](https://github.com/aws/aws-sdk-go-v2) | Lưu trữ ảnh bằng chứng trên Cloudflare R2 (`warded-proofs`), phân phối qua Edge CDN (`file.529studio.site`), phục vụ dán nhãn tập trung cho toàn đội ngũ |
| **Thị giác máy tính** | [![Python](https://img.shields.io/badge/Python-3.11+-3776AB?logo=python&logoColor=white)](https://python.org/) [![PyTorch](https://img.shields.io/badge/PyTorch-EE4C2C?logo=pytorch&logoColor=white)](https://pytorch.org/) [![OpenCV](https://img.shields.io/badge/OpenCV-5C3EE8?logo=opencv&logoColor=white)](https://opencv.org/) [![OpenVINO](https://img.shields.io/badge/Intel_OpenVINO-Runtime-0071C5?logo=intel&logoColor=white)](https://openvino.ai/) | Python 3.11+, suy luận mô hình OpenVINO IR YOLOv8n-seg FP16, phân tích kẹt xe, phân đoạn mặt nước và phân tầng vật lý 2 lớp đo độ ngập bánh xe máy (ADR-009) |
| **Dịch vụ Thời tiết & Radar** | [![Open-Meteo](https://img.shields.io/badge/Open--Meteo-Weather_API-FF8C00?logo=weatherdotcom&logoColor=white)](https://open-meteo.com/) [![RainViewer](https://img.shields.io/badge/RainViewer-Radar_API-0284C7?logo=radar&logoColor=white)](https://www.rainviewer.com/api.html) | Dữ liệu khí tượng, nhiệt độ, lượng mưa thời gian thực (Open-Meteo) và tile ảnh radar phản hồi mưa trực tiếp (RainViewer) |
| **DevOps & Công cụ** | [![CI](https://img.shields.io/badge/GitHub_Actions-CI-2ea44f?logo=githubactions&logoColor=white)](https://github.com/features/actions) [![License](https://img.shields.io/badge/License-AGPL--3.0-blue)](LICENSE) [![Geo-Enrich](https://img.shields.io/badge/Geo--Enrich-Point--in--Polygon-708090?logo=python&logoColor=white)](tools/geo-enrich) | CI/CD GitHub Actions, golangci-lint, ESLint, Makefile điều phối tập trung, công cụ gán Phường point-in-polygon |

### Bảng Thông Số Cấu Hình Dịch Vụ

#### Dịch vụ Backend (`services/go-server`)

| Biến môi trường | Giá trị mặc định | Diễn giải |
|---|---|---|
| `PORT` | `3000` | Cổng dịch vụ HTTP |
| `STORAGE_DRIVER` | `local` | Trình điều khiển lưu trữ (`local` cho ổ cứng máy chủ hoặc `r2` / `s3` cho Cloudflare R2) |
| `STORAGE_LOCAL_ROOT` | `./data/proofs` | Thư mục lưu trữ bằng chứng cục bộ khi dùng driver `local` |
| `R2_ACCOUNT_ID` | `<your-cloudflare-account-id>` | Định danh tài khoản Cloudflare |
| `R2_ENDPOINT` | `https://<account_id>.r2.cloudflarestorage.com` | Base endpoint chuẩn S3 REST API cho Cloudflare R2 |
| `R2_BUCKET_NAME` | `warded-proofs` | Tên bucket lưu trữ đối tượng ảnh |
| `R2_REGION` | `auto` | Vùng ký xác thực AWS Signature V4 của R2 |
| `R2_PUBLIC_URL` | `https://file.529studio.site` | Tên miền phân phối tĩnh Cloudflare Edge CDN |
| `CV_ENGINE_URL` | `http://localhost:8000` | Địa chỉ mạng kết nối tới máy chủ suy luận mô hình CV |
| `ANALYZER` | `none` | Trình phân tích giao thông (`none`, `mock`, `vlm`, `onnx`) |
| `SNAPSHOT_UPSTREAM_BASE_URL` | *(bắt buộc)* | Cổng snapshot camera giao thông Sở GTVT TP.HCM |
| `TELEGRAM_BOT_TOKEN` | `""` | Token Telegram Bot API để phát cảnh báo ngập lụt (để trống để chạy ở chế độ giả lập) |
| `TELEGRAM_DEBOUNCE_MINUTES` | `30` | Cửa sổ trượt chống spam cảnh báo cho từng điểm ngập (phút) |
| `HARVESTER_ENABLED` | `false` | Bật module chạy nền tự động thu thập ảnh theo lịch và theo mưa |
| `HARVESTER_SCHEDULE_HOURS` | `7,12,17` | Lịch thu thập khung giờ cao điểm theo giờ Việt Nam ICT (UTC+7, ví dụ `07:30,12:00,17:30`) |
| `HARVESTER_RAIN_COOLDOWN_MINUTES` | `60` | Cửa sổ trượt giãn cách giữa các lần kích hoạt thu thập theo mưa (phút) |
| `HARVESTER_CONCURRENCY` | `15` | Số lượng luồng worker thu thập ảnh và đẩy lên R2 đồng thời |
| `HARVESTER_CATCHUP_MINUTES` | `120` | Cửa sổ bù khởi động: slot bị lỡ trong khung này sẽ chạy một lần khi server khởi động muộn hoặc khởi động lại (0 để tắt) |

#### Dịch vụ Suy Luận Mô Hình (`services/cv-engine`)

| Thông số | Giá trị triển khai | Diễn giải |
|---|---|---|
| **Máy chủ Host** | `inference worker` | Máy chủ phần cứng độc lập, kết nối mạng nội bộ an toàn qua private mesh VPN mesh |
| **Engine thực thi** | Intel OpenVINO 2024.4 IR | Runtime tối ưu hóa FP16 suy luận trên CPU cho kiến trúc YOLOv8n (~6.3 MB) |
| **Kích thước ảnh** | 416x416 (kèm tiền xử lý letterbox & NMS) | Cân bằng giữa bảo toàn tỷ lệ khung hình, chống trùng lặp hộp nhận diện và độ trễ thấp |
| **Giới hạn tài nguyên** | 2 vCPU / 2 GB RAM | Mức trần tài nguyên cấu hình qua Docker Compose stack |
| **Endpoint API** | `POST /predict`, `GET /healthz` | Nhận ảnh JPEG nhị phân, phản hồi danh sách xe đếm được và nhịp tim sức khỏe |

### Kiểm chứng vận hành: cảnh báo ngập Telegram trên Staging

Trước khi mở đăng ký cảnh báo công khai, cần kiểm chứng phát cảnh báo live trên
staging bằng script pre-flight:

```bash
# Thông tin xác thực chỉ đọc từ biến môi trường hoặc file .env (không bao giờ hardcode)
TELEGRAM_BOT_TOKEN=<your-telegram-bot-token> TELEGRAM_CHAT_ID=<your-telegram-chat-id> \
  ./scripts/verify_staging_telegram.sh
```

Script kiểm tra tính hợp lệ của bot token (`getMe`), chuẩn thông điệp song ngữ
không emoji, và việc phát trực tiếp qua `sendMessage` (HTTP 200). Cơ chế chống
spam trượt 30 phút và che giấu token được xác thực tất định bằng
`go test -race ./cmd/server -run TestNotifyIntegration`.

## Tài liệu kiến trúc chuyên sâu
- [`docs/SYSTEM_DESIGN.md`](docs/SYSTEM_DESIGN.md) — thiết kế caching & roadmap CV gốc.
- [`docs/ADR-001-flood-alert-architecture.md`](docs/ADR-001-flood-alert-architecture.md) — đánh giá khả thi cảnh báo ngập lụt, lựa chọn kênh thông báo (Telegram/Zalo/Web Push/SMS), và quyết định chuyển backend sang Go (ADR-002).
- [`docs/ADR-009-zero-vlm-flood-segmentation.md`](docs/ADR-009-zero-vlm-flood-segmentation.md) — đặc tả kỹ thuật phân đoạn ảnh ngập nước on-premise, benchmark OpenVINO và phân tầng vật lý 2 lớp (Zero-VLM).

## Vì sao chuyển sang Go?
Không phải vì lưu lượng hiện tại đã vượt ngưỡng (chưa vượt), mà vì các thành phần mới cần xây (Hot Set Scheduler, Notification Adapter, state store, R2 object storage) hợp với goroutine/channel hơn, và rủi ro viết mới bằng Go thấp hơn nhiều so với việc viết lại `server.js` đang chạy ổn. Phần suy luận mô hình CV (YOLO/ONNX/OpenVINO, xử lý ảnh nặng) được đưa lên node phần cứng `inference worker` riêng biệt để bảo toàn CPU cho máy chủ web và backend.

## Giấy phép

Dự án này được phát hành theo **GNU Affero General Public License v3.0**
(AGPL-3.0). Xem [`LICENSE`](LICENSE) để đọc toàn văn.

AGPL-3.0 là giấy phép copyleft có điều khoản mạng: nếu bạn chạy một phiên bản
đã chỉnh sửa của phần mềm này dưới dạng dịch vụ mạng, bạn phải công bố mã nguồn
tương ứng cho người dùng của dịch vụ đó. Tự triển khai bản không chỉnh sửa thì
không phát sinh nghĩa vụ bổ sung.

- Đóng góp được chấp nhận theo cùng giấy phép, xem [`CONTRIBUTING.md`](CONTRIBUTING.md).
- Báo cáo lỗ hổng bảo mật theo [`SECURITY.md`](SECURITY.md).
