/**
 * Commuter Personal Journey Glance, Bottleneck Isolation & Detour Suggestions.
 * Implements S17-T4 / Issue #512.
 */

import type { PassabilityVerdict, CorridorRouteSummary } from '../types/corridor.ts';
import type { VehicleType } from '../types/vehicle.ts';
import { VEHICLE_PROFILES } from '../types/vehicle.ts';
import type { CommuteBottleneck, DetourSuggestion, CommuteEvaluation } from '../types/commute.ts';
import type { WeatherData } from './weatherUtils.ts';

export const CORRIDOR_DETOUR_MAP: Record<string, string[]> = {
  nguyen_huu_canh: ['dien_bien_phu'],
  dien_bien_phu: ['nguyen_huu_canh', 'pham_van_dong'],
  cach_mang_thang_tam: ['nam_ky_khoi_nghia'],
  nam_ky_khoi_nghia: ['cach_mang_thang_tam'],
  cong_hoa: ['pham_van_dong', 'cach_mang_thang_tam'],
  pham_van_dong: ['cong_hoa', 'dien_bien_phu'],
  vo_van_kiet: ['nguyen_van_linh'],
  nguyen_van_linh: ['vo_van_kiet'],
  mai_chi_tho: ['dien_bien_phu'],
};

export const CORRIDOR_NAMES: Record<string, string> = {
  vo_van_kiet: 'Đại lộ Võ Văn Kiệt',
  mai_chi_tho: 'Đường Mai Chí Thọ',
  dien_bien_phu: 'Đường Điện Biên Phủ',
  nguyen_huu_canh: 'Đường Nguyễn Hữu Cảnh',
  cong_hoa: 'Đường Cộng Hòa',
  pham_van_dong: 'Đường Phạm Văn Đồng',
  nam_ky_khoi_nghia: 'Nam Kỳ Khởi Nghĩa - Nguyễn Văn Trỗi',
  cach_mang_thang_tam: 'Đường Cách Mạng Tháng 8',
  nguyen_van_linh: 'Đại lộ Nguyễn Văn Linh',
  quoc_lo_1: 'Quốc lộ 1',
};

export const CORRIDOR_APPROX_COORDS: Record<string, [number, number]> = {
  vo_van_kiet: [10.749, 106.663],
  mai_chi_tho: [10.782, 106.740],
  dien_bien_phu: [10.787, 106.695],
  nguyen_huu_canh: [10.789, 106.716],
  cong_hoa: [10.802, 106.645],
  pham_van_dong: [10.828, 106.722],
  nam_ky_khoi_nghia: [10.783, 106.691],
  cach_mang_thang_tam: [10.782, 106.672],
  nguyen_van_linh: [10.728, 106.677],
  quoc_lo_1: [10.750, 106.594],
};

function haversineDistanceKm(lat1: number, lon1: number, lat2: number, lon2: number): number {
  const R = 6371; // Earth's radius in km
  const dLat = ((lat2 - lat1) * Math.PI) / 180;
  const dLon = ((lon2 - lon1) * Math.PI) / 180;
  const a =
    Math.sin(dLat / 2) * Math.sin(dLat / 2) +
    Math.cos((lat1 * Math.PI) / 180) *
      Math.cos((lat2 * Math.PI) / 180) *
      Math.sin(dLon / 2) *
      Math.sin(dLon / 2);
  const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
  return R * c;
}

/**
 * Finds connecting arterial corridors between origin and destination coordinates.
 */
export function findConnectingCorridors(
  origin: { lat: number; lng: number },
  destination: { lat: number; lng: number }
): string[] {
  let closestOriginKey = 'vo_van_kiet';
  let minOriginDist = Infinity;
  let closestDestKey = 'vo_van_kiet';
  let minDestDist = Infinity;

  for (const [key, coords] of Object.entries(CORRIDOR_APPROX_COORDS)) {
    const distToOrigin = haversineDistanceKm(origin.lat, origin.lng, coords[0], coords[1]);
    if (distToOrigin < minOriginDist) {
      minOriginDist = distToOrigin;
      closestOriginKey = key;
    }

    const distToDest = haversineDistanceKm(destination.lat, destination.lng, coords[0], coords[1]);
    if (distToDest < minDestDist) {
      minDestDist = distToDest;
      closestDestKey = key;
    }
  }

  if (closestOriginKey === closestDestKey) {
    return [closestOriginKey];
  }
  return [closestOriginKey, closestDestKey];
}

function passabilityRank(v: PassabilityVerdict): number {
  switch (v) {
    case 'IMPASSABLE':
      return 3;
    case 'CAUTION':
      return 2;
    case 'UNKNOWN':
      return 1;
    default:
      return 0;
  }
}

export interface EvaluateCommuteParams {
  corridorKeys: string[];
  vehicleType: VehicleType;
  corridorSummaries?: Record<string, CorridorRouteSummary> | CorridorRouteSummary[];
  weather?: WeatherData | null;
}

