/**
 * Tests for the commuter-map marker clustering helpers (S14-T3 / Issue #443).
 *
 * Contract under test:
 *  - cameras aggregate below zoom 14 and expand to individual markers at 14+;
 *  - the badge bubbles up the worst hazard level so a Level 3 flood is never
 *    hidden by aggregation;
 *  - Level 3 draws a red danger ring, Level 2 amber, everything else dark zinc;
 *  - the grid is deterministic, so SSE ticks cannot make badges swap places;
 *  - a camera with an active crowdsourced report is never absorbed into a badge.
 */

import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  CLUSTER_COLLISION_PADDING_PX,
  CLUSTER_GRID_PX,
  CLUSTER_MIN_DIAMETER_PX,
  CLUSTER_MAX_DIAMETER_PX,
  CLUSTER_ZOOM_THRESHOLD,
  DEGENERATE_BOUNDS_PAD_DEG,
  buildCameraClusters,
  getAdaptiveGridPx,
  getClusterDiameter,
  getClusterHazardVisual,
  lngToWorldX,
  latToWorldY,
  shouldClusterMarkers,
  worstHazardLevel,
  type ClusterPoint,
} from './mapClustering.ts';
import { MAP_CLUSTER_I18N, MAP_TOOLTIP_I18N, MAP_CONFIG, CARTO_DARK_MATTER } from '../config/constants.ts';

const VI_DIACRITICS = /[ăâđêôơưàáảãạằắẳẵặầấẩẫậèéẻẽẹềếểễệìíỉĩịòóỏõọồốổỗộờớởỡợùúủũụừứửữựỳýỷỹỵ]/iu;
const EMOJI = /\p{Extended_Pictographic}/u;
const VARIATION_SELECTOR = /\uFE0F/u;

/** Two cameras ~11 m apart on Nguyễn Huệ, District 1. */
const NEAR_A: ClusterPoint = { id: 'cam-a', latitude: 10.7743, longitude: 106.7035 };
const NEAR_B: ClusterPoint = { id: 'cam-b', latitude: 10.7744, longitude: 106.7035 };
/** ~11 km away, a separate neighbourhood. */
const FAR: ClusterPoint = { id: 'cam-far', latitude: 10.8743, longitude: 106.7035 };

const flat = (result: ReturnType<typeof buildCameraClusters>) =>
  result.clusters.map((cluster) => ({
    key: cluster.key,
    count: cluster.count,
    ids: cluster.cameraIds,
    hazard: cluster.hazardLevel,
  }));

describe('projection', () => {
  test('longitude maps linearly into world pixels', () => {
    assert.equal(lngToWorldX(-180, 0), 0);
    assert.equal(lngToWorldX(0, 0), 128);
    assert.equal(lngToWorldX(180, 0), 256);
    assert.equal(lngToWorldX(0, 2), 512);
  });

  test('latitude is Mercator-projected and clamped at the poles', () => {
    assert.equal(latToWorldY(0, 0), 128);
    assert.ok(latToWorldY(10.77, 12) < latToWorldY(-10.77, 12));
    assert.ok(Number.isFinite(latToWorldY(90, 12)));
    assert.ok(Number.isFinite(latToWorldY(-90, 12)));
  });
});

