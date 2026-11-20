/**
 * Dynamic Vehicle Clearance & Hazard Evaluation Utility (Issue #510).
 * Tailors flood verdicts and clearance thresholds to commuter vehicle profiles.
 */

import type { PassabilityVerdict } from '../types/corridor.ts';
import {
  VEHICLE_PROFILES,
  DEFAULT_VEHICLE_TYPE,
  VEHICLE_STORAGE_KEY,
  type VehicleType,
  type VehicleProfile,
} from '../types/vehicle.ts';

export interface VehicleHazardEvaluation {
  verdict: PassabilityVerdict;
  statusColor: string;
  label: {
    vi: string;
    en: string;
  };
  profile: VehicleProfile;
  depthCm: number | null;
}

/**
 * Returns the VehicleProfile for the given type, falling back safely to scooter.
 */
export function getVehicleProfile(type?: string | null): VehicleProfile {
  if (type && type in VEHICLE_PROFILES) {
    return VEHICLE_PROFILES[type as VehicleType];
  }
  return VEHICLE_PROFILES[DEFAULT_VEHICLE_TYPE];
}

/**
 * Loads the stored vehicle profile from localStorage (client-side only).
 */
export function getStoredVehicleProfile(): VehicleType {
  if (typeof window === 'undefined') return DEFAULT_VEHICLE_TYPE;
  try {
    const saved = localStorage.getItem(VEHICLE_STORAGE_KEY);
    if (saved && saved in VEHICLE_PROFILES) {
      return saved as VehicleType;
    }
  } catch {
    // Ignore storage quota or access errors
  }
  return DEFAULT_VEHICLE_TYPE;
}

/**
 * Persists the selected vehicle profile into localStorage.
 */
export function setStoredVehicleProfile(type: VehicleType): void {
  if (typeof window === 'undefined') return;
  try {
    localStorage.setItem(VEHICLE_STORAGE_KEY, type);
  } catch {
    // Ignore storage quota or access errors
  }
}

/**
 * Evaluates dynamic hazard level and passability verdict based on vehicle physical clearance.
 */
export function evaluateVehicleHazard({
  maxDepthCm,
  worstSeverity = 'UNKNOWN',
  worstFloodSeverity = 'UNKNOWN',
  maxHazardLevel,
  vehicleType = DEFAULT_VEHICLE_TYPE,
  hasVerifiedCameras = true,
}: {
  maxDepthCm?: number | null;
  worstSeverity?: string;
  worstFloodSeverity?: string;
  maxHazardLevel?: number | null;
  vehicleType?: VehicleType;
  hasVerifiedCameras?: boolean;
}): VehicleHazardEvaluation {
  const profile = getVehicleProfile(vehicleType);
  const depth = typeof maxDepthCm === 'number' && !Number.isNaN(maxDepthCm) ? maxDepthCm : null;

  let verdict: PassabilityVerdict;
  let statusColor: string;

  // 1. Critical impassable condition: exceeds vehicle impassable depth, or unmeasured depth with deep flood
  const isDepthImpassable = depth !== null && depth >= profile.impassableDepthCm;
  const isFallbackCritical = depth === null && ((maxHazardLevel != null && maxHazardLevel >= 3) || worstFloodSeverity === 'FLOODED_DEEP');

  if (isDepthImpassable || isFallbackCritical) {
    verdict = 'IMPASSABLE';
    statusColor = '#ef4444'; // Red
  }
  // 2. Cautionary condition: exceeds vehicle caution depth, heavy/congested traffic, or unmeasured shallow flood
  else {
    const isDepthCaution = depth !== null && depth >= profile.cautionDepthCm;
    const isFallbackCaution = depth === null && ((maxHazardLevel != null && maxHazardLevel >= 2) || worstFloodSeverity === 'FLOODED_SHALLOW');
    const isTrafficHeavy = worstSeverity === 'CONGESTED' || worstSeverity === 'HEAVY';

    if (isDepthCaution || isFallbackCaution || isTrafficHeavy) {
      verdict = 'CAUTION';
      statusColor = '#eab308'; // Amber/Yellow
    }
    // 3. Normal passable flow
    else if (hasVerifiedCameras) {
      verdict = 'PASSABLE';
      statusColor = '#22c55e'; // Green
    }
    // 4. Insufficient telemetry
    else {
      verdict = 'UNKNOWN';
      statusColor = '#71717a'; // Zinc/Muted
    }
  }

  return {
    verdict,
    statusColor,
    label: profile.verdictLabels[verdict],
    profile,
    depthCm: depth,
  };
}
