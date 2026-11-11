/**
 * Unit tests for Issue #400 (S12-T2) — `geoDistance.ts` pure core.
 * Normative source: `docs/sprints/s12_t2_spec.md` §1, §4.1, §6, §7.1.
 *
 * TDD note: this suite is authored before the implementation lands. Every
 * expectation is derived from the spec contracts, so a failing assertion means
 * the implementation drifted from the contract — never relax the test to fit
 * the implementation.
 */
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';

import {
  NEAR_ME_RADIUS_METERS,
  DISTANCE_BAND_METERS,
  LOW_ACCURACY_WARN_METERS,
  LOW_ACCURACY_BAD_METERS,
  FLY_TO_ZOOM,
  haversineMeters,
  distanceBand,
  isWithinBounds,
  isValidGeoPoint,
  formatDistance,
  compareNearMeResults,
  buildNearMeResults,
  type GeoPoint,
  type NearMeResult,
} from './geoDistance.ts';
import { NEAR_ME_I18N } from '../config/constants.ts';
import type { Camera } from '../hooks/useCameras.ts';
import type { TrafficVerdict } from './trafficUtils.ts';

/** HCMC city centre — same origin used by `MAP_CONFIG.DEFAULT_CENTER`. */
const HCMC: GeoPoint = { latitude: 10.7769, longitude: 106.7009 };

/** Metres per degree of latitude for R = 6 371 008.8 m. */
const METERS_PER_DEG_LAT = 111195.08023353292;

const NOW = Date.now();

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function makeCamera(overrides: Partial<Camera> = {}): Camera {
  return { id: 'cam-fixture', isOnline: true, ...overrides };
}

/** Camera whose great-circle distance north of `HCMC` is ~`meters`. */
function cameraAt(id: string, meters: number, overrides: Partial<Camera> = {}): Camera {
  return makeCamera({
    id,
    latitude: HCMC.latitude + meters / METERS_PER_DEG_LAT,
    longitude: HCMC.longitude,
    ...overrides,
  });
}

function floodVerdict(
  cameraId: string,
  severity: string,
  overrides: Partial<TrafficVerdict> = {},
): TrafficVerdict {
  return {
    camera_id: cameraId,
    severity,
    flood_severity: severity,
    confidence: 0.92,
    observed_at: new Date(NOW - 5_000).toISOString(),
    timestamp: new Date(NOW - 5_000).toISOString(),
    ...overrides,
  };
}

function trafficVerdict(
  cameraId: string,
  severity: string,
  overrides: Partial<TrafficVerdict> = {},
): TrafficVerdict {
  return {
    camera_id: cameraId,
    severity,
    confidence: 0.88,
    observed_at: new Date(NOW - 5_000).toISOString(),
    timestamp: new Date(NOW - 5_000).toISOString(),
    ...overrides,
  };
}

function result(
  id: string,
  distanceMeters: number,
  floodSeverity: string | null = null,
  trafficSeverity: string | null = null,
  floodStale = false,
  trafficStale = false,
): NearMeResult {
  return {
    camera: makeCamera({ id }),
    distanceMeters,
    distanceBand: distanceBand(distanceMeters),
    floodSeverity,
    floodStale,
    trafficSeverity,
    trafficStale,
  };
}

function ids(results: NearMeResult[]): string[] {
  return results.map((r) => r.camera.id);
}

// ---------------------------------------------------------------------------
// §4.1 constants
// ---------------------------------------------------------------------------

describe('geoDistance constants (spec §4.1)', () => {
  test('exposes the documented tuning constants', () => {
    assert.equal(NEAR_ME_RADIUS_METERS, 1500);
    assert.equal(DISTANCE_BAND_METERS, 250);
    assert.equal(LOW_ACCURACY_WARN_METERS, 100);
    assert.equal(LOW_ACCURACY_BAD_METERS, 500);
    assert.equal(FLY_TO_ZOOM, 15);
    assert.ok(LOW_ACCURACY_WARN_METERS < LOW_ACCURACY_BAD_METERS);
    assert.equal(NEAR_ME_RADIUS_METERS % DISTANCE_BAND_METERS, 0);
  });
});

// ---------------------------------------------------------------------------
// Haversine
// ---------------------------------------------------------------------------

