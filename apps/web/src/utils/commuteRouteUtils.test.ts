/**
 * Unit Tests for Commute Route Utilities, Bottleneck Isolation & Detour Suggestions.
 * Verifies S17-T4 / Issue #512 Acceptance Criteria.
 */

import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
  evaluateCommuteRoute,
  findConnectingCorridors,
  formatCommuteDuration,
  formatCommuteDelay,
  CORRIDOR_DETOUR_MAP,
} from './commuteRouteUtils.ts';
import type { CorridorRouteSummary } from '../types/corridor.ts';

describe('S17-T4 / Issue #512: Commuter Journey Glance & Detour Recommendation', () => {
  const sampleSummaries: Record<string, CorridorRouteSummary> = {
    nguyen_huu_canh: {
      road_key: 'nguyen_huu_canh',
      display_name: 'Đường Nguyễn Hữu Cảnh',
      description: 'Tôn Đức Thắng - Cầu Sài Gòn',
      length_km: 3.2,
      free_flow_speed_kmh: 35.0,
      free_flow_seconds: 329,
      estimated_seconds: 689,
      delay_seconds: 360,
      worst_severity: 'CONGESTED',
      worst_flood_severity: 'FLOODED_DEEP',
      max_depth_cm: 25.0,
      max_hazard_level: 3,
      passability_verdict: 'IMPASSABLE',
      status_color: '#ef4444',
      total_cameras: 8,
      verified_cameras: 6,
      coverage_pct: 75.0,
      chokepoint_count: 2,
    },
    dien_bien_phu: {
      road_key: 'dien_bien_phu',
      display_name: 'Đường Điện Biên Phủ',
      description: 'Ngã 7 Lý Thái Tổ - Cầu Sài Gòn',
      length_km: 6.8,
      free_flow_speed_kmh: 35.0,
      free_flow_seconds: 699,
      estimated_seconds: 750,
      delay_seconds: 51,
      worst_severity: 'CLEAR',
      worst_flood_severity: 'DRY',
      max_depth_cm: 0.0,
      max_hazard_level: 0,
      passability_verdict: 'PASSABLE',
      status_color: '#22c55e',
      total_cameras: 12,
      verified_cameras: 10,
      coverage_pct: 83.3,
      chokepoint_count: 0,
    },
    vo_van_kiet: {
      road_key: 'vo_van_kiet',
      display_name: 'Đại lộ Võ Văn Kiệt',
      description: 'Bình Tân - Quận 1',
      length_km: 13.5,
      free_flow_speed_kmh: 40.0,
      free_flow_seconds: 1215,
      estimated_seconds: 1300,
      delay_seconds: 85,
      worst_severity: 'CLEAR',
      worst_flood_severity: 'DRY',
      max_depth_cm: 0.0,
      max_hazard_level: 0,
      passability_verdict: 'PASSABLE',
      status_color: '#22c55e',
      total_cameras: 20,
      verified_cameras: 18,
      coverage_pct: 90.0,
      chokepoint_count: 0,
    },
    nguyen_van_linh: {
      road_key: 'nguyen_van_linh',
      display_name: 'Đại lộ Nguyễn Văn Linh',
      description: 'Quốc lộ 1 - Phú Mỹ Hưng',
      length_km: 17.8,
      free_flow_speed_kmh: 45.0,
      free_flow_seconds: 1424,
      estimated_seconds: 1500,
      delay_seconds: 76,
      worst_severity: 'CLEAR',
      worst_flood_severity: 'DRY',
      max_depth_cm: 0.0,
      max_hazard_level: 0,
      passability_verdict: 'PASSABLE',
      status_color: '#22c55e',
      total_cameras: 15,
      verified_cameras: 14,
      coverage_pct: 93.3,
      chokepoint_count: 0,
    },
  };

  it('Scenario 1: Evaluates multi-corridor journey distance and duration summation', () => {
    const evalResult = evaluateCommuteRoute({
      corridorKeys: ['nguyen_van_linh', 'vo_van_kiet'],
      vehicleType: 'scooter',
      corridorSummaries: sampleSummaries,
    });

    assert.equal(evalResult.passability, 'PASSABLE');
    assert.equal(evalResult.status_color, '#22c55e');
    assert.equal(evalResult.total_distance_km, 31.3); // 17.8 + 13.5
    assert.equal(evalResult.free_flow_seconds, 2639);
    assert.equal(evalResult.delay_seconds, 161);
    assert.equal(evalResult.worst_bottleneck, null);
    assert.equal(evalResult.detour, null);
  });

  it('Scenario 2: Bottleneck isolation: identifies deepest flood checkpoint on route', () => {
    const evalResult = evaluateCommuteRoute({
      corridorKeys: ['nguyen_huu_canh'],
      vehicleType: 'scooter',
      corridorSummaries: sampleSummaries,
    });

    assert.equal(evalResult.passability, 'IMPASSABLE');
    assert.equal(evalResult.status_color, '#ef4444');
    assert.ok(evalResult.worst_bottleneck !== null);
    assert.equal(evalResult.worst_bottleneck.corridor_key, 'nguyen_huu_canh');
    assert.equal(evalResult.worst_bottleneck.flood_depth_cm, 25.0);
    assert.match(evalResult.worst_bottleneck.description.vi, /Đoạn ngập sâu nhất: Đường Nguyễn Hữu Cảnh: 25 cm/);
    assert.match(evalResult.worst_bottleneck.description.vi, /Nguy cơ chết máy/);
  });

  it('Scenario 3: Vehicle clearance threshold adaptation on flooded corridor', () => {
    // 25 cm flood:
    // Scooter (impassable threshold: 20 cm) => IMPASSABLE
    const scooterEval = evaluateCommuteRoute({
      corridorKeys: ['nguyen_huu_canh'],
      vehicleType: 'scooter',
      corridorSummaries: sampleSummaries,
    });
    assert.equal(scooterEval.passability, 'IMPASSABLE');

    // Motorbike (caution threshold: 15 cm, impassable: 28 cm) => CAUTION
    const motorbikeEval = evaluateCommuteRoute({
      corridorKeys: ['nguyen_huu_canh'],
      vehicleType: 'motorbike',
      corridorSummaries: sampleSummaries,
    });
    assert.equal(motorbikeEval.passability, 'CAUTION');

    // SUV (caution threshold: 25 cm, impassable: 40 cm) => CAUTION at 25cm
    const suvEval = evaluateCommuteRoute({
      corridorKeys: ['nguyen_huu_canh'],
      vehicleType: 'suv',
      corridorSummaries: sampleSummaries,
    });
    assert.equal(suvEval.passability, 'CAUTION');
  });

  it('Scenario 4: Detour recommendation: recommends Điện Biên Phủ when Nguyễn Hữu Cảnh is flooded', () => {
    const evalResult = evaluateCommuteRoute({
      corridorKeys: ['nguyen_huu_canh'],
      vehicleType: 'scooter',
      corridorSummaries: sampleSummaries,
    });

    assert.ok(evalResult.detour !== null);
    assert.equal(evalResult.detour.original_corridor_key, 'nguyen_huu_canh');
    assert.equal(evalResult.detour.detour_corridor_key, 'dien_bien_phu');
    assert.equal(evalResult.detour.passability_verdict, 'PASSABLE');
    assert.match(evalResult.detour.reason.vi, /Tránh điểm ngập trên Đường Nguyễn Hữu Cảnh/);
    assert.match(evalResult.detour.reason.vi, /Đường Điện Biên Phủ khô ráo/);
  });

  it('Scenario 5: Spatial corridor interpolation from coordinates', () => {
    // Landmark 81 (10.795, 106.721) to Ben Nghe (10.776, 106.703)
    const corridors = findConnectingCorridors(
      { lat: 10.795, lng: 106.721 },
      { lat: 10.776, lng: 106.703 }
    );
    assert.ok(corridors.length > 0);
    assert.ok(corridors.includes('nguyen_huu_canh') || corridors.includes('dien_bien_phu'));
  });

  it('Scenario 6: Bilingual duration and delay formatting compliance', () => {
    assert.equal(formatCommuteDuration(1440, 'vi'), '24 phút');
    assert.equal(formatCommuteDuration(1440, 'en'), '24 mins');
    assert.equal(formatCommuteDuration(30, 'vi'), '< 1 phút');

    assert.equal(formatCommuteDelay(480, 'vi'), '+8 phút trễ');
    assert.equal(formatCommuteDelay(480, 'en'), '+8 mins delay');
    assert.equal(formatCommuteDelay(0, 'vi'), 'Đúng giờ');
    assert.equal(formatCommuteDelay(0, 'en'), 'On time');
  });

  it('Scenario 7: Canonical detour graph completeness', () => {
    assert.deepEqual(CORRIDOR_DETOUR_MAP.nguyen_huu_canh, ['dien_bien_phu']);
    assert.deepEqual(CORRIDOR_DETOUR_MAP.vo_van_kiet, ['nguyen_van_linh']);
    assert.ok(CORRIDOR_DETOUR_MAP.cong_hoa.includes('pham_van_dong'));
  });

  it('Scenario 8: Commute preset routes completeness', async () => {
    const { COMMUTE_PRESETS } = await import('./commuteStorage.ts');
    assert.ok(COMMUTE_PRESETS.length >= 4);
    const nhcPreset = COMMUTE_PRESETS.find(p => p.id === 'binh_thanh_q1');
    assert.ok(nhcPreset !== undefined);
    assert.deepEqual(nhcPreset.corridors, ['nguyen_huu_canh']);
  });
});
