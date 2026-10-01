import test from 'node:test';
import assert from 'node:assert/strict';
import {
  getCachedCorridorStatus,
  setCachedCorridorStatus,
  clearCorridorCache,
  formatObservationAge,
  fetchCorridorStatus,
  getCorridorWorstVerdict,
  type CorridorStatus,
} from './corridorUtils.ts';
import { CORRIDOR_I18N } from '../config/constants.ts';

const sampleStatus: CorridorStatus = {
  road_key: 'vo_van_kiet',
  worst_severity: 'CONGESTED',
  coverage_pct: 75.0,
  oldest_observation_age_s: 45,
  total_cameras: 20,
  verified_cameras: 15,
  cameras: [
    {
      camera_id: 'cam-1',
      name: 'Võ Văn Kiệt - Ký Con',
      severity: 'CONGESTED',
      stale: false,
    },
  ],
};

test('corridorUtils cache operations and case-insensitivity', () => {
  clearCorridorCache();

  // 1. Initial state is empty
  assert.equal(getCachedCorridorStatus('vo_van_kiet'), null);

  // 2. Set cache entry
  setCachedCorridorStatus('vo_van_kiet', sampleStatus);
  const cached = getCachedCorridorStatus('vo_van_kiet');
  assert.ok(cached);
  assert.equal(cached.road_key, 'vo_van_kiet');
  assert.equal(cached.worst_severity, 'CONGESTED');

  // Case-insensitive lookup
  const cachedUpper = getCachedCorridorStatus('VO_VAN_KIET');
  assert.ok(cachedUpper);
  assert.equal(cachedUpper.coverage_pct, 75.0);

  // 3. Clear cache
  clearCorridorCache();
  assert.equal(getCachedCorridorStatus('vo_van_kiet'), null);
});

test('formatObservationAge formats seconds and minutes correctly in vi and en', () => {
  assert.equal(formatObservationAge(null), null);
  assert.equal(formatObservationAge(undefined), null);

  // Seconds < 60
  assert.equal(formatObservationAge(45, 'vi'), '45s trước');
  assert.equal(formatObservationAge(45, 'en'), '45s ago');

  // Exact minutes
  assert.equal(formatObservationAge(120, 'vi'), '2 phút trước');
  assert.equal(formatObservationAge(120, 'en'), '2m ago');

  // Minutes + seconds
  assert.equal(formatObservationAge(145, 'vi'), '2p 25s trước');
  assert.equal(formatObservationAge(145, 'en'), '2m 25s ago');
});

test('CORRIDOR_I18N contains all required keys with Vietnamese typography', () => {
  assert.equal(CORRIDOR_I18N.UNKNOWN.vi, 'Chưa có dữ liệu');
  assert.equal(CORRIDOR_I18N.UNKNOWN.en, 'No data');
  assert.equal(CORRIDOR_I18N.COVERAGE.vi(80), 'Phủ sóng: 80%');
  assert.equal(CORRIDOR_I18N.COVERAGE.en(80), 'Coverage: 80%');
  assert.equal(CORRIDOR_I18N.OLDEST.vi('45s trước'), 'Quan sát cũ nhất: 45s trước');
  assert.equal(CORRIDOR_I18N.OLDEST.en('45s ago'), 'Oldest observation: 45s ago');
});

test('getCorridorWorstVerdict accurately evaluates worst real non-stale verdict', () => {
  const cams = [{ id: 'c1' }, { id: 'c2' }, { id: 'c3' }];

  // 1. Empty / null
  assert.equal(getCorridorWorstVerdict([], new Map()), null);
  assert.equal(getCorridorWorstVerdict(cams, undefined), null);

  // 2. All stale
  const staleVerdicts = new Map([
    ['c1', { severity: 'CONGESTED', stale: true }],
    ['c2', { severity: 'CLEAR', stale: true }],
  ]);
  assert.equal(getCorridorWorstVerdict(cams, staleVerdicts), null);

  // 3. Clear only
  const clearVerdicts = new Map([
    ['c1', { severity: 'CLEAR', stale: false }],
  ]);
  assert.equal(getCorridorWorstVerdict(cams, clearVerdicts), 'CLEAR');

  // 4. Moderate + Clear
  const modVerdicts = new Map([
    ['c1', { severity: 'CLEAR', stale: false }],
    ['c2', { severity: 'MODERATE', stale: false }],
  ]);
  assert.equal(getCorridorWorstVerdict(cams, modVerdicts), 'MODERATE');

  // 5. Congested + Moderate + Clear (worst is Congested)
  const congVerdicts = new Map([
    ['c1', { severity: 'CLEAR', stale: false }],
    ['c2', { severity: 'MODERATE', stale: false }],
    ['c3', { severity: 'CONGESTED', stale: false }],
  ]);
  assert.equal(getCorridorWorstVerdict(cams, congVerdicts), 'CONGESTED');

  // 6. Stale congested is ignored; fresh moderate wins
  const partialStale = new Map([
    ['c1', { severity: 'CLEAR', stale: false }],
    ['c2', { severity: 'MODERATE', stale: false }],
    ['c3', { severity: 'CONGESTED', stale: true }],
  ]);
  assert.equal(getCorridorWorstVerdict(cams, partialStale), 'MODERATE');
});

test('fetchCorridorStatus network and caching behavior', async () => {
  clearCorridorCache();

  // Mock global fetch
  const originalFetch = globalThis.fetch;
  let fetchCallCount = 0;

  globalThis.fetch = async (url: string | URL | Request) => {
    fetchCallCount++;
    const urlStr = String(url);
    if (urlStr.includes('vo_van_kiet')) {
      return {
        ok: true,
        status: 200,
        json: async () => sampleStatus,
      } as Response;
    }
    return {
      ok: false,
      status: 404,
      json: async () => ({ error: 'not found' }),
    } as Response;
  };

  try {
    // 1. First fetch triggers network call
    const res1 = await fetchCorridorStatus('vo_van_kiet');
    assert.ok(res1);
    assert.equal(res1.road_key, 'vo_van_kiet');
    assert.equal(fetchCallCount, 1);

    // 2. Second fetch hits 30s cache without network call
    const res2 = await fetchCorridorStatus('vo_van_kiet');
    assert.ok(res2);
    assert.equal(res2.worst_severity, 'CONGESTED');
    assert.equal(fetchCallCount, 1);

    // 3. 404 unknown road returns null
    const resUnknown = await fetchCorridorStatus('unknown_road_xyz');
    assert.equal(resUnknown, null);
    assert.equal(fetchCallCount, 2);
  } finally {
    globalThis.fetch = originalFetch;
    clearCorridorCache();
  }
});
