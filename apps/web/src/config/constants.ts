// =======================================================
// CAMERA VIEW - SYSTEM-WIDE FRONTEND CONSTANTS
// =======================================================

export const CAMERA_ZONES = Object.freeze({
  BA_RIA_VUNG_TAU: 'Bà Rịa - Vũng Tàu',
  BINH_DUONG: 'Bình Dương',
  CAT_LAI: 'Cụm Cảng Cát Lái',
  QUAN_7_NHA_BE: 'Quận 7 - Nhà Bè',
  THU_DUC: 'TP. Thủ Đức',
  BINH_TAN_BINH_CHANH: 'Bình Tân - Bình Chánh',
  Q12_HOC_MON_CU_CHI: 'Q.12 - Hóc Môn - Củ Chi',
  NOI_THANH: 'Khu vực Nội thành TP.HCM',
});

export const CAMERA_STATUS = Object.freeze({
  ONLINE: 'UP',
  OFFLINE: 'DOWN',
});

export const API_ENDPOINTS = Object.freeze({
  CAMERAS: '/api/cameras',
  CAMERA_DETAIL: (id: string) => `/api/cameras/${encodeURIComponent(id)}`,
  SNAPSHOT: (id: string) => `/api/snapshot/${encodeURIComponent(id)}`,
  TRAFFIC_STATUS: '/api/traffic-status',
  FLOOD_STATUS: '/api/flood-status',
  FLOOD_HEX: '/api/flood-hex',
  ZONES: '/api/zones',
  PHUONG: '/api/phuong',
  ROADS: '/api/roads',
  ROAD_STATUS: (roadKey: string) => `/api/roads/${encodeURIComponent(roadKey)}/status`,
  WEATHER: '/api/weather',
  WEATHER_RADAR: '/api/weather/radar',
  FLOOD_VERIFY: '/api/flood/verify',
  REPORT_FLOOD: (id: string) => `/api/v1/cameras/${encodeURIComponent(id)}/report-flood`,
  FLOOD_USER_REPORTS: '/api/flood/user-reports',
  FLOOD_CHOKEPOINTS: '/api/flood/chokepoints',
  FLOOD_QUOTA: '/api/flood/quota',
  SSE_LIVE: '/api/live/stream',
  WS_LIVE: '/ws/live',
  ARCHIVER_DATES: '/api/archiver/dates',
  ARCHIVER_SNAPSHOTS: '/api/archiver/snapshots',
  ARCHIVER_LABELS: '/api/archiver/labels',
  ARCHIVER_RAW: '/api/archiver/snapshots/raw',
  HOTSET_STATUS: '/api/v1/cameras/hotset/status',
  FORECAST: (id: string) => `/api/v1/cameras/${encodeURIComponent(id)}/forecast`,
  SUBSCRIPTIONS: '/api/subscriptions',
  SUBSCRIPTIONS_TAXONOMY: '/api/subscriptions/taxonomy',
});

export const getApiUrl = (endpoint: string): string => {
  if (endpoint.startsWith('http://') || endpoint.startsWith('https://')) {
    return endpoint;
  }
  const base = process.env.NEXT_PUBLIC_API_URL || '';
  return `${base}${endpoint}`;
};

export const getWsUrl = (endpoint: string): string => {
  if (process.env.NEXT_PUBLIC_WS_URL) {
    return `${process.env.NEXT_PUBLIC_WS_URL}${endpoint}`;
  }
  if (typeof window !== 'undefined') {
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${proto}//${window.location.host}${endpoint}`;
  }
  return `ws://localhost:3000${endpoint}`;
};

export const TRAFFIC_SEVERITY = Object.freeze({
  CLEAR: 'CLEAR',
  MODERATE: 'MODERATE',
  HEAVY: 'HEAVY',
  CONGESTED: 'CONGESTED',
});

export const FLOOD_SEVERITY = Object.freeze({
  DRY: 'DRY',
  WET: 'WET',
  FLOODED_SHALLOW: 'FLOODED_SHALLOW',
  FLOODED_DEEP: 'FLOODED_DEEP',
});

export const HOTSET_CONFIG = Object.freeze({
  VERDICT_TTL_SECONDS: 180,
  VERDICT_TTL_MS: 180 * 1000,
});