describe('haversineMeters (spec §4.1, R = 6371008.8)', () => {
  test('returns 0 for identical points', () => {
    assert.equal(haversineMeters(HCMC, { ...HCMC }), 0);
  });

  test('matches the reference distance for 0.01° of latitude', () => {
    const d = haversineMeters(HCMC, { latitude: 10.7869, longitude: 106.7009 });
    assert.ok(Math.abs(d - 1111.95) < 0.1, `expected ~1111.95 m, got ${d}`);
  });

  test('matches the reference distance for 0.01° of longitude at 10.7769°', () => {
    const d = haversineMeters(HCMC, { latitude: 10.7769, longitude: 106.7109 });
    assert.ok(Math.abs(d - 1092.34) < 0.1, `expected ~1092.34 m, got ${d}`);
  });

  test('matches the HCMC → Hà Nội reference distance', () => {
    const d = haversineMeters(HCMC, { latitude: 21.0278, longitude: 105.8342 });
    assert.ok(Math.abs(d - 1_143_599.2) < 5, `expected ~1143599 m, got ${d}`);
  });

  test('is symmetric within floating-point tolerance', () => {
    const a: GeoPoint = { latitude: 10.7626, longitude: 106.6602 };
    const b: GeoPoint = { latitude: 10.8231, longitude: 106.6297 };
    assert.ok(Math.abs(haversineMeters(a, b) - haversineMeters(b, a)) < 1e-9);
  });

  test('is never negative and never NaN', () => {
    const samples: Array<[GeoPoint, GeoPoint]> = [
      [HCMC, HCMC],
      [HCMC, { latitude: -33.8688, longitude: 151.2093 }],
      [{ latitude: 90, longitude: 180 }, { latitude: -90, longitude: -180 }],
      [{ latitude: 0, longitude: 0 }, { latitude: 0, longitude: 180 }],
    ];
    for (const [a, b] of samples) {
      const d = haversineMeters(a, b);
      assert.ok(Number.isFinite(d), `expected finite distance for ${JSON.stringify([a, b])}`);
      assert.ok(d >= 0);
    }
  });

  test('clamps the asin argument for antipodal points (πR, no NaN)', () => {
    const d = haversineMeters({ latitude: 0, longitude: 0 }, { latitude: 0, longitude: 180 });
    assert.ok(Math.abs(d - Math.PI * 6371008.8) < 1, `expected πR ≈ 20015114 m, got ${d}`);
  });
});

// ---------------------------------------------------------------------------
// Distance bands
// ---------------------------------------------------------------------------

describe('distanceBand (spec §1: min(5, floor(d / 250)))', () => {
  test('maps the documented 249.9 / 250 boundary', () => {
    assert.equal(distanceBand(0), 0);
    assert.equal(distanceBand(249.9), 0);
    assert.equal(distanceBand(250), 1);
  });

  test('maps the documented 1499.9 / 1500 / 1500.1 boundary', () => {
    assert.equal(distanceBand(1499.9), 5);
    assert.equal(distanceBand(1500), 5);
    assert.equal(distanceBand(1500.1), 5);
  });

  test('maps every intermediate band edge', () => {
    assert.equal(distanceBand(1), 0);
    assert.equal(distanceBand(499.9), 1);
    assert.equal(distanceBand(500), 2);
    assert.equal(distanceBand(749.9), 2);
    assert.equal(distanceBand(750), 3);
    assert.equal(distanceBand(1249.9), 4);
    assert.equal(distanceBand(1250), 5);
  });

  test('saturates at band 5 for far distances', () => {
    assert.equal(distanceBand(1_500.4), 5);
    assert.equal(distanceBand(42_000), 5);
  });
});

// ---------------------------------------------------------------------------
// Bounds / coordinate validity
// ---------------------------------------------------------------------------

describe('isWithinBounds (spec §4.1, AC13)', () => {
  const bounds: [[number, number], [number, number]] = [
    [10.15, 106.35],
    [11.35, 107.45],
  ];

  test('accepts a point inside the box', () => {
    assert.equal(isWithinBounds({ latitude: 10.7769, longitude: 106.7009 }, bounds), true);
  });

  test('accepts every corner and edge exactly (inclusive bounds)', () => {
    assert.equal(isWithinBounds({ latitude: 10.15, longitude: 106.35 }, bounds), true);
    assert.equal(isWithinBounds({ latitude: 11.35, longitude: 107.45 }, bounds), true);
    assert.equal(isWithinBounds({ latitude: 10.15, longitude: 107.45 }, bounds), true);
    assert.equal(isWithinBounds({ latitude: 11.35, longitude: 106.35 }, bounds), true);
    assert.equal(isWithinBounds({ latitude: 10.15, longitude: 106.9 }, bounds), true);
    assert.equal(isWithinBounds({ latitude: 10.9, longitude: 107.45 }, bounds), true);
  });

  test('rejects points just outside each edge', () => {
    assert.equal(isWithinBounds({ latitude: 10.1499, longitude: 106.7009 }, bounds), false);
    assert.equal(isWithinBounds({ latitude: 11.3501, longitude: 106.7009 }, bounds), false);
    assert.equal(isWithinBounds({ latitude: 10.7769, longitude: 106.3499 }, bounds), false);
    assert.equal(isWithinBounds({ latitude: 10.7769, longitude: 107.4501 }, bounds), false);
  });

  test('rejects Hà Nội (outside the mapped area)', () => {
    assert.equal(isWithinBounds({ latitude: 21.0278, longitude: 105.8342 }, bounds), false);
  });
});

