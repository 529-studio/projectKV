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
  SSE_LIVE: '/api/live/stream',
  WS_LIVE: '/ws/live',
});

export const getApiUrl = (endpoint: string): string => {
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
});

export const MAP_CONFIG = Object.freeze({
  DEFAULT_CENTER: [10.7769, 106.7009],
  DEFAULT_ZOOM: 11,
  MIN_ZOOM: 10,
  MAX_ZOOM: 18,
  MAX_BOUNDS: [
    [10.15, 106.35],
    [11.35, 107.45],
  ],
  MAX_BOUNDS_VISCOSITY: 0.75,
  TILE_URL: 'https://tile.openstreetmap.org/{z}/{x}/{y}.png',
  TILE_ATTRIBUTION: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors',
  REFRESH_INTERVAL_SECONDS: 10,
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