export const FUNCTIONAL_COLORS = Object.freeze({
  CLEAR: '#22c55e',      
  MODERATE: '#eab308',   
  HEAVY: '#ef4444',      
  CONGESTED: '#ef4444',  
  ONLINE_DEFAULT: '#ffffff',
  OFFLINE_DEFAULT: '#52525b',
  MAP_STROKE: '#000000',
  AMBER_CANDIDATE: '#f59e0b',
  AMBER_ESCALATED: '#d97706',
});

const DEFAULT_MAP_TILE_URL = getApiUrl('/api/tiles/{z}/{x}/{y}.png');
// The proxy serves tiles with `Cache-Control: immutable` for 30 days, which is
// correct for pixels but wrong across a basemap theme change: a browser that has
// the light theme cached would keep showing it under the new renderer forever.
// Bump this token whenever the served cartography changes. The proxy ignores the
// query string; it exists only to give the browser a new cache key.
const MAP_TILE_THEME_VERSION = 'osm-dark-2';
const withThemeVersion = (url: string): string =>
  `${url}${url.includes('?') ? '&' : '?'}v=${MAP_TILE_THEME_VERSION}`;
const RESOLVED_MAP_TILE_URL = withThemeVersion(
  process.env.NEXT_PUBLIC_MAP_TILE_URL || DEFAULT_MAP_TILE_URL,
);

export const MAP_CONFIG = Object.freeze({
  PREFER_CANVAS: true,
  DEFAULT_CENTER: [10.7769, 106.7009],
  DEFAULT_ZOOM: 11,
  MIN_ZOOM: 10,
  MAX_ZOOM: 18,
  MAX_BOUNDS: [
    [10.15, 106.35],
    [11.35, 107.45],
  ],
  MAX_BOUNDS_VISCOSITY: 0.75,
  // The basemap is served through the Go tile proxy, never a third-party CDN the
  // browser talks to directly (Issue #443, ADR: tile licensing). The proxy's
  // upstream is MAP_TILE_UPSTREAM_URL; setting NEXT_PUBLIC_MAP_TILE_URL bypasses
  // the proxy, e.g. for a licensed CARTO Dark Matter deployment.
  TILE_URL: RESOLVED_MAP_TILE_URL,
  TILE_ATTRIBUTION:
    process.env.NEXT_PUBLIC_MAP_TILE_ATTRIBUTION ||
    '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
  // The keyless default upstream is OpenStreetMap raster cartography; the Go
  // proxy re-renders each tile as high-contrast dark cartography before caching
  // it (internal/tilecache/darken.go), so the bytes on the wire are already dark.
  // A licensed native dark provider (CARTO) is opt-in through the two
  // NEXT_PUBLIC_MAP_TILE_* variables documented on CARTO_DARK_MATTER.
  // Cameras collapse into count badges below this zoom; at and above it every
  // camera is an individual marker (Issue #443).
  CLUSTER_ZOOM_THRESHOLD: 14,
  // Grid cell width in CSS pixels at the current zoom. A cluster is therefore
  // roughly one thumb-width across, which is what makes it tappable.
  CLUSTER_GRID_PX: 64,
  REFRESH_INTERVAL_SECONDS: 10,
});

/**
 * The licensed provider template, kept here so an operator can switch in one
 * environment variable and compare the response pixel-for-pixel.
 *
 * CARTO now serves an "API KEY REQUIRED" placeholder for keyless requests on
 * every style, so this is opt-in: set MAP_TILE_UPSTREAM_URL to
 * `https://basemaps.cartocdn.com/dark_all` plus MAP_TILE_API_KEY on the backend,
 * set NEXT_PUBLIC_MAP_TILE_URL to this template (native dark, no filter) and
 * NEXT_PUBLIC_MAP_TILE_ATTRIBUTION to this attribution.
 */
export const CARTO_DARK_MATTER = Object.freeze({
  TILE_URL: 'https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png',
  SUBDOMAINS: 'abcd',
  ATTRIBUTION:
    '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors &copy; <a href="https://carto.com/attributions">CARTO</a>',
});

