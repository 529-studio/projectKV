# Plan: Sửa Lỗi Hover Map Macro & Nâng Cấp Điều Hướng Camera (< >)

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Khắc phục triệt để lỗi không hover/click được camera ở bản đồ lớn (macro hex view), sửa lỗi quét bôi đen text trong Camera Detail, và nâng cấp hệ thống nút `<` `>` thành bộ điều hướng tuần tự mượt mà (carousel navigation) cho cả Pinned Dock lẫn camera theo tuyến đường (Corridor).

**Architecture:** 
1. **Map Marker Hit-target & Hover:** Khắc phục lỗi Leaflet không nhận `radius` trong `setStyle()` bằng cách gọi `setRadius()` trực quan; nâng bán kính tối thiểu lên $\ge 4\text{px}$ để đảm bảo hit-target; giữ viền tương phản cao khi hover.
2. **Modal Text Selection:** Thêm `select-none` toàn diện cho container `CameraModal`, ngắt hoàn toàn pointer propagation ở các nút con của dock (`onPointerDown`/`onPointerUp`), và xóa vùng chọn (`removeAllRanges()`) khi mở modal.
3. **Sequential Camera Navigation (< >):**
   - **CameraModal Overlay:** Bổ sung 2 nút tròn `<` và `>` nổi trên khung ảnh snapshot (theo kiểu carousel Instagram như ảnh mẫu của user) kèm phím tắt bàn phím `ArrowLeft` / `ArrowRight`.
   - **Context Resolver:** Tự động xác định danh sách camera kế cận dựa theo ngữ cảnh:
     - Nếu mở từ Pinned Dock: duyệt tuần tự trong `pinnedCameras`.
     - Nếu mở từ Tuyến đường (hoặc click trên bản đồ): tự động gom các camera cùng tên đường (ví dụ tất cả cam trên "Điện Biên Phủ" qua `extractCorridor`), cho phép bấm `<` `>` duyệt dọc theo suốt tuyến đường mà không cần đóng mở modal.
   - **Dock Tile Buttons:** Giữ nguyên 2 nút `◀` `▶` trên card dock, điều chỉnh hành vi khi bấm là mở/chuyển ngay sang camera trước/sau trong dock, chặn hoàn toàn pointer bubbling.

**Tech Stack:** Next.js (App Router), React, TypeScript, Tailwind CSS, Leaflet.

---

## 1. Báo Cáo Điều Tra Nguyên Nhân Gốc (Root Cause Analysis)

### 1.1. Tại sao ở bản đồ lớn (macro) có bật Vùng tổng hợp / Vùng mưa thì chỉ hover/bấm được cam đỏ/xanh, còn cam trắng thì không hiện tooltip và không bấm được?
1. **Co bán kính quá nhỏ (`targetR = 2.5px`):** Trong `MapComponent.tsx` (dòng 138, 245), khi zoom ở mức vĩ mô ($\le 13$) và bật Hex Overlay, các camera chưa có phân tích AI (`!marker.hasAIVerdict`, hiển thị màu trắng hoặc xám) bị gán `isMacroHexDimmed = true`. Bán kính bị giảm xuống còn `Math.max(2.5, Math.round(r * 0.7)) = 2.5px` (đường kính chỉ đúng 5 pixel) cùng với độ mờ `opacity: 0.35, fillOpacity: 0.25`.
2. **Lỗi `setStyle` của Leaflet không cập nhật bán kính:** Trong sự kiện `marker.on('mouseover')`:
   ```ts
   marker.setStyle({ radius: hoverRadius, ... })
   ```
   Trong thư viện Leaflet, hàm `setStyle()` của `CircleMarker` **hoàn toàn bỏ qua thuộc tính `radius`** (Leaflet chỉ hỗ trợ đổi bán kính qua `marker.setRadius(newRadius)`). Do đó, khi rê chuột vào, chấm tròn vẫn giữ kích thước li ti 2.5px.
3. **Mất nét và giật `mouseout` liên tục:** Khi chuột lướt vào chấm 2.5px, `setStyle` vẽ lại phần tử SVG `<path>`, khiến con trỏ chuột trượt ra ngoài ranh giới 2.5px chỉ sau 1 pixel di chuyển. Trình duyệt lập tức kích hoạt `mouseout`, đóng tooltip ngay trong tích tắc (người dùng cảm giác như không bao giờ hover được).
4. **Cam đỏ/xanh hoạt động vì sao?** Vì các camera có kết quả AI (`hasAIVerdict = true`) không bị kích hoạt `isMacroHexDimmed`, nên giữ nguyên bán kính chuẩn (3.5px - 6px), độ mờ đậm nét (0.95), và có viền tương phản cao nên hit-box đủ lớn để chuột kích hoạt ổn định.