/**
 * Evaluates aggregated passability, travel duration, delays, worst bottleneck, and detour options.
 */
export function evaluateCommuteRoute({
  corridorKeys,
  vehicleType,
  corridorSummaries = {},
  weather,
}: EvaluateCommuteParams): CommuteEvaluation {
  const summariesMap: Record<string, CorridorRouteSummary> = Array.isArray(corridorSummaries)
    ? corridorSummaries.reduce((acc, s) => {
        acc[s.road_key] = s;
        return acc;
      }, {} as Record<string, CorridorRouteSummary>)
    : corridorSummaries;

  const validKeys = Array.from(new Set(corridorKeys.filter(k => Boolean(k && k.trim()))));
  const activeSummaries: CorridorRouteSummary[] = [];

  let totalDistanceKm = 0;
  let freeFlowSeconds = 0;
  let estimatedSeconds = 0;
  let delaySeconds = 0;
  let worstVerdict: PassabilityVerdict = 'PASSABLE';
  let worstStatusColor = '#22c55e';
  let worstBottleneck: CommuteBottleneck | null = null;
  const hazardousCorridorKeys: string[] = [];

  const vehicleProfile = VEHICLE_PROFILES[vehicleType] || VEHICLE_PROFILES.scooter;

  for (const key of validKeys) {
    const summary = summariesMap[key];
    const corridorName = CORRIDOR_NAMES[key] || key;

    if (!summary) {
      // Fallback default calculation if corridor status is pending
      const defaultLen = 5.0;
      const defaultSec = Math.round((defaultLen / 35.0) * 3600);
      totalDistanceKm += defaultLen;
      freeFlowSeconds += defaultSec;
      estimatedSeconds += defaultSec;
      activeSummaries.push({
        road_key: key,
        display_name: corridorName,
        description: corridorName,
        length_km: defaultLen,
        free_flow_speed_kmh: 35.0,
        free_flow_seconds: defaultSec,
        estimated_seconds: defaultSec,
        delay_seconds: 0,
        worst_severity: 'UNKNOWN',
        worst_flood_severity: 'UNKNOWN',
        passability_verdict: 'UNKNOWN',
        status_color: '#71717a',
        total_cameras: 0,
        verified_cameras: 0,
        coverage_pct: 0,
        chokepoint_count: 0,
      });
      continue;
    }

    activeSummaries.push(summary);
    totalDistanceKm += summary.length_km;
    freeFlowSeconds += summary.free_flow_seconds;
    estimatedSeconds += summary.estimated_seconds;
    delaySeconds += summary.delay_seconds;

    // Adapt passability verdict to vehicle clearance
    let effectiveVerdict: PassabilityVerdict = summary.passability_verdict;
    let effectiveColor = summary.status_color;

    if (summary.max_depth_cm !== undefined && summary.max_depth_cm !== null) {
      const depth = summary.max_depth_cm;
      if (depth >= vehicleProfile.impassableDepthCm) {
        effectiveVerdict = 'IMPASSABLE';
        effectiveColor = '#ef4444';
      } else if (depth >= vehicleProfile.cautionDepthCm) {
        effectiveVerdict = 'CAUTION';
        effectiveColor = '#eab308';
      } else if (summary.worst_severity === 'CONGESTED' || summary.worst_severity === 'HEAVY') {
        effectiveVerdict = 'CAUTION';
        effectiveColor = '#eab308';
      } else if (summary.verified_cameras > 0) {
        effectiveVerdict = 'PASSABLE';
        effectiveColor = '#22c55e';
      }
    }

    if (passabilityRank(effectiveVerdict) > passabilityRank(worstVerdict)) {
      worstVerdict = effectiveVerdict;
      worstStatusColor = effectiveColor;
    }

    if (effectiveVerdict === 'IMPASSABLE' || effectiveVerdict === 'CAUTION') {
      hazardousCorridorKeys.push(key);
    }

    // Bottleneck extraction: check if this corridor has the deepest flood or heaviest congestion
    const hasWaterDepth = summary.max_depth_cm !== undefined && summary.max_depth_cm !== null && summary.max_depth_cm > 0;
    const isCongested = summary.worst_severity === 'CONGESTED' || summary.worst_severity === 'HEAVY';

    if (hasWaterDepth || isCongested) {
      const currentBottleneckDepth = worstBottleneck?.flood_depth_cm ?? -1;
      const thisDepth = summary.max_depth_cm ?? 0;

      if (!worstBottleneck || thisDepth > currentBottleneckDepth) {
        let descVi = '';
        let descEn = '';

        if (thisDepth >= vehicleProfile.impassableDepthCm) {
          descVi = `Đoạn ngập sâu nhất: ${corridorName}: ${thisDepth} cm (Nguy cơ chết máy với ${vehicleProfile.name.vi})`;
          descEn = `Deepest flood: ${corridorName}: ${thisDepth} cm (Engine stall hazard for ${vehicleProfile.name.en})`;
        } else if (thisDepth >= vehicleProfile.cautionDepthCm) {
          descVi = `Đoạn ngập sâu nhất: ${corridorName}: ${thisDepth} cm (Cảnh báo xe gầm thấp)`;
          descEn = `Deepest flood: ${corridorName}: ${thisDepth} cm (Low clearance caution)`;
        } else if (thisDepth > 0) {
          descVi = `Đoạn đọng nước: ${corridorName}: ${thisDepth} cm`;
          descEn = `Water accumulation: ${corridorName}: ${thisDepth} cm`;
        } else {
          descVi = `Ùn tắc nghiêm trọng tại: ${corridorName}`;
          descEn = `Severe traffic congestion on: ${corridorName}`;
        }

        worstBottleneck = {
          camera_id: `cam-${key}-choke`,
          name: corridorName,
          severity: summary.worst_severity,
          flood_severity: summary.worst_flood_severity,
          flood_depth_cm: summary.max_depth_cm,
          flood_hazard_level: summary.max_hazard_level,
          corridor_key: key,
          corridor_name: corridorName,
          description: {
            vi: descVi,
            en: descEn,
          },
        };
      }
    }
  }

  // Detour search: if route is IMPASSABLE or CAUTION, check detour candidates
  let detour: DetourSuggestion | null = null;
  if (hazardousCorridorKeys.length > 0) {
    for (const badKey of hazardousCorridorKeys) {
      const bypassCandidates = CORRIDOR_DETOUR_MAP[badKey] || [];
      const badName = CORRIDOR_NAMES[badKey] || badKey;

      for (const candKey of bypassCandidates) {
        if (validKeys.includes(candKey)) continue; // Avoid recommending corridors already on path

        const candSummary = summariesMap[candKey];
        const candName = CORRIDOR_NAMES[candKey] || candKey;

        if (candSummary) {
          const candDepth = candSummary.max_depth_cm ?? 0;
          const isCandPassable =
            candSummary.passability_verdict === 'PASSABLE' ||
            (candDepth < vehicleProfile.cautionDepthCm &&
              candSummary.worst_severity !== 'CONGESTED' &&
              candSummary.worst_severity !== 'HEAVY');

          if (isCandPassable) {
            detour = {
              original_corridor_key: badKey,
              detour_corridor_key: candKey,
              detour_corridor_name: candName,
              passability_verdict: candSummary.passability_verdict,
              max_depth_cm: candSummary.max_depth_cm,
              delay_seconds: candSummary.delay_seconds,
              reason: {
                vi: `Tránh điểm ngập trên ${badName}; ${candName} khô ráo, thông thoáng.`,
                en: `Avoid water hazard on ${badName}; ${candName} is dry and passable.`,
              },
            };
            break;
          }
        }
      }
      if (detour) break;
    }
  }

  // Weather & Rain Summary
  const rainPct = weather ? Math.round(weather.precipitation_probability_percent) : 0;
  let rainSummaryVi = 'Khô ráo, không mưa';
  let rainSummaryEn = 'Dry, no rain';
  if (rainPct >= 70) {
    rainSummaryVi = `Mưa lớn diện rộng (${rainPct}%)`;
    rainSummaryEn = `High rain likelihood (${rainPct}%)`;
  } else if (rainPct >= 40) {
    rainSummaryVi = `Khả năng có mưa rào (${rainPct}%)`;
    rainSummaryEn = `Scattered showers possible (${rainPct}%)`;
  }

  return {
    passability: worstVerdict,
    status_color: worstStatusColor,
    worst_bottleneck: worstBottleneck,
    total_distance_km: Math.round(totalDistanceKm * 10) / 10,
    free_flow_seconds: freeFlowSeconds,
    estimated_seconds: estimatedSeconds,
    delay_seconds: Math.max(0, delaySeconds),
    rain_probability_pct: rainPct,
    rain_summary: {
      vi: rainSummaryVi,
      en: rainSummaryEn,
    },
    corridors: validKeys,
    corridor_summaries: activeSummaries,
    detour,
    vehicle: vehicleType,
  };
}

export function formatCommuteDuration(seconds: number, lang: 'vi' | 'en'): string {
  if (seconds < 60) return lang === 'vi' ? '< 1 phút' : '< 1 min';
  const minutes = Math.round(seconds / 60);
  return `${minutes} ${lang === 'vi' ? 'phút' : 'mins'}`;
}

export function formatCommuteDelay(delaySeconds: number, lang: 'vi' | 'en'): string {
  const delayMins = Math.round(delaySeconds / 60);
  if (delayMins <= 0) {
    return lang === 'vi' ? 'Đúng giờ' : 'On time';
  }
  return `+${delayMins} ${lang === 'vi' ? 'phút trễ' : 'mins delay'}`;
}
