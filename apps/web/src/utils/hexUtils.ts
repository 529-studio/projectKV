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

export interface HexCell {
  h3_id: string;
  resolution: number;
  severity: string;
  camera_count: number;
  worst_camera: string;
  worst_camera_name: string;
  polygon: [number, number][];
}

/**
 * Normalizes and validates raw SSE hex payload into a typed HexCell object.
 * Returns null if the payload is malformed or lacks polygon coordinates.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function normalizeHexCell(raw: any): HexCell | null {
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
  const severity = typeof raw.severity === 'string' ? raw.severity.toUpperCase().trim() : 'CLEAR';
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
    severity,
    camera_count,
    worst_camera,
    worst_camera_name,
    polygon,
  };
}

/**
 * Maps hex cell severity to approved functional colors and low-opacity fills per ADR-004 §D11.
 * - Green/Yellow: 0.18 fill opacity
 * - Red: 0.22 fill opacity
 */
export function getHexColorAndOpacity(severity: string): { fillColor: string; fillOpacity: number } {
  const s = severity.toUpperCase().trim();
  switch (s) {
    case 'CLEAR':
    case 'DRY':
      return { fillColor: '#22c55e', fillOpacity: 0.18 };
    case 'MODERATE':
    case 'WET':
      return { fillColor: '#eab308', fillOpacity: 0.18 };
    case 'HEAVY':
    case 'CONGESTED':
    case 'FLOODED_SHALLOW':
    case 'FLOODED_DEEP':
      return { fillColor: '#ef4444', fillOpacity: 0.22 };
    default:
      return { fillColor: '#71717a', fillOpacity: 0.15 };
  }
}

/**
 * Returns whether hex cells should be visible at the given zoom level.
 * Macro and corridor zoom (11–14) is visible; micro block zoom (15+) fades out to give focus to street level.
 */
export function isHexVisibleAtZoom(zoom: number): boolean {
  return zoom <= 14;
}

/**
 * Returns whether pointer events and interactivity should be active for hex cells.
 * At macro zoom (<= 13): cells are visible and interactive.
 * At micro zoom (>= 14): cells are hidden, pointer-events are disabled ('none') so camera markers are never blocked.
 */
export function getHexInteractivityState(zoom: number): {
  isVisible: boolean;
  pointerEvents: 'auto' | 'none';
  className: string;
} {
  const isVisible = isHexVisibleAtZoom(zoom);
  return {
    isVisible,
    pointerEvents: isVisible ? 'auto' : 'none',
    className: `hex-cell-polygon${isVisible ? '' : ' hex-cell-hidden'}`,
  };
}

/**
 * Formats hex cell tooltip content.
 * Requirement: Shows camera count and worst camera name.
 * Strictly NEVER shows a ward name (ADR-004 §D11 Correction 2).
 */
export function formatHexTooltip(cell: HexCell, lang: 'vi' | 'en' = 'vi'): string {
  const countText = HEX_I18N.CAMERAS_MONITORING[lang](cell.camera_count);
  const worstLabel = HEX_I18N.WORST_CAMERA[lang];
  const worstName = cell.worst_camera_name || cell.worst_camera || (lang === 'vi' ? 'Không rõ' : 'Unknown');

  return `<div style="font-family:var(--font-mono, monospace); font-size:10.5px; line-height:1.4;">` +
    `<div style="font-weight:700; color:#ffffff;">${countText}</div>` +
    `<div style="margin-top:2px; color:#d4d4d8;">` +
      `<span style="color:#a1a1aa;">${worstLabel}:</span> ` +
      `<span style="font-weight:600; color:#f4f4f5;">${worstName}</span>` +
    `</div>` +
  `</div>`;
}