// =======================================================
// MARKER CLUSTERING (S14-T3 / Issue #443)
// =======================================================
// A cluster badge must answer "how many cameras" and "is any of them a Level 3
// flood" without the rider opening anything. Copy is VI (default, full
// diacritics) plus EN, zero emoji.
export const MAP_CLUSTER_I18N = Object.freeze({
  COUNT: {
    vi: (n: number) => `${n} camera giám sát`,
    en: (n: number) => `${n} ${n === 1 ? 'camera' : 'cameras'}`,
  },
  BADGE_ARIA: {
    vi: (n: number, hazard: string) => `Cụm ${n} camera. ${hazard}. Bấm để phóng to.`,
    en: (n: number, hazard: string) => `Cluster of ${n} cameras. ${hazard}. Tap to zoom in.`,
  },
  WORST_HAZARD: {
    vi: (label: string) => `Ngập nặng nhất: ${label}`,
    en: (label: string) => `Worst flood: ${label}`,
  },
  NO_HAZARD: {
    vi: 'Chưa có dữ liệu ngập',
    en: 'No flood data',
  },
  LEVEL_PREFIX: {
    vi: (level: number) => `Mức ${level}`,
    en: (level: number) => `Level ${level}`,
  },
  ZOOM_HINT: {
    vi: 'Bấm để phóng to cụm camera',
    en: 'Tap to zoom into this camera cluster',
  },
});

// =======================================================
// MAP MARKER TOOLTIP (S14-T3 / Issue #443)
// =======================================================
// The tooltip used to hardcode Vietnamese for ghost and duplicate cameras even
// in the English UI. Every rider-facing line now comes from this catalogue.
export const MAP_TOOLTIP_I18N = Object.freeze({
  CAMERA_CONFIRMED_DEEP: {
    vi: 'Camera xác nhận ngập sâu',
    en: 'Camera-verified deep flood',
  },
  CAMERA_CONFIRMED_SHALLOW: {
    vi: 'Camera xác nhận ngập nông mép đường',
    en: 'Camera-verified shallow flood',
  },
  CAMERA_OBSERVED_WET: {
    vi: 'Camera ghi nhận đường ướt do mưa',
    en: 'Camera-observed wet road',
  },
  GHOST: {
    vi: 'MẤT TÍN HIỆU THỰC ĐỊA',
    en: 'LOST LIVE SIGNAL',
  },
  DUPLICATE_SOURCE: {
    vi: 'TRÙNG NGUỒN',
    en: 'DUPLICATE SOURCE',
  },
});



export const VERDICT_I18N = Object.freeze({
  STALE_BADGE: {
    vi: 'Dữ liệu cũ',
    en: 'Stale data',
  },
  STALE_DESC: {
    vi: 'Dữ liệu cũ (>3 phút)',
    en: 'Stale (>3 min)',
  },
  DEMO_BADGE: {
    vi: 'DỮ LIỆU MẪU',
    en: 'DEMO',
  },
  TRAFFIC_PREFIX: {
    vi: 'Giao thông',
    en: 'Traffic',
  },
  SEVERITY: {
    CLEAR: {
      vi: 'Thông thoáng',
      en: 'Clear',
    },
    MODERATE: {
      vi: 'Đông xe',
      en: 'Moderate',
    },
    HEAVY: {
      vi: 'Ùn tắc',
      en: 'Congested',
    },
    CONGESTED: {
      vi: 'Ùn tắc',
      en: 'Congested',
    },
    UNKNOWN: {
      vi: 'Chưa xác định',
      en: 'Unknown',
    },
  },
});

export const HEX_CONFIG = Object.freeze({
  MIN_ZOOM: 11,
  MAX_ZOOM: 13,
  FILL_OPACITY_CLEAR: 0.18,
  FILL_OPACITY_MODERATE: 0.18,
  FILL_OPACITY_CONGESTED: 0.22,
  STROKE_COLOR: '#52525b',
  STROKE_WEIGHT: 1,
});

export const HEX_I18N = Object.freeze({
  TOGGLE: {
    vi: 'Vùng tổng hợp',
    en: 'Zone Overview',
  },
  CAMERAS_MONITORING: {
    vi: (n: number) => `${n} camera đang giám sát`,
    en: (n: number) => `${n} ${n === 1 ? 'camera' : 'cameras'} monitoring`,
  },
  WORST_CAMERA: {
    vi: 'Tình trạng nặng nhất',
    en: 'Worst status',
  },
  ZONE_CLEAR: {
    vi: 'Thông thoáng (vùng)',
    en: 'Clear (zone)',
  },
  ZONE_MODERATE: {
    vi: 'Đông xe (vùng)',
    en: 'Moderate (zone)',
  },
  ZONE_CONGESTED: {
    vi: 'Ùn tắc (vùng)',
    en: 'Congested (zone)',
  },
});