describe('buildCameraClusters — zoom threshold (spec §2)', () => {
  test('nearby cameras collapse into one count badge below zoom 14', () => {
    const result = buildCameraClusters([NEAR_A, NEAR_B], { zoom: 12 });
    assert.equal(result.clustered, true);
    assert.equal(result.clusters.length, 1);
    assert.equal(result.clusters[0].count, 2);
    assert.deepEqual(result.clusters[0].cameraIds.sort(), ['cam-a', 'cam-b']);
  });

  test('zoom 14 expands every camera into its own marker', () => {
    const result = buildCameraClusters([NEAR_A, NEAR_B], { zoom: CLUSTER_ZOOM_THRESHOLD });
    assert.equal(result.clustered, false);
    assert.equal(result.clusters.length, 2);
    for (const cluster of result.clusters) {
      assert.equal(cluster.count, 1);
      assert.equal(cluster.cameraIds.length, 1);
    }
  });

  test('the threshold is exclusive on the zoom value itself', () => {
    assert.equal(buildCameraClusters([NEAR_A, NEAR_B], { zoom: 13.99 }).clustered, true);
    assert.equal(buildCameraClusters([NEAR_A, NEAR_B], { zoom: 14 }).clustered, false);
    assert.equal(buildCameraClusters([NEAR_A, NEAR_B], { zoom: 14.01 }).clustered, false);
  });

  test('cameras in different cells stay separate', () => {
    const result = buildCameraClusters([NEAR_A, FAR], { zoom: 11 });
    assert.equal(result.clusters.length, 2);
    assert.deepEqual(result.clusters.map((c) => c.count), [1, 1]);
  });

  test('a lower zoom merges what a higher zoom separates', () => {
    const mid: ClusterPoint = { id: 'cam-mid', latitude: 10.7843, longitude: 106.7035 };
    assert.equal(buildCameraClusters([NEAR_A, mid], { zoom: 11 }).clusters.length, 1);
    assert.equal(buildCameraClusters([NEAR_A, mid], { zoom: 13 }).clusters.length, 2);
  });

  test('the flood lens can force individual markers at city zoom', () => {
    const result = buildCameraClusters([NEAR_A, NEAR_B], { zoom: 11, enabled: false });
    assert.equal(result.clustered, false);
    assert.equal(result.clusters.length, 2);
  });

  test('the grid cell width is configurable', () => {
    // Far apart at every zoom, so only an enormous cell can merge them.
    assert.equal(buildCameraClusters([NEAR_A, FAR], { zoom: 12 }).clusters.length, 2);
    assert.equal(
      buildCameraClusters([NEAR_A, FAR], { zoom: 12, gridPx: 1_000_000_000 }).clusters.length,
      1,
    );
  });
});

describe('buildCameraClusters — determinism and invalid input', () => {
  test('input order never changes badge identity or order', () => {
    const a = buildCameraClusters([NEAR_A, NEAR_B, FAR], { zoom: 11 });
    const b = buildCameraClusters([FAR, NEAR_B, NEAR_A], { zoom: 11 });
    assert.deepEqual(flat(a), flat(b));
  });

  test('a camera without a finite position is dropped, never clustered at 0,0', () => {
    const broken: ClusterPoint[] = [
      { id: 'nan', latitude: Number.NaN, longitude: 106.7 },
      { id: 'inf', latitude: 10.7, longitude: Number.POSITIVE_INFINITY },
      { id: 'out-of-range', latitude: 95, longitude: 106.7 },
      { id: '', latitude: 10.7, longitude: 106.7 },
    ];
    const result = buildCameraClusters([...broken, NEAR_A], { zoom: 10 });
    assert.deepEqual(result.clusters.map((c) => c.cameraIds).flat(), ['cam-a']);
  });

  test('an empty network is an empty cluster list', () => {
    assert.deepEqual(buildCameraClusters([], { zoom: 10 }), { clustered: true, clusters: [] });
  });
});

