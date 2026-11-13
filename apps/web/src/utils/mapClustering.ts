/**
 * Pure marker-clustering helpers for the commuter map (S14-T3 / Issue #443).
 *
 * The map must stay readable in direct sunlight on a phone at city zoom: ~2,000
 * cameras rendered as individual dots merge into a solid smear, and the one
 * Level 3 flood in the middle of it becomes invisible. Cameras therefore collapse
 * into count badges below `CLUSTER_ZOOM_THRESHOLD` (14), and each badge bubbles up
 * the worst hazard level inside it so severe flooding is never hidden by
 * aggregation.
 *
 * Everything here is projection maths on plain objects: no Leaflet, no DOM, no
 * React. That keeps the band boundaries, the grid and the bubble-up rule
 * testable, and one implementation is shared by map, tests and tooltips.
 *
 * Safety exceptions encoded below (deliberate, not accidental):
 *  - `forceIndividualIds` keeps a camera with an active crowdsourced flood report
 *    visible as its own marker at every zoom. A user-report ring is a live safety
 *    signal and must not be absorbed into a badge.
 *  - A camera whose position is not a finite lat/lng is dropped rather than
 *    clustered at (0, 0) in the Gulf of Guinea.
 */

import type { HazardLevel } from './hazardUtils.ts';

/** Individual markers at and above this zoom (spec §2). */
export const CLUSTER_ZOOM_THRESHOLD = 14;

/** Grid cell width in CSS pixels. */
export const CLUSTER_GRID_PX = 64;

/** Web Mercator tile edge, in CSS pixels, at zoom 0. */
export const WORLD_TILE_SIZE_PX = 256;

/** Beyond ±85.0511° the Mercator projection is undefined. */
export const MERCATOR_MAX_LATITUDE = 85.05112878;

/** Minimum diameter of a cluster badge in CSS pixels (kiosk touch target). */
export const CLUSTER_MIN_DIAMETER_PX = 48;

/** Maximum diameter so a dense badge never covers a street. */
export const CLUSTER_MAX_DIAMETER_PX = 68;

/**
 * A degenerate one-camera bounds would make `flyToBounds` jump to max zoom, so a
 * zero-area box is widened by this many degrees (~55 m at the equator).
 */
export const DEGENERATE_BOUNDS_PAD_DEG = 0.0005;

export interface ClusterPoint {
  id: string;
  latitude: number;
  longitude: number;
}

export interface ClusterBounds {
  south: number;
  west: number;
  north: number;
  east: number;
}

export interface CameraCluster {
  /** Stable identity: grid cell of the first member, or the camera id when solo. */
  key: string;
  count: number;
  cameraIds: string[];
  /** Mean position, used as the badge anchor. */
  latitude: number;
  longitude: number;
  bounds: ClusterBounds;
  /** Worst hazard level inside the cluster; `null` when nothing is known. */
  hazardLevel: HazardLevel | null;
}

export interface ClusterOptions {
  zoom: number;
  gridPx?: number;
  thresholdZoom?: number;
  /** `false` forces individual markers (flood lens). Defaults to `true`. */
  enabled?: boolean;
  /** Hazard level per camera id, `null`/undefined when unknown. */
  levelFor?: (cameraId: string) => HazardLevel | null | undefined;
  /** Cameras that must never be absorbed into a cluster (active user reports). */
  forceIndividualIds?: ReadonlySet<string>;
}

export interface ClusterResult {
  /** True when aggregation is active at this zoom. */
  clustered: boolean;
  clusters: CameraCluster[];
}

/**
 * Longitude -> world pixel X at the given zoom.
 */
export function lngToWorldX(longitude: number, zoom: number): number {
  return ((longitude + 180) / 360) * WORLD_TILE_SIZE_PX * Math.pow(2, zoom);
}

/**
 * Latitude -> world pixel Y at the given zoom (Web Mercator, EPSG:3857).
 */