export const CORRIDOR_I18N = Object.freeze({
  COVERAGE: {
    vi: (pct: number) => `Phủ sóng: ${pct}%`,
    en: (pct: number) => `Coverage: ${pct}%`,
  },
  OLDEST: {
    vi: (age: string) => `Quan sát cũ nhất: ${age}`,
    en: (age: string) => `Oldest observation: ${age}`,
  },
  UNKNOWN: {
    vi: 'Chưa có dữ liệu',
    en: 'No data',
  },
});

export const MOBILE_SEARCH_I18N = Object.freeze({
  PLACEHOLDER: {
    vi: 'Tìm camera, tuyến đường, phường...',
    en: 'Search cameras, roads, wards...',
  },
  BADGE: {
    vi: 'Đường / Phường',
    en: 'Street / Ward',
  },
  DRAWER_TITLE: {
    vi: 'Tìm & lọc camera',
    en: 'Search & filter',
  },
  CLOSE_LABEL: {
    vi: 'Bản đồ',
    en: 'Map',
  },
  TRIGGER_LABEL: {
    vi: 'Tìm camera theo đường, phường',
    en: 'Search cameras by street, ward',
  },
});

export const PINNED_DOCK_CAP = 12;

export const PINNED_DOCK_I18N = Object.freeze({
  TITLE: {
    vi: 'DOCK THEO DÕI',
    en: 'MONITORING DOCK',
  },
  COLLAPSED_PILL: {
    vi: (count: number) => `Đang theo dõi: ${count} cam`,
    en: (count: number) => `Monitoring: ${count} ${count === 1 ? 'cam' : 'cams'}`,
  },
  PIN_COUNT_BADGE: {
    vi: (count: number, cap: number) => `Đang ghim ${count} / Tối đa ${cap} camera`,
    en: (count: number, cap: number) => `Pinned ${count} / Max ${cap} cameras`,
  },
  PIN_COUNT_BADGE_COMPACT: {
    vi: (count: number, cap: number) => `${count}/${cap} tối đa`,
    en: (count: number, cap: number) => `${count}/${cap} max`,
  },
  PIN_COUNT_TOOLTIP: {
    vi: (count: number, cap: number) => `Đang ghim ${count} trên tối đa ${cap} camera`,
    en: (count: number, cap: number) => `Pinned ${count} out of maximum ${cap} cameras`,
  },
  CLEAR_ALL: {
    vi: '[XÓA HẾT]',
    en: '[CLEAR ALL]',
  },
  COLLAPSE: {
    vi: 'Thu gọn',
    en: 'Collapse',
  },
  EXPAND: {
    vi: 'Mở rộng',
    en: 'Expand',
  },
  UNPIN: {
    vi: 'Bỏ ghim',
    en: 'Unpin',
  },
  SIZE_PRESETS: {
    S: { vi: 'Nhỏ', en: 'Small' },
    M: { vi: 'Vừa', en: 'Medium' },
    L: { vi: 'Lớn', en: 'Large' },
  },
  VIEW_MODE: {
    STRIP: { vi: 'Dải', en: 'Strip' },
    GRID: { vi: 'Lưới', en: 'Grid' },
  },
  FULL_WIDTH_GRID: {
    vi: 'Lưới toàn chiều rộng',
    en: 'Full-width grid',
  },
  PIN_CAP_REACHED: {
    vi: 'Đã đạt giới hạn ghim (tối đa 12 camera)',
    en: 'Pin limit reached (maximum 12 cameras)',
  },
  PIN_LIMIT_BADGE: {
    vi: 'ĐÃ ĐẠT GIỚI HẠN',
    en: 'PIN CAP REACHED',
  },
  SEVERITY: {
    CLEAR: {
      vi: 'Thông thoáng',
      en: 'Clear',
    },
    MODERATE: {
      vi: 'Vừa phải',
      en: 'Moderate',
    },
    HEAVY: {
      vi: 'Đông đúc',
      en: 'Heavy',
    },
    CONGESTED: {
      vi: 'Tắc nghẽn',
      en: 'Congested',
    },
  },
  NO_ANALYSIS: {
    vi: 'Chưa phân tích',
    en: 'No analysis',
  },
  ONLINE: {
    vi: 'Trực tuyến',
    en: 'Online',
  },
  OFFLINE: {
    vi: 'Mất kết nối',
    en: 'Offline',
  },
  STALE: {
    vi: 'Cũ',
    en: 'Stale',
  },
  JUST_NOW: {
    vi: 'Vừa xong',
    en: 'Just now',
  },
  SECONDS_AGO: {
    vi: (s: number) => `${s}s trước`,
    en: (s: number) => `${s}s ago`,
  },
  MOVE_LEFT: {
    vi: 'Di chuyển sang trái',
    en: 'Move left',
  },
  MOVE_RIGHT: {
    vi: 'Di chuyển sang phải',
    en: 'Move right',
  },
  DRAG_TO_REORDER: {
    vi: 'Kéo để sắp xếp lại',
    en: 'Drag to reorder',
  },
});

