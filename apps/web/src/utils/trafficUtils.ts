export interface TrafficVerdict {
  camera_id: string;
  severity: string;
  flood_severity?: string;
  confidence: number;
  source?: string;
  observed_at?: string;
  stale?: boolean;
  timestamp: string;
  // Physical hazard layer (Issue #401, spec §5.2). Additive and optional: a verdict
  // from a pre-migration build simply omits them, and a pre-migration row reads back
  // null, which keeps the legacy flood badge rendering (AC12/AC14).
  flood_depth_cm?: number | null;
  flood_hazard_level?: 1 | 2 | 3 | null;
  flood_hazard_basis?: string;
  flood_depth_confidence?: string;
  flood_calibration_ref?: string;
}

export const DEFAULT_VERDICT_TTL_MS = 180 * 1000;

/**
 * Validates whether a verdict is valid and within the reasonable retention window (24h).
 */
export function isVerdictValid(v?: TrafficVerdict | null): v is TrafficVerdict {
  if (!v || !v.severity) return false;
  const timeStr = v.observed_at || v.timestamp;
  if (!timeStr) return false;
  const ts = new Date(timeStr).getTime();
  if (isNaN(ts)) return false;
  const ageMs = Date.now() - ts;
  // Allow up to 1 minute of forward clock drift and retain verdicts up to 24 hours.
  return ageMs >= -60000 && ageMs <= 24 * 60 * 60 * 1000;
}

/**
 * Checks whether a verdict is stale either from server flag or age exceeding TTL.
 */
export function isVerdictStale(v?: TrafficVerdict | null, ttlMs: number = DEFAULT_VERDICT_TTL_MS): boolean {
  if (!v) return false;
  if (v.stale) return true;
  const timeStr = v.observed_at || v.timestamp;
  if (!timeStr) return false;
  const ts = new Date(timeStr).getTime();
  if (isNaN(ts)) return false;
  return Date.now() - ts > ttlMs;
}

/**
 * Trust & safety guard: fabricated mock verdicts must never surface on a
 * production/staging build. In dev mode (appEnv === 'dev'), mock verdicts are allowed
 * and will display the DEMO badge.
 */
export function isMockHidden(source?: string, appEnv?: string): boolean {
  if (source !== 'mock') return false;
  if (appEnv === 'dev') return false;
  return process.env.NODE_ENV === 'production' || appEnv !== 'dev';
}

/**
 * Normalizes raw verdict payload into a strongly typed TrafficVerdict with observed_at and stale.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function normalizeVerdict(raw: any, ttlMs: number = DEFAULT_VERDICT_TTL_MS): TrafficVerdict {
  const timeStr = raw.observed_at || raw.timestamp;
  const isStale = Boolean(raw.stale) || (Boolean(timeStr) && (Date.now() - new Date(timeStr).getTime() > ttlMs));
  // An explicit snake_case null means "never measured" and must survive the alias
  // fallback; only `undefined` defers to the camelCase spelling (AC13).
  const pick = <T,>(snake: T | undefined, camel: T | undefined): T | undefined =>
    snake !== undefined ? snake : camel;
  return {
    ...raw,
    camera_id: raw.camera_id || raw.cameraId,
    flood_severity: raw.flood_severity || raw.floodSeverity,
    observed_at: raw.observed_at || raw.timestamp,
    stale: isStale,
    flood_depth_cm: pick(raw.flood_depth_cm, raw.floodDepthCm),
    flood_hazard_level: pick(raw.flood_hazard_level, raw.floodHazardLevel),
    flood_hazard_basis: pick(raw.flood_hazard_basis, raw.floodHazardBasis),
    flood_depth_confidence: pick(raw.flood_depth_confidence, raw.floodDepthConfidence),
    flood_calibration_ref: pick(raw.flood_calibration_ref, raw.floodCalibrationRef),
  };
}

/**
 * Localizes traffic severity enum into human-readable label with correct Vietnamese diacritics.
 */
export function getSeverityLabel(severity?: string, lang: 'vi' | 'en' = 'vi'): string {
  const sev = (severity || '').toUpperCase();
  if (sev === 'CLEAR') return lang === 'vi' ? 'Thông thoáng' : 'Clear';
  if (sev === 'MODERATE') return lang === 'vi' ? 'Đông xe' : 'Moderate';
  if (sev === 'HEAVY' || sev === 'CONGESTED') return lang === 'vi' ? 'Ùn tắc' : 'Congested';
  if (sev === 'UNKNOWN') return lang === 'vi' ? 'Chưa xác định' : 'Unknown';
  return severity || '';
}
