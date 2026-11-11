# S12-T2 Functional Specification — Commuter Last-Mile Geolocation (GPS "Near-Me")

**Issue**: [#400](https://github.com/529-studio/projectKV/issues/400) · **Sprint**: 12 · **Branch**: `feat/400-commuter-geolocation`
**Owner**: Product Owner · **Consumers**: Lead Developer, Tester
**Depends on**: #416 (merged); existing `useCameras` / `useFlood` / `useTraffic` contracts
**Charter**: `docs/ADR-013-sprint-11-architecture.md` §D6, `docs/HANDOFF-s12.md` §6–§7

---

## 0. Estimation (AGENTS.md Rule 3)

| Phase | Scope | Active effort |
|:---|:---|:---:|
| P0 | `geoDistance.ts` pure core + tests (Haversine, radius, sort, format, bounds) | 7–9 h |
| P1 | `useGeolocation.ts` state machine + pure mappers + tests | 6–8 h |
| P2 | `NearMeModal.tsx` (4 mandatory states, list, a11y, 48 px ergonomics) | 9–11 h |
| P3 | `page.tsx` integration + `MapComponent` user marker/rings | 5–7 h |
| P4 | VI/EN catalog, live manual evidence, docs, PR | 7–9 h |
| **Total** | | **34–44 h active** |

**Delivery window**: 2026-11-12 → 2026-11-19 (5–6 focused days + review).

---

## 1. Decision on `HANDOFF-s12.md` §7 (sort metric)

**Answer: neither pure distance nor pure severity. Use 250 m distance bands, then flood severity, then traffic severity, then exact distance.**

Pure distance puts a dry camera 80 m away above a deep flood 180 m away; pure severity puts a deep flood 1.4 km away above a shallow flood 120 m away. 250 m is roughly one HCMC block / 30–40 s of riding, so severity decides what to avoid first inside the same immediate surroundings.

Comparator (pure exported function, unit-tested, locale-independent):

```ts
// 1. distanceBand asc  (0..5 = min(5, floor(d / 250)))
// 2. floodRank desc    (DEEP 4 > SHALLOW 3 > WET 2 > DRY 1 > none/stale 0)
// 3. trafficRank desc  (CONGESTED 4 > HEAVY 3 > MODERATE 2 > CLEAR 1 > none/stale 0)
// 4. distanceMeters asc
// 5. camera.id asc (code-point compare)
```

Only verdicts passing `isVerdictStale()` / `isFloodVerdictStale()` (180 s TTL) contribute rank; stale verdicts rank 0 and render a `Dữ liệu cũ / Stale data` badge. `now` is injected once per query, never read inside the comparator.

Worked example (radius 1 500 m): band 0 lists A (120 m, DRY); band 1 orders B (300 m, DEEP), D (400 m, DEEP), C (310 m, SHALLOW); band 3 places E (900 m, DEEP) fifth; band 5 places F (1 400 m, DEEP) before G (1 499 m, DRY). Severity never crosses a band boundary. `SORT_NOTE` states this.

---

## 2. Scope / Non-goals

**In scope**: 1-click GPS button (≥48 × 48 px), one-shot + watch geolocation, 1 500 m inclusive radius, distance-banded severity sorting, bottom sheet with 4 mandatory states plus data-error and out-of-area variants, user marker + rings, map pan, camera selection, VI/EN catalog, offline/stale/unknown handling, no coordinate persistence.

**Out of scope**: routing, push alerts, reverse geocoding, server-side positions, native permission UX, background follow-me, isochrone ranking, camera API changes, new packages.

---

## 3. User stories & acceptance criteria

### US-400.1 — One-click nearest cameras
As a soaked commuter I press one button and immediately see cameras around me without pinching the map.
- **AC1** Trigger visible on desktop and mobile, hit area ≥48 × 48 px, keyboard focusable, `aria-label` present.
- **AC2** One activation reuses a fix younger than 60 s or starts a single request; double taps never create a second in-flight request. The sheet opens immediately in `locating` state, not after the fix resolves.
- **AC3** On success the map pans to the fix at zoom 15 (no animation under reduced motion) and draws a user marker plus the 1 500 m ring.

### US-400.2 — Actionable proximity list
- **AC4** Only valid-coordinate cameras, excluding waterway/ghost/duplicate, with `distance ≤ 1 500 m` inclusive.
- **AC5** Ordering follows §1 and is deterministic.
- **AC6** Rows show name, road/ward, distance, flood and traffic state, plus offline/stale badges.
- **AC7** Distance is metres below 1 km (nearest 10 m), else km with one decimal; VI uses a comma, EN a point.
- **AC8** Clicking a row closes the sheet and opens the existing `CameraModal`; two modals never stack.

### US-400.3 — Graceful failure with recovery advice
- **AC9** Denied, timeout, unavailable, insecure, unsupported and invalid-position have distinct bilingual titles, bodies and numbered steps where actionable.
- **AC10** Every failure offers `Thử lại / Retry` (48 px) and `Xem toàn bộ bản đồ / View full map`; the app stays usable without GPS.
- **AC11** Denial is never retried automatically; no permission prompt on page load.

### US-400.4 — Honest about uncertainty
- **AC12** Accuracy is shown (`Sai số ±N m`); >100 m warns; >500 m advises retrying in an open area. Results stay visible so hazards are never silently hidden.
- **AC13** A fix outside `MAP_CONFIG.MAX_BOUNDS` shows the out-of-area state with `Về trung tâm TP.HCM` and never claims "no flooding nearby".
- **AC14** Zero results shows an empty state naming the radius, plus the offline count when offline cameras were hidden.

### US-400.5 — Privacy and bilingual integrity
- **AC15** Coordinates stay in the browser: no endpoint call, no web storage, no logging.
- **AC16** Every string exists in `NEAR_ME_I18N` in both languages with full VI diacritics and zero emoji/pictographs (icons are inline SVG).
- **AC17** Switching language while the sheet is open reformats all text and distances instantly, with no refetch.

---

## 4. File contracts

### 4.1 `apps/web/src/utils/geoDistance.ts` (new)

Pure, React-free. No `Date.now()` in comparators.

```ts
export const NEAR_ME_RADIUS_METERS = 1500;
export const DISTANCE_BAND_METERS = 250;
export const LOW_ACCURACY_WARN_METERS = 100;
export const LOW_ACCURACY_BAD_METERS = 500;
export const FLY_TO_ZOOM = 15;

export interface GeoPoint { latitude: number; longitude: number; }

export function haversineMeters(a: GeoPoint, b: GeoPoint): number; // R = 6371008.8, clamp asin arg
export function distanceBand(distanceMeters: number): number;      // min(5, floor(d / 250))
export function isWithinBounds(p: GeoPoint, b: [[number, number], [number, number]]): boolean;
export function isValidGeoPoint(p?: Partial<GeoPoint> | null): boolean; // finite, |lat|<=90, |lon|<=180
export function formatDistance(distanceMeters: number, lang: 'vi' | 'en'): string;
export function compareNearMeResults(a: NearMeResult, b: NearMeResult): number;

export interface NearMeQuery {
  origin: GeoPoint;
  cameras: Camera[];
  floodVerdicts?: Map<string, TrafficVerdict>;
  trafficVerdicts?: Map<string, TrafficVerdict>;
  now?: number;              // injectable clock
  hideOffline?: boolean;     // default true
  radiusMeters?: number;     // default 1500
}
export interface NearMeResult {
  camera: Camera;
  distanceMeters: number;
  distanceBand: number;
  floodSeverity: string | null;   // FLOODED_DEEP | FLOODED_SHALLOW | WET | DRY | null
  floodStale: boolean;
  trafficSeverity: string | null;
  trafficStale: boolean;
}
export function buildNearMeResults(query: NearMeQuery): NearMeResult[];
```

Filter order: drop null camera → waterway/`WATERWAY_MONITOR` → `isGhost` → `duplicateOf` → invalid/missing coordinates → when `hideOffline`, `isCameraOffline(cam)` → compute distance → keep `distance <= radius + 0.5` → sort.

`formatDistance`: `0–999 m` → nearest 10 m (`340 m`); `≥1000 m` → one decimal, `.`→`,` for VI (`1,2 km` / `1.2 km`); non-finite → `—`.

### 4.2 `apps/web/src/hooks/useGeolocation.ts` (new)

```ts
export type GeolocationStatus =
  | 'idle' | 'locating' | 'success'
  | 'denied' | 'timeout' | 'unavailable'
  | 'unsupported' | 'insecure' | 'invalid';
export interface GeolocationFix {
  latitude: number; longitude: number; accuracy: number; timestamp: number;
}
export type GeolocationErrorCode =
  | 'PERMISSION_DENIED' | 'TIMEOUT' | 'POSITION_UNAVAILABLE'
  | 'UNSUPPORTED' | 'INSECURE_CONTEXT' | 'INVALID_POSITION';
export interface UseGeolocationOptions {
  timeoutMs?: number;      // 10000
  maximumAgeMs?: number;   // 30000
  highAccuracy?: boolean;  // true
}
export interface UseGeolocationReturn {
  status: GeolocationStatus; fix: GeolocationFix | null;
  error: GeolocationErrorCode | null;
  isSupported: boolean; isWatching: boolean;
  request: () => void;   // one-shot, no-op while locating
  refresh: () => void;   // alias of request, allowed after success
  startWatch: () => void; stopWatch: () => void; // stopWatch also on unmount
}
```

- Initialize `idle` / `isSupported: false`; detect support and secure context in `useEffect` to avoid hydration mismatch. Never call geolocation during SSR.
- `request()`: `!isSecureContext` → `insecure`; missing API → `unsupported`; else `locating` + `getCurrentPosition(...)`. A ref ignores concurrent calls and clears in both callbacks and on unmount.
- Errors: 1 → `denied`/`PERMISSION_DENIED`, 2 → `unavailable`, 3 → `timeout`, unknown → `unavailable`, invalid coordinates → `invalid`/`INVALID_POSITION`. `startWatch()` mirrors the options; each fix updates `fix`/`success`, watch errors map the same and stop the watch. `stopWatch()` is idempotent.
- Export pure `mapGeolocationErrorCode(code)` and `normalizeGeolocationPosition(raw)` for tests. No coordinate logging, storage, or network transmission.

### 4.3 `apps/web/src/components/NearMeModal.tsx` (new)

```ts
export interface NearMeModalProps {
  isOpen: boolean; onClose: () => void;
  status: GeolocationStatus; fix: GeolocationFix | null; geoError: GeolocationErrorCode | null;
  results: NearMeResult[]; offlineCountInRadius: number;
  camerasLoading: boolean; camerasError: Error | null;
  hideOffline: boolean; isOutsideServiceArea: boolean; lang: 'vi' | 'en';
  onRetry: () => void; onRecenter: () => void; onToggleOffline: () => void;
  onCameraClick: (cam: Camera) => void; onViewFullMap: () => void; onBackToCity: () => void;
}
```

State precedence (first match wins, normative): (1) `!isOpen` → nothing; (2) `camerasError && results.length === 0` → data-error + retry; (3) `locating`, or `camerasLoading` with `success` → 4-row skeleton (`aria-busy`); (4) `denied | timeout | unavailable | insecure | unsupported | invalid` → matching error state + recovery steps; (5) `isOutsideServiceArea` → out-of-area + back-to-city; (6) `results.length === 0` → empty state with offline hint when `offlineCountInRadius > 0`; (7) success list.

Layout / ergonomics:

- Mobile: bottom sheet `fixed inset-x-0 bottom-0`, max-height 80 vh, drag handle, rounded top, `z-[950]`; backdrop `z-[940]` `bg-black/70`. Desktop: centered `max-w-md` dialog. `CameraModal` (`z-[9999]`) stays above.
- `zinc-950` surface, `zinc-800` borders, `zinc-100/400` text; flood accents only as a dot always paired with a text label. Every interactive element `min-h-[48px] min-w-[48px]`; rows are 64 px buttons. Header: title, `COUNT`, visible `Đóng / Close`. Footer: `SORT_NOTE` + `PRIVACY_NOTE`.
- `role="dialog" aria-modal="true" aria-labelledby`; `aria-live="polite"` announcements; sheet focused on open, Tab trapped, Esc closes, focus restored to trigger, body scroll locked.
- First 10 rows, then `SHOW_MORE` in steps of 10; header count = total. Reduced motion: no slide-in, `setView` instead of `flyTo`.

### 4.4 `apps/web/src/app/page.tsx` (edit)

- Instantiate `useGeolocation()` once; add `nearMeOpen` state.
- Trigger (48 × 48 px, inline crosshair SVG, visible text `Quanh tôi` on `md+`, icon-only with `aria-label` on mobile):
  - Desktop: in the existing map overlay control stack beside `WeatherWidget`, `bg-zinc-950/95 border-zinc-700`.
  - Mobile: floating `left-3` at `bottom-3.5` (`bottom-[4.5rem]` when the pin dock is visible) so it never overlaps the search FAB at `right-3`.
- Click: open sheet → reuse `fix` younger than 60 s, else `request()`.
- On first new fix: `map.flyTo([lat, lon], 15, { duration: 0.8 })` (or `setView` under reduced motion) exactly once; later watch fixes never move the map. Queue the fix if `map` is null and apply on `onMapReady`.
- Sheet open + permission granted → `startWatch()`; close/unmount → `stopWatch()`.
- `results = useMemo(() => buildNearMeResults({ origin: fix, cameras, floodVerdicts, trafficVerdicts: verdicts, hideOffline }), […])`; `offlineCountInRadius` = same query with `hideOffline: false` minus visible.
- `isOutsideServiceArea = fix ? !isWithinBounds(fix, MAP_CONFIG.MAX_BOUNDS) : false`.
- Row click: `setNearMeOpen(false); handleSelectCamera(cam);` — existing effect pans/zooms. Pass `nearMeLocation={fix}` and keep the marker for the session.

### 4.5 `apps/web/src/components/MapComponent.tsx` (edit)

New optional prop `nearMeLocation?: { latitude: number; longitude: number; accuracy: number } | null`, rendered in a dedicated layer group (never the layer cleared on every verdict tick): accuracy circle (`L.circle`, radius `max(accuracy, 30)`, `#71717a`, weight 1, `dashArray: '4 4'`, no fill), 1 500 m ring (`#52525b`, `dashArray: '2 6'`), user marker (`L.circleMarker`, radius 7, `#e4e4e7` stroke, `#18181b` fill); all non-interactive.

Cleanup on change/unmount; `pointer-events: none` so cameras stay clickable. No plugin, no new tile source.

---

## 5. Edge cases (normative)

| # | Condition | Detection | Behavior |
|:--:|:---|:---|:---|
| E1 | Unsupported API or insecure context | no `navigator.geolocation`, `!window.isSecureContext` | `unsupported` / `insecure` with targeted advice; API never called |
| E2 | Denied / dismissed | code 1 | `denied`, 3 numbered steps + iOS line, retry + view map, no auto-retry |
| E3 | Timeout | code 3 | `timeout`; open-sky / precise-location advice |
| E4 | Unavailable / invalid coordinates | code 2 or `!isValidGeoPoint` | `unavailable` / `invalid`; advice + retry, no pan |
| E5 | Accuracy 100–500 m | `accuracy > 100` | results shown + `LOW_ACCURACY_WARN` |
| E6 | Accuracy > 500 m | `accuracy > 500` | warning + `LOW_ACCURACY_BAD`; results still shown |
| E7 | Outside service bounds | `!isWithinBounds(fix, MAX_BOUNDS)` | out-of-area + back-to-city |
| E8 | No cameras, or only offline cameras, in 1.5 km | `results.length === 0` | empty state naming radius + view full map; offline count + include action |
| E9 | Missing coords / ghost / duplicate / waterway | filter | excluded, never rendered as 0 m |
| E10 | Stale / absent flood verdict | `isFloodVerdictStale`, map miss | rank 0; `STALE` or `UNKNOWN` badge; row still listed |
| E11 | User moves or pans with sheet open | watch fix / drag | list re-sorts; map never auto re-centers |
| E12 | Sheet closed before fix resolves | `!isOpen` | result ignored, sheet never reopens automatically |
| E13 | Rapid / double tap | `status === 'locating'` | single-flight, exactly one prompt |
| E14 | Tab backgrounded during request | timeout | timeout state on return, retry available |
| E15 | Language switch while open | prop change | all text/distances reformat, no refetch |
| E16 | Camera API slow / failed | `camerasLoading` / `camerasError` | skeleton / data-error + retry; GPS state preserved |
| E17 | 50+ matches / long names | count, CSS | first 10 + show-more; 2-line clamp, distance never truncated |
| E18 | Reduced motion | media query | no sheet slide, `setView` instead of `flyTo` |
| E19 | Rotate / resize | existing `ResizeObserver` | sheet ≤80 vh, backdrop covers viewport |
| E20 | Map not ready | `map === null` | queue fix, apply on `onMapReady`, no crash |
| E21 | CameraModal open on row click | ordering | near-me closes first, one modal visible |
| E22 | Offline preference toggled | `hideOffline` | list and badges update without refetching position |

---

## 6. Bilingual catalog — `NEAR_ME_I18N` (constants.ts)

Complete, zero emoji/pictographs, VI default with full diacritics. Function values take the same argument in both languages.

```ts
export const NEAR_ME_I18N = Object.freeze({
  BUTTON_LABEL: { vi: 'Quanh tôi', en: 'Near me' },
  BUTTON_ARIA: { vi: 'Tìm camera trong bán kính 1,5 km quanh vị trí của bạn', en: 'Find cameras within 1.5 km of your location' },
  SHEET_TITLE: { vi: 'Quanh bạn', en: 'Near you' },
  COUNT: { vi: (n: number) => `${n} camera trong bán kính 1,5 km`, en: (n: number) => `${n} ${n === 1 ? 'camera' : 'cameras'} within 1.5 km` },
  PRIVACY_NOTE: { vi: 'Vị trí chỉ được xử lý trên thiết bị của bạn và không được lưu lại.', en: 'Your location is processed only on this device and is never stored.' },
  SORT_NOTE: { vi: 'Sắp xếp theo khoảng cách (mỗi bước 250 m), sau đó theo mức ngập.', en: 'Sorted by distance (250 m bands), then flood severity.' },
  LOCATING_TITLE: { vi: 'Đang xác định vị trí', en: 'Locating you' },
  LOCATING_BODY: { vi: 'Vui lòng cho phép trình duyệt truy cập vị trí. Quá trình này thường mất vài giây.', en: 'Please allow location access in your browser. This usually takes a few seconds.' },
  ACCURACY: { vi: (m: number) => `Sai số ±${m} m`, en: (m: number) => `Accuracy ±${m} m` },
  LOW_ACCURACY_WARN: { vi: 'Sai số GPS lớn, kết quả có thể lệch so với vị trí thực tế.', en: 'GPS accuracy is low, results may be offset from your real position.' },
  LOW_ACCURACY_BAD: { vi: 'Hãy thử lại ở nơi thoáng hơn hoặc bật chế độ định vị chính xác cao.', en: 'Try again in a more open area or enable high-accuracy location.' },
  DENIED_TITLE: { vi: 'Trình duyệt đang chặn quyền vị trí', en: 'Location access is blocked' },
  DENIED_BODY: { vi: 'Warded cần vị trí của bạn để tìm camera gần nhất. Bạn có thể bật lại quyền này bất cứ lúc nào.', en: 'Warded needs your location to find the nearest cameras. You can re-enable it at any time.' },
  DENIED_STEPS: {
    vi: ['Mở biểu tượng ổ khóa cạnh thanh địa chỉ.', 'Chọn Quyền vị trí, sau đó chọn Cho phép.', 'Tải lại trang và bấm Quanh tôi.'],
    en: ['Open the lock icon next to the address bar.', 'Select Location permissions, then Allow.', 'Reload the page and press Near me.'],
  },
  DENIED_STEPS_IOS: { vi: 'Trên iPhone: Cài đặt > Safari > Vị trí > Cho phép.', en: 'On iPhone: Settings > Safari > Location > Allow.' },
  TIMEOUT_TITLE: { vi: 'Hết thời gian xác định vị trí', en: 'Location request timed out' },
  TIMEOUT_BODY: { vi: 'GPS chưa phản hồi trong 10 giây. Hãy thử lại ở nơi thoáng hơn hoặc kiểm tra dịch vụ định vị.', en: 'GPS did not respond within 10 seconds. Try again in a more open area or check location services.' },
  UNAVAILABLE_TITLE: { vi: 'Không lấy được vị trí', en: 'Location unavailable' },
  UNAVAILABLE_BODY: { vi: 'Thiết bị chưa có tín hiệu GPS hoặc dịch vụ định vị đang tắt. Hãy bật định vị rồi thử lại.', en: 'The device has no GPS signal or location services are off. Turn location on and try again.' },
  INVALID_TITLE: { vi: 'Vị trí không hợp lệ', en: 'Invalid location' },
  INVALID_BODY: { vi: 'Thiết bị trả về tọa độ không hợp lệ. Hãy thử lại hoặc dùng bản đồ để tìm camera.', en: 'The device returned invalid coordinates. Try again or use the map to find cameras.' },
  INSECURE_TITLE: { vi: 'Cần kết nối bảo mật', en: 'Secure connection required' },
  INSECURE_BODY: { vi: 'Trình duyệt chỉ cho phép định vị trên kết nối HTTPS. Hãy mở Warded bằng địa chỉ HTTPS.', en: 'Browsers only allow geolocation over HTTPS. Open Warded using an HTTPS address.' },
  UNSUPPORTED_TITLE: { vi: 'Trình duyệt không hỗ trợ định vị', en: 'Geolocation is not supported' },
  UNSUPPORTED_BODY: { vi: 'Hãy dùng Chrome, Safari hoặc Edge phiên bản mới, hoặc tìm camera trực tiếp trên bản đồ.', en: 'Use a recent version of Chrome, Safari or Edge, or find cameras directly on the map.' },
  OUTSIDE_AREA_TITLE: { vi: 'Bạn đang ở ngoài vùng phủ bản đồ', en: 'You are outside the mapped area' },
  OUTSIDE_AREA_BODY: { vi: 'Vị trí hiện tại nằm ngoài phạm vi TP.HCM và vùng lân cận, nên kết quả quanh bạn có thể không có.', en: 'Your position is outside Ho Chi Minh City and its surroundings, so nearby results may be empty.' },
  EMPTY_TITLE: { vi: 'Không có camera nào trong bán kính 1,5 km', en: 'No cameras within 1.5 km' },
  EMPTY_BODY: { vi: 'Khu vực quanh bạn chưa có camera giám sát. Bạn vẫn có thể xem toàn bộ bản đồ.', en: 'There are no monitoring cameras around you yet. You can still browse the full map.' },
  EMPTY_OFFLINE_HINT: { vi: (n: number) => `Có ${n} camera mất tín hiệu trong khu vực này.`, en: (n: number) => `${n} ${n === 1 ? 'camera is' : 'cameras are'} offline in this area.` },
  SHOW_OFFLINE: { vi: 'Hiện camera mất tín hiệu', en: 'Show offline cameras' },
  HIDE_OFFLINE: { vi: 'Ẩn camera mất tín hiệu', en: 'Hide offline cameras' },
  DATA_ERROR_TITLE: { vi: 'Không tải được danh sách camera', en: 'Could not load cameras' },
  DATA_ERROR_BODY: { vi: 'Kết nối tới máy chủ đang gián đoạn. Vị trí của bạn vẫn được giữ nguyên.', en: 'The server connection is interrupted. Your location is preserved.' },
  RETRY: { vi: 'Thử lại', en: 'Retry' },
  RECENTER: { vi: 'Về vị trí của tôi', en: 'Recenter on me' },
  CLOSE: { vi: 'Đóng', en: 'Close' },
  VIEW_FULL_MAP: { vi: 'Xem toàn bộ bản đồ', en: 'View full map' },
  BACK_TO_CITY: { vi: 'Về trung tâm TP.HCM', en: 'Back to Ho Chi Minh City center' },
  SHOW_MORE: { vi: (n: number) => `Xem thêm ${n} camera`, en: (n: number) => `Show ${n} more ${n === 1 ? 'camera' : 'cameras'}` },
  ROW_ARIA: {
    vi: (name: string, distance: string, flood: string) => `${name}, cách ${distance}, ${flood}. Mở chi tiết camera.`,
    en: (name: string, distance: string, flood: string) => `${name}, ${distance} away, ${flood}. Open camera details.`,
  },
  SEVERITY: {
    DEEP: { vi: 'Ngập sâu', en: 'Deep flood' },
    SHALLOW: { vi: 'Ngập nông', en: 'Shallow flood' },
    WET: { vi: 'Đường ướt', en: 'Wet road' },
    DRY: { vi: 'Khô ráo', en: 'Dry' },
    UNKNOWN: { vi: 'Chưa có dữ liệu', en: 'No data' },
  },
  TRAFFIC: {
    CLEAR: { vi: 'Thông thoáng', en: 'Clear' },
    MODERATE: { vi: 'Đông xe', en: 'Moderate' },
    HEAVY: { vi: 'Ùn tắc', en: 'Heavy' },
    CONGESTED: { vi: 'Tắc nghẽn', en: 'Congested' },
    UNKNOWN: { vi: 'Chưa xác định', en: 'Unknown' },
  },
  BADGE: {
    OFFLINE: { vi: 'Mất tín hiệu', en: 'Offline' },
    STALE: { vi: 'Dữ liệu cũ', en: 'Stale data' },
    NO_DATA: { vi: 'Chưa có dữ liệu', en: 'No data' },
  },
});
```

`SEVERITY`/`TRAFFIC`/`BADGE` are the only verdict labels the modal may render. Empty/error copy never promises safety ("không ngập"): absence of data is stated as absence of cameras.

---

## 7. Tester plan

### 7.1 Automated (`cd apps/web && npm test`)

`geoDistance.test.ts`: Haversine accuracy and symmetry; band boundaries (249.9/250 and 1 499.9/1 500/1 500.1); all filters; §1 ordering including stale ranking and stable tiebreak; `formatDistance` VI/EN punctuation and `—`; `isWithinBounds` inside/edge/outside; catalog leaf parity, non-empty, no `\p{Extended_Pictographic}`, VI diacritics.

`useGeolocation.test.ts` (pure helpers, no DOM): `mapGeolocationErrorCode(1|2|3|99)`; `normalizeGeolocationPosition` valid/NaN/lat 91/missing accuracy → null; support and secure-context detection with injected fakes.

Gates: `npm test`, `npx tsc --noEmit`, `npm run lint`, `npm run build`; `package.json` unchanged.

### 7.2 Manual — Rule 10 scenarios (live `make dev-all`)

| ID | Scenario / evidence |
|:--|:---|
| M1 | Desktop Chrome, allow, press `Quanh tôi` — sheet results, marker/rings, header count equals list |
| M2 | Deny permission — denied state + steps, no automatic retry |
| M3 | DevTools sensor with no/delayed fix — timeout state |
| M4 | Override to Hà Nội (21.0278, 105.8342) — out-of-area state, back-to-city works |
| M5 | Override to a camera-free area (Mekong) — empty state with radius, view-full-map closes sheet |
| M6 | Mobile 390 × 844 — bottom sheet, no FAB/dock overlap, targets measured ≥48 × 48 px |
| M7 | Click a row — sheet closes, one `CameraModal`, map at zoom 15 |
| M8 | VI ↔ EN while open — all labels/distances switch, no refetch |
| M9 | Pan map manually — no snap-back, list unchanged |
| M10 | Rapid double tap — one request, one prompt |
| M11 | Throttle/block camera API — skeleton then success; data-error + retry |
| M12 | Reduced-motion emulation — no slide/fly, still usable |
| M13 | Toggle offline cameras — list and badges update |
| M14 | Keyboard Tab/Shift+Tab/Esc — focus trapped, Esc closes, focus restored |

Evidence: `docs/reports/s12_t2_manual_evidence.json` + `.md`, screenshots in `docs/proofs/s12_t2/`, real captures only (ADR-013 D1).

### 7.3 Definition of Done

- [ ] New files match the contracts; `page.tsx` and `MapComponent.tsx` integrated; every §5 case verified live.
- [ ] Unit suite, typecheck, lint, build clean; no new dependency.
- [ ] Catalog complete, VI diacritics correct, zero emoji, 48 px minimum measured.
- [ ] No coordinate persistence or logging; Network tab shows no position payload.
- [ ] M1–M14 evidence attached; PR starts with `Close #400`, targets `dev`, follows the PR template.