export const FLOOD_VERIFY_I18N = Object.freeze({
  BUTTON_LABEL: {
    vi: 'Xác minh ngập nước',
    en: 'Verify flood status',
  },
  VERIFYING: {
    vi: 'Đang xác minh VLM...',
    en: 'Verifying with VLM...',
  },
  COOLDOWN: {
    vi: (seconds: number) => `Chờ xác minh (${seconds}s)`,
    en: (seconds: number) => `Cooldown (${seconds}s)`,
  },
  BADGE_PREFIX: {
    vi: 'XÁC MINH VLM',
    en: 'VLM VERIFIED',
  },
  RATE_LIMITED: {
    vi: 'Đã đạt giới hạn yêu cầu (10 RPM / 300 RPD)',
    en: 'Rate limit reached (10 RPM / 300 RPD)',
  },
  ERROR_GENERIC: {
    vi: 'Tạm thời gián đoạn',
    en: 'Temporarily unavailable',
  },
  SEVERITY: {
    DRY: {
      vi: 'Khô ráo',
      en: 'Dry',
    },
    WET: {
      vi: 'Đường ướt',
      en: 'Wet road',
    },
    FLOODED_SHALLOW: {
      vi: 'Ngập nông',
      en: 'Shallow flood',
    },
    FLOODED_DEEP: {
      vi: 'Ngập sâu',
      en: 'Deep flood',
    },
    UNKNOWN: {
      vi: 'Không rõ',
      en: 'Unknown',
    },
  },
});

export const FLOOD_REPORT_I18N = Object.freeze({
  BUTTON_LABEL: {
    vi: 'Báo cáo ngập tại đây',
    en: 'Report flood here',
  },
  REPORTING: {
    vi: 'Đang gửi báo cáo...',
    en: 'Submitting report...',
  },
  COOLDOWN: {
    vi: (seconds: number) => `Đã báo cáo (${seconds}s)`,
    en: (seconds: number) => `Reported (${seconds}s)`,
  },
  SUCCESS_CHIP: {
    vi: 'Cảm ơn bạn đã hỗ trợ ghi nhận',
    en: 'Thank you for your report',
  },
  RATE_LIMITED: {
    vi: 'Bạn đã gửi báo cáo gần đây',
    en: 'You have submitted a report recently',
  },
  ERROR_GENERIC: {
    vi: 'Không thể gửi báo cáo',
    en: 'Unable to submit report',
  },
});

export const USER_FLOOD_REPORT_I18N = Object.freeze({
  BADGE_CANDIDATE: {
    vi: '[Báo cáo từ người dân: Có ngập]',
    en: '[Crowdsourced: Flooding reported]',
  },
  BADGE_ESCALATED: {
    vi: '[Báo cáo từ người dân: Có ngập (Nhiều nguồn xác nhận)]',
    en: '[Crowdsourced: Flooding reported (Multiple confirmations)]',
  },
  REMAINING_MINUTES: {
    vi: (m: number) => `(còn ${m} phút)`,
    en: (m: number) => `(${m}m remaining)`,
  },
  LEGEND_LABEL: {
    vi: 'Báo cáo từ người dân (Có ngập)',
    en: 'Crowdsourced reports (Flooded)',
  },
});

export const HOTSET_LEGEND_I18N = Object.freeze({
  CLEAR: {
    vi: '[Xanh: Thông thoáng]',
    en: '[Green: Clear]',
  },
  MODERATE: {
    vi: '[Vàng: Đông xe]',
    en: '[Yellow: Moderate]',
  },
  CONGESTED: {
    vi: '[Đỏ: Ùn ứ]',
    en: '[Red: Congested]',
  },
});

