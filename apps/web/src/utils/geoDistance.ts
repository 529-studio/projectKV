/**
 * S12-T2 / Issue #400 — pure geospatial core for the "Near me" commuter list.
 *
 * Contract: `docs/sprints/s12_t2_spec.md` §1, §4.1, §5. React-free, and no
 * clock read inside comparators: `buildNearMeResults` resolves staleness once
 * per query from the injected `now`, then sorts with a pure comparator.
 */
import type { Camera } from '../hooks/useCameras';
import type { TrafficVerdict } from './trafficUtils';
import { isCameraOffline, isWaterwayCamera } from './cameraFilterUtils.ts';

/** Inclusive search radius around the user fix (metres). */
export const NEAR_ME_RADIUS_METERS = 1500;
/** Distance band width: 250 m is roughly one HCMC block / 30–40 s of riding. */
export const DISTANCE_BAND_METERS = 250;
/** Accuracy above this warns the user that results may be offset. */
export const LOW_ACCURACY_WARN_METERS = 100;
/** Accuracy above this advises retrying in an open area. */
export const LOW_ACCURACY_BAD_METERS = 500;
/** Zoom applied when the map flies to the first fix. */
export const FLY_TO_ZOOM = 15;
/** Verdict TTL shared with the live Hot Set pipeline (180 s). */
export const NEAR_ME_VERDICT_TTL_MS = 180 * 1000;

const EARTH_RADIUS_METERS = 6371008.8;

const FLOOD_RANK: Record<string, number> = {
  FLOODED_DEEP: 4,
  FLOODED_SHALLOW: 3,
  WET: 2,
  DRY: 1,
};

const TRAFFIC_RANK: Record<string, number> = {
  CONGESTED: 4,
  HEAVY: 3,
  MODERATE: 2,
  CLEAR: 1,
};

export interface GeoPoint {
  latitude: number;
  longitude: number;
}


export interface NearMeQuery {
  origin: GeoPoint;
  cameras: Camera[];
  floodVerdicts?: Map<string, TrafficVerdict>;
  trafficVerdicts?: Map<string, TrafficVerdict>;
  /** Injectable clock, resolved once per query. Defaults to `Date.now()`. */
  now?: number;
  /** Default true: offline cameras are hidden unless the user opts in. */
  hideOffline?: boolean;
  /** Default {@link NEAR_ME_RADIUS_METERS}. */
  radiusMeters?: number;
}

export interface NearMeResult {
  camera: Camera;
  distanceMeters: number;
  distanceBand: number;
  floodSeverity: string | null;
  floodStale: boolean;
  trafficSeverity: string | null;
  trafficStale: boolean;
}

function toRadians(degrees: number): number {
  return (degrees * Math.PI) / 180;
}

/**
 * Great-circle distance in metres (Haversine, R = 6 371 008.8 m). The `asin`
 * argument is clamped so antipodal input yields πR instead of NaN.
 */
export function haversineMeters(a: GeoPoint, b: GeoPoint): number {
  const lat1 = toRadians(a.latitude);
  const lat2 = toRadians(b.latitude);
  const deltaLat = lat2 - lat1;
  const deltaLon = toRadians(b.longitude - a.longitude);

  const sinLat = Math.sin(deltaLat / 2);
  const sinLon = Math.sin(deltaLon / 2);
  const h = sinLat * sinLat + Math.cos(lat1) * Math.cos(lat2) * sinLon * sinLon;
  const clamped = Math.min(1, Math.max(0, h));

  return 2 * EARTH_RADIUS_METERS * Math.asin(Math.sqrt(clamped));
}

/** `min(5, floor(d / 250))`; non-finite or negative input collapses to band 0. */
export function distanceBand(distanceMeters: number): number {
  if (!Number.isFinite(distanceMeters) || distanceMeters <= 0) return 0;
  return Math.min(5, Math.floor(distanceMeters / DISTANCE_BAND_METERS));
}

/**
 * Inclusive latitude/longitude box test. `bounds` follows Leaflet's
 * `[[south, west], [north, east]]` literal used by `MAP_CONFIG.MAX_BOUNDS`.
 */
export function isWithinBounds(
  p: GeoPoint,
  b: [[number, number], [number, number]],
): boolean {
  if (!p || !b || b.length !== 2) return false;
  const [first, second] = b;
  if (!first || !second) return false;

  const south = Math.min(first[0], second[0]);
  const north = Math.max(first[0], second[0]);
  const west = Math.min(first[1], second[1]);
  const east = Math.max(first[1], second[1]);

  const { latitude, longitude } = p;
  if (!Number.isFinite(latitude) || !Number.isFinite(longitude)) return false;

  return latitude >= south && latitude <= north && longitude >= west && longitude <= east;
}

/** Finite coordinates inside legal WGS84 ranges: |lat| <= 90, |lon| <= 180. */
export function isValidGeoPoint(p?: Partial<GeoPoint> | null): boolean {
  if (!p) return false;
  const { latitude, longitude } = p;
  if (typeof latitude !== 'number' || typeof longitude !== 'number') return false;
  if (!Number.isFinite(latitude) || !Number.isFinite(longitude)) return false;
  return Math.abs(latitude) <= 90 && Math.abs(longitude) <= 180;
}

