/**
 * Pure geospatial geometry utilities for Turn-by-Turn Navigation Hazard Alerts.
 * Implements S18-T1 / Issue #523.
 */

import type { CheckpointCamera, PassabilityVerdict } from '../types/corridor.ts';
import type { NavigationHazardAlert } from '../types/navigation.ts';
import type { VehicleType } from '../types/vehicle.ts';
import { VEHICLE_PROFILES } from '../types/vehicle.ts';

const EARTH_RADIUS_METERS = 6371008.8;

export interface PolylineProjection {
  segmentIndex: number;
  t: number;
  projectedPoint: [number, number]; // [lon, lat]
  orthogonalDistanceMeters: number;
}

function toRad(deg: number): number {
  return (deg * Math.PI) / 180;
}

/**
 * Great-circle distance between two [lon, lat] points in meters (Haversine).
 */
export function haversineCoords(
  coord1: [number, number],
  coord2: [number, number]
): number {
  const [lon1, lat1] = coord1;
  const [lon2, lat2] = coord2;

  const phi1 = toRad(lat1);
  const phi2 = toRad(lat2);
  const deltaPhi = toRad(lat2 - lat1);
  const deltaLambda = toRad(lon2 - lon1);

  const sinPhi = Math.sin(deltaPhi / 2);
  const sinLambda = Math.sin(deltaLambda / 2);

  const a = sinPhi * sinPhi + Math.cos(phi1) * Math.cos(phi2) * sinLambda * sinLambda;
  const clamped = Math.min(1, Math.max(0, a));
  return 2 * EARTH_RADIUS_METERS * Math.asin(Math.sqrt(clamped));
}

/**
 * Projects a point [lon, lat] orthogonally onto a 2D polyline [[lon, lat], ...].
 * Returns the segment index, interpolation factor t (clamped to [0, 1]), projected point, and orthogonal distance in meters.
 */
export function projectPointOntoPolyline(
  point: [number, number],
  polyline: [number, number][]
): PolylineProjection | null {
  if (!polyline || polyline.length < 2) {
    return null;
  }

  const [pLon, pLat] = point;
  let bestProjection: PolylineProjection | null = null;
  let minOrthogonalDist = Infinity;

  for (let i = 0; i < polyline.length - 1; i++) {
    const [aLon, aLat] = polyline[i];
    const [bLon, bLat] = polyline[i + 1];

    const midLat = toRad((aLat + bLat) / 2);
    const cosLat = Math.cos(midLat);

    // Metric flat-earth approximation for local segment
    const dx = toRad(bLon - aLon) * cosLat * EARTH_RADIUS_METERS;
    const dy = toRad(bLat - aLat) * EARTH_RADIUS_METERS;
    const segLenSq = dx * dx + dy * dy;

    let t = 0;
    if (segLenSq > 0) {
      const px = toRad(pLon - aLon) * cosLat * EARTH_RADIUS_METERS;
      const py = toRad(pLat - aLat) * EARTH_RADIUS_METERS;
      t = (px * dx + py * dy) / segLenSq;
    }

    const clampedT = Math.max(0, Math.min(1, t));

    // Projected point in WGS84 coordinates
    const projLon = aLon + clampedT * (bLon - aLon);
    const projLat = aLat + clampedT * (bLat - aLat);

    // Orthogonal distance from point to projected point
    const orthDist = haversineCoords(point, [projLon, projLat]);

    if (orthDist < minOrthogonalDist) {
      minOrthogonalDist = orthDist;
      bestProjection = {
        segmentIndex: i,
        t: clampedT,
        projectedPoint: [projLon, projLat],
        orthogonalDistanceMeters: orthDist,
      };
    }
  }

  return bestProjection;
}

/**
 * Calculates the cumulative chainage (distance in meters) along the polyline from its start to the point's projection.
 */
export function calculateCheckpointChainage(
  point: [number, number],
  polyline: [number, number][]
): number {
  const projection = projectPointOntoPolyline(point, polyline);
  if (!projection) return 0;

  let cumulativeMeters = 0;

  // Add full lengths of all segments before the projection segment
  for (let i = 0; i < projection.segmentIndex; i++) {
    cumulativeMeters += haversineCoords(polyline[i], polyline[i + 1]);
  }

  // Add the partial distance along the projection segment
  const segLength = haversineCoords(
    polyline[projection.segmentIndex],
    polyline[projection.segmentIndex + 1]
  );
  cumulativeMeters += projection.t * segLength;

  return cumulativeMeters;
}

/**
 * Formats a metric distance into bilingual human-readable format.
 * < 1000m: rounded to nearest 10m (e.g. "430 m")
 * >= 1000m: 1 decimal place with locale separator (e.g. "1,5 km" vs "1.5 km")
 */
