export interface HotsetCameraStatus {
  camera_id: string;
  severity: string;
  saturation_index: number;
  pcu_total: number;
  confidence: number;
  source: string;
  observed_at: string;
  stale: boolean;
}

export interface HotsetStatusResponse {
  total: number;
  updated_at: string;
  cameras: HotsetCameraStatus[];
}

/**
 * Arterial Hot Set Saturation Colors (Issue #251 / S7-T6)
 * - SI < 0.30: Emerald green (#10b981)
 * - 0.30 <= SI < 0.60: Amber yellow (#f59e0b)
 * - SI >= 0.60: Rose red (#ef4444)
 */
export const SATURATION_COLORS = Object.freeze({
  CLEAR: '#10b981',
  MODERATE: '#f59e0b',
  CONGESTED: '#ef4444',
});

export const DEFAULT_MARKER_COLORS = Object.freeze({
  ONLINE: '#ffffff',
  OFFLINE: '#52525b',
});

/**
 * Maps Saturation Index (SI) to exact design color.
 */
export function getSaturationColor(si: number): string {
  if (si < 0.30) {
    return SATURATION_COLORS.CLEAR;
  }
  if (si < 0.60) {
    return SATURATION_COLORS.MODERATE;
  }
  return SATURATION_COLORS.CONGESTED;
}

/**
 * Checks whether saturation index indicates severe congestion (SI >= 0.60).
 */
export function isHotsetCongested(si: number): boolean {
  return si >= 0.60;
}

export interface HotsetMarkerStyle {
  isHotset: boolean;
  fillColor: string;
  isCongested: boolean;
  isStale: boolean;
}

/**
 * Resolves marker style for a camera given optional Hot Set status and online state.
 */
export function getHotsetMarkerStyle(
  status?: HotsetCameraStatus | null,
  isOnline: boolean = true
): HotsetMarkerStyle {
  if (!status) {
    return {
      isHotset: false,
      fillColor: isOnline ? DEFAULT_MARKER_COLORS.ONLINE : DEFAULT_MARKER_COLORS.OFFLINE,
      isCongested: false,
      isStale: false,
    };
  }

  const isStale = Boolean(status.stale);
  const fillColor = getSaturationColor(status.saturation_index ?? 0);
  // Stale markers suppress pulsing animation for trust & safety
  const isCongested = !isStale && isHotsetCongested(status.saturation_index ?? 0);

  return {
    isHotset: true,
    fillColor,
    isCongested,
    isStale,
  };
}

/**
 * Formats Hot Set arterial saturation and PCU details for map tooltips without emojis.
 */
export function buildHotsetTooltipSection(
  status: HotsetCameraStatus,
  lang: 'vi' | 'en' = 'vi'
): string {
  const pct = Math.round((status.saturation_index ?? 0) * 100);
  const pcu = Math.round(status.pcu_total ?? 0);

  if (lang === 'vi') {
    return `<br><span style="color:#e4e4e7; font-family:monospace; font-size:10px;">Độ bão hòa (SI): <b>${pct}%</b> • PCU: <b>${pcu}</b></span>`;
  }
  return `<br><span style="color:#e4e4e7; font-family:monospace; font-size:10px;">Saturation (SI): <b>${pct}%</b> • PCU: <b>${pcu}</b></span>`;
}

/**
 * Converts raw HotsetStatusResponse into a Map keyed by camera_id.
 */
export function parseHotsetStatusMap(
  data?: HotsetStatusResponse | null
): Map<string, HotsetCameraStatus> {
  const map = new Map<string, HotsetCameraStatus>();
  if (!data || !Array.isArray(data.cameras)) {
    return map;
  }

  data.cameras.forEach((cam) => {
    if (cam && cam.camera_id) {
      map.set(cam.camera_id, cam);
    }
  });

  return map;
}

/**
 * Pure fetcher function for hotset status endpoint.
 * Isolated from React state for testing and headless execution.
 */
export async function fetchHotsetStatus(
  url: string,
  customFetch: typeof fetch = fetch,
  signal?: AbortSignal
): Promise<{
  hotsetStatus: Map<string, HotsetCameraStatus>;
  total: number;
  updatedAt: string | null;
  error: string | null;
}> {
  try {
    const res = await customFetch(url, {
      signal,
    });

    if (!res.ok) {
      return {
        hotsetStatus: new Map(),
        total: 0,
        updatedAt: null,
        error: `HTTP error ${res.status}`,
      };
    }

    const data: HotsetStatusResponse = await res.json();
    const map = parseHotsetStatusMap(data);

    return {
      hotsetStatus: map,
      total: data?.total ?? map.size,
      updatedAt: data?.updated_at ?? null,
      error: null,
    };
  } catch (err: unknown) {
    if (err instanceof Error && err.name === 'AbortError') {
      return {
        hotsetStatus: new Map(),
        total: 0,
        updatedAt: null,
        error: null,
      };
    }
    const message = err instanceof Error ? err.message : 'Unknown error';
    return {
      hotsetStatus: new Map(),
      total: 0,
      updatedAt: null,
      error: message,
    };
  }
}
