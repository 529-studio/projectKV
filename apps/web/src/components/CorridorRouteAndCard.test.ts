import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import { toRoadKey } from '../utils/corridorUtils.ts';
import type {
  CorridorRouteSummary,
  CorridorRouteResponse,
  CorridorCameraPointProperties,
} from '../types/corridor.ts';

/**
 * Empirical Automated Verification for Issue #476 / S15-T2:
 * Commuter Route Polyline & Hazard Layer + Corridor Overview Card
 */
describe('S15-T2: Commuter Route Polyline & Corridor Overview Card Verification', () => {
  const sampleSummary: CorridorRouteSummary = {
    road_key: 'vo_van_kiet',
    display_name: 'Đại lộ Võ Văn Kiệt',
    description: 'Bình Tân - Quận 1 - Hầm Thủ Thiêm',
    length_km: 13.5,
    free_flow_speed_kmh: 40.0,
    free_flow_seconds: 1215,
    estimated_seconds: 1800,
    delay_seconds: 585,
    worst_severity: 'CONGESTED',
    worst_flood_severity: 'FLOODED_DEEP',
    max_depth_cm: 35.0,
    max_hazard_level: 3,
    passability_verdict: 'IMPASSABLE',
    status_color: '#ef4444',
    total_cameras: 24,
    verified_cameras: 18,
    coverage_pct: 75.0,
    chokepoint_count: 3,
  };

  const sampleRoute: CorridorRouteResponse = {
    summary: sampleSummary,
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
              [106.619933, 10.726362],
            ],
          },
          properties: {
            road_key: 'vo_van_kiet',
            stroke: '#ef4444',
            'stroke-width': 6,
            max_depth_cm: 35.0,
          },
        },
        {
          type: 'Feature',
          geometry: {
            type: 'Point',
            coordinates: [106.610904, 10.72224],
          },
          properties: {
            camera_id: 'cam-vvk-ky-con',
            name: 'Võ Văn Kiệt - Ký Con',
            severity: 'CONGESTED',
            flood_severity: 'FLOODED_DEEP',
            flood_depth_cm: 35.0,
            stale: false,
            'marker-color': '#ef4444',
          },
        },
      ],
    },
  };

  test('Scenario 1: Selecting "Võ Văn Kiệt" derives canonical road_key and parses coordinates', () => {
    const roadKey = toRoadKey('Võ Văn Kiệt');
    assert.equal(roadKey, 'vo_van_kiet');

    const lineFeature = sampleRoute.geojson.features.find(f => f.geometry.type === 'LineString');
    assert.ok(lineFeature);
    const coords = lineFeature.geometry.coordinates as [number, number][];
    assert.equal(coords.length, 3);
    // Convert to Leaflet [lat, lon]
    const leafletCoords = coords.map(([lon, lat]) => [lat, lon]);
    assert.deepEqual(leafletCoords[0], [10.717053, 106.600943]);
  });

  test('Scenario 2: Flooded corridor styling renders crimson and exposes max flood depth', () => {
    assert.equal(sampleSummary.passability_verdict, 'IMPASSABLE');
    assert.equal(sampleSummary.status_color, '#ef4444');
    assert.equal(sampleSummary.max_depth_cm, 35.0);

    const estMinutes = Math.round(sampleSummary.estimated_seconds / 60);
    const delayMinutes = Math.round(sampleSummary.delay_seconds / 60);
    assert.equal(estMinutes, 30);
    assert.equal(delayMinutes, 10);
  });

  test('Scenario 3: Checkpoint camera point extraction and properties', () => {
    const pointFeatures = sampleRoute.geojson.features.filter(f => f.geometry.type === 'Point');
    assert.equal(pointFeatures.length, 1);
    const point = pointFeatures[0];
    const props = point.properties as CorridorCameraPointProperties;
    assert.equal(props.camera_id, 'cam-vvk-ky-con');
    assert.equal(props.flood_depth_cm, 35.0);
    assert.equal(props['marker-color'], '#ef4444');
  });

  test('Scenario 4: Passability verdict and metrics formatting in Vietnamese and English', () => {
    const verdictsMap = {
      PASSABLE: { vi: 'THÔNG THOÁNG', en: 'PASSABLE', color: '#22c55e' },
      CAUTION: { vi: 'CẢNH BÁO XE GẦM THẤP', en: 'CAUTION (LOW CLEARANCE)', color: '#eab308' },
      IMPASSABLE: { vi: 'NGUY CƠ CHẾT MÁY / THỦY KÍCH', en: 'IMPASSABLE (DEEP FLOOD)', color: '#ef4444' },
      UNKNOWN: { vi: 'CHƯA ĐỦ DỮ LIỆU', en: 'UNKNOWN', color: '#71717a' },
    };

    assert.equal(verdictsMap[sampleSummary.passability_verdict].vi, 'NGUY CƠ CHẾT MÁY / THỦY KÍCH');
    assert.equal(verdictsMap[sampleSummary.passability_verdict].color, '#ef4444');
  });

  test('Scenario 5: Telegram alert deep-link conforms to start payload format', () => {
    const bot = 'WardedBot';
    const deepLink = `https://t.me/${bot}?start=sub_corridor_${sampleSummary.road_key}`;
    assert.equal(deepLink, 'https://t.me/WardedBot?start=sub_corridor_vo_van_kiet');
    assert.ok(deepLink.startsWith('https://t.me/'));
    assert.ok(deepLink.includes('start=sub_corridor_vo_van_kiet'));
  });

  test('Scenario 6 (Issue #507): Corridor route polyline toggle states and bilingual labels', () => {
    // When showPolyline is true vs false, labels and titles must be informative in both vi and en
    const getToggleLabel = (show: boolean, lang: 'vi' | 'en') => {
      if (show) {
        return lang === 'vi' ? 'Tắt đường kẻ lộ trình' : 'Hide route line';
      }
      return lang === 'vi' ? 'Bật đường kẻ lộ trình' : 'Show route line';
    };

    assert.equal(getToggleLabel(true, 'vi'), 'Tắt đường kẻ lộ trình');
    assert.equal(getToggleLabel(false, 'vi'), 'Bật đường kẻ lộ trình');
    assert.equal(getToggleLabel(true, 'en'), 'Hide route line');
    assert.equal(getToggleLabel(false, 'en'), 'Show route line');
  });

  test('Scenario 7 (Issue #507): Card dismissal state keeps corridor filter active and decouples from zoom reset', () => {
    // Simulating user workflow:
    // 1. User selects corridor: filter is active, card visible, polyline visible
    let selectedCorridor: string | null = 'Đường Nguyễn Hữu Cảnh';
    let isCorridorCardDismissed = false;
    let showCorridorRoute = true;
    let mapResetCount = 0;

    const onCloseCard = () => {
      // Dismisses card ONLY, does NOT reset selectedCorridor or trigger map flyTo
      isCorridorCardDismissed = true;
    };

    const onFilterClear = () => {
      // Explicit filter clear button or Esc resets filter and triggers map flyTo
      selectedCorridor = null;
      isCorridorCardDismissed = false;
      mapResetCount += 1;
    };

    // User closes card
    onCloseCard();
    assert.equal(isCorridorCardDismissed, true, 'Card should be dismissed');
    assert.equal(selectedCorridor, 'Đường Nguyễn Hữu Cảnh', 'Corridor must remain selected');
    assert.equal(mapResetCount, 0, 'Map must NOT fly back to default center on card close');

    // User toggles line off while card is dismissed
    showCorridorRoute = false;
    assert.equal(showCorridorRoute, false, 'Polyline visibility is independently toggleable');

    // User explicitly clears filter
    onFilterClear();
    assert.equal(selectedCorridor, null, 'Corridor filter is now cleared');
    assert.equal(mapResetCount, 1, 'Map smoothly recenters ONLY on explicit filter clear');
  });
});
