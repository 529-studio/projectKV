import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  isVerdictValid,
  isVerdictStale,
  isMockHidden,
  normalizeVerdict,
  getSeverityLabel,
  DEFAULT_VERDICT_TTL_MS,
  type TrafficVerdict,
} from '../utils/trafficUtils.ts';
import { VERDICT_I18N } from '../config/constants.ts';

describe('Traffic Verdict Provenance and Staleness (Issue #64)', () => {
  const now = Date.now();
  const freshTime = new Date(now - 30 * 1000).toISOString();
  const staleTime = new Date(now - 250 * 1000).toISOString();
  const expiredTime = new Date(now - 48 * 3600 * 1000).toISOString();

  test('isVerdictValid accepts fresh verdicts and rejects empty/ancient ones', () => {
    assert.equal(isVerdictValid(null), false);
    assert.equal(isVerdictValid(undefined), false);
    assert.equal(isVerdictValid({ camera_id: 'c1', severity: '', confidence: 1, timestamp: freshTime }), false);
    assert.equal(isVerdictValid({ camera_id: 'c1', severity: 'CLEAR', confidence: 1, timestamp: '' }), false);

    const freshVerdict: TrafficVerdict = {
      camera_id: 'c1',
      severity: 'CLEAR',
      confidence: 0.9,
      observed_at: freshTime,
      timestamp: freshTime,
    };
    assert.equal(isVerdictValid(freshVerdict), true);

    const ancientVerdict: TrafficVerdict = {
      camera_id: 'c2',
      severity: 'CLEAR',
      confidence: 0.9,
      observed_at: expiredTime,
      timestamp: expiredTime,
    };
    assert.equal(isVerdictValid(ancientVerdict), false);
  });

  test('isVerdictStale returns true when stale flag is set or age > TTL', () => {
    assert.equal(isVerdictStale(null), false);

    const freshVerdict: TrafficVerdict = {
      camera_id: 'c1',
      severity: 'CLEAR',
      confidence: 0.9,
      observed_at: freshTime,
      timestamp: freshTime,
      stale: false,
    };
    assert.equal(isVerdictStale(freshVerdict), false);

    const frozenVerdict: TrafficVerdict = {
      camera_id: 'c1',
      severity: 'CLEAR',
      confidence: 0.9,
      observed_at: freshTime,
      timestamp: freshTime,
      stale: true,
    };
    assert.equal(isVerdictStale(frozenVerdict), true);

    const agedVerdict: TrafficVerdict = {
      camera_id: 'c1',
      severity: 'CLEAR',
      confidence: 0.9,
      observed_at: staleTime,
      timestamp: staleTime,
      stale: false,
    };
    assert.equal(isVerdictStale(agedVerdict, DEFAULT_VERDICT_TTL_MS), true);
  });

  test('isMockHidden hides mock verdicts only in production or non-dev environments', () => {
    assert.equal(isMockHidden('vlm', 'production'), false);
    assert.equal(isMockHidden('onnx', 'dev'), false);
    assert.equal(isMockHidden('mock', 'production'), true);
    assert.equal(isMockHidden('mock', 'staging'), true);
    assert.equal(isMockHidden('mock', 'dev'), false);
  });

  test('normalizeVerdict correctly populates observed_at and stale', () => {
    const raw = {
      camera_id: 'cam101',
      severity: 'CONGESTED',
      confidence: 0.95,
      source: 'mock',
      timestamp: freshTime,
    };
    const normalized = normalizeVerdict(raw);
    assert.equal(normalized.camera_id, 'cam101');
    assert.equal(normalized.observed_at, freshTime);
    assert.equal(normalized.stale, false);

    const rawStale = {
      camera_id: 'cam102',
      severity: 'HEAVY',
      confidence: 0.8,
      timestamp: staleTime,
    };
    const normalizedStale = normalizeVerdict(rawStale);
    assert.equal(normalizedStale.stale, true);

    // cameraId (camelCase) alias
    const rawAlias = {
      cameraId: 'cam103',
      severity: 'CLEAR',
      confidence: 0.9,
      timestamp: freshTime,
    };
    const normalizedAlias = normalizeVerdict(rawAlias);
    assert.equal(normalizedAlias.camera_id, 'cam103');
  });

  test('VERDICT_I18N contains valid vi and en translations with proper Vietnamese diacritics', () => {
    assert.equal(VERDICT_I18N.STALE_BADGE.vi, 'Dữ liệu cũ');
    assert.equal(VERDICT_I18N.STALE_BADGE.en, 'Stale data');

    assert.equal(VERDICT_I18N.STALE_DESC.vi, 'Dữ liệu cũ (>3 phút)');
    assert.equal(VERDICT_I18N.STALE_DESC.en, 'Stale (>3 min)');

    assert.equal(VERDICT_I18N.DEMO_BADGE.vi, 'DỮ LIỆU MẪU');
    assert.equal(VERDICT_I18N.DEMO_BADGE.en, 'DEMO');

    assert.equal(VERDICT_I18N.TRAFFIC_PREFIX.vi, 'Giao thông');
    assert.equal(VERDICT_I18N.TRAFFIC_PREFIX.en, 'Traffic');

    assert.equal(VERDICT_I18N.SEVERITY.CONGESTED.vi, 'Ùn tắc');
    assert.equal(VERDICT_I18N.SEVERITY.CONGESTED.en, 'Congested');
  });

  test('getSeverityLabel localizes severities correctly with Vietnamese diacritics', () => {
    assert.equal(getSeverityLabel('CLEAR', 'vi'), 'Thông thoáng');
    assert.equal(getSeverityLabel('CLEAR', 'en'), 'Clear');

    assert.equal(getSeverityLabel('MODERATE', 'vi'), 'Đông xe');
    assert.equal(getSeverityLabel('MODERATE', 'en'), 'Moderate');

    assert.equal(getSeverityLabel('HEAVY', 'vi'), 'Ùn tắc');
    assert.equal(getSeverityLabel('HEAVY', 'en'), 'Congested');

    assert.equal(getSeverityLabel('CONGESTED', 'vi'), 'Ùn tắc');
    assert.equal(getSeverityLabel('CONGESTED', 'en'), 'Congested');

    // Case insensitivity
    assert.equal(getSeverityLabel('congested', 'vi'), 'Ùn tắc');
    assert.equal(getSeverityLabel('clear', 'vi'), 'Thông thoáng');

    // Fallback for unknown / empty
    assert.equal(getSeverityLabel('UNKNOWN', 'vi'), 'UNKNOWN');
    assert.equal(getSeverityLabel('', 'vi'), '');
  });
});

describe('Traffic Sync Indicator State Logic', () => {
  test('syncing state is active only when analysisEnabled is true and verdicts count is 0', () => {
    const shouldShowSyncing = (analysisEnabled: boolean, verdictsCount: number, dismissed: boolean) => {
      if (!analysisEnabled || dismissed) return false;
      return verdictsCount === 0;
    };

    assert.equal(shouldShowSyncing(true, 0, false), true);
    assert.equal(shouldShowSyncing(true, 10, false), false);
    assert.equal(shouldShowSyncing(false, 0, false), false);
    assert.equal(shouldShowSyncing(true, 0, true), false);
  });

  test('synced success state triggers when transitioning from 0 to >0 verdicts', () => {
    let prevCount = 0;
    const currentCount = 5;
    const shouldTriggerSuccess = prevCount === 0 && currentCount > 0;
    assert.equal(shouldTriggerSuccess, true);

    prevCount = 5;
    const subsequentCount = 6;
    const shouldRetrigger = prevCount === 0 && subsequentCount > 0;
    assert.equal(shouldRetrigger, false);
  });
});