describe('isValidGeoPoint (spec §4.1)', () => {
  test('accepts finite coordinates within legal ranges', () => {
    assert.equal(isValidGeoPoint(HCMC), true);
    assert.equal(isValidGeoPoint({ latitude: 0, longitude: 0 }), true);
    assert.equal(isValidGeoPoint({ latitude: 90, longitude: 180 }), true);
    assert.equal(isValidGeoPoint({ latitude: -90, longitude: -180 }), true);
  });

  test('rejects missing, empty or null points', () => {
    assert.equal(isValidGeoPoint(), false);
    assert.equal(isValidGeoPoint(null), false);
    assert.equal(isValidGeoPoint({}), false);
    assert.equal(isValidGeoPoint({ latitude: 10.7769 }), false);
    assert.equal(isValidGeoPoint({ longitude: 106.7009 }), false);
  });

  test('rejects out-of-range latitudes and longitudes', () => {
    assert.equal(isValidGeoPoint({ latitude: 90.0001, longitude: 106.7 }), false);
    assert.equal(isValidGeoPoint({ latitude: -90.0001, longitude: 106.7 }), false);
    assert.equal(isValidGeoPoint({ latitude: 10.7, longitude: 180.0001 }), false);
    assert.equal(isValidGeoPoint({ latitude: 10.7, longitude: -180.0001 }), false);
  });

  test('rejects non-finite coordinates', () => {
    assert.equal(isValidGeoPoint({ latitude: Number.NaN, longitude: 106.7 }), false);
    assert.equal(isValidGeoPoint({ latitude: 10.7, longitude: Number.NaN }), false);
    assert.equal(isValidGeoPoint({ latitude: Number.POSITIVE_INFINITY, longitude: 106.7 }), false);
    assert.equal(isValidGeoPoint({ latitude: 10.7, longitude: Number.NEGATIVE_INFINITY }), false);
  });
});

// ---------------------------------------------------------------------------
// formatDistance
// ---------------------------------------------------------------------------

describe('formatDistance (spec §4.1, AC7)', () => {
  test('renders metres below 1 km to the nearest 10 m', () => {
    assert.equal(formatDistance(0, 'vi'), '0 m');
    assert.equal(formatDistance(0, 'en'), '0 m');
    assert.equal(formatDistance(340, 'vi'), '340 m');
    assert.equal(formatDistance(344, 'en'), '340 m');
    assert.equal(formatDistance(345, 'vi'), '350 m');
    assert.equal(formatDistance(999, 'en'), '1000 m');
  });

  test('renders km with one decimal in EN (point)', () => {
    assert.equal(formatDistance(1000, 'en'), '1.0 km');
    assert.equal(formatDistance(1234, 'en'), '1.2 km');
    assert.equal(formatDistance(1499, 'en'), '1.5 km');
    assert.equal(formatDistance(1500, 'en'), '1.5 km');
  });

  test('renders km with one decimal in VI (comma)', () => {
    assert.equal(formatDistance(1000, 'vi'), '1,0 km');
    assert.equal(formatDistance(1234, 'vi'), '1,2 km');
    assert.equal(formatDistance(1499, 'vi'), '1,5 km');
    assert.equal(formatDistance(1500, 'vi'), '1,5 km');
  });

  test('never emits an English point in VI or a Vietnamese comma in EN', () => {
    for (const metres of [1000, 1234, 1250, 1400]) {
      assert.ok(!formatDistance(metres, 'vi').includes('.'), `VI output for ${metres} m`);
      assert.ok(!formatDistance(metres, 'en').includes(','), `EN output for ${metres} m`);
    }
  });

  test('renders an em dash for non-finite input', () => {
    for (const lang of ['vi', 'en'] as const) {
      assert.equal(formatDistance(Number.NaN, lang), '—');
      assert.equal(formatDistance(Number.POSITIVE_INFINITY, lang), '—');
      assert.equal(formatDistance(Number.NEGATIVE_INFINITY, lang), '—');
    }
  });
});

// ---------------------------------------------------------------------------
// buildNearMeResults — filters (§4.1 filter order, E9)
// ---------------------------------------------------------------------------

