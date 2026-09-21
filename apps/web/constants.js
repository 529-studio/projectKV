// =======================================================
// CAMERA VIEW - SYSTEM-WIDE FRONTEND CONSTANTS
// =======================================================

// Geographic zone classifications matching backend constants.go
const CAMERA_ZONES = Object.freeze({
  BA_RIA_VUNG_TAU: 'Bà Rịa - Vũng Tàu',
  BINH_DUONG: 'Bình Dương',
  CAT_LAI: 'Cụm Cảng Cát Lái',
  QUAN_7_NHA_BE: 'Quận 7 - Nhà Bè',
  THU_DUC: 'TP. Thủ Đức',
  BINH_TAN_BINH_CHANH: 'Bình Tân - Bình Chánh',
  Q12_HOC_MON_CU_CHI: 'Q.12 - Hóc Môn - Củ Chi',
  NOI_THANH: 'Khu vực Nội thành TP.HCM',
});

// Camera feed operational statuses matching backend constants.go
const CAMERA_STATUS = Object.freeze({
  ONLINE: 'UP',
  OFFLINE: 'DOWN',
});

// Backend REST and WebSocket API endpoints matching docs/GO_BACKEND_ARCHITECTURE.md §4
const API_ENDPOINTS = Object.freeze({
  CAMERAS: '/api/cameras',
  CAMERA_DETAIL: (id) => `/api/cameras/${encodeURIComponent(id)}`,
  SNAPSHOT: (id) => `/api/snapshot/${encodeURIComponent(id)}`,
  TRAFFIC_STATUS: '/api/traffic-status',
  ZONES: '/api/zones',
  WS_LIVE: '/ws/live',
});

// Traffic congestion severity levels
const TRAFFIC_SEVERITY = Object.freeze({
  CLEAR: 'CLEAR',
  MODERATE: 'MODERATE',
  HEAVY: 'HEAVY',
  CONGESTED: 'CONGESTED',
});

// Functional accent colors strictly for status indicators per AGENTS.md §1
const FUNCTIONAL_COLORS = Object.freeze({
  CLEAR: '#22c55e',      // Green: Clear / Active
  MODERATE: '#eab308',   // Yellow: Slow / Moderate
  CONGESTED: '#ef4444',  // Red: Congested / Bottleneck
  ONLINE_DEFAULT: '#ffffff',
  OFFLINE_DEFAULT: '#52525b',
  MAP_STROKE: '#000000',
});

// Map bounding box locking to Southeast mainland Vietnam per AGENTS.md §2
const MAP_CONFIG = Object.freeze({
  DEFAULT_CENTER: [10.7769, 106.7009],
  DEFAULT_ZOOM: 11,
  MIN_ZOOM: 9,
  MAX_ZOOM: 18,
  MAX_BOUNDS: [
    [10.15, 106.35],
    [11.35, 107.45],
  ],
  REFRESH_INTERVAL_SECONDS: 10,
});

// Export globally for browser scripts and Node/CommonJS test environments
if (typeof window !== 'undefined') {
  window.CAMERA_ZONES = CAMERA_ZONES;
  window.CAMERA_STATUS = CAMERA_STATUS;
  window.API_ENDPOINTS = API_ENDPOINTS;
  window.TRAFFIC_SEVERITY = TRAFFIC_SEVERITY;
  window.FUNCTIONAL_COLORS = FUNCTIONAL_COLORS;
  window.MAP_CONFIG = MAP_CONFIG;
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    CAMERA_ZONES,
    CAMERA_STATUS,
    API_ENDPOINTS,
    TRAFFIC_SEVERITY,
    FUNCTIONAL_COLORS,
    MAP_CONFIG,
  };
}