export function latToWorldY(latitude: number, zoom: number): number {
  const clamped = Math.max(-MERCATOR_MAX_LATITUDE, Math.min(MERCATOR_MAX_LATITUDE, latitude));
  const sin = Math.sin((clamped * Math.PI) / 180);
  return (
    (0.5 - Math.log((1 + sin) / (1 - sin)) / (4 * Math.PI)) *
    WORLD_TILE_SIZE_PX *
    Math.pow(2, zoom)
  );
}

function isFiniteCoordinate(point: ClusterPoint): boolean {
  return (
    Number.isFinite(point.latitude) &&
    Number.isFinite(point.longitude) &&
    point.latitude >= -90 &&
    point.latitude <= 90 &&
    point.longitude >= -180 &&
    point.longitude <= 180
  );
}

/**
 * Bubble up the worst hazard level: Level 3 beats Level 2 beats Level 1, and an
 * empty/unknown collection is `null` — never Level 1, because absence of a
 * measurement is not safety (AC14/AC10 of Issue #401).
 */
export function worstHazardLevel(
  levels: ReadonlyArray<HazardLevel | null | undefined>,
): HazardLevel | null {
  let worst: HazardLevel | null = null;
  for (const level of levels) {
    if (level === 1 || level === 2 || level === 3) {
      if (worst === null || level > worst) worst = level;
    }
  }
  return worst;
}

/**
 * Badge diameter for a cluster count. Grows logarithmically, clamped to the
 * touch-target floor and the street-visibility ceiling.
 */
export function getClusterDiameter(count: number): number {
  const safeCount = Number.isFinite(count) ? Math.max(1, Math.floor(count)) : 1;
  const grown = CLUSTER_MIN_DIAMETER_PX + Math.floor(Math.log2(safeCount)) * 4;
  return Math.min(CLUSTER_MAX_DIAMETER_PX, grown);
}

function boundsOf(points: ReadonlyArray<ClusterPoint>): ClusterBounds {
  let south = Number.POSITIVE_INFINITY;
  let west = Number.POSITIVE_INFINITY;
  let north = Number.NEGATIVE_INFINITY;
  let east = Number.NEGATIVE_INFINITY;

  for (const point of points) {
    if (point.latitude < south) south = point.latitude;
    if (point.latitude > north) north = point.latitude;
    if (point.longitude < west) west = point.longitude;
    if (point.longitude > east) east = point.longitude;
  }

  if (north - south < DEGENERATE_BOUNDS_PAD_DEG) {
    const center = (north + south) / 2;
    south = center - DEGENERATE_BOUNDS_PAD_DEG / 2;
    north = center + DEGENERATE_BOUNDS_PAD_DEG / 2;
  }
  if (east - west < DEGENERATE_BOUNDS_PAD_DEG) {
    const center = (east + west) / 2;
    west = center - DEGENERATE_BOUNDS_PAD_DEG / 2;
    east = center + DEGENERATE_BOUNDS_PAD_DEG / 2;
  }

  return { south, west, north, east };
}

function toCluster(
  key: string,
  members: ReadonlyArray<ClusterPoint>,
  levelFor: ((id: string) => HazardLevel | null | undefined) | undefined,
): CameraCluster {
  let latSum = 0;
  let lngSum = 0;
  for (const member of members) {
    latSum += member.latitude;
    lngSum += member.longitude;
  }
  const cameraIds = members.map((member) => member.id);
  const levels = levelFor ? members.map((member) => levelFor(member.id)) : [];
  return {
    key,
    count: members.length,
    cameraIds,
    latitude: latSum / members.length,
    longitude: lngSum / members.length,
    bounds: boundsOf(members),
    hazardLevel: worstHazardLevel(levels),
  };
}

/**
 * Group cameras into square pixel cells at the current zoom.
 *
 * Below the threshold the result is one cluster per non-empty grid cell; at or
 * above it (or when disabled) every camera is its own count-1 cluster, so the
 * caller can iterate one shape on both paths. Output is deterministic: input is
 * sorted by id first, so SSE ticks that reshuffle the camera array cannot make
 * badges swap places.
 */