describe('hazard bubble-up (spec §2, CRITICAL SAFETY)', () => {
  test('the worst level wins regardless of position or id order', () => {
    const levels: Record<string, 1 | 2 | 3> = { 'cam-a': 1, 'cam-b': 3, 'cam-c': 2 };
    const result = buildCameraClusters([NEAR_A, NEAR_B], {
      zoom: 12,
      levelFor: (id) => levels[id] ?? null,
    });
    assert.equal(result.clusters[0].hazardLevel, 3);
  });

  test('Level 2 is reported for a cluster of Level 1 and Level 2', () => {
    const levels: Record<string, 1 | 2> = { 'cam-a': 1, 'cam-b': 2 };
    const result = buildCameraClusters([NEAR_A, NEAR_B], {
      zoom: 12,
      levelFor: (id) => levels[id] ?? null,
    });
    assert.equal(result.clusters[0].hazardLevel, 2);
  });

  test('an unknown hazard is null, never Level 1', () => {
    const result = buildCameraClusters([NEAR_A, NEAR_B], { zoom: 12, levelFor: () => null });
    assert.equal(result.clusters[0].hazardLevel, null);
    const missing = buildCameraClusters([NEAR_A, NEAR_B], { zoom: 12 });
    assert.equal(missing.clusters[0].hazardLevel, null);
  });

  test('worstHazardLevel ignores null and out-of-vocabulary values', () => {
    assert.equal(worstHazardLevel([]), null);
    assert.equal(worstHazardLevel([null, undefined]), null);
    assert.equal(worstHazardLevel([null, 2, undefined]), 2);
    assert.equal(worstHazardLevel([1, 3, 2]), 3);
    assert.equal(worstHazardLevel([0 as unknown as 1, 4 as unknown as 3]), null);
  });

  test('a camera with an active crowdsourced report is never absorbed', () => {
    const result = buildCameraClusters([NEAR_A, NEAR_B], {
      zoom: 12,
      forceIndividualIds: new Set(['cam-b']),
    });
    assert.equal(result.clustered, true);
    const solo = result.clusters.find((c) => c.cameraIds.includes('cam-b'));
    assert.ok(solo);
    assert.equal(solo.count, 1);
    const grouped = result.clusters.find((c) => c.cameraIds.includes('cam-a'));
    assert.ok(grouped);
    assert.equal(grouped.count, 1);
  });

  test('a cluster bounds every member and anchors on their centroid', () => {
    const result = buildCameraClusters([NEAR_A, NEAR_B], { zoom: 12 });
    const cluster = result.clusters[0];
    assert.ok(cluster.latitude <= 10.7744 && cluster.latitude >= 10.7743);
    assert.equal(cluster.longitude, 106.7035);
    assert.ok(cluster.bounds.south <= 10.7743);
    assert.ok(cluster.bounds.north >= 10.7744);
    assert.ok(cluster.bounds.west <= 106.7035);
    assert.ok(cluster.bounds.east >= 106.7035);
  });

  test('a single-camera bounds is padded so flyToBounds cannot jump to max zoom', () => {
    const result = buildCameraClusters([NEAR_A], { zoom: 12 });
    const { bounds } = result.clusters[0];
    assert.ok(bounds.north - bounds.south >= DEGENERATE_BOUNDS_PAD_DEG - 1e-12);
    assert.ok(bounds.east - bounds.west >= DEGENERATE_BOUNDS_PAD_DEG - 1e-12);
  });
});

describe('badge geometry', () => {
  test('diameter never drops below the 48 px touch target', () => {
    for (const count of [1, 2, 5, 1000]) {
      assert.ok(getClusterDiameter(count) >= CLUSTER_MIN_DIAMETER_PX);
    }
  });

  test('diameter grows with count and is capped for street visibility', () => {
    assert.ok(getClusterDiameter(64) > getClusterDiameter(2));
    assert.ok(getClusterDiameter(100000) <= CLUSTER_MAX_DIAMETER_PX);
    assert.equal(getClusterDiameter(-5), CLUSTER_MIN_DIAMETER_PX);
    assert.equal(getClusterDiameter(Number.NaN), CLUSTER_MIN_DIAMETER_PX);
  });
});

describe('shouldClusterMarkers', () => {
  test('the flood lens always expands', () => {
    assert.equal(shouldClusterMarkers(10, true), false);
    assert.equal(shouldClusterMarkers(13.9, true), false);
  });

  test('other lenses cluster below 14 and expand at 14+', () => {
    assert.equal(shouldClusterMarkers(10, false), true);
    assert.equal(shouldClusterMarkers(13.99, false), true);
    assert.equal(shouldClusterMarkers(14, false), false);
    assert.equal(shouldClusterMarkers(18, false), false);
  });
});

describe('getClusterHazardVisual — danger ring, amber, zinc (spec §2)', () => {
  test('Level 3 draws a red danger ring', () => {
    const visual = getClusterHazardVisual(3);
    assert.equal(visual.level, 3);
    assert.equal(visual.hasDangerRing, true);
    assert.match(visual.ringClass, /red/);
    assert.match(visual.surfaceClass, /red/);
  });

  test('Level 2 uses amber without a danger ring', () => {
    const visual = getClusterHazardVisual(2);
    assert.equal(visual.hasDangerRing, false);
    assert.match(visual.ringClass, /amber/);
    assert.doesNotMatch(visual.ringClass, /red/);
  });

  test('no hazard and Level 1 use the standard dark zinc badge', () => {
    for (const level of [null, 1] as const) {
      const visual = getClusterHazardVisual(level);
      assert.equal(visual.hasDangerRing, false);
      assert.match(visual.ringClass, /zinc/);
      assert.doesNotMatch(visual.surfaceClass, /red|amber/);
    }
  });

  test('the three hazard states are visually distinct', () => {
    const surfaces = [null, 2, 3].map((level) => getClusterHazardVisual(level as 2 | 3 | null).surfaceClass);
    assert.equal(new Set(surfaces).size, 3);
  });

  test('every visual token is emoji-free', () => {
    for (const level of [null, 1, 2, 3] as const) {
      const visual = getClusterHazardVisual(level);
      for (const token of [visual.ringClass, visual.surfaceClass, visual.indicatorClass]) {
        assert.doesNotMatch(token, EMOJI);
      }
    }
  });
});

