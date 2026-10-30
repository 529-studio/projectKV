## Lesson 42: Map Overlay Coordinate Zoning & Hierarchical Collision Prevention (Desktop & Mobile)

- **Incident & Tracking:** User bug report on Desktop view (active corridor filter chip trapped behind top-right WeatherWidget and MapLegend trigger) and Mobile view (CameraFilter crashing into WeatherWidget at `top-28`, and collapsed dock pill directly occluding the bottom arterial status legend).
- **Symptom:**
  1. On desktop viewports ($\le 1200\text{px}$ or with 340px sidebar open), selecting a corridor (e.g. "Đường Cộng Hòa") caused the active filter chip to overflow horizontally into `top-3.5 right-3`. Because right-hand controls were declared later in the DOM with the same `z-[800]`, the WeatherWidget and Legend trigger sat directly on top of the filter chip, rendering the `[✕ Xóa lọc]` button unclickable and partially invisible.
  2. On mobile viewports, `CameraFilter` (`[Đang hiển thị 9 / 837 camera] [Hiện camera mất tín hiệu]`, width ~356px) crashed into `WeatherWidget` hardcoded at `top-28 right-3`, truncating the button text.
  3. At the bottom of the mobile screen, the collapsed pinned dock pill (`fixed bottom-4 left-1/2 -translate-x-1/2 z-[820]`) rendered directly over the arterial status legend (`absolute bottom-2.5 left-1/2 -translate-x-1/2 z-[700]`).
- **Root Cause:**
  1. **Lack of Visual Layering Zoning**: Placing contextual query breadcrumbs (`activeFilterLabel`) in the same horizontal flex row as permanent system toolbars (`HexOverlay` + `CameraFilter`) created an inflexible $>760\text{px}$ row that easily collided with top-right controls on standard laptop screens.
  2. **Uncoordinated Absolute Anchors**: Assigning independent absolute coordinates (`top-28 right-3`, `bottom-4` vs `bottom-2.5`) across decoupled components without a unified layout coordinator or responsive clearance logic.
  3. **Verbosity on Small Viewports**: Long button labels (`[Hiện camera mất tín hiệu]`) consumed excessive width on mobile, leaving no space for sibling controls.
- **Mandatory Takeaways & Engineering Resolutions:**
  1. **Dedicated Contextual Sub-Rows**:
     - System toolbars (Lens switcher, Camera filter) occupy Row 1.
     - Contextual query chips (active corridor/ward filters, search scope) MUST occupy their own dedicated sub-row (Row 2) beneath the toolbar, granting full horizontal expansion width across the map and zero risk of overlapping top-right controls.
  2. **Unified Mobile Header Integration**:
     - Relocate compact status widgets (like `WeatherWidget`) into the top mobile quick-search header row (`top-3 left-3 right-3`) alongside the search trigger, rather than scattering absolute elements at arbitrary `top-N` offsets across the map canvas.
  3. **Responsive Control Labeling**:
     - Implement responsive text breakpoints (`sm:hidden` vs `hidden sm:inline`) for toolbar buttons (`[Hiện camera mất tín hiệu]` -> `[Hiện offline]`, `Đang hiển thị 9 / 837 camera` -> `9 / 837 cam`), shrinking control width from 360px to 190px on mobile.
  4. **Dynamic Clearance Elevation & Framer Motion Centering**:
      - **Framer Motion Transform Isolation**: When using `left-1/2` with `-translate-x-1/2` on a `<motion.div>`, Framer Motion's `animate={{ y: 0, scale: 1 }}` generates inline `transform: translateY(0px) scale(1)`, completely stripping Tailwind's CSS class `translateX(-50%)` and pushing elements to `left: 50%` (stuck off-center). Centered Framer Motion elements MUST declare `x: '-50%'` directly within `initial`, `animate`, and `exit` props.
      - **Harmonized Bottom Clearance Row**: When the collapsed dock pill is active (`bottom-3.5`), all sibling floating controls—the `MapLegend` `(?)` trigger on the left, the arterial status legend in the center, and the search FAB on the right—must elevate together to `bottom-[4.5rem]` ($72\text{px}$). This maintains a single, aligned horizontal row with $18\text{px}$ clearance above the dock pill and zero mutual overlap.
      - **Compact Single-Line Mobile Legends**: Enforce `whitespace-nowrap` and responsive label truncation (`hidden sm:inline` for `[Xanh: ]`, `[Vàng: ]`, `[Đỏ: ]`) so legend indicators render as `● Thông thoáng ● Đông xe ● Ùn ứ` on mobile, preventing awkward word breaks into 3 tall vertical rows.