export const FORECAST_I18N = Object.freeze({
  HEADER: {
    TITLE: {
      vi: 'DỰ BÁO KẸT XE NGẮN HẠN',
      en: 'SHORT-HORIZON CONGESTION FORECAST',
    },
    SUBTITLE: {
      vi: 'Dự báo chỉ số bão hòa dòng xe (SI) và xu hướng ùn tắc',
      en: 'Projected saturation index (SI) and congestion trend',
    },
  },
  HORIZONS: {
    NOW: {
      vi: 'Hiện tại',
      en: 'Now',
    },
    MINUTES: {
      vi: (m: number) => `Sau ${m} phút`,
      en: (m: number) => `+${m} min`,
    },
  },
  METRICS: {
    SATURATION_INDEX: {
      vi: 'Chỉ số bão hòa (SI):',
      en: 'Saturation Index (SI):',
    },
    CONFIDENCE_BAND: {
      vi: 'Khoảng tin cậy 95%:',
      en: '95% Confidence Band:',
    },
    SPEED: {
      vi: 'Tốc độ dự kiến:',
      en: 'Projected Speed:',
    },
    CONFIDENCE: {
      vi: 'Độ tin cậy:',
      en: 'Confidence:',
    },
  },
  PROVENANCE: {
    TITLE: {
      vi: 'Thông tin mô hình:',
      en: 'Model Provenance:',
    },
    VALIDATED: {
      vi: 'ĐÃ KIỂM ĐỊNH THỰC ĐỊA',
      en: 'FIELD VALIDATED',
    },
    UNVALIDATED: {
      vi: 'CHƯA KIỂM ĐỊNH',
      en: 'UNVALIDATED',
    },
    MAE: {
      vi: 'Sai số MAE:',
      en: 'MAE Error:',
    },
  },
  AVAILABILITY: {
    INSUFFICIENT_HISTORY: {
      TITLE: {
        vi: 'CHƯA ĐỦ DỮ LIỆU LỊCH SỬ',
        en: 'INSUFFICIENT HISTORY DATA',
      },
      DESC: {
        vi: (min: number, window: number, observed: number) =>
          `Cần tối thiểu ${min} mẫu quan sát trong ${window} phút qua để kích hoạt mô hình dự báo (hiện có: ${observed} mẫu).`,
        en: (min: number, window: number, observed: number) =>
          `Requires at least ${min} observations in the last ${window} minutes to activate forecast (currently: ${observed} samples).`,
      },
    },
    MODEL_UNVALIDATED: {
      TITLE: {
        vi: 'MÔ HÌNH ĐANG KIỂM ĐỊNH',
        en: 'MODEL UNDER VALIDATION',
      },
      DESC: {
        vi: 'Mô hình dự báo đang trong giai đoạn kiểm định thực địa, tạm thời khóa số liệu dự đoán.',
        en: 'Forecast model is currently undergoing empirical validation; numeric predictions are held.',
      },
    },
    STALE: {
      TITLE: {
        vi: 'DỮ LIỆU DỰ BÁO ĐÃ CŨ',
        en: 'FORECAST DATA IS STALE',
      },
      DESC: {
        vi: (age: number) =>
          `Quan sát gần nhất cách đây ${age.toFixed(1)} phút (>15 phút). Biểu đồ được làm mờ để phản ánh độ tin cậy suy giảm.`,
        en: (age: number) =>
          `Latest observation was ${age.toFixed(1)} minutes ago (>15 min). Chart is dimmed to reflect reduced confidence.`,
      },
      BADGE: {
        vi: '[DỮ LIỆU CŨ]',
        en: '[STALE]',
      },
    },
  },
  SEVERITY: {
    CLEAR: {
      vi: 'Thông thoáng',
      en: 'Clear',
    },
    MODERATE: {
      vi: 'Đông vừa',
      en: 'Moderate',
    },
    HEAVY: {
      vi: 'Đông đúc',
      en: 'Heavy',
    },
    CONGESTED: {
      vi: 'Kẹt xe',
      en: 'Congested',
    },
    UNKNOWN: {
      vi: 'Chưa xác định',
      en: 'Unknown',
    },
  },
  STATES: {
    LOADING: {
      vi: 'Đang tải dữ liệu dự báo...',
      en: 'Loading forecast data...',
    },
    ERROR: {
      vi: 'Không thể tải dữ liệu dự báo',
      en: 'Unable to load forecast data',
    },
    RETRY: {
      vi: '↻ Thử lại',
      en: '↻ Retry',
    },
  },
});

