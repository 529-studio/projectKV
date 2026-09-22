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
  ZONES: '/api/zones',
  WS_LIVE: '/ws/live',
});

/**
 * Resolves the full URL for an API endpoint.
 * - If NEXT_PUBLIC_API_BASE_URL is set, prefixes with it.
 * - If running in local development on port 3001, defaults to http://localhost:3000.
 * - Otherwise (production/staging sharing origin with Go server), uses relative path.
 */
export const getApiUrl = (endpoint: string): string => {
  if (process.env.NEXT_PUBLIC_API_BASE_URL !== undefined && process.env.NEXT_PUBLIC_API_BASE_URL !== '') {
    let base = process.env.NEXT_PUBLIC_API_BASE_URL.replace(/\/$/, '');
    if (base.endsWith('/api') && endpoint.startsWith('/api')) {
      base = base.slice(0, -4);
    }
    return `${base}${endpoint}`;
  }
  if (typeof window !== 'undefined' && window.location.port === '3001') {
    return `http://localhost:3000${endpoint}`;
  }
  return endpoint;
};

/**
 * Resolves the full WebSocket URL.
 * - If NEXT_PUBLIC_WS_URL is set, uses it.
 * - If running in local development on port 3001, defaults to ws://localhost:3000/ws/live.
 * - Otherwise, dynamically derives wss:// (or ws://) from current window.location.
 */
export const getWebSocketUrl = (endpoint: string = API_ENDPOINTS.WS_LIVE): string => {
  if (process.env.NEXT_PUBLIC_WS_URL !== undefined && process.env.NEXT_PUBLIC_WS_URL !== '') {
    const base = process.env.NEXT_PUBLIC_WS_URL.replace(/\/$/, '');
    return `${base}${endpoint}`;
  }
  if (typeof window !== 'undefined') {
    if (window.location.port === '3001') {
      return `ws://localhost:3000${endpoint}`;
    }
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    return `${protocol}//${window.location.host}${endpoint}`;
  }
  return `ws://localhost:3000${endpoint}`;
};

export const TRAFFIC_SEVERITY = Object.freeze({
  CLEAR: 'CLEAR',
  MODERATE: 'MODERATE',
  HEAVY: 'HEAVY',
  CONGESTED: 'CONGESTED',
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
  MIN_ZOOM: 9,
  MAX_ZOOM: 18,
  MAX_BOUNDS: [
    [10.15, 106.35],
    [11.35, 107.45],
  ],
  REFRESH_INTERVAL_SECONDS: 10,
});
