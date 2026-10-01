export interface CameraVerdict {
  camera_id: string;
  name?: string;
  severity: string;
  flood_severity?: string;
  confidence?: number;
  source?: string;
  observed_at?: string;
  stale: boolean;
}

export interface CorridorStatus {
  road_key: string;
  worst_severity: string; // "CLEAR" | "MODERATE" | "HEAVY" | "CONGESTED" | "UNKNOWN"
  coverage_pct: number;
  oldest_observation_age_s: number | null;
  total_cameras: number;
  verified_cameras: number;
  cameras: CameraVerdict[];
}

export const CORRIDOR_CACHE_TTL_MS = 30_000;
const corridorCache = new Map<string, { data: CorridorStatus; timestamp: number }>();

export const getCorridorStatusUrl = (roadKey: string): string => {
  const base = (typeof process !== 'undefined' && process.env?.NEXT_PUBLIC_API_URL) || '';
  return `${base}/api/roads/${encodeURIComponent(roadKey)}/status`;
};

export function getCachedCorridorStatus(roadKey: string): CorridorStatus | null {
  const entry = corridorCache.get(roadKey.toLowerCase());
  if (!entry) return null;
  if (Date.now() - entry.timestamp > CORRIDOR_CACHE_TTL_MS) {
    corridorCache.delete(roadKey.toLowerCase());
    return null;
  }
  return entry.data;
}

export function setCachedCorridorStatus(roadKey: string, data: CorridorStatus): void {
  corridorCache.set(roadKey.toLowerCase(), { data, timestamp: Date.now() });
}

export function clearCorridorCache(): void {
  corridorCache.clear();
}

export interface VerdictLike {
  severity?: string;
  stale?: boolean;
}

export interface CameraLike {
  id: string;
  road_key?: string | null;
}

/**
 * Returns the worst non-stale verdict among cameras on a corridor.
 * Returns null if no camera has a fresh, valid verdict.
 */
export function getCorridorWorstVerdict(
  cameras: CameraLike[],
  verdicts?: Map<string, VerdictLike>
): 'CONGESTED' | 'MODERATE' | 'CLEAR' | null {
  if (!verdicts || verdicts.size === 0 || !cameras || cameras.length === 0) {
    return null;
  }

  let hasClear = false;
  let hasModerate = false;
  let hasCongested = false;

  for (const cam of cameras) {
    const v = verdicts.get(cam.id);
    if (!v || v.stale || !v.severity) continue;
    const s = v.severity.toUpperCase().trim();
    if (s === 'CONGESTED' || s === 'HEAVY') {
      hasCongested = true;
      break; // Highest severity
    } else if (s === 'MODERATE') {
      hasModerate = true;
    } else if (s === 'CLEAR') {
      hasClear = true;
    }
  }

  if (hasCongested) return 'CONGESTED';
  if (hasModerate) return 'MODERATE';
  if (hasClear) return 'CLEAR';
  return null;
}

/**
 * Formats oldest observation age into human-readable string.
 */
export function formatObservationAge(ageSeconds: number | null | undefined, lang: 'vi' | 'en' = 'vi'): string | null {
  if (ageSeconds === null || ageSeconds === undefined) return null;
  if (ageSeconds < 60) {
    return lang === 'vi' ? `${ageSeconds}s trước` : `${ageSeconds}s ago`;
  }
  const mins = Math.floor(ageSeconds / 60);
  const secs = ageSeconds % 60;
  if (secs === 0) {
    return lang === 'vi' ? `${mins} phút trước` : `${mins}m ago`;
  }
  return lang === 'vi' ? `${mins}p ${secs}s trước` : `${mins}m ${secs}s ago`;
}

/**
 * Fetches corridor status on demand with in-memory 30s cache.
 */
export async function fetchCorridorStatus(roadKey: string): Promise<CorridorStatus | null> {
  const key = roadKey.trim().toLowerCase();
  if (!key) return null;

  const cached = getCachedCorridorStatus(key);
  if (cached) return cached;

  try {
    const res = await fetch(getCorridorStatusUrl(key));
    if (!res.ok) {
      if (res.status === 404) return null;
      throw new Error(`Failed to fetch corridor status: ${res.status}`);
    }
    const data: CorridorStatus = await res.json();
    setCachedCorridorStatus(key, data);
    return data;
  } catch (err) {
    console.warn(`[corridorUtils] Failed to fetch status for road ${key}:`, err);
    return null;
  }
}
