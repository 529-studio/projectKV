import test from 'node:test';
import assert from 'node:assert/strict';
import {
  fetchCorridorRoute,
  fetchCorridorsOverview,
  getCachedCorridorRoute,
  setCachedCorridorRoute,
  clearCorridorRouteCache,
} from './useCorridorRoute.ts';
import type { CorridorRouteResponse, CorridorRouteSummary } from '../types/corridor.ts';

const sampleRoute: CorridorRouteResponse = {
  summary: {
    road_key: 'vo_van_kiet',
    display_name: 'Đại lộ Võ Văn Kiệt',
    description: 'Bình Tân - Quận 1 - Hầm Thủ Thiêm',
    length_km: 13.5,
    free_flow_speed_kmh: 40.0,
    free_flow_seconds: 1215,
    estimated_seconds: 1500,
    delay_seconds: 285,
    worst_severity: 'CONGESTED',
    worst_flood_severity: 'DRY',
    max_depth_cm: 0,
    passability_verdict: 'CAUTION',
    status_color: '#eab308',
    total_cameras: 24,
    verified_cameras: 18,
    coverage_pct: 75.0,
    chokepoint_count: 2,
  },
  geojson: {
    type: 'FeatureCollection',
    features: [
      {
        type: 'Feature',
        geometry: {
          type: 'LineString',
          coordinates: [
            [106.600943, 10.717053],
            [106.610904, 10.72224],
          ],
        },
        properties: {
          road_key: 'vo_van_kiet',
          stroke: '#eab308',
          'stroke-width': 6,
        },
      },
    ],
  },
};

const sampleOverview: CorridorRouteSummary[] = [
  sampleRoute.summary,
  {
    road_key: 'mai_chi_tho',
    display_name: 'Đường Mai Chí Thọ',
    description: 'Hầm Thủ Thiêm - Nút giao An Phú',
    length_km: 6.5,
    free_flow_speed_kmh: 45.0,
    free_flow_seconds: 520,
    estimated_seconds: 520,
    delay_seconds: 0,
    worst_severity: 'CLEAR',
    worst_flood_severity: 'DRY',
    max_depth_cm: 0,
    passability_verdict: 'PASSABLE',
    status_color: '#22c55e',
    total_cameras: 14,
    verified_cameras: 12,
    coverage_pct: 85.7,
    chokepoint_count: 0,
  },
];

test('useCorridorRoute cache operations and case-insensitivity', () => {
  clearCorridorRouteCache();

  assert.equal(getCachedCorridorRoute('vo_van_kiet'), null);

  setCachedCorridorRoute('vo_van_kiet', sampleRoute);

  const cached = getCachedCorridorRoute('vo_van_kiet');
  assert.ok(cached);
  assert.equal(cached.summary.road_key, 'vo_van_kiet');
  assert.equal(cached.summary.passability_verdict, 'CAUTION');

  // Case-insensitive
  const cachedUpper = getCachedCorridorRoute('VO_VAN_KIET');
  assert.ok(cachedUpper);
  assert.equal(cachedUpper.summary.status_color, '#eab308');

  clearCorridorRouteCache();
  assert.equal(getCachedCorridorRoute('vo_van_kiet'), null);
});

test('fetchCorridorRoute network call, caching, and 404 handling', async () => {
  clearCorridorRouteCache();
  const originalFetch = globalThis.fetch;
  let fetchCallCount = 0;

  globalThis.fetch = async (url: string | URL | Request) => {
    fetchCallCount++;
    const urlStr = String(url);
    if (urlStr.includes('/api/roads/vo_van_kiet/route')) {
      return {
        ok: true,
        status: 200,
        json: async () => sampleRoute,
      } as Response;
    }
    if (urlStr.includes('/api/corridors/overview')) {
      return {
        ok: true,
        status: 200,
        json: async () => sampleOverview,
      } as Response;
    }
    return {
      ok: false,
      status: 404,
      json: async () => ({ error: 'not found' }),
    } as Response;
  };

  try {
    // 1. Fetch route
    const r1 = await fetchCorridorRoute('vo_van_kiet');
    assert.ok(r1);
    assert.equal(r1.summary.road_key, 'vo_van_kiet');
    assert.equal(r1.geojson.features.length, 1);
    assert.equal(fetchCallCount, 1);

    // 2. Cached route hit
    const r2 = await fetchCorridorRoute('vo_van_kiet');
    assert.ok(r2);
    assert.equal(fetchCallCount, 1);

    // 3. 404 route
    const rUnknown = await fetchCorridorRoute('unknown_corridor');
    assert.equal(rUnknown, null);
    assert.equal(fetchCallCount, 2);

    // 4. Overviews call
    const overviews = await fetchCorridorsOverview();
    assert.equal(overviews.length, 2);
    assert.equal(overviews[0].road_key, 'vo_van_kiet');
    assert.equal(overviews[1].road_key, 'mai_chi_tho');
    assert.equal(fetchCallCount, 3);
  } finally {
    globalThis.fetch = originalFetch;
    clearCorridorRouteCache();
  }
});