// =======================================================
// NEAR ME (S12-T2 / Issue #400) — COMMUTER LAST-MILE I18N
// Coordinates stay in the browser: no endpoint, no storage.
// =======================================================

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

// =======================================================
// PHYSICAL HAZARD LAYER (Issue #401 / Sprint 12 Task 3)
// =======================================================
// Riders act on centimetres, not on area ratios: 0.25 across a 12 m arterial and
// 0.25 in a 3 m alley are different dangers, and neither says whether the water
// reaches the exhaust outlet. Labels are keyed by hazard level (spec §2.3) and
// every leaf ships in VI (default, full diacritics) and EN, zero emoji.
//
// LEVEL_1_ADVICE may say "you can pass" only because a depth exists.
// AVOIDANCE_ADVICE deliberately contains no number: avoidance proves the lane is
// blocked, never how deep the water is (AC10).
export const HAZARD_I18N = Object.freeze({
  LEVEL_1: { vi: 'Bấp bênh / Bắn nước', en: 'Splash zone — low risk' },
  LEVEL_2: { vi: 'Cảnh báo xe ga gầm thấp', en: 'Caution — low-clearance scooters' },
  LEVEL_3: { vi: 'Nguy cơ chết máy cao', en: 'High stall risk — do not enter' },
  LEVEL_1_ADVICE: {
    vi: 'Nước dưới 10 cm. Vẫn có thể đi qua nhưng nên giảm tốc độ và tránh bắn nước.',
    en: 'Water is under 10 cm. You can pass, but slow down and avoid splashing others.',
  },
  LEVEL_2_ADVICE: {
    vi: 'Nước ngập 10–20 cm, sát miệng ống xả. Xe ga gầm thấp nên tránh hoặc đi thật chậm.',
    en: 'Water is 10–20 cm, level with the exhaust outlet. Low-clearance scooters should avoid or go very slowly.',
  },
  LEVEL_3_ADVICE: {
    vi: 'Nước trên 20 cm, nguy cơ chết máy và hỏng động cơ. Không nên đi vào.',
    en: 'Water is over 20 cm. Engine stall and damage risk. Do not enter.',
  },
  AVOIDANCE_ADVICE: {
    vi: 'Nước chiếm phần lớn lòng đường và dòng xe đang dừng lại phía trước. Không nên đi vào.',
    en: 'Water covers most of the roadway and traffic is stopping upstream. Do not enter.',
  },
  DEPTH_UNMEASURED: { vi: 'Chưa đo được độ sâu', en: 'Depth not measurable' },
  DEPTH_VALUE: {
    vi: (cm: string) => `Độ sâu: ${cm}`,
    en: (cm: string) => `Depth: ${cm}`,
  },
  REF_MOTORCYCLE: { vi: 'đo theo chiều cao xe máy 110 cm', en: 'measured against the 110 cm motorcycle height' },
  REF_CURB: { vi: 'đo theo lề đường 15 cm', en: 'measured against the 15 cm curb' },
  REF_OVERRIDE: { vi: 'đo theo hiệu chuẩn riêng của camera', en: 'measured with this camera’s own calibration' },
  METER_ARIA: {
    vi: (cm: string) => `Độ sâu nước ${cm}, thang từ 0 đến 30 cm`,
    en: (cm: string) => `Water depth ${cm}, scale 0 to 30 cm`,
  },
  LEVEL_ARIA: {
    vi: (n: number) => `Mức nguy hiểm ${n} trên 3`,
    en: (n: number) => `Hazard level ${n} of 3`,
  },
  STALE: { vi: 'Dữ liệu cũ', en: 'Stale data' },
});