/**
 * `0–999 m` → nearest 10 m (`340 m`); `>= 1000 m` → one decimal, `.` for EN
 * and `,` for VI (`1.2 km` / `1,2 km`); non-finite → `—`.
 */
export function formatDistance(distanceMeters: number, lang: 'vi' | 'en'): string {
  if (!Number.isFinite(distanceMeters)) return '—';

  const metres = Math.max(0, distanceMeters);
  if (metres < 1000) {
    const rounded = Math.round(metres / 10) * 10;
    return `${rounded} m`;
  }

  const kilometres = (metres / 1000).toFixed(1);
  return `${lang === 'vi' ? kilometres.replace('.', ',') : kilometres} km`;
}

function normalizeSeverity(severity?: string | null): string | null {
  if (typeof severity !== 'string') return null;
  const normalized = severity.toUpperCase().trim();
  return normalized.length > 0 ? normalized : null;
}

function rankOf(table: Record<string, number>, severity: string | null, stale: boolean): number {
  if (!severity || stale) return 0;
  return table[severity] ?? 0;
}

/**
 * Verdict staleness at the injected instant: server-flagged stale wins,
 * otherwise the observation age must not exceed the 180 s TTL.
 */
function isVerdictStaleAt(
  verdict: TrafficVerdict | null | undefined,
  now: number,
  ttlMs: number = NEAR_ME_VERDICT_TTL_MS,
): boolean {
  if (!verdict) return false;
  if (verdict.stale) return true;
  const timeStr = verdict.observed_at || verdict.timestamp;
  if (!timeStr) return false;
  const observedAt = new Date(timeStr).getTime();
  if (!Number.isFinite(observedAt)) return false;
  return now - observedAt > ttlMs;
}

/**
 * Deterministic ordering (spec §1): distance band asc → flood rank desc →
 * traffic rank desc → exact distance asc → camera id code-point asc.
 * Stale verdicts keep their severity but sink to rank 0.
 */
export function compareNearMeResults(a: NearMeResult, b: NearMeResult): number {
  if (a.distanceBand !== b.distanceBand) return a.distanceBand - b.distanceBand;

  const floodDelta =
    rankOf(FLOOD_RANK, b.floodSeverity, b.floodStale) -
    rankOf(FLOOD_RANK, a.floodSeverity, a.floodStale);
  if (floodDelta !== 0) return floodDelta;

  const trafficDelta =
    rankOf(TRAFFIC_RANK, b.trafficSeverity, b.trafficStale) -
    rankOf(TRAFFIC_RANK, a.trafficSeverity, a.trafficStale);
  if (trafficDelta !== 0) return trafficDelta;

  if (a.distanceMeters !== b.distanceMeters) return a.distanceMeters - b.distanceMeters;

  const idA = a.camera?.id ?? '';
  const idB = b.camera?.id ?? '';
  if (idA === idB) return 0;
  return idA < idB ? -1 : 1;
}

/**
 * Builds the radius-scoped proximity list. Filter order is normative:
 * null camera → waterway → ghost → duplicate → invalid coordinates →
 * offline (when hidden) → distance within `radius + 0.5` → sort.
 */
export function buildNearMeResults(query: NearMeQuery): NearMeResult[] {
  if (!query || !isValidGeoPoint(query.origin)) return [];

  const {
    origin,
    cameras,
    floodVerdicts,
    trafficVerdicts,
    hideOffline = true,
    radiusMeters = NEAR_ME_RADIUS_METERS,
  } = query;

  const now = typeof query.now === 'number' ? query.now : Date.now();
  const maxDistance = (Number.isFinite(radiusMeters) ? radiusMeters : NEAR_ME_RADIUS_METERS) + 0.5;

  const results: NearMeResult[] = [];

  for (const camera of cameras ?? []) {
    if (!camera) continue;
    if (isWaterwayCamera(camera)) continue;
    if (camera.isGhost) continue;
    if (camera.duplicateOf) continue;
    if (!isValidGeoPoint(camera)) continue;
    if (hideOffline && isCameraOffline(camera)) continue;

    const rawDistance = haversineMeters(origin, {
      latitude: camera.latitude as number,
      longitude: camera.longitude as number,
    });
    if (!Number.isFinite(rawDistance)) continue;

    // Round to millimetre precision so sub-millimetre floating-point noise from
    // the trigonometric round-trip cannot shift a 250 m band boundary.
    const distanceMeters = Math.round(rawDistance * 1000) / 1000;
    if (distanceMeters > maxDistance) continue;

    const floodVerdict = floodVerdicts?.get(camera.id);
    const trafficVerdict = trafficVerdicts?.get(camera.id);

    const floodSeverity = normalizeSeverity(
      floodVerdict?.flood_severity ?? floodVerdict?.severity,
    );
    const floodStale = floodSeverity ? isVerdictStaleAt(floodVerdict, now) : false;

    const trafficSeverity = normalizeSeverity(trafficVerdict?.severity);
    const trafficStale = trafficSeverity ? isVerdictStaleAt(trafficVerdict, now) : false;

    results.push({
      camera,
      distanceMeters,
      distanceBand: distanceBand(distanceMeters),
      floodSeverity,
      floodStale,
      trafficSeverity,
      trafficStale,
    });
  }

  return results.sort(compareNearMeResults);
}
