export const FLOOD_CONFIG = Object.freeze({
  MIN_ZOOM: 11,
  MAX_ZOOM: 13,
  COLOR_DEEP: '#1d4ed8',      // Deep royal blue for heavy rain
  COLOR_SHALLOW: '#0284c7',   // Vibrant ocean blue for moderate rain
  COLOR_WET: '#38bdf8',       // Light sky blue for light rain
  COLOR_DRY: '#22c55e',
  COLOR_UNKNOWN: '#71717a',
  STROKE_DEEP: '#1e40af',
  STROKE_SHALLOW: '#0369a1',
  STROKE_WET: '#38bdf8',
  STROKE_DRY: '#16a34a',
  STROKE_UNKNOWN: '#52525b',
  FILL_OPACITY_DEEP: 0.18,
  FILL_OPACITY_SHALLOW: 0.12,
  FILL_OPACITY_WET: 0.07,
  FILL_OPACITY_DRY: 0.06,
  FILL_OPACITY_UNKNOWN: 0.08,
  STROKE_WEIGHT_DEEP: 1.0,
  STROKE_WEIGHT_SHALLOW: 0.75,
  STROKE_WEIGHT_WET: 0.5,
  STROKE_WEIGHT_DRY: 0.5,
});

export const FLOOD_I18N = Object.freeze({
  LAYER_TRAFFIC: {
    vi: 'Giao thông',
    en: 'Traffic',
  },
  LAYER_TRAFFIC_SHORT: {
    vi: 'G.thông',
    en: 'Traffic',
  },
  LAYER_FLOOD: {
    vi: 'Vùng mưa',
    en: 'Rain zones',
  },
  LAYER_RADAR: {
    vi: 'Dự báo radar',
    en: 'Radar forecast',
  },
  LAYER_RADAR_SUBTITLE: {
    vi: 'Dự báo — không phải quan sát thực tế',
    en: 'Forecast — not a live observation',
  },
  LAYER_RADAR_SHORT: {
    vi: 'Radar',
    en: 'Radar',
  },
  LAYER_FLOOD_VERIFIED: {
    vi: 'Camera xác nhận ngập',
    en: 'Camera-verified flooding',
  },
  LAYER_FLOOD_VERIFIED_SUBTITLE: {
    vi: 'Xác nhận bởi camera giám sát',
    en: 'Verified by surveillance cameras',
  },
  LAYER_FLOOD_VERIFIED_SHORT: {
    vi: 'Camera',
    en: 'Camera',
  },
  TOOLTIP_TITLE: {
    vi: 'Thời tiết khu vực',
    en: 'Area Weather',
  },
  DEEP_WATER: {
    vi: 'Mưa to (nguy cơ ngập)',
    en: 'Heavy rain (flood risk)',
  },
  SHALLOW_WATER: {
    vi: 'Mưa vừa',
    en: 'Moderate rain',
  },
  WET_ROAD: {
    vi: 'Mưa nhẹ',
    en: 'Light rain',
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
    vi: 'Điểm mưa/ngập nặng nhất',
    en: 'Worst rain/flood spot',
  },
  FILTER_CHIP: {
    vi: (n: number) => `Khu vực có mưa (${n})`,
    en: (n: number) => `Rain areas (${n})`,
  },
  ACTIVE_ALERT_BADGE: {
    vi: (n: number) => `Vùng có mưa • ${n}`,
    en: (n: number) => `Rain zones • ${n}`,
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
 * Macro and corridor zoom (11–14) is visible; micro block zoom (15+) fades out to give focus to individual camera markers.
 */
export function isFloodVisibleAtZoom(zoom: number): boolean {
  return zoom <= 14;
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
 * Styling for radar forecast hex cells: dashed border and subtle fill.
 * Visually distinguishes forecast data from camera-verified observations.
 */
export function getRadarHexColorAndOpacity(severity: string): {
  fillColor: string;
  fillOpacity: number;
  strokeColor: string;
  strokeWeight: number;
  dashArray: string;
} {
  const base = getFloodHexColorAndOpacity(severity);
  return {
    fillColor: base.fillColor,
    fillOpacity: Math.max(0.08, base.fillOpacity * 0.85),
    strokeColor: base.strokeColor,
    strokeWeight: 1.0,
    dashArray: '5, 5',
  };
}

/**
 * Styling for camera-verified flood hex cells: solid prominent border and higher fill.
 * Visual precedence ensures camera observations are never hidden behind radar clouds.
 */
export function getVerifiedFloodHexColorAndOpacity(severity: string): {
  fillColor: string;
  fillOpacity: number;
  strokeColor: string;
  strokeWeight: number;
} {
  const base = getFloodHexColorAndOpacity(severity);
  return {
    fillColor: base.fillColor,
    fillOpacity: Math.min(0.35, base.fillOpacity + 0.08),
    strokeColor: base.strokeColor,
    strokeWeight: 1.75,
  };
}

/**
 * Formats radar forecast hex cell tooltip content.
 * Explicitly states: "Dự báo — không phải quan sát thực tế" / "Forecast — not a live observation".
 */
export function formatRadarHexTooltip(cell: FloodHexCell, lang: 'vi' | 'en' = 'vi'): string {
  const style = getRadarHexColorAndOpacity(cell.flood_severity);
  const severityText = getFloodSeverityLabel(cell.flood_severity, lang);
  const layerTitle = FLOOD_I18N.LAYER_RADAR[lang];
  const layerSubtitle = FLOOD_I18N.LAYER_RADAR_SUBTITLE[lang];
  const sourceName = cell.worst_camera.startsWith('open-meteo')
    ? (lang === 'vi' ? 'Dự báo Open-Meteo' : 'Open-Meteo forecast')
    : (lang === 'vi' ? 'Radar thời tiết RainViewer' : 'RainViewer weather radar');

  let advice = '';
  const s = (cell.flood_severity || '').toUpperCase().trim();
  if (s === 'FLOODED_DEEP') {
    advice = lang === 'vi' ? 'Mây đối lưu đậm, nguy cơ ngập cục bộ' : 'Heavy convective cloud, localized flood risk';
  } else if (s === 'FLOODED_SHALLOW') {
    advice = lang === 'vi' ? 'Mưa vừa diện rộng, chú ý đường trơn' : 'Moderate rain area, slick roads';
  } else if (s === 'WET') {
    advice = lang === 'vi' ? 'Mưa nhỏ rải rác / Khả năng có mưa' : 'Scattered light rain / chance of rain';
  }

  return `<div style="font-family:var(--font-mono, monospace); font-size:10.5px; line-height:1.4;">` +
    `<div style="display:flex; align-items:center; gap:6px; margin-bottom:2px;">` +
      `<span style="display:inline-block; width:8px; height:8px; border-radius:50%; background:${style.fillColor};"></span>` +
      `<span style="font-weight:700; color:#ffffff;">${layerTitle} • ${severityText}</span>` +
    `</div>` +
    `<div style="font-size:9.5px; color:#38bdf8; margin-bottom:4px; font-style:italic;">` +
      `⚠ ${layerSubtitle}` +
    `</div>` +
    (advice ? `<div style="color:#e4e4e7; margin-bottom:3px;">${advice}</div>` : '') +
    `<div style="margin-top:2px; color:#d4d4d8;">` +
      `<span style="color:#71717a;">${lang === 'vi' ? 'Nguồn dữ liệu' : 'Data source'}:</span> ` +
      `<span style="font-weight:600; color:#f4f4f5;">${sourceName}</span>` +
    `</div>` +
  `</div>`;
}

/**
 * Formats camera-verified flood hex cell tooltip content.
 * Strictly NEVER shows ward names (ADR-004 §D11 Correction 2).
 * Shows severity badge, camera count, worst flood camera name, and explicit verification subtitle.
 */
export function formatFloodHexTooltip(cell: FloodHexCell, lang: 'vi' | 'en' = 'vi'): string {
  // If a radar cell is passed, delegate to radar tooltip
  const isRadarSource = cell.worst_camera.startsWith('radar') || cell.worst_camera.startsWith('open-meteo');
  if (isRadarSource && cell.camera_count === 0) {
    return formatRadarHexTooltip(cell, lang);
  }

  const style = getVerifiedFloodHexColorAndOpacity(cell.flood_severity);
  const severityText = getFloodSeverityLabel(cell.flood_severity, lang);
  const layerTitle = FLOOD_I18N.LAYER_FLOOD_VERIFIED[lang];
  const layerSubtitle = FLOOD_I18N.LAYER_FLOOD_VERIFIED_SUBTITLE[lang];
  const countText = FLOOD_I18N.CAMERAS_MONITORING[lang](cell.camera_count);
  const worstLabel = FLOOD_I18N.WORST_FLOOD_CAMERA[lang];
  const worstName = cell.worst_camera_name || cell.worst_camera || (lang === 'vi' ? 'Không rõ' : 'Unknown');

  // Commuter practical travel guidance
  let advice = '';
  const s = (cell.flood_severity || '').toUpperCase().trim();
  if (s === 'FLOODED_DEEP') {
    advice = lang === 'vi' ? 'Camera xác nhận ngập sâu, phương tiện hạn chế qua lại' : 'Camera-verified deep flood, avoid transit';
  } else if (s === 'FLOODED_SHALLOW') {
    advice = lang === 'vi' ? 'Camera xác nhận ngập nông mép đường, giảm tốc độ' : 'Camera-verified shallow curbside flood, slow down';
  } else if (s === 'WET') {
    advice = lang === 'vi' ? 'Camera ghi nhận mặt đường ướt' : 'Camera-observed wet road surface';
  }

  return `<div style="font-family:var(--font-mono, monospace); font-size:10.5px; line-height:1.4;">` +
    `<div style="display:flex; align-items:center; gap:6px; margin-bottom:2px;">` +
      `<span style="display:inline-block; width:8px; height:8px; border-radius:50%; background:${style.fillColor};"></span>` +
      `<span style="font-weight:700; color:#ffffff;">${layerTitle} • ${severityText}</span>` +
    `</div>` +
    `<div style="font-size:9.5px; color:#4ade80; margin-bottom:4px;">` +
      `✓ ${layerSubtitle}` +
    `</div>` +
    (advice ? `<div style="color:#e4e4e7; margin-bottom:3px;">${advice}</div>` : '') +
    `<div style="color:#a1a1aa;">${countText}</div>` +
    `<div style="margin-top:2px; color:#d4d4d8;">` +
      `<span style="color:#71717a;">${worstLabel}:</span> ` +
      `<span style="font-weight:600; color:#f4f4f5;">${worstName}</span>` +
    `</div>` +
  `</div>`;
}