// =======================================================
// CITIZEN SUBSCRIPTIONS (S12-T4 / Issue #402)
// =======================================================
// Every leaf ships in VI (default, full diacritics) and EN, zero emoji.
// The bot's replies use the same vocabulary; see internal/telegrambot/commands.go.
export const SUBSCRIPTION_I18N = Object.freeze({
  TITLE: { vi: 'Đăng ký nhận cảnh báo', en: 'Subscribe to alerts' },
  SUBTITLE: {
    vi: 'Chọn phạm vi bạn muốn nhận cảnh báo ngập qua Telegram.',
    en: 'Choose the flood alerts you want to receive on Telegram.',
  },
  SCOPE_ALL: { vi: 'Toàn thành phố', en: 'City-wide' },
  SCOPE_ZONE: { vi: 'Khu vực', en: 'Zone' },
  SCOPE_WARD: { vi: 'Phường / Xã', en: 'Ward / Commune' },
  SCOPE_CHOKEPOINT: { vi: 'Điểm ngập', en: 'Chokepoint' },
  ZONE_LABEL: { vi: 'Khu vực vận hành', en: 'Operational zone' },
  WARD_LABEL: { vi: 'Phường / Xã', en: 'Ward / Commune' },
  CHOKEPOINT_LABEL: { vi: 'Camera / Điểm ngập', en: 'Camera / Chokepoint' },
  ZONE_PLACEHOLDER: { vi: 'Chọn khu vực', en: 'Select a zone' },
  SEARCH_WARD: { vi: 'Tìm phường hoặc xã', en: 'Search for a ward' },
  SEARCH_CHOKEPOINT: { vi: 'Tìm điểm ngập hoặc camera', en: 'Search for a chokepoint or camera' },
  TELEGRAM_CTA: { vi: 'Mở Telegram để đăng ký', en: 'Open Telegram to subscribe' },
  TELEGRAM_HINT: {
    vi: 'Bạn sẽ nhận tin nhắn xác nhận từ bot ngay sau khi bấm.',
    en: 'You will receive a confirmation message from the bot right after tapping.',
  },
  BOT_UNAVAILABLE: {
    vi: 'Bot Telegram chưa được cấu hình. Bạn vẫn có thể nhập Chat ID bên dưới.',
    en: 'The Telegram bot is not configured yet. You can still enter your chat ID below.',
  },
  MANUAL_TOGGLE: { vi: 'Nhập Chat ID thủ công', en: 'Enter chat ID manually' },
  CHAT_ID_LABEL: { vi: 'Chat ID Telegram', en: 'Telegram chat ID' },
  CHAT_ID_HINT: {
    vi: 'Chuỗi 5 đến 20 chữ số. Gửi /start cho bot để lấy Chat ID của bạn.',
    en: '5 to 20 digits. Send /start to the bot to get your chat ID.',
  },
  CHAT_ID_INVALID: { vi: 'Chat ID phải gồm 5 đến 20 chữ số.', en: 'Chat ID must contain 5 to 20 digits.' },
  SUBMIT: { vi: 'Đăng ký', en: 'Subscribe' },
  SUBMITTING: { vi: 'Đang đăng ký', en: 'Subscribing' },
  SUCCESS_TITLE: { vi: 'Đã đăng ký', en: 'Subscribed' },
  SUCCESS_BODY: {
    vi: 'Bạn sẽ nhận cảnh báo ngập cho phạm vi đã chọn. Gửi /stop để huỷ bất cứ lúc nào.',
    en: 'You will receive flood alerts for the selected scope. Send /stop to unsubscribe at any time.',
  },
  ALREADY_HINT: { vi: 'Phạm vi này đã được đăng ký trước đó.', en: 'This scope is already active.' },
  ERROR_NETWORK: {
    vi: 'Không kết nối được máy chủ. Vui lòng thử lại.',
    en: 'Could not reach the server. Please try again.',
  },
  ERROR_RATE_LIMIT: {
    vi: 'Bạn đã gửi quá nhiều yêu cầu. Vui lòng thử lại sau một phút.',
    en: 'You sent too many requests. Please try again in a minute.',
  },
  ERROR_UNKNOWN_TARGET: {
    vi: 'Phạm vi không hợp lệ. Vui lòng chọn lại.',
    en: 'Invalid scope. Please choose again.',
  },
  RETRY: { vi: 'Thử lại', en: 'Retry' },
  CLOSE: { vi: 'Đóng', en: 'Close' },
  PRIVACY_NOTE: {
    vi: 'Chúng tôi chỉ lưu Chat ID và phạm vi bạn chọn. Gửi /stop để huỷ bất cứ lúc nào.',
    en: 'We store only your chat ID and chosen scope. Send /stop to unsubscribe at any time.',
  },
  OPEN_TELEGRAM: { vi: 'Mở Telegram', en: 'Open Telegram' },
});
