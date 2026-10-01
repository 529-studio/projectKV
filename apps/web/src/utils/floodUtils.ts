export const FLOOD_CONFIG = Object.freeze({
  MIN_ZOOM: 11,
  MAX_ZOOM: 13,
  COLOR_DEEP: '#1d4ed8',
  COLOR_SHALLOW: '#0284c7',
  COLOR_WET: '#64748b',
  COLOR_DRY: '#22c55e',
  COLOR_UNKNOWN: '#71717a',
  STROKE_DEEP: '#1e40af',
  STROKE_SHALLOW: '#0369a1',
  STROKE_WET: '#475569',
  STROKE_DRY: '#16a34a',
  STROKE_UNKNOWN: '#52525b',
  FILL_OPACITY_DEEP: 0.26,
  FILL_OPACITY_SHALLOW: 0.22,
  FILL_OPACITY_WET: 0.15,
  FILL_OPACITY_DRY: 0.10,
  FILL_OPACITY_UNKNOWN: 0.12,
  STROKE_WEIGHT_DEEP: 1.5,
  STROKE_WEIGHT_SHALLOW: 1.2,
  STROKE_WEIGHT_WET: 1.0,
  STROKE_WEIGHT_DRY: 1.0,
});

export const FLOOD_I18N = Object.freeze({
  LAYER_TRAFFIC: {
    vi: 'Giao thông',
    en: 'Traffic',
  },
  LAYER_FLOOD: {
    vi: 'Ngập nước',
    en: 'Flooding',
  },
  TOOLTIP_TITLE: {
    vi: 'Cảnh báo ngập',
    en: 'Flood Alert',
  },
  DEEP_WATER: {
    vi: 'Ngập sâu (nguy hiểm)',
    en: 'Deep water (hazardous)',
  },
  SHALLOW_WATER: {
    vi: 'Ngập nhẹ / Vũng nước',
    en: 'Shallow flood / Puddles',
  },
  WET_ROAD: {
    vi: 'Đường ướt / Mưa',
    en: 'Wet road / Rain',
  },
  DRY_ROAD: {
    vi: 'Khô ráo',
    en: 'Dry road',
  },
  UNKNOWN_STATUS: {
    vi: 'Chưa xác minh',
    en: 'Unverified / Unknown',
  },
  CAMERAS_MONITORING: {
    vi: (n: number) => `${n} camera giám sát`,
    en: (n: number) => `${n} ${n === 1 ? 'camera' : 'cameras'} monitoring`,
  },
  WORST_FLOOD_CAMERA: {
    vi: 'Điểm ngập nặng nhất',
    en: 'Worst flood spot',
  },
  FILTER_CHIP: {
    vi: (n: number) => `🌧️ Điểm ngập (${n})`,
    en: (n: number) => `🌧️ Flooded (${n})`,
  },
  ACTIVE_ALERT_BADGE: {
    vi: (n: number) => `🌧️ Vùng ngập • ${n}`,
    en: (n: number) => `🌧️ Flooded • ${n}`,
  },
});

export interface FloodHexCell {
  h3_id: string;
  resolution: number;
  flood_severity: string; // FLOODED_DEEP | FLOODED_SHALLOW | WET | DRY | UNKNOWN
  camera_count: number;
  worst_camera: string;
  worst_camera_name: string;
  polygon: [number, number][];
}

/**
 * Normalizes and validates raw SSE flood hex payload into a typed FloodHexCell object.
 * Returns null if the payload is malformed or lacks valid polygon coordinates.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function normalizeFloodHexCell(raw: any): FloodHexCell | null {
  if (!raw || typeof raw !== 'object') return null;

  const h3_id = typeof raw.h3_id === 'string' ? raw.h3_id : typeof raw.h3Id === 'string' ? raw.h3Id : '';
  if (!h3_id) return null;

  const rawPoly = Array.isArray(raw.polygon) ? raw.polygon : [];
  if (rawPoly.length < 3) return null;

  const polygon: [number, number][] = [];
  for (const pt of rawPoly) {
    if (Array.isArray(pt) && pt.length >= 2 && typeof pt[0] === 'number' && typeof pt[1] === 'number') {
      polygon.push([pt[0], pt[1]]);
    } else {
      return null;
    }
  }

  const resolution = typeof raw.resolution === 'number' ? raw.resolution : 7;
  const flood_severity = typeof raw.flood_severity === 'string'
    ? raw.flood_severity.toUpperCase().trim()
    : typeof raw.floodSeverity === 'string'
      ? raw.floodSeverity.toUpperCase().trim()
      : 'UNKNOWN';

  const camera_count = typeof raw.camera_count === 'number'
    ? raw.camera_count
    : typeof raw.cameraCount === 'number'
      ? raw.cameraCount
      : 0;

  const worst_camera = typeof raw.worst_camera === 'string' ? raw.worst_camera : '';
  const worst_camera_name = typeof raw.worst_camera_name === 'string'
    ? raw.worst_camera_name
    : typeof raw.worstCameraName === 'string'
      ? raw.worstCameraName
      : worst_camera;

  return {
    h3_id,
    resolution,
    flood_severity,
    camera_count,
    worst_camera,
    worst_camera_name,
    polygon,
  };
}

/**
 * Maps flood severity to functional colors, borders, and opacities per Issue #106 / ADR-004 §D11.
 * Honesty rule: NEVER returns "OK to go" or safe labels.
 */
