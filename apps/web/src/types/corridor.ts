/**
 * Types for Arterial Corridors, GeoJSON Routes, and Passability Verdicts.
 * Enforces S15-T1 & S15-T2 commuter corridor specifications.
 */

export type PassabilityVerdict = 'PASSABLE' | 'CAUTION' | 'IMPASSABLE' | 'UNKNOWN';

export interface CorridorRouteSummary {
  road_key: string;
  display_name: string;
  description: string;
  length_km: number;
  free_flow_speed_kmh: number;
  free_flow_seconds: number;
  estimated_seconds: number;
  delay_seconds: number;
  worst_severity: string;
  worst_flood_severity: string;
  max_depth_cm?: number | null;
  max_hazard_level?: number | null;
  passability_verdict: PassabilityVerdict;
  status_color: string;
  total_cameras: number;
  verified_cameras: number;
  coverage_pct: number;
  chokepoint_count: number;
}

export interface RouteLineStringGeometry {
  type: 'LineString';
  coordinates: [number, number][]; // [longitude, latitude] in WGS84
}

export interface RoutePointGeometry {
  type: 'Point';
  coordinates: [number, number]; // [longitude, latitude] in WGS84
}

export type GeoJSONGeometry = RouteLineStringGeometry | RoutePointGeometry;

export interface CorridorLineProperties {
  road_key: string;
  display_name: string;
  description: string;
  length_km: number;
  free_flow_seconds: number;
  estimated_seconds: number;
  delay_seconds: number;
  worst_severity: string;
  worst_flood_severity: string;
  passability_verdict: PassabilityVerdict;
  status_color: string;
  total_cameras: number;
  verified_cameras: number;
  coverage_pct: number;
  stroke: string;
  'stroke-width': number;
  'stroke-opacity': number;
  max_depth_cm?: number;
  max_hazard_level?: number;
}

export interface CorridorCameraPointProperties {
  camera_id: string;
  name: string;
  severity: string;
  flood_severity: string;
  stale: boolean;
  'marker-color': string;
  flood_depth_cm?: number;
  flood_hazard_level?: number;
}

export interface GeoJSONFeature {
  type: 'Feature';
  geometry: GeoJSONGeometry;
  properties: CorridorLineProperties | CorridorCameraPointProperties | Record<string, unknown>;
}

export interface GeoJSONFeatureCollection {
  type: 'FeatureCollection';
  features: GeoJSONFeature[];
}

export interface CorridorRouteResponse {
  summary: CorridorRouteSummary;
  geojson: GeoJSONFeatureCollection;
}

export interface CheckpointCamera {
  camera_id: string;
  name: string;
  severity: string;
  flood_severity?: string;
  flood_depth_cm?: number;
  stale: boolean;
  coordinates: [number, number]; // [longitude, latitude]
}
