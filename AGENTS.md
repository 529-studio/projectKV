# AGENTS.md - Quy tắc & Hướng dẫn Phát triển Dự án

Tài liệu này định nghĩa các nguyên tắc cốt lõi, tiêu chuẩn thiết kế và quy tắc kỹ thuật bắt buộc cho toàn bộ AI Agents và lập trình viên khi tham gia phát triển codebase này.

---

## 1. Triết lý Thiết kế (Design Philosophy)

> **"Tối giản • Đơn sắc • Thực dụng • Ưu tiên UX"**

*   **Đơn sắc & Trực quan (Monochromatic & High-Contrast)**:
    *   Tông màu chủ đạo: Đen (`#000000`), Xám đậm (`#121212`, `#1e1e1e`), Trắng (`#ffffff`) và Xám nhạt (`#888888`).
    *   Màu sắc chức năng chỉ dùng khi thật sự cần cảnh báo trạng thái: Đỏ (Cấm / Kẹt xe), Xanh (Thông thoáng / Được chạy), Vàng (Chậm).
    *   **Tuyệt đối KHÔNG sử dụng**: Hiệu ứng bóng bẩy, gradient màu mè, bóng đổ rực rỡ (glow/heavy shadows), bo tròn bong bóng lòe loẹt.
*   **Ngôn ngữ Giao diện (Language Standard)**:
    *   **Bắt buộc sử dụng Tiếng Việt CÓ DẤU đầy đủ**: Tuyệt đối **KHÔNG** sử dụng tiếng Việt không dấu trong giao diện, tiêu đề, nút bấm, nhãn mác, thông báo hay tài liệu.
    *   Dùng từ ngữ kỹ thuật, chính xác, chuẩn chính tả và ngữ pháp tiếng Việt.
*   **Tự Động Hóa AI (Zero-Click AI)**:
    *   Tính năng AI Vision phân tích ảnh phải chạy **tự động hoàn toàn (Automatic)** khi mở/chọn camera hoặc chạy nền có cache.
    *   **Tuyệt đối không bắt người dùng phải bấm thêm nút "Soi bằng AI"** thủ công.
*   **Loại Bỏ Tính Năng Thừa (No Bloat Features)**:
    *   Không tích hợp các nút bấm chia sẻ Zalo, chuyển hướng Waze khi chưa có yêu cầu cụ thể.
    *   Tập trung 100% vào việc hiển thị camera, tra cứu và tự động đánh giá hiện trường.

---

## 2. Quy tắc Bản đồ & Dữ liệu (Map & Spatial Rules)

*   **Tile Layer sạch, không dính Watermark**:
    *   Không sử dụng các tile server thương mại chưa kích hoạt bản quyền (tránh lỗi watermark *"API KEY REQUIRED"*).
    *   Sử dụng OpenStreetMap chuẩn (`https://tile.openstreetmap.org/{z}/{x}/{y}.png`) kết hợp bộ lọc đơn sắc (Grayscale/Neutral) trong CSS để không làm rối mắt người điều độ.
*   **Chủ quyền Lãnh thổ Việt Nam**:
    *   Bắt buộc duy trì cấu hình `maxBounds` khóa chặt khung nhìn trong đất liền Đông Nam Bộ (`[10.15, 106.35]` đến `[11.35, 107.45]`), triệt tiêu rủi ro tranh chấp biển đảo.
*   **Chống quá tải hiển thị Marker (Clutter Prevention)**:
    *   Không vẽ dày đặc 800 vòng tròn lớn che kín toàn bộ bản đồ.
    *   Camera bình thường: Render dạng chấm nhỏ li ti (`circleMarker` bán kính 2.5px, viền đen sắc nét).
    *   Camera đang được chọn/theo dõi: Hiển thị dạng Dot to hơn (đường kính 20px, viền đậm, bring to front trên các layer khác, số thứ tự `#1`, `#2` nằm gọn trong hình tròn), tuyệt đối không dùng hộp text dài che lấp bản đồ.
*   **Không Vẽ Tuyến Cố Định / Tuyến Mặc Định (No Default Routes)**:
    *   Không ép buộc vẽ bất kỳ tuyến đường mặc định nào khi mở app.
    *   Ứng dụng tập trung 100% vào việc kiểm tra camera tự do theo thời gian thực (Free Camera Explorer) tại các nút thắt then chốt để điều độ nắm thực tế hiện trường trước khi ra quyết định điều xe.

---

## 3. Quy tắc Kỹ thuật & Hiệu năng (Engineering Rules)

*   **Không thêm thư viện (Zero-Bloat)**:
    *   Không cài thêm các UI framework nặng nề (Bootstrap, Tailwind build tool cồng kềnh, jQuery).
    *   Giữ HTML/CSS/JS thuần, tải trang tức thì.
*   **Bảo vệ API & Chống chặn IP**:
    *   Mọi ảnh snapshot từ cổng giao thông phải đi qua proxy của backend (`server.js` hoặc `worker.js`) kèm cache tối thiểu 10–15 giây.
    *   Không bao giờ để client fetch trực tiếp hàng loạt ảnh từ `giaothong.hochiminhcity.gov.vn`.
*   **Quy định Giờ Cấm Tải xe đầu kéo**:
    *   Luôn kiểm tra và cảnh báo 2 khung giờ cấm xe đầu kéo tại Bình Dương: **06:00 - 08:00** và **16:30 - 18:30**.
    *   AI Vision (Gemini 3.8 Flash) phải trả về văn bản ngắn gọn, tập trung vào 3 yếu tố:
        1. Mật độ xe container và chiều dài hàng xe nối đuôi.
        2. Nguy cơ ngập nước / sự cố cản đường.
        3. Khuyến nghị hành động (Xuất bến / Chờ / Đổi tuyến).