export function getFloodHexColorAndOpacity(severity: string): {
  fillColor: string;
  fillOpacity: number;
  strokeColor: string;
  strokeWeight: number;
} {
  const s = (severity || '').toUpperCase().trim();
  switch (s) {
    case 'FLOODED_DEEP':
      return {
        fillColor: FLOOD_CONFIG.COLOR_DEEP,
        fillOpacity: FLOOD_CONFIG.FILL_OPACITY_DEEP,
        strokeColor: FLOOD_CONFIG.STROKE_DEEP,
        strokeWeight: FLOOD_CONFIG.STROKE_WEIGHT_DEEP,
      };
    case 'FLOODED_SHALLOW':
      return {
        fillColor: FLOOD_CONFIG.COLOR_SHALLOW,
        fillOpacity: FLOOD_CONFIG.FILL_OPACITY_SHALLOW,
        strokeColor: FLOOD_CONFIG.STROKE_SHALLOW,
        strokeWeight: FLOOD_CONFIG.STROKE_WEIGHT_SHALLOW,
      };
    case 'WET':
      return {
        fillColor: FLOOD_CONFIG.COLOR_WET,
        fillOpacity: FLOOD_CONFIG.FILL_OPACITY_WET,
        strokeColor: FLOOD_CONFIG.STROKE_WET,
        strokeWeight: FLOOD_CONFIG.STROKE_WEIGHT_WET,
      };
    case 'DRY':
      return {
        fillColor: FLOOD_CONFIG.COLOR_DRY,
        fillOpacity: FLOOD_CONFIG.FILL_OPACITY_DRY,
        strokeColor: FLOOD_CONFIG.STROKE_DRY,
        strokeWeight: FLOOD_CONFIG.STROKE_WEIGHT_DRY,
      };
    default:
      return {
        fillColor: FLOOD_CONFIG.COLOR_UNKNOWN,
        fillOpacity: FLOOD_CONFIG.FILL_OPACITY_UNKNOWN,
        strokeColor: FLOOD_CONFIG.STROKE_UNKNOWN,
        strokeWeight: 1.0,
      };
  }
}

/**
 * Returns whether flood hex cells should be visible at the given zoom level.
 * Macro zoom (11–13) is visible; micro zoom (14+) fades out to give focus to individual camera markers.
 */
export function isFloodVisibleAtZoom(zoom: number): boolean {
  return zoom <= 13;
}

/**
 * Returns whether pointer events and interactivity should be active for flood hex cells.
 * At macro zoom (<= 13): cells are visible and interactive.
 * At micro zoom (>= 14): cells are hidden, pointer-events are disabled ('none') so camera markers are never blocked.
 */
export function getFloodInteractivityState(zoom: number): {
  isVisible: boolean;
  pointerEvents: 'auto' | 'none';
  className: string;
} {
  const isVisible = isFloodVisibleAtZoom(zoom);
  return {
    isVisible,
    pointerEvents: isVisible ? 'auto' : 'none',
    className: `hex-cell-polygon${isVisible ? '' : ' hex-cell-hidden'}`,
  };
}

/**
 * Returns localized label for flood severity.
 * Honesty rule: Never says "OK to go", unverified or unknown shown as "Chưa xác minh" / "Unverified / Unknown".
 */
export function getFloodSeverityLabel(severity: string, lang: 'vi' | 'en' = 'vi'): string {
  const s = (severity || '').toUpperCase().trim();
  switch (s) {
    case 'FLOODED_DEEP':
      return FLOOD_I18N.DEEP_WATER[lang];
    case 'FLOODED_SHALLOW':
      return FLOOD_I18N.SHALLOW_WATER[lang];
    case 'WET':
      return FLOOD_I18N.WET_ROAD[lang];
    case 'DRY':
      return FLOOD_I18N.DRY_ROAD[lang];
    default:
      return FLOOD_I18N.UNKNOWN_STATUS[lang];
  }
}

/**
 * Formats flood hex cell tooltip content.
 * Strictly NEVER shows ward names (ADR-004 §D11 Correction 2).
 * Shows severity badge, camera count, and worst flood camera name.
 */
export function formatFloodHexTooltip(cell: FloodHexCell, lang: 'vi' | 'en' = 'vi'): string {
  const style = getFloodHexColorAndOpacity(cell.flood_severity);
  const severityText = getFloodSeverityLabel(cell.flood_severity, lang);
  const countText = FLOOD_I18N.CAMERAS_MONITORING[lang](cell.camera_count);
  const worstLabel = FLOOD_I18N.WORST_FLOOD_CAMERA[lang];
  const worstName = cell.worst_camera_name || cell.worst_camera || (lang === 'vi' ? 'Không rõ' : 'Unknown');

  return `<div style="font-family:var(--font-mono, monospace); font-size:10.5px; line-height:1.4;">` +
    `<div style="display:flex; align-items:center; gap:6px; margin-bottom:4px;">` +
      `<span style="display:inline-block; width:8px; height:8px; border-radius:50%; background:${style.fillColor};"></span>` +
      `<span style="font-weight:700; color:#ffffff;">${severityText}</span>` +
    `</div>` +
    `<div style="color:#d4d4d8;">${countText}</div>` +
    `<div style="margin-top:2px; color:#d4d4d8;">` +
      `<span style="color:#a1a1aa;">${worstLabel}:</span> ` +
      `<span style="font-weight:600; color:#f4f4f5;">${worstName}</span>` +
    `</div>` +
  `</div>`;
}