### 1.2. Tại sao mở camera lên thì bị lỗi quét bôi đen (scan text) toàn bộ chữ trong modal?
1. **Xung đột Pointer Events trên Dock:** Trong `PinnedDock.tsx`, container của mỗi thẻ camera sử dụng `onPointerDown` và `onPointerUp` để hỗ trợ kéo thả (drag-to-reorder). Các nút con (`◀`, `▶`) trước đây chỉ có `onClick={(e) => e.stopPropagation()}` nhưng **không chặn `onPointerDown` và `onPointerUp`**.
2. **Kích hoạt mở modal trong lúc chuột đang nhả:** Khi người dùng bấm chuột vào thẻ dock, chuỗi sự kiện `pointerdown -> pointerup` trên thẻ kích hoạt `onCameraClick(cam)`. Modal `CameraModal` xuất hiện ngay lập tức bên dưới vị trí con trỏ chuột trong lúc hành vi nhả chuột của hệ điều hành/trình duyệt đang hoàn tất.
3. **Thiếu CSS `select-none`:** `CameraModal.tsx` không hề có lớp `select-none`. Trình duyệt hiểu thao tác nhấp-thả vừa rồi như một thao tác "drag-selection" (quét khối) và tự động bôi đen toàn bộ các thẻ text (`<h2>`, `<span>`, badge, footer) như trong ảnh chụp `uploaded_media_0_1791015771411.png`.

---

## 2. Thiết Kế Chi Tiết UX Điều Hướng `<` `>` (Navigation System)

### 2.1. Nút `<` `>` trên Camera Detail Modal (Ảnh mẫu `uploaded_media_2_1791015771411.png`)
- **Vị trí & Giao diện:**
  - Nút `<` (Previous): Nằm ở cạnh trái của khung ảnh snapshot, căn giữa theo chiều dọc.
  - Nút `>` (Next): Nằm ở cạnh phải của khung ảnh snapshot, căn giữa theo chiều dọc.
  - Thiết kế: Nút tròn bán trong suốt (`bg-zinc-950/80 hover:bg-zinc-900 border border-zinc-700/80 text-white w-9 h-9 flex items-center justify-center rounded-full shadow-lg backdrop-blur-sm cursor-pointer transition-transform active:scale-95`).
  - Trạng thái biên: Khi ở camera đầu tiên, nút `<` bị ẩn hoặc disable (`opacity-25 pointer-events-none`). Tương tự cho nút `>` ở camera cuối cùng.
- **Phím tắt:** Hỗ trợ bấm phím mũi tên trái `ArrowLeft` để lùi cam, mũi tên phải `ArrowRight` để tiến cam.
- **Nhãn hiển thị ngữ cảnh (Context Indicator):**
  - Hiển thị nhỏ gọn, tinh tế trên thanh header của modal: ví dụ `[Ghim: 2/5]` nếu đang xem dock, hoặc `[Điện Biên Phủ: 3/10]` nếu đang xem camera trên tuyến đường Điện Biên Phủ.

### 2.2. Nút `◀` `▶` trên Thẻ Dock (Pinned Dock Tiles)
- **Hành vi mới:**
  - Bấm `◀`: Lập tức chuyển sang camera liền trước trong danh sách ghim (`pinnedCameras[index - 1]`).
  - Bấm `▶`: Lập tức chuyển sang camera liền sau trong danh sách ghim (`pinnedCameras[index + 1]`).
  - Đảm bảo chặn triệt để: `onPointerDown={(e) => e.stopPropagation()}` và `onPointerUp={(e) => e.stopPropagation()}` để không kích hoạt kéo thả thẻ hay gây lỗi bôi đen chữ.
  - Việc sắp xếp thứ tự (Reorder) vẫn được thực hiện mượt mà bằng cách kéo thả chuột/ngón tay (Pointer drag-and-drop đã có).