describe('cluster configuration and copy', () => {
  test('the map config agrees with the clustering contract', () => {
    assert.equal(MAP_CONFIG.CLUSTER_ZOOM_THRESHOLD, CLUSTER_ZOOM_THRESHOLD);
    assert.equal(MAP_CONFIG.CLUSTER_ZOOM_THRESHOLD, 14);
    assert.equal(MAP_CONFIG.CLUSTER_GRID_PX, CLUSTER_GRID_PX);
    assert.ok(MAP_CONFIG.CLUSTER_GRID_PX >= CLUSTER_MIN_DIAMETER_PX);
  });

  test('the proxied tile URL and attribution are honest for the keyless default', () => {
    assert.match(MAP_CONFIG.TILE_URL, /\/api\/tiles\/\{z\}\/\{x\}\/\{y\}\.png\?v=osm-dark-\d+$/);
    assert.match(MAP_CONFIG.TILE_ATTRIBUTION, /OpenStreetMap/);
    assert.match(MAP_CONFIG.TILE_ATTRIBUTION, /openstreetmap\.org\/copyright/);
    // The proxy darkens the tiles before caching, so the frontend needs no filter
    // and must not credit a provider it is not serving.
    assert.equal('DARK_FILTER' in (MAP_CONFIG as unknown as Record<string, unknown>), false);
  });

  test('the licensed CARTO Dark Matter template stays available as an opt-in', () => {
    assert.equal(CARTO_DARK_MATTER.TILE_URL, 'https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png');
    assert.match(CARTO_DARK_MATTER.TILE_URL, /dark_all/);
    assert.match(CARTO_DARK_MATTER.ATTRIBUTION, /carto\.com\/attributions/);
    assert.equal(CARTO_DARK_MATTER.SUBDOMAINS, 'abcd');
  });

  test('MAP_CLUSTER_I18N is a frozen vi/en catalogue', () => {
    assert.ok(Object.isFrozen(MAP_CLUSTER_I18N));
    for (const [name, leaf] of Object.entries(MAP_CLUSTER_I18N)) {
      assert.deepEqual(Object.keys(leaf).sort(), ['en', 'vi'], `${name} must be a vi/en pair`);
    }
  });

  test('Vietnamese cluster copy keeps full diacritics and no emoji', () => {
    for (const [name, leaf] of Object.entries(MAP_CLUSTER_I18N)) {
      const vi = typeof leaf.vi === 'string' ? leaf.vi : (leaf.vi as unknown as (a: number, b: string) => string)(3, 'Mức 3');
      const en = typeof leaf.en === 'string' ? leaf.en : (leaf.en as unknown as (a: number, b: string) => string)(3, 'Level 3');
      assert.match(vi, VI_DIACRITICS, `${name}.vi lost its diacritics`);
      assert.notEqual(vi, en, `${name} is not translated`);
      assert.doesNotMatch(vi, EMOJI);
      assert.doesNotMatch(en, EMOJI);
      assert.doesNotMatch(vi, VARIATION_SELECTOR);
      assert.doesNotMatch(en, VARIATION_SELECTOR);
    }
  });

  test('the badge aria label states the count, the worst hazard and the action', () => {
    const vi = MAP_CLUSTER_I18N.BADGE_ARIA.vi(12, 'Mức 3');
    assert.match(vi, /12 camera/);
    assert.match(vi, /Mức 3/);
    assert.match(vi, /phóng to/);
    const en = MAP_CLUSTER_I18N.BADGE_ARIA.en(12, 'Level 3');
    assert.match(en, /12 cameras/);
    assert.match(en, /Level 3/);
    assert.match(en, /zoom/);
  });

  test('marker tooltip copy is centralized, bilingual and emoji-free', () => {
    assert.ok(Object.isFrozen(MAP_TOOLTIP_I18N));
    for (const [name, leaf] of Object.entries(MAP_TOOLTIP_I18N)) {
      assert.deepEqual(Object.keys(leaf).sort(), ['en', 'vi'], `${name} must be a vi/en pair`);
      assert.match(leaf.vi, VI_DIACRITICS, `${name}.vi lost its diacritics`);
      assert.notEqual(leaf.vi, leaf.en, `${name} is not translated`);
      assert.doesNotMatch(leaf.vi, EMOJI);
      assert.doesNotMatch(leaf.en, EMOJI);
    }
    // The ghost/duplicate lines used to be Vietnamese-only in the English UI.
    assert.equal(MAP_TOOLTIP_I18N.GHOST.en, 'LOST LIVE SIGNAL');
    assert.equal(MAP_TOOLTIP_I18N.DUPLICATE_SOURCE.en, 'DUPLICATE SOURCE');
  });

  test('the count copy singularises in English only', () => {
    assert.equal(MAP_CLUSTER_I18N.COUNT.vi(1), '1 camera giám sát');
    assert.equal(MAP_CLUSTER_I18N.COUNT.vi(3), '3 camera giám sát');
    assert.equal(MAP_CLUSTER_I18N.COUNT.en(1), '1 camera');
    assert.equal(MAP_CLUSTER_I18N.COUNT.en(3), '3 cameras');
  });

  describe('deconflictClusters & collision prevention (Issue #452)', () => {
    test('padding constant is defined and at least 8 px', () => {
      assert.ok(typeof CLUSTER_COLLISION_PADDING_PX === 'number');
      assert.ok(CLUSTER_COLLISION_PADDING_PX >= 8);
    });

    test('overlapping aggregate clusters in adjacent cells merge deterministically', () => {
      // 4 cameras creating two adjacent cells close to each other
      const cam1: ClusterPoint = { id: 'c1', latitude: 10.7743, longitude: 106.7035 };
      const cam2: ClusterPoint = { id: 'c2', latitude: 10.7744, longitude: 106.7036 };
      const cam3: ClusterPoint = { id: 'c3', latitude: 10.7750, longitude: 106.7042 };
      const cam4: ClusterPoint = { id: 'c4', latitude: 10.7751, longitude: 106.7043 };

      const result = buildCameraClusters([cam1, cam2, cam3, cam4], {
        zoom: 11,
        levelFor: (id) => (id === 'c3' ? 3 : 1),
      });

      // At zoom 11, cameras in close proximity must not form overlapping badges
      const aggregateClusters = result.clusters.filter((c) => c.count >= 2);
      for (let i = 0; i < aggregateClusters.length; i++) {
        const cA = aggregateClusters[i];
        const xA = lngToWorldX(cA.longitude, 11);
        const yA = latToWorldY(cA.latitude, 11);
        const dA = getClusterDiameter(cA.count);

        for (let j = i + 1; j < aggregateClusters.length; j++) {
          const cB = aggregateClusters[j];
          const xB = lngToWorldX(cB.longitude, 11);
          const yB = latToWorldY(cB.latitude, 11);
          const dB = getClusterDiameter(cB.count);

          const dist = Math.hypot(xB - xA, yB - yA);
          assert.ok(
            dist >= (dA + dB) / 2 + CLUSTER_COLLISION_PADDING_PX,
            `Clusters collide: dist=${dist.toFixed(2)}px < required=${((dA + dB) / 2 + CLUSTER_COLLISION_PADDING_PX).toFixed(2)}px`,
          );
        }
      }

      // Worst hazard level 3 bubbles up to the merged cluster
      const topCluster = result.clusters.find((c) => c.cameraIds.includes('c3'));
      assert.ok(topCluster);
      assert.equal(topCluster.hazardLevel, 3);
    });

    test('getAdaptiveGridPx scales grid size inversely with zoom to prevent city clutter (Issue #454)', () => {
      assert.equal(getAdaptiveGridPx(10), 180);
      assert.equal(getAdaptiveGridPx(11), 150);
      assert.equal(getAdaptiveGridPx(12), 105);
      assert.equal(getAdaptiveGridPx(13), 70);
      // Custom override takes precedence
      assert.equal(getAdaptiveGridPx(11, 200), 200);
    });
  });
});