export function formatAlertDistance(distanceMeters: number): { vi: string; en: string } {
  if (!Number.isFinite(distanceMeters) || distanceMeters < 0) {
    return { vi: '0 m', en: '0 m' };
  }

  if (distanceMeters < 1000) {
    const rounded = Math.round(distanceMeters / 10) * 10;
    return {
      vi: `${rounded} m`,
      en: `${rounded} m`,
    };
  }

  const roundedTenths = Math.round(distanceMeters / 100) / 10;
  const kmStr = roundedTenths.toFixed(1);
  return {
    vi: `${kmStr.replace('.', ',')} km`,
    en: `${kmStr} km`,
  };
}

export interface GenerateNavigationAlertsParams {
  polylineCoordinates: [number, number][]; // [[lon, lat], ...]
  checkpoints: CheckpointCamera[];
  vehicleType: VehicleType;
  userPosition?: [number, number]; // [lon, lat]
  reverseDirection?: boolean;
}

/**
 * Transforms raw camera checkpoints and corridor polyline into an ordered sequence of turn-by-turn navigation alerts.
 */
export function generateNavigationAlerts({
  polylineCoordinates,
  checkpoints,
  vehicleType,
  userPosition,
  reverseDirection = false,
}: GenerateNavigationAlertsParams): NavigationHazardAlert[] {
  if (!polylineCoordinates || polylineCoordinates.length < 2 || !checkpoints || checkpoints.length === 0) {
    return [];
  }

  const effectivePolyline = reverseDirection
    ? [...polylineCoordinates].reverse()
    : polylineCoordinates;

  const profile = VEHICLE_PROFILES[vehicleType] || VEHICLE_PROFILES.scooter;

  // User position chainage along polyline
  const userChainage = userPosition
    ? calculateCheckpointChainage(userPosition, effectivePolyline)
    : 0;

  const processedAlerts: NavigationHazardAlert[] = [];

  for (const checkpoint of checkpoints) {
    if (!checkpoint.coordinates || checkpoint.coordinates.length < 2) continue;

    // In CheckpointCamera, coordinates are [lon, lat]
    const checkpointCoords: [number, number] = [
      checkpoint.coordinates[0],
      checkpoint.coordinates[1],
    ];

    const chainage = calculateCheckpointChainage(checkpointCoords, effectivePolyline);
    const distanceAhead = chainage - userChainage;
    const isAhead = distanceAhead >= -50; // allow 50m grace zone before marking passed
    const absDistanceAhead = Math.max(0, distanceAhead);

    // Hazard evaluation
    const depth = checkpoint.flood_depth_cm ?? 0;
    let severity: PassabilityVerdict = 'PASSABLE';
    let hazardType: 'flood' | 'traffic' | 'clear' = 'clear';
    let excessDepth: number | null = null;
    let adviceVi = 'Đoạn đường khô ráo, lưu thông bình thường.';
    let adviceEn = 'Road is dry, normal traffic flow.';

    if (depth >= profile.impassableDepthCm) {
      severity = 'IMPASSABLE';
      hazardType = 'flood';
      excessDepth = Math.round(depth - profile.impassableDepthCm);
      adviceVi = `Nguy cơ chết máy với ${profile.name.vi}: Ngập ${depth}cm (vượt gầm ${profile.impassableDepthCm}cm).`;
      adviceEn = `Engine stall hazard for ${profile.name.en}: Water depth ${depth}cm (exceeds clearance ${profile.impassableDepthCm}cm).`;
    } else if (depth >= profile.cautionDepthCm) {
      severity = 'CAUTION';
      hazardType = 'flood';
      adviceVi = `Cảnh báo ngập với ${profile.name.vi}: Mực nước ${depth}cm, di chuyển chậm giữa tim đường.`;
      adviceEn = `Flood caution for ${profile.name.en}: Water depth ${depth}cm, proceed slowly along road crest.`;
    } else if (checkpoint.severity === 'CONGESTED' || checkpoint.severity === 'HEAVY') {
      severity = 'CAUTION';
      hazardType = 'traffic';
      adviceVi = `Ùn tắc giao thông, tốc độ di chuyển chậm qua nút giao.`;
      adviceEn = `Heavy congestion, slow traffic through intersection.`;
    }

    processedAlerts.push({
      id: checkpoint.camera_id,
      name: checkpoint.name,
      distanceAlongRouteMeters: Math.round(chainage),
      distanceAheadMeters: Math.round(absDistanceAhead),
      distanceFormatted: formatAlertDistance(absDistanceAhead),
      hazardType,
      severity,
      depthCm: depth > 0 ? depth : null,
      excessDepthCm: excessDepth,
      vehicleType,
      actionableAdvice: {
        vi: adviceVi,
        en: adviceEn,
      },
      coordinates: [checkpoint.coordinates[1], checkpoint.coordinates[0]], // [lat, lon] for Leaflet
      isAhead,
      stale: checkpoint.stale,
    });
  }

  // Sort strictly by distance along route ascending
  processedAlerts.sort((a, b) => a.distanceAlongRouteMeters - b.distanceAlongRouteMeters);

  return processedAlerts;
}