### 2.3. Điều Hướng Tuyến Đường Cho Camera Thường Trên Bản Đồ (Corridor Navigation)
- Khi người dùng bấm vào một camera bất kỳ trên bản đồ hoặc trong cây phân cấp:
  - Nếu camera đang nằm trong danh sách ghim: ngữ cảnh điều hướng là `pinnedCameras`.
  - Nếu người dùng đang chọn 1 tuyến đường (`selectedCorridor`): ngữ cảnh là các camera của tuyến đó.
  - Nếu camera được bấm tự do trên bản đồ (không chọn bộ lọc): hệ thống dùng hàm `extractCorridor(cam.displayName, cam.title)` (ví dụ "Điện Biên Phủ") để tự động gom toàn bộ các camera trên cùng tuyến đường đó theo thứ tự tọa độ hoặc tên giao lộ (e.g. Điện Biên Phủ - Cách Mạng Tháng Tám -> Điện Biên Phủ - Đinh Tiên Hoàng -> Điện Biên Phủ - Hai Bà Trưng...).
  - Nhờ đó, người dùng có thể bấm `<` hoặc `>` để "đi dọc tuyến đường" và kiểm tra tình hình giao thông cực nhanh mà không cần đóng mở modal nhiều lần.

---

## 3. Kế Hoạch Thực Hiện Chi Tiết (Tasks)

### Task 1: Sửa lỗi Hit-box & Hover trên Bản đồ lớn (`MapComponent.tsx`)
**Files:**
- Modify: `apps/web/src/components/MapComponent.tsx`

**Chi tiết:**
1. Trong sự kiện `map.on('zoomend')`: đặt giới hạn dưới của `targetR` cho camera dimmed là $\ge 4.0\text{px}$ (thay vì 2.5px).
2. Khi khởi tạo marker: đặt `radius` cho dimmed marker tối thiểu là $4.0\text{px}$.
3. Trong `marker.on('mouseover')`:
   - Phải gọi `marker.setRadius(hoverRadius)` với `hoverRadius = Math.max(6.0, curBase + 2)`.
   - Giữ màu viền trắng `#ffffff` với `weight: 2`, `opacity: 1`, `fillOpacity: 0.9`.
4. Trong `marker.on('mouseout')`:
   - Phải gọi `marker.setRadius(isDim ? targetDimRadius : curBase)`.
   - Khôi phục `color`, `weight`, `opacity`, `fillOpacity`.

- [ ] **Step 1:** Cập nhật `MapComponent.tsx` gọi đúng `marker.setRadius()` trong `mouseover`, `mouseout`, và điều chỉnh ngưỡng bán kính tối thiểu lên $\ge 4\text{px}$.
- [ ] **Step 2:** Kiểm tra thao tác hover và click trên các chấm trắng khi bật Hex Overlay ở zoom $\le 13$.

---

### Task 2: Khắc phục lỗi Bôi Đen Text (`CameraModal.tsx` & `PinnedDock.tsx`)
**Files:**
- Modify: `apps/web/src/components/CameraModal.tsx`
- Modify: `apps/web/src/components/PinnedDock.tsx`

**Chi tiết:**
1. Trong `PinnedDock.tsx`:
   - Thêm `onPointerDown={(e) => e.stopPropagation()}` và `onPointerUp={(e) => e.stopPropagation()}` vào cả nút Unpin (`×`) lẫn các nút `◀` `▶`.
2. Trong `CameraModal.tsx`:
   - Thêm class `select-none` vào root container và các thành phần văn bản.
   - Thêm `useEffect` khi mount: gọi `window.getSelection()?.removeAllRanges()` để xóa sạch bất kỳ vùng chọn văn bản vô tình nào trước đó.

- [ ] **Step 1:** Chặn pointer bubbling trong `PinnedDock.tsx`.
- [ ] **Step 2:** Thêm `select-none` và xóa selection trong `CameraModal.tsx`.
- [ ] **Step 3:** Kiểm tra mở modal từ dock không còn bị bôi đen bất kỳ chữ nào.

---

### Task 3: Xây dựng Module Định Tuyến Camera Ngữ Cảnh (`useCameraSequence.ts` / utility)
**Files:**
- Create/Modify: `apps/web/src/utils/cameraSequence.ts`
- Modify: `apps/web/src/app/page.tsx`