describe('buildNearMeResults filters (spec §4.1, AC4, E9)', () => {
  test('keeps only valid, in-radius, non-waterway, non-ghost, non-duplicate cameras', () => {
    const cameras: Camera[] = [
      cameraAt('valid', 120),
      cameraAt('valid-2', 900),
      cameraAt('ghost', 100, { isGhost: true }),
      cameraAt('duplicate', 110, { duplicateOf: 'valid' }),
      cameraAt('waterway-flag', 130, { isWaterway: true }),
      cameraAt('waterway-status', 140, { status: 'WATERWAY_MONITOR' }),
      makeCamera({ id: 'no-coords' }),
      makeCamera({ id: 'bad-lat', latitude: 91, longitude: 106.7009 }),
      makeCamera({ id: 'nan-lon', latitude: 10.7769, longitude: Number.NaN }),
      cameraAt('far-away', 2600),
    ];

    const out = buildNearMeResults({ origin: HCMC, cameras, now: NOW });
    assert.deepEqual(ids(out).sort(), ['valid', 'valid-2']);
  });

  test('drops null camera entries without throwing', () => {
    const cameras = [
      cameraAt('valid', 120),
      null as unknown as Camera,
      undefined as unknown as Camera,
    ];
    const out = buildNearMeResults({ origin: HCMC, cameras, now: NOW });
    assert.deepEqual(ids(out), ['valid']);
  });

  test('hides offline cameras by default (hideOffline defaults to true)', () => {
    const cameras: Camera[] = [
      cameraAt('online', 120),
      cameraAt('offline-no-signal', 130, { isOnline: false, status: 'NO_SIGNAL' }),
      cameraAt('offline-not-image', 140, { isOnline: false, status: 'NOT_IMAGE' }),
    ];

    const hidden = buildNearMeResults({ origin: HCMC, cameras, now: NOW });
    assert.deepEqual(ids(hidden), ['online']);

    const explicit = buildNearMeResults({ origin: HCMC, cameras, hideOffline: true, now: NOW });
    assert.deepEqual(ids(explicit), ['online']);
  });

  test('keeps offline cameras when hideOffline is false, but still drops waterway monitors', () => {
    const cameras: Camera[] = [
      cameraAt('online', 120),
      cameraAt('offline', 130, { isOnline: false, status: 'NO_SIGNAL' }),
      cameraAt('waterway', 140, { isWaterway: true, status: 'WATERWAY_MONITOR' }),
    ];

    const out = buildNearMeResults({ origin: HCMC, cameras, hideOffline: false, now: NOW });
    assert.deepEqual(ids(out).sort(), ['offline', 'online']);
  });

  test('applies an inclusive 1 500 m radius (+0.5 m tolerance)', () => {
    const cameras: Camera[] = [
      cameraAt('near', 120),
      cameraAt('edge', 1500),
      cameraAt('just-outside', 1500.6),
      cameraAt('far', 1900),
    ];

    const out = buildNearMeResults({ origin: HCMC, cameras, now: NOW });
    assert.deepEqual(ids(out).sort(), ['edge', 'near']);
  });

  test('honours a custom radiusMeters', () => {
    const cameras: Camera[] = [cameraAt('a', 300), cameraAt('b', 900), cameraAt('c', 1400)];
    const out = buildNearMeResults({ origin: HCMC, cameras, radiusMeters: 1000, now: NOW });
    assert.deepEqual(ids(out).sort(), ['a', 'b']);
  });

  test('reports real great-circle distances', () => {
    const out = buildNearMeResults({
      origin: HCMC,
      cameras: [cameraAt('a', 300)],
      now: NOW,
    });
    assert.equal(out.length, 1);
    assert.ok(Math.abs(out[0].distanceMeters - 300) < 0.05, `got ${out[0].distanceMeters}`);
    assert.equal(out[0].distanceBand, 1);
  });

  test('returns an empty array when nothing qualifies', () => {
    const out = buildNearMeResults({ origin: HCMC, cameras: [], now: NOW });
    assert.deepEqual(out, []);
  });
});

// ---------------------------------------------------------------------------
// buildNearMeResults — verdict projection (E10)
// ---------------------------------------------------------------------------

describe('buildNearMeResults verdict projection (spec §4.1, §5 E10)', () => {
  test('projects fresh flood and traffic severities', () => {
    const cameras = [cameraAt('cam', 300)];
    const floodVerdicts = new Map<string, TrafficVerdict>([
      ['cam', floodVerdict('cam', 'FLOODED_SHALLOW')],
    ]);
    const trafficVerdicts = new Map<string, TrafficVerdict>([
      ['cam', trafficVerdict('cam', 'HEAVY')],
    ]);

    const [row] = buildNearMeResults({
      origin: HCMC,
      cameras,
      floodVerdicts,
      trafficVerdicts,
      now: NOW,
    });

    assert.equal(row.floodSeverity, 'FLOODED_SHALLOW');
    assert.equal(row.floodStale, false);
    assert.equal(row.trafficSeverity, 'HEAVY');
    assert.equal(row.trafficStale, false);
  });

  test('marks absent verdicts as null with floodStale/trafficStale false', () => {
    const [row] = buildNearMeResults({ origin: HCMC, cameras: [cameraAt('cam', 300)], now: NOW });

    assert.equal(row.floodSeverity, null);
    assert.equal(row.floodStale, false);
    assert.equal(row.trafficSeverity, null);
    assert.equal(row.trafficStale, false);
  });

  test('flags stale flood verdicts and keeps the row listed', () => {
    const floodVerdicts = new Map<string, TrafficVerdict>([
      ['cam', floodVerdict('cam', 'FLOODED_DEEP', { stale: true })],
    ]);

    const out = buildNearMeResults({
      origin: HCMC,
      cameras: [cameraAt('cam', 300)],
      floodVerdicts,
      now: NOW,
    });

    assert.equal(out.length, 1);
    assert.equal(out[0].floodStale, true);
  });

  test('flags stale traffic verdicts and keeps the row listed', () => {
    const trafficVerdicts = new Map<string, TrafficVerdict>([
      ['cam', trafficVerdict('cam', 'CONGESTED', { stale: true })],
    ]);

    const out = buildNearMeResults({
      origin: HCMC,
      cameras: [cameraAt('cam', 300)],
      trafficVerdicts,
      now: NOW,
    });

    assert.equal(out.length, 1);
    assert.equal(out[0].trafficStale, true);
  });
});

// ---------------------------------------------------------------------------
// buildNearMeResults — ordering (spec §1, AC5)
// ---------------------------------------------------------------------------

