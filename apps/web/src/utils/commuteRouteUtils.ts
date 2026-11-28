/**
 * Commuter Personal Journey Glance, Bottleneck Isolation & Detour Suggestions.
 * Implements S17-T4 / Issue #512.
 */

import type { PassabilityVerdict, CorridorRouteSummary } from '../types/corridor.ts';
import type { VehicleType } from '../types/vehicle.ts';
import { VEHICLE_PROFILES } from '../types/vehicle.ts';
import type { CommuteBottleneck, CommuteDirection, DetourSuggestion, CommuteEvaluation } from '../types/commute.ts';
import type { WeatherData } from './weatherUtils.ts';

export const CORRIDOR_DETOUR_MAP: Record<string, string[]> = {
  nguyen_huu_canh: ['dien_bien_phu'],
  dien_bien_phu: ['nguyen_huu_canh', 'pham_van_dong', 'mai_chi_tho'],
  cach_mang_thang_tam: ['nam_ky_khoi_nghia'],
  nam_ky_khoi_nghia: ['cach_mang_thang_tam', 'dien_bien_phu'],
  cong_hoa: ['pham_van_dong', 'cach_mang_thang_tam'],
  pham_van_dong: ['cong_hoa', 'dien_bien_phu', 'quoc_lo_1'],
  vo_van_kiet: ['nguyen_van_linh'],
  nguyen_van_linh: ['vo_van_kiet', 'quoc_lo_1'],
  mai_chi_tho: ['dien_bien_phu', 'vo_van_kiet'],
  quoc_lo_1: ['pham_van_dong', 'nguyen_van_linh'],
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

export const MAX_COMMUTE_SEGMENTS = 3;

function findClosestCorridorKey(point: { lat: number; lng: number }): string {
  let closestKey = 'vo_van_kiet';
  let minDist = Infinity;
  for (const [key, coords] of Object.entries(CORRIDOR_APPROX_COORDS)) {
    const dist = haversineDistanceKm(point.lat, point.lng, coords[0], coords[1]);
    if (dist < minDist) {
      minDist = dist;
      closestKey = key;
    }
  }
  return closestKey;
}

/**
 * Finds ordered connecting arterial corridors between origin, optional waypoint, and destination coordinates.
 */
export function findConnectingCorridors(
  origin: { lat: number; lng: number },
  destination: { lat: number; lng: number },
  waypoint?: { lat: number; lng: number } | null
): string[] {
  const stops = waypoint ? [origin, waypoint, destination] : [origin, destination];
  const ordered: string[] = [];
  for (const stop of stops) {
    const key = findClosestCorridorKey(stop);
    if (!ordered.includes(key)) {
      ordered.push(key);
    }
    if (ordered.length >= MAX_COMMUTE_SEGMENTS) break;
  }
  return ordered;
}

export function reorderCorridorSegments(
  segments: string[],
  index: number,
  direction: 'up' | 'down'
): string[] {
  const target = direction === 'up' ? index - 1 : index + 1;
  if (index < 0 || index >= segments.length || target < 0 || target >= segments.length) {
    return segments;
  }
  const next = [...segments];
  const [moved] = next.splice(index, 1);
  next.splice(target, 0, moved);
  return next;
}

export function resolveOrderedCorridorsForDirection(
  corridors: string[],
  direction: CommuteDirection
): string[] {
  if (direction === 'to_home') {
    return [...corridors].reverse();
  }
  return corridors;
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
const MAX_DETOUR_SUGGESTIONS = 4;

function resolveEffectivePassability(
  summary: CorridorRouteSummary,
  vehicleType: VehicleType
): { verdict: PassabilityVerdict; color: string } {
  const vehicleProfile = VEHICLE_PROFILES[vehicleType] || VEHICLE_PROFILES.scooter;
  if (summary.max_depth_cm !== undefined && summary.max_depth_cm !== null) {
    if (summary.max_depth_cm >= vehicleProfile.impassableDepthCm) {
      return { verdict: 'IMPASSABLE', color: '#ef4444' };
    }
    if (summary.max_depth_cm >= vehicleProfile.cautionDepthCm) {
      return { verdict: 'CAUTION', color: '#eab308' };
    }
    if (summary.worst_severity === 'CONGESTED' || summary.worst_severity === 'HEAVY') {
      return { verdict: 'CAUTION', color: '#eab308' };
    }
    if (summary.verified_cameras > 0) {
      return { verdict: 'PASSABLE', color: '#22c55e' };
    }
  }
  return { verdict: summary.passability_verdict, color: summary.status_color };
}

function resolveCandidateVerdict(
  summary: CorridorRouteSummary,
  vehicleType: VehicleType
): PassabilityVerdict {
  return resolveEffectivePassability(summary, vehicleType).verdict;
}

function isStrictlySaferDetour(
  badVerdict: PassabilityVerdict,
  badSummary: CorridorRouteSummary,
  candVerdict: PassabilityVerdict,
  candSummary: CorridorRouteSummary
): boolean {
  if (candVerdict === 'PASSABLE') return true;
  if (badVerdict !== 'IMPASSABLE' || candVerdict !== 'CAUTION') return false;
  if (badSummary.max_depth_cm != null && candSummary.max_depth_cm != null) {
    return candSummary.max_depth_cm < badSummary.max_depth_cm;
  }
  return (badSummary.max_depth_cm ?? 0) > 0;
}

function buildDetourSuggestions(
  hazardousCorridorKeys: string[],
  validKeys: string[],
  summariesMap: Record<string, CorridorRouteSummary>,
  evaluatedSummaryByKey: Record<string, CorridorRouteSummary>,
  vehicleType: VehicleType
): DetourSuggestion[] {
  if (hazardousCorridorKeys.length === 0) return [];

  const validSet = new Set(validKeys);
  const orderedBad = [...hazardousCorridorKeys].sort((a, b) => {
    const sa = evaluatedSummaryByKey[a];
    const sb = evaluatedSummaryByKey[b];
    const rankDiff = passabilityRank(sb.passability_verdict) - passabilityRank(sa.passability_verdict);
    if (rankDiff !== 0) return rankDiff;
    return (sb.max_depth_cm ?? 0) - (sa.max_depth_cm ?? 0);
  });

  const bestByPair = new Map<string, DetourSuggestion>();
  const recordCandidate = (cand: DetourSuggestion) => {
    const dedupeKey = `${cand.original_corridor_key}->${cand.detour_corridor_key}`;
    const existing = bestByPair.get(dedupeKey);
    if (!existing) {
      bestByPair.set(dedupeKey, cand);
      return;
    }
    if (Boolean(cand.is_dry_route) !== Boolean(existing.is_dry_route)) {
      if (cand.is_dry_route) bestByPair.set(dedupeKey, cand);
      return;
    }
    const hopDiff = (cand.detour_path?.length ?? 1) - (existing.detour_path?.length ?? 1);
    if (hopDiff !== 0) {
      if (hopDiff < 0) bestByPair.set(dedupeKey, cand);
      return;
    }
    const depthDiff = (cand.max_depth_cm ?? 0) - (existing.max_depth_cm ?? 0);
    if (depthDiff !== 0) {
      if (depthDiff < 0) bestByPair.set(dedupeKey, cand);
      return;
    }
    if ((cand.estimated_delta_seconds ?? 0) < (existing.estimated_delta_seconds ?? 0)) {
      bestByPair.set(dedupeKey, cand);
    }
  };

  for (const badKey of orderedBad) {
    const badSummary = evaluatedSummaryByKey[badKey];
    const badName = CORRIDOR_NAMES[badKey] || badKey;

    for (const c1 of CORRIDOR_DETOUR_MAP[badKey] || []) {
      if (validSet.has(c1)) continue;
      const sum1 = summariesMap[c1];
      if (!sum1) continue;

      const cand1Verdict = resolveCandidateVerdict(sum1, vehicleType);
      const cand1Name = CORRIDOR_NAMES[c1] || sum1.display_name || c1;
      const safer1 = isStrictlySaferDetour(badSummary.passability_verdict, badSummary, cand1Verdict, sum1);

      if (safer1) {
        const isDry = cand1Verdict === 'PASSABLE';
        recordCandidate({
          original_corridor_key: badKey,
          original_corridor_name: badName,
          detour_corridor_key: c1,
          detour_corridor_name: cand1Name,
          detour_path: [c1],
          detour_path_names: [cand1Name],
          passability_verdict: cand1Verdict,
          max_depth_cm: sum1.max_depth_cm,
          delay_seconds: sum1.delay_seconds,
          estimated_delta_seconds: sum1.estimated_seconds - badSummary.estimated_seconds,
          is_dry_route: isDry,
          reason: {
            vi: isDry
              ? `Tránh điểm ngập trên ${badName}; ${cand1Name} khô ráo, thông thoáng.`
              : `Tránh điểm ngập sâu trên ${badName}; ${cand1Name} ít ngập hơn.`,
            en: isDry
              ? `Avoid water hazard on ${badName}; ${cand1Name} is dry and passable.`
              : `Avoid deep flood on ${badName}; ${cand1Name} has lower hazard.`,
          },
        });
      }

      if (!safer1 && cand1Verdict !== 'PASSABLE') continue;
      for (const c2 of CORRIDOR_DETOUR_MAP[c1] || []) {
        if (c2 === badKey || c2 === c1 || validSet.has(c2)) continue;
        const sum2 = summariesMap[c2];
        if (!sum2) continue;

        const cand2Verdict = resolveCandidateVerdict(sum2, vehicleType);
        if (cand2Verdict !== 'PASSABLE') continue;

        const cand2Name = CORRIDOR_NAMES[c2] || sum2.display_name || c2;
        const pathVerdict: PassabilityVerdict = cand1Verdict === 'PASSABLE' ? 'PASSABLE' : cand1Verdict;
        const isDryPath = pathVerdict === 'PASSABLE';
        const d1 = sum1.max_depth_cm ?? null;
        const d2 = sum2.max_depth_cm ?? null;
        const maxDepth = d1 === null && d2 === null ? null : Math.max(d1 ?? 0, d2 ?? 0);

        recordCandidate({
          original_corridor_key: badKey,
          original_corridor_name: badName,
          detour_corridor_key: c2,
          detour_corridor_name: cand2Name,
          detour_path: [c1, c2],
          detour_path_names: [cand1Name, cand2Name],
          passability_verdict: pathVerdict,
          max_depth_cm: maxDepth,
          delay_seconds: sum1.delay_seconds + sum2.delay_seconds,
          estimated_delta_seconds: sum1.estimated_seconds + sum2.estimated_seconds - badSummary.estimated_seconds,
          is_dry_route: isDryPath,
          reason: {
            vi: `Lộ trình vòng 2 chặng tránh ${badName}: qua ${cand1Name} rồi ${cand2Name}.`,
            en: `2-segment bypass around ${badName}: via ${cand1Name} then ${cand2Name}.`,
          },
        });
      }
    }
  }

  return Array.from(bestByPair.values())
    .sort((a, b) => {
      if (Boolean(a.is_dry_route) !== Boolean(b.is_dry_route)) {
        return a.is_dry_route ? -1 : 1;
      }
      const hopDiff = (a.detour_path?.length ?? 1) - (b.detour_path?.length ?? 1);
      if (hopDiff !== 0) return hopDiff;
      const depthDiff = (a.max_depth_cm ?? 0) - (b.max_depth_cm ?? 0);
      if (depthDiff !== 0) return depthDiff;
      const deltaDiff = (a.estimated_delta_seconds ?? 0) - (b.estimated_delta_seconds ?? 0);
      if (deltaDiff !== 0) return deltaDiff;
      return `${a.original_corridor_key}:${a.detour_corridor_key}`.localeCompare(
        `${b.original_corridor_key}:${b.detour_corridor_key}`
      );
    })
    .slice(0, MAX_DETOUR_SUGGESTIONS);
}

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
  const evaluatedSummaryByKey: Record<string, CorridorRouteSummary> = {};

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
    const { verdict: effectiveVerdict, color: effectiveColor } = resolveEffectivePassability(summary, vehicleType);

    if (passabilityRank(effectiveVerdict) > passabilityRank(worstVerdict)) {
      worstVerdict = effectiveVerdict;
      worstStatusColor = effectiveColor;
    }

    evaluatedSummaryByKey[key] = {
      ...summary,
      passability_verdict: effectiveVerdict,
      status_color: effectiveColor,
    };

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

  const detours = buildDetourSuggestions(
    hazardousCorridorKeys,
    validKeys,
    summariesMap,
    evaluatedSummaryByKey,
    vehicleType
  );
  const detour: DetourSuggestion | null = detours[0] || null;

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
    detours,
    vehicle: vehicleType,
  };
}

export function formatDetourDelta(deltaSeconds: number, lang: 'vi' | 'en'): string {
  const deltaMins = Math.round(deltaSeconds / 60);
  if (deltaMins === 0) {
    return lang === 'vi' ? 'Tương đương' : 'Same ETA';
  }
  const unit = lang === 'vi' ? 'phút' : 'mins';
  return deltaMins > 0 ? `+${deltaMins} ${unit}` : `${deltaMins} ${unit}`;
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