**Chi tiết:**
1. Tạo hàm `getCameraSequence`:
   ```ts
   export interface CameraSequenceResult {
     prevCamera: Camera | null;
     nextCamera: Camera | null;
     currentIndex: number;
     totalCount: number;
     contextLabel: string;
   }
   ```
   - Nhận vào: `currentCamera`, `pinnedCameras`, `cameras`, `selectedCorridor`, `lang`.
   - Logic:
     - Nếu `currentCamera` nằm trong `pinnedCameras`: danh sách là `pinnedCameras`, nhãn là `[Ghim: {idx+1}/{total}]`.
     - Nếu có `selectedCorridor`: danh sách là các camera thuộc corridor đó.
     - Mặc định: gom nhóm các camera có cùng `extractCorridor(cam.displayName, cam.title)`. Nếu danh sách $> 1$, nhãn là `[{corridorName}: {idx+1}/{total}]`.
2. Truyền `onPrevCamera`, `onNextCamera`, `sequenceInfo` vào `CameraModal`.

- [ ] **Step 1:** Viết utility `cameraSequence.ts` và unit test.
- [ ] **Step 2:** Tích hợp tính toán sequence vào `page.tsx`.

---

### Task 4: Nâng cấp Giao diện Điều Hướng `<` `>` Trong `CameraModal.tsx`
**Files:**
- Modify: `apps/web/src/components/CameraModal.tsx`

**Chi tiết:**
1. Thêm props vào `CameraModalProps`:
   ```ts
   onPrevCamera?: () => void;
   onNextCamera?: () => void;
   hasPrevCamera?: boolean;
   hasNextCamera?: boolean;
   sequenceLabel?: string;
   ```
2. Thêm 2 nút tròn nổi `<` và `>` bên trái và bên phải ảnh snapshot:
   - Nút tròn semi-transparent, icon chevron tối giản, hiển thị rõ ràng trên nền video/ảnh.
   - Bấm `<` gọi `onPrevCamera()`, bấm `>` gọi `onNextCamera()`.
   - Ẩn hoặc disable khi không có camera trước/sau.
3. Lắng nghe phím tắt `ArrowLeft` và `ArrowRight` trong `useEffect` để chuyển nhanh bằng bàn phím.
4. Hiển thị `sequenceLabel` trên thanh tiêu đề của modal.

- [ ] **Step 1:** Thêm UI overlay `<` và `>` vào `CameraModal.tsx`.
- [ ] **Step 2:** Thêm keyboard navigation (`ArrowLeft`, `ArrowRight`).
- [ ] **Step 3:** Kiểm tra chuyển camera mượt mà, ảnh snapshot và verdict tự động cập nhật ngay lập tức theo camera mới.

---

### Task 5: Cập nhật Hành vi Nút `◀` `▶` trên Pinned Dock
**Files:**
- Modify: `apps/web/src/components/PinnedDock.tsx`

**Chi tiết:**
1. Sửa sự kiện của nút `◀` trên thẻ camera:
   - Khi bấm: gọi `onCameraClick(pinnedCameras[index - 1])` để chuyển ngay sang camera liền trước.
2. Sửa sự kiện của nút `▶` trên thẻ camera:
   - Khi bấm: gọi `onCameraClick(pinnedCameras[index + 1])` để chuyển ngay sang camera liền sau.
3. Cập nhật tooltip/title phù hợp: "Chuyển sang camera trước" / "Chuyển sang camera sau".

- [ ] **Step 1:** Sửa logic click của `◀` `▶` trong `PinnedDock.tsx`.
- [ ] **Step 2:** Kiểm tra bấm `◀` / `▶` trực tiếp trên dock chuyển đổi camera chuẩn xác.

---

### Task 6: Kiểm thử Tổng thể & Hoàn thiện
- [ ] **Step 1:** Chạy test suite `npm test` trong `apps/web`.
- [ ] **Step 2:** Chạy build `npm run build` trong `apps/web`.
- [ ] **Step 3:** Kiểm tra trực quan toàn bộ các kịch bản:
  1. Bản đồ lớn zoom 11-13, bật Vùng tổng hợp / Vùng mưa: hover và click chấm trắng, tooltip hiện bình thường.
  2. Bấm mở camera từ dock: text không bị bôi đen.
  3. Bấm `<` và `>` trong modal dock: chuyển đổi mượt mà giữa các camera đã ghim.
  4. Bấm mở camera trên đường Điện Biên Phủ từ bản đồ: bấm `<` và `>` chuyển qua lại giữa các camera dọc đường Điện Biên Phủ.