describe('buildNearMeResults ordering (spec §1 worked example)', () => {
  test('reproduces the documented band → severity → distance order', () => {
    const cameras = [
      cameraAt('A', 120),
      cameraAt('B', 300),
      cameraAt('C', 310),
      cameraAt('D', 400),
      cameraAt('E', 900),
      cameraAt('F', 1400),
      cameraAt('G', 1499),
    ];
    const floodVerdicts = new Map<string, TrafficVerdict>([
      ['A', floodVerdict('A', 'DRY')],
      ['B', floodVerdict('B', 'FLOODED_DEEP')],
      ['C', floodVerdict('C', 'FLOODED_SHALLOW')],
      ['D', floodVerdict('D', 'FLOODED_DEEP')],
      ['E', floodVerdict('E', 'FLOODED_DEEP')],
      ['F', floodVerdict('F', 'FLOODED_DEEP')],
      ['G', floodVerdict('G', 'DRY')],
    ]);

    const out = buildNearMeResults({ origin: HCMC, cameras, floodVerdicts, now: NOW });
    assert.deepEqual(ids(out), ['A', 'B', 'D', 'C', 'E', 'F', 'G']);
  });

  test('flood severity outranks distance inside the same band', () => {
    const cameras = [cameraAt('dry-near', 250), cameraAt('deep-far', 490)];
    const floodVerdicts = new Map<string, TrafficVerdict>([
      ['dry-near', floodVerdict('dry-near', 'DRY')],
      ['deep-far', floodVerdict('deep-far', 'FLOODED_DEEP')],
    ]);

    const out = buildNearMeResults({ origin: HCMC, cameras, floodVerdicts, now: NOW });
    assert.deepEqual(ids(out), ['deep-far', 'dry-near']);
  });

  test('distance never crosses a band boundary for a worse hazard', () => {
    const cameras = [cameraAt('dry-band-0', 240), cameraAt('deep-band-1', 260)];
    const floodVerdicts = new Map<string, TrafficVerdict>([
      ['dry-band-0', floodVerdict('dry-band-0', 'DRY')],
      ['deep-band-1', floodVerdict('deep-band-1', 'FLOODED_DEEP')],
    ]);

    const out = buildNearMeResults({ origin: HCMC, cameras, floodVerdicts, now: NOW });
    assert.deepEqual(ids(out), ['dry-band-0', 'deep-band-1']);
  });

  test('traffic severity outranks distance inside the same band and flood rank', () => {
    const cameras = [
      cameraAt('t-none', 250),
      cameraAt('t-clear', 260),
      cameraAt('t-moderate', 270),
      cameraAt('t-heavy', 280),
      cameraAt('t-congested', 290),
    ];
    const trafficVerdicts = new Map<string, TrafficVerdict>([
      ['t-clear', trafficVerdict('t-clear', 'CLEAR')],
      ['t-moderate', trafficVerdict('t-moderate', 'MODERATE')],
      ['t-heavy', trafficVerdict('t-heavy', 'HEAVY')],
      ['t-congested', trafficVerdict('t-congested', 'CONGESTED')],
    ]);

    const out = buildNearMeResults({ origin: HCMC, cameras, trafficVerdicts, now: NOW });
    assert.deepEqual(ids(out), ['t-congested', 't-heavy', 't-moderate', 't-clear', 't-none']);
  });

  test('flood severity outranks traffic severity', () => {
    const cameras = [cameraAt('flooded', 300), cameraAt('congested', 310)];
    const floodVerdicts = new Map<string, TrafficVerdict>([
      ['flooded', floodVerdict('flooded', 'FLOODED_DEEP')],
    ]);
    const trafficVerdicts = new Map<string, TrafficVerdict>([
      ['congested', trafficVerdict('congested', 'CONGESTED')],
    ]);

    const out = buildNearMeResults({
      origin: HCMC,
      cameras,
      floodVerdicts,
      trafficVerdicts,
      now: NOW,
    });
    assert.deepEqual(ids(out), ['flooded', 'congested']);
  });

  test('orders by distance when band, flood and traffic all tie', () => {
    const cameras = [cameraAt('far', 400), cameraAt('near', 260), cameraAt('mid', 330)];
    const out = buildNearMeResults({ origin: HCMC, cameras, now: NOW });
    assert.deepEqual(ids(out), ['near', 'mid', 'far']);
  });

  test('breaks full ties by camera id in code-point order', () => {
    const cameras = [
      makeCamera({ id: 'cam-9', latitude: HCMC.latitude + 300 / METERS_PER_DEG_LAT, longitude: HCMC.longitude }),
      makeCamera({ id: 'cam-10', latitude: HCMC.latitude + 300 / METERS_PER_DEG_LAT, longitude: HCMC.longitude }),
      makeCamera({ id: 'cam-2', latitude: HCMC.latitude + 300 / METERS_PER_DEG_LAT, longitude: HCMC.longitude }),
    ];

    const out = buildNearMeResults({ origin: HCMC, cameras, now: NOW });
    assert.deepEqual(ids(out), ['cam-10', 'cam-2', 'cam-9']);
  });

  test('ranks stale flood verdicts as rank 0, below a fresh DRY verdict', () => {
    const cameras = [cameraAt('stale-deep', 260), cameraAt('fresh-dry', 270), cameraAt('unknown', 280)];
    const floodVerdicts = new Map<string, TrafficVerdict>([
      ['stale-deep', floodVerdict('stale-deep', 'FLOODED_DEEP', { stale: true })],
      ['fresh-dry', floodVerdict('fresh-dry', 'DRY')],
    ]);

    const out = buildNearMeResults({ origin: HCMC, cameras, floodVerdicts, now: NOW });
    assert.deepEqual(ids(out), ['fresh-dry', 'stale-deep', 'unknown']);
  });

  test('ranks stale traffic verdicts as rank 0, below a fresh CLEAR verdict', () => {
    const cameras = [cameraAt('stale-congested', 260), cameraAt('fresh-clear', 280)];
    const trafficVerdicts = new Map<string, TrafficVerdict>([
      ['stale-congested', trafficVerdict('stale-congested', 'CONGESTED', { stale: true })],
      ['fresh-clear', trafficVerdict('fresh-clear', 'CLEAR')],
    ]);

    const out = buildNearMeResults({ origin: HCMC, cameras, trafficVerdicts, now: NOW });
    assert.deepEqual(ids(out), ['fresh-clear', 'stale-congested']);
  });

  test('stale flood verdict at a shorter distance still sinks below fresh DRY', () => {
    const cameras = [cameraAt('stale-wet', 250), cameraAt('fresh-dry', 499)];
    const floodVerdicts = new Map<string, TrafficVerdict>([
      ['stale-wet', floodVerdict('stale-wet', 'WET', { stale: true })],
      ['fresh-dry', floodVerdict('fresh-dry', 'DRY')],
    ]);

    const out = buildNearMeResults({ origin: HCMC, cameras, floodVerdicts, now: NOW });
    assert.deepEqual(ids(out), ['fresh-dry', 'stale-wet']);
  });
});