export function buildCameraClusters(
  points: ReadonlyArray<ClusterPoint>,
  options: ClusterOptions,
): ClusterResult {
  const {
    zoom,
    gridPx = CLUSTER_GRID_PX,
    thresholdZoom = CLUSTER_ZOOM_THRESHOLD,
    enabled = true,
    levelFor,
    forceIndividualIds,
  } = options;

  const valid = points
    .filter((point) => typeof point.id === 'string' && point.id.length > 0 && isFiniteCoordinate(point))
    .slice()
    .sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));

  const clustered = Boolean(enabled) && zoom < thresholdZoom;

  if (!clustered) {
    return {
      clustered: false,
      clusters: valid.map((point) => toCluster(point.id, [point], levelFor)),
    };
  }

  const cellSize = Number.isFinite(gridPx) && gridPx > 0 ? gridPx : CLUSTER_GRID_PX;
  const cells = new Map<string, ClusterPoint[]>();
  const individual: ClusterPoint[] = [];

  for (const point of valid) {
    if (forceIndividualIds?.has(point.id)) {
      individual.push(point);
      continue;
    }
    const cellX = Math.floor(lngToWorldX(point.longitude, zoom) / cellSize);
    const cellY = Math.floor(latToWorldY(point.latitude, zoom) / cellSize);
    const key = `${cellX}:${cellY}`;
    const bucket = cells.get(key);
    if (bucket) bucket.push(point);
    else cells.set(key, [point]);
  }

  const clusters: CameraCluster[] = [];
  for (const [key, members] of cells) {
    clusters.push(toCluster(key, members, levelFor));
  }
  for (const point of individual) {
    clusters.push(toCluster(point.id, [point], levelFor));
  }

  return { clustered: true, clusters };
}

/**
 * Whether the map should aggregate at all. The flood lens is the one lens where
 * every camera is evidence for a water depth, so it always expands.
 */
export function shouldClusterMarkers(zoom: number, isFloodLens: boolean): boolean {
  return !isFloodLens && zoom < CLUSTER_ZOOM_THRESHOLD;
}

/** Colour tokens for a cluster badge's hazard bubble-up (spec §2). */
export interface ClusterHazardVisual {
  level: HazardLevel | null;
  /** `true` only for Level 3: a red danger ring the rider sees from arm's length. */
  hasDangerRing: boolean;
  /** Tailwind ring classes drawn around the badge surface. */
  ringClass: string;
  /** Surface + text + border classes for the badge itself. */
  surfaceClass: string;
  /** Small indicator dot in the badge corner. */
  indicatorClass: string;
}

/**
 * Level 3 -> red danger ring, Level 2 -> amber, everything else -> dark zinc.
 *
 * This is the one place chroma is allowed on the hazard layer: the badge sits on
 * a photographic basemap where a monochrome ring disappears, and the ring encodes
 * "stop, water is deep" rather than a decorative category. The depth block itself
 * stays monochrome and never relies on colour alone.
 */
export function getClusterHazardVisual(level: HazardLevel | null): ClusterHazardVisual {
  switch (level) {
    case 3:
      return {
        level,
        hasDangerRing: true,
        ringClass: 'ring-2 ring-red-500 ring-offset-2 ring-offset-zinc-950',
        surfaceClass: 'bg-zinc-950 text-red-300 border-red-500',
        indicatorClass: 'bg-red-500',
      };
    case 2:
      return {
        level,
        hasDangerRing: false,
        ringClass: 'ring-2 ring-amber-400 ring-offset-2 ring-offset-zinc-950',
        surfaceClass: 'bg-zinc-950 text-amber-200 border-amber-400',
        indicatorClass: 'bg-amber-400',
      };
    default:
      return {
        level,
        hasDangerRing: false,
        ringClass: 'ring-1 ring-zinc-600',
        surfaceClass: 'bg-zinc-950 text-zinc-100 border-zinc-600',
        indicatorClass: 'bg-zinc-500',
      };
  }
}