// ---------------------------------------------------------------------------
// compareNearMeResults (pure comparator used by sort)
// ---------------------------------------------------------------------------

describe('compareNearMeResults (spec §1 comparator)', () => {
  test('orders by distance band first', () => {
    const near = result('near', 240, 'DRY');
    const far = result('far', 260, 'FLOODED_DEEP');
    assert.ok(compareNearMeResults(near, far) < 0);
    assert.ok(compareNearMeResults(far, near) > 0);
  });

  test('orders flood rank DEEP > SHALLOW > WET > DRY > none', () => {
    const deep = result('deep', 300, 'FLOODED_DEEP');
    const shallow = result('shallow', 300, 'FLOODED_SHALLOW');
    const wet = result('wet', 300, 'WET');
    const dry = result('dry', 300, 'DRY');
    const unknown = result('unknown', 300, null);

    const order = [deep, shallow, wet, dry, unknown];
    assert.deepEqual(order.slice().sort(compareNearMeResults).map((r) => r.camera.id), [
      'deep',
      'shallow',
      'wet',
      'dry',
      'unknown',
    ]);
  });

  test('orders stale flood verdicts as rank 0', () => {
    const stale = result('stale', 260, 'FLOODED_DEEP', null, true);
    const dry = result('dry', 300, 'DRY', null, false);
    assert.ok(compareNearMeResults(stale, dry) > 0, 'stale must sort after fresh DRY');
    assert.ok(compareNearMeResults(dry, stale) < 0);
  });

  test('orders traffic rank CONGESTED > HEAVY > MODERATE > CLEAR > none', () => {
    const congested = result('congested', 300, null, 'CONGESTED');
    const heavy = result('heavy', 300, null, 'HEAVY');
    const moderate = result('moderate', 300, null, 'MODERATE');
    const clear = result('clear', 300, null, 'CLEAR');
    const unknown = result('unknown', 300, null, null);

    assert.deepEqual(
      [congested, heavy, moderate, clear, unknown].slice().sort(compareNearMeResults).map((r) => r.camera.id),
      ['congested', 'heavy', 'moderate', 'clear', 'unknown'],
    );
  });

  test('orders stale traffic verdicts as rank 0', () => {
    const stale = result('stale', 260, null, 'CONGESTED', false, true);
    const clear = result('clear', 300, null, 'CLEAR');
    assert.ok(compareNearMeResults(stale, clear) > 0);
  });

  test('orders by distance when band and both verdicts tie', () => {
    const a = result('a', 300, 'DRY', 'CLEAR');
    const b = result('b', 420, 'DRY', 'CLEAR');
    assert.ok(compareNearMeResults(a, b) < 0);
  });

  test('breaks full ties by camera id (code-point order)', () => {
    const a = result('cam-10', 300);
    const b = result('cam-2', 300);
    const c = result('cam-9', 300);
    assert.ok(compareNearMeResults(a, b) < 0);
    assert.ok(compareNearMeResults(b, c) < 0);
    assert.deepEqual([c, b, a].sort(compareNearMeResults).map((r) => r.camera.id), ['cam-10', 'cam-2', 'cam-9']);
  });

  test('returns 0 for identical results and is antisymmetric otherwise', () => {
    const a = result('a', 300, 'DRY', 'CLEAR');
    const b = result('b', 310, 'FLOODED_DEEP', 'HEAVY');
    const c = result('c', 900, 'WET', 'MODERATE');

    assert.equal(compareNearMeResults(a, a), 0);
    for (const [x, y] of [
      [a, b],
      [a, c],
      [b, c],
    ] as Array<[NearMeResult, NearMeResult]>) {
      assert.equal(Math.sign(compareNearMeResults(x, y)), -Math.sign(compareNearMeResults(y, x)));
    }
  });
});

// ---------------------------------------------------------------------------
// NEAR_ME_I18N catalog (spec §6, AC16)
// ---------------------------------------------------------------------------

type CatalogLeaf = string | string[] | ((...args: never[]) => string);

function collectLeaves(node: unknown, lang: 'vi' | 'en', path = '', out = new Map<string, CatalogLeaf>()) {
  if (node === null || typeof node !== 'object' || Array.isArray(node)) return out;
  const record = node as Record<string, unknown>;
  if ('vi' in record && 'en' in record) {
    out.set(path, record[lang] as CatalogLeaf);
    return out;
  }
  for (const key of Object.keys(record)) {
    collectLeaves(record[key], lang, path ? `${path}.${key}` : key, out);
  }
  return out;
}

function callLeaf(leaf: CatalogLeaf): string {
  return typeof leaf === 'function' ? (leaf as unknown as (...args: number[]) => string)(2) : '';
}

const viLeaves = collectLeaves(NEAR_ME_I18N, 'vi');
const enLeaves = collectLeaves(NEAR_ME_I18N, 'en');

describe('NEAR_ME_I18N catalog (spec §6, AC16)', () => {
  test('exposes the same key paths in VI and EN', () => {
    assert.ok(viLeaves.size > 30, `expected a complete catalog, got ${viLeaves.size} leaves`);
    assert.deepEqual([...viLeaves.keys()].sort(), [...enLeaves.keys()].sort());
  });

  test('every leaf is a non-empty string, non-empty string array or function', () => {
    for (const [path, leaf] of [...viLeaves, ...enLeaves]) {
      if (typeof leaf === 'string') {
        assert.ok(leaf.trim().length > 0, `${path} must be non-empty`);
        continue;
      }
      if (Array.isArray(leaf)) {
        assert.ok(leaf.length > 0, `${path} must not be an empty list`);
        for (const entry of leaf) {
          assert.equal(typeof entry, 'string', `${path} entries must be strings`);
          assert.ok(entry.trim().length > 0, `${path} entries must be non-empty`);
        }
        continue;
      }
      assert.equal(typeof leaf, 'function', `${path} must be a string, string[] or function`);
      assert.ok(callLeaf(leaf).trim().length > 0, `${path}(...) must return a non-empty string`);
    }
  });

  test('VI and EN render the same shape of argument-driven copy', () => {
    assert.equal(NEAR_ME_I18N.COUNT.vi(1), '1 camera trong bán kính 1,5 km');
    assert.equal(NEAR_ME_I18N.COUNT.vi(3), '3 camera trong bán kính 1,5 km');
    assert.equal(NEAR_ME_I18N.COUNT.en(1), '1 camera within 1.5 km');
    assert.equal(NEAR_ME_I18N.COUNT.en(3), '3 cameras within 1.5 km');
    assert.equal(NEAR_ME_I18N.ACCURACY.vi(35), 'Sai số ±35 m');
    assert.equal(NEAR_ME_I18N.ACCURACY.en(35), 'Accuracy ±35 m');
    assert.equal(NEAR_ME_I18N.EMPTY_OFFLINE_HINT.en(1), '1 camera is offline in this area.');
    assert.equal(NEAR_ME_I18N.EMPTY_OFFLINE_HINT.en(2), '2 cameras are offline in this area.');
    assert.equal(NEAR_ME_I18N.SHOW_MORE.vi(3), 'Xem thêm 3 camera');
    assert.equal(NEAR_ME_I18N.SHOW_MORE.en(1), 'Show 1 more camera');
    assert.equal(
      NEAR_ME_I18N.ROW_ARIA.vi('Ngã tư Hàng Xanh', '340 m', 'Ngập sâu'),
      'Ngã tư Hàng Xanh, cách 340 m, Ngập sâu. Mở chi tiết camera.',
    );
    assert.equal(
      NEAR_ME_I18N.ROW_ARIA.en('Hang Xanh', '340 m', 'Deep flood'),
      'Hang Xanh, 340 m away, Deep flood. Open camera details.',
    );
  });

  test('every VI leaf carries Vietnamese diacritics (never untranslated ASCII)', () => {
    for (const [path, leaf] of viLeaves) {
      const text = typeof leaf === 'function' ? callLeaf(leaf) : Array.isArray(leaf) ? leaf.join(' ') : leaf;
      assert.ok(
        /[^\u0000-\u007F]/.test(text),
        `${path} must keep VI diacritics, got "${text}"`,
      );
    }
  });

  test('every leaf is free of emoji and pictographs', () => {
    const pictographic = /\p{Extended_Pictographic}/u;
    for (const [path, leaf] of [...viLeaves, ...enLeaves]) {
      const text = typeof leaf === 'function' ? callLeaf(leaf) : Array.isArray(leaf) ? leaf.join(' ') : leaf;
      assert.ok(!pictographic.test(text), `${path} must not contain emoji/pictographs: "${text}"`);
      assert.ok(!/[\u{1F000}-\u{1FAFF}\u{2600}-\u{27BF}\u{FE0F}\u{200D}]/u.test(text), `${path} must be plain text`);
    }
  });

  test('renders the documented VI copy with full diacritics', () => {
    assert.equal(NEAR_ME_I18N.BUTTON_LABEL.vi, 'Quanh tôi');
    assert.equal(NEAR_ME_I18N.BUTTON_LABEL.en, 'Near me');
    assert.equal(NEAR_ME_I18N.SHEET_TITLE.vi, 'Quanh bạn');
    assert.equal(NEAR_ME_I18N.DENIED_TITLE.vi, 'Trình duyệt đang chặn quyền vị trí');
    assert.equal(NEAR_ME_I18N.TIMEOUT_TITLE.vi, 'Hết thời gian xác định vị trí');
    assert.equal(NEAR_ME_I18N.UNAVAILABLE_TITLE.vi, 'Không lấy được vị trí');
    assert.equal(NEAR_ME_I18N.OUTSIDE_AREA_TITLE.vi, 'Bạn đang ở ngoài vùng phủ bản đồ');
    assert.equal(NEAR_ME_I18N.EMPTY_TITLE.vi, 'Không có camera nào trong bán kính 1,5 km');
    assert.equal(
      NEAR_ME_I18N.SORT_NOTE.vi,
      'Sắp xếp theo khoảng cách (mỗi bước 250 m), sau đó theo mức ngập.',
    );
    assert.equal(NEAR_ME_I18N.RETRY.vi, 'Thử lại');
    assert.equal(NEAR_ME_I18N.CLOSE.vi, 'Đóng');
    assert.equal(NEAR_ME_I18N.CLOSE.en, 'Close');
    assert.deepEqual(NEAR_ME_I18N.DENIED_STEPS.vi, [
      'Mở biểu tượng ổ khóa cạnh thanh địa chỉ.',
      'Chọn Quyền vị trí, sau đó chọn Cho phép.',
      'Tải lại trang và bấm Quanh tôi.',
    ]);
    assert.equal(
      NEAR_ME_I18N.DENIED_STEPS_IOS.vi,
      'Trên iPhone: Cài đặt > Safari > Vị trí > Cho phép.',
    );
  });

  test('renders the documented severity, traffic and badge labels', () => {
    assert.deepEqual(
      {
        DEEP: NEAR_ME_I18N.SEVERITY.DEEP.vi,
        SHALLOW: NEAR_ME_I18N.SEVERITY.SHALLOW.vi,
        WET: NEAR_ME_I18N.SEVERITY.WET.vi,
        DRY: NEAR_ME_I18N.SEVERITY.DRY.vi,
        UNKNOWN: NEAR_ME_I18N.SEVERITY.UNKNOWN.vi,
      },
      { DEEP: 'Ngập sâu', SHALLOW: 'Ngập nông', WET: 'Đường ướt', DRY: 'Khô ráo', UNKNOWN: 'Chưa có dữ liệu' },
    );
    assert.deepEqual(
      {
        CLEAR: NEAR_ME_I18N.TRAFFIC.CLEAR.vi,
        MODERATE: NEAR_ME_I18N.TRAFFIC.MODERATE.vi,
        HEAVY: NEAR_ME_I18N.TRAFFIC.HEAVY.vi,
        CONGESTED: NEAR_ME_I18N.TRAFFIC.CONGESTED.vi,
        UNKNOWN: NEAR_ME_I18N.TRAFFIC.UNKNOWN.vi,
      },
      { CLEAR: 'Thông thoáng', MODERATE: 'Đông xe', HEAVY: 'Ùn tắc', CONGESTED: 'Tắc nghẽn', UNKNOWN: 'Chưa xác định' },
    );
    assert.equal(NEAR_ME_I18N.BADGE.OFFLINE.vi, 'Mất tín hiệu');
    assert.equal(NEAR_ME_I18N.BADGE.STALE.vi, 'Dữ liệu cũ');
    assert.equal(NEAR_ME_I18N.BADGE.NO_DATA.en, 'No data');
  });

  test('empty and error copy never promises the area is safe', () => {
    const unsafe = /không ngập|không có ngập|an toàn|no flooding|no flood|safe/i;
    for (const [path, leaf] of [...viLeaves, ...enLeaves]) {
      if (!/(EMPTY|OUTSIDE_AREA|DATA_ERROR|UNAVAILABLE|INVALID|TIMEOUT|DENIED)/.test(path)) continue;
      const text = typeof leaf === 'function' ? callLeaf(leaf) : Array.isArray(leaf) ? leaf.join(' ') : leaf;
      assert.ok(!unsafe.test(text), `${path} must not promise safety: "${text}"`);
    }
  });
});
