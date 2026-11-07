/**
 * forecast.test.ts - Unit Tests for Short-Horizon Forecast API, Utils & UI Logic (S10-T6)
 * Executed via node --test runner with experimental strip types.
 */

import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import {
  clamp,
  siToSvgY,
  computeTimelinePoints,
  buildConfidenceBandPath,
  buildTrendPolyline,
  isPredictionRenderable,
  getSeverityStyle,
  isValidForecastResponse,
  DEFAULT_SVG_DIMENSIONS,
} from './forecastUtils.ts';
import { FORECAST_I18N } from '../config/constants.ts';
import type { ForecastMultiResponse, HorizonPrediction } from '../types/forecast.ts';

describe('Forecast API & Contract Validation (ADR-012 §D5, S10-T6)', () => {
  const validMock: ForecastMultiResponse = {
    camera_id: 'cam-01',
    road_key: 'cong-hoa',
    generated_at: '2026-11-18T10:00:00Z',
    base_time: '2026-11-18T10:00:00Z',
    availability: 'ok',
    experimental: false,
    current: {
      observed_at: '2026-11-18T09:59:30Z',
      saturation_index: 0.45,
      severity: 'MODERATE',
      speed_kmh: 24.5,
      confidence: 0.95,
    },
    horizons: [
      {
        horizon_minutes: 10,
        target_time: '2026-11-18T10:10:00Z',
        predicted_saturation_index: 0.52,
        predicted_severity: 'HEAVY',
        predicted_speed_kmh: 21.0,
        confidence: 0.90,
        confidence_intervals: {
          saturation_index: { lower_95: 0.42, upper_95: 0.62 },
          speed_kmh: { lower_95: 18.0, upper_95: 25.0 },
        },
      },
      {
        horizon_minutes: 20,
        target_time: '2026-11-18T10:20:00Z',
        predicted_saturation_index: 0.60,
        predicted_severity: 'HEAVY',
        predicted_speed_kmh: 18.5,
        confidence: 0.85,
        confidence_intervals: {
          saturation_index: { lower_95: 0.48, upper_95: 0.72 },
          speed_kmh: { lower_95: 15.0, upper_95: 23.0 },
        },
      },
      {
        horizon_minutes: 30,
        target_time: '2026-11-18T10:30:00Z',
        predicted_saturation_index: 0.70,
        predicted_severity: 'CONGESTED',
        predicted_speed_kmh: 14.0,
        confidence: 0.80,
        confidence_intervals: {
          saturation_index: { lower_95: 0.55, upper_95: 0.85 },
          speed_kmh: { lower_95: 10.0, upper_95: 19.0 },
        },
      },
    ],
    model: {
      name: 'ridge_delta',
      version: '1.0.0',
      target_mode: 'multi_horizon',
      fit_source: 'live_history',
      validated: true,
      fit_rows: 5000,
      fit_cameras: 12,
      fit_window: { start: '2026-11-01T00:00:00Z', end: '2026-11-15T00:00:00Z' },
      fit_data_sha256: 'data_sha_mock',
      artifact_sha256: 'artifact_sha_mock',
      mae: 0.038,
      metrics: [
        { horizon_minutes: 10, mae: 0.025, rmse: 0.035, sample_count: 1000 },
        { horizon_minutes: 20, mae: 0.032, rmse: 0.045, sample_count: 1000 },
        { horizon_minutes: 30, mae: 0.038, rmse: 0.052, sample_count: 1000 },
      ],
    },
    source_data: {
      sample_count: 6,
      window_minutes: 45,
      last_observed_at: '2026-11-18T09:59:30Z',
      age_minutes: 0.5,
      stale: false,
    },
  };

  test('isValidForecastResponse accepts compliant multi-horizon response', () => {
    assert.equal(isValidForecastResponse(validMock), true);
  });

  test('isValidForecastResponse rejects null or non-object payloads', () => {
    assert.equal(isValidForecastResponse(null), false);
    assert.equal(isValidForecastResponse(undefined), false);
    assert.equal(isValidForecastResponse('bad-payload'), false);
    assert.equal(isValidForecastResponse(12345), false);
  });

  test('isValidForecastResponse rejects responses missing camera_id or invalid availability', () => {
    const corrupt1 = { ...validMock, camera_id: 123 as unknown as string };
    assert.equal(isValidForecastResponse(corrupt1), false);

    const corrupt2 = { ...validMock, availability: 'unknown_status' as unknown as ForecastMultiResponse['availability'] };
    assert.equal(isValidForecastResponse(corrupt2), false);
  });

  test('isValidForecastResponse rejects responses missing horizons array or current state', () => {
    const corrupt1 = { ...validMock, horizons: 'not-array' as unknown as ForecastMultiResponse['horizons'] };
    assert.equal(isValidForecastResponse(corrupt1), false);

    const corrupt2 = { ...validMock, current: null as unknown as ForecastMultiResponse['current'] };
    assert.equal(isValidForecastResponse(corrupt2), false);
  });
});

describe('Forecast Geometric Utilities & SVG Calculations (S10-T6)', () => {
  const horizons: HorizonPrediction[] = [
    {
      horizon_minutes: 10,
      target_time: '2026-11-18T10:10:00Z',
      predicted_saturation_index: 0.50,
      predicted_severity: 'MODERATE',
      predicted_speed_kmh: 22.0,
      confidence: 0.90,
      confidence_intervals: {
        saturation_index: { lower_95: 0.40, upper_95: 0.60 },
        speed_kmh: { lower_95: 18.0, upper_95: 26.0 },
      },
    },
    {
      horizon_minutes: 20,
      target_time: '2026-11-18T10:20:00Z',
      predicted_saturation_index: 0.65,
      predicted_severity: 'HEAVY',
      predicted_speed_kmh: 17.0,
      confidence: 0.85,
      confidence_intervals: {
        saturation_index: { lower_95: 0.50, upper_95: 0.78 },
        speed_kmh: { lower_95: 13.0, upper_95: 21.0 },
      },
    },
    {
      horizon_minutes: 30,
      target_time: '2026-11-18T10:30:00Z',
      predicted_saturation_index: 0.80,
      predicted_severity: 'CONGESTED',
      predicted_speed_kmh: 12.0,
      confidence: 0.80,
      confidence_intervals: {
        saturation_index: { lower_95: 0.62, upper_95: 0.92 },
        speed_kmh: { lower_95: 8.0, upper_95: 16.0 },
      },
    },
  ];

  test('clamp bounds numerical ranges accurately', () => {
    assert.equal(clamp(0.5, 0, 1), 0.5);
    assert.equal(clamp(-0.2, 0, 1), 0);
    assert.equal(clamp(1.5, 0, 1), 1);
  });

  test('siToSvgY accurately maps SI to Y coordinate inverted space', () => {
    const { viewHeight, padding } = DEFAULT_SVG_DIMENSIONS;
    const plotHeight = viewHeight - padding.top - padding.bottom;

    // SI = 1.0 (highest congestion) maps to top of plot area
    assert.equal(siToSvgY(1.0), padding.top);

    // SI = 0.0 (empty road) maps to bottom of plot area
    assert.equal(siToSvgY(0.0), padding.top + plotHeight);

    // SI = 0.5 maps to exact middle
    assert.equal(siToSvgY(0.5), padding.top + plotHeight * 0.5);
  });

  test('computeTimelinePoints generates 4 monotonic points (Now, +10m, +20m, +30m)', () => {
    const points = computeTimelinePoints(0.35, 28.0, 'MODERATE', 0.95, horizons, 'vi');
    assert.equal(points.length, 4);

    // Point 0 (Current)
    assert.equal(points[0].index, 0);
    assert.equal(points[0].minutes, 0);
    assert.equal(points[0].label, 'Hiện tại');
    assert.equal(points[0].si, 0.35);

    // Point 1 (+10m)
    assert.equal(points[1].index, 1);
    assert.equal(points[1].minutes, 10);
    assert.equal(points[1].label, 'Sau 10 phút');
    assert.equal(points[1].si, 0.50);

    // Point 2 (+20m)
    assert.equal(points[2].index, 2);
    assert.equal(points[2].minutes, 20);
    assert.equal(points[2].label, 'Sau 20 phút');

    // Point 3 (+30m)
    assert.equal(points[3].index, 3);
    assert.equal(points[3].minutes, 30);
    assert.equal(points[3].label, 'Sau 30 phút');

    // Monotonically increasing X coordinates
    for (let i = 1; i < points.length; i++) {
      assert.ok(points[i].x > points[i - 1].x, `Expected X to strictly increase at step ${i}`);
    }

    // CI upper is above CI lower (so yUpper <= yLower in SVG coordinates)
    points.forEach((p) => {
      assert.ok(p.yUpper <= p.yLower, `Expected SVG yUpper <= yLower for point ${p.minutes}m`);
    });
  });

  test('buildConfidenceBandPath constructs valid SVG path closing with Z', () => {
    const points = computeTimelinePoints(0.35, 28.0, 'MODERATE', 0.95, horizons, 'en');
    const path = buildConfidenceBandPath(points);

    assert.ok(path.startsWith('M '), 'Path must start with M command');
    assert.ok(path.endsWith(' Z'), 'Path must close with Z command');
    assert.ok(path.includes(' L '), 'Path must contain L commands for vertices');
  });

  test('buildTrendPolyline constructs valid space-separated coordinate pairs', () => {
    const points = computeTimelinePoints(0.35, 28.0, 'MODERATE', 0.95, horizons, 'en');
    const polyline = buildTrendPolyline(points);
    const pairs = polyline.split(' ');

    assert.equal(pairs.length, 4);
    pairs.forEach((pair) => {
      const [x, y] = pair.split(',');
      assert.ok(!isNaN(Number(x)));
      assert.ok(!isNaN(Number(y)));
    });
  });
});

describe('Availability & Fail-Closed Gating Logic (ADR-012 D5)', () => {
  test('isPredictionRenderable only allows ok and stale', () => {
    assert.equal(isPredictionRenderable('ok'), true);
    assert.equal(isPredictionRenderable('stale'), true);
    assert.equal(isPredictionRenderable('insufficient_history'), false);
    assert.equal(isPredictionRenderable('model_unvalidated'), false);
  });

  test('getSeverityStyle returns valid monochromatic classes for all severities', () => {
    const clear = getSeverityStyle('CLEAR');
    assert.ok(clear.border.length > 0);
    assert.ok(clear.bg.length > 0);

    const moderate = getSeverityStyle('MODERATE');
    assert.ok(moderate.border.includes('amber'));

    const heavy = getSeverityStyle('HEAVY');
    assert.ok(heavy.border.includes('orange'));

    const congested = getSeverityStyle('CONGESTED');
    assert.ok(congested.border.includes('rose'));

    const unknown = getSeverityStyle('UNKNOWN');
    assert.ok(unknown.border.includes('zinc'));
  });
});

describe('FORECAST_I18N Localization & Zero-Emoji Rule (AGENTS.md Rule 1)', () => {
  const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}\u{1FA00}-\u{1FAFF}]/u;

  test('FORECAST_I18N contains bilingual vi and en keys without missing values', () => {
    assert.ok(FORECAST_I18N.HEADER.TITLE.vi);
    assert.ok(FORECAST_I18N.HEADER.TITLE.en);
    assert.ok(FORECAST_I18N.HEADER.SUBTITLE.vi);
    assert.ok(FORECAST_I18N.HEADER.SUBTITLE.en);
    assert.ok(FORECAST_I18N.HORIZONS.NOW.vi);
    assert.ok(FORECAST_I18N.HORIZONS.NOW.en);
    assert.ok(FORECAST_I18N.HORIZONS.MINUTES.vi(10));
    assert.ok(FORECAST_I18N.HORIZONS.MINUTES.en(10));
    assert.ok(FORECAST_I18N.AVAILABILITY.INSUFFICIENT_HISTORY.TITLE.vi);
    assert.ok(FORECAST_I18N.AVAILABILITY.INSUFFICIENT_HISTORY.TITLE.en);
    assert.ok(FORECAST_I18N.AVAILABILITY.INSUFFICIENT_HISTORY.DESC.vi(4, 45, 1));
    assert.ok(FORECAST_I18N.AVAILABILITY.INSUFFICIENT_HISTORY.DESC.en(4, 45, 1));
    assert.ok(FORECAST_I18N.AVAILABILITY.MODEL_UNVALIDATED.TITLE.vi);
    assert.ok(FORECAST_I18N.AVAILABILITY.MODEL_UNVALIDATED.TITLE.en);
    assert.ok(FORECAST_I18N.AVAILABILITY.STALE.TITLE.vi);
    assert.ok(FORECAST_I18N.AVAILABILITY.STALE.TITLE.en);
    assert.ok(FORECAST_I18N.AVAILABILITY.STALE.BADGE.vi);
    assert.ok(FORECAST_I18N.AVAILABILITY.STALE.BADGE.en);
    assert.ok(FORECAST_I18N.STATES.LOADING.vi);
    assert.ok(FORECAST_I18N.STATES.LOADING.en);
    assert.ok(FORECAST_I18N.STATES.ERROR.vi);
    assert.ok(FORECAST_I18N.STATES.ERROR.en);
    assert.ok(FORECAST_I18N.STATES.RETRY.vi);
    assert.ok(FORECAST_I18N.STATES.RETRY.en);
  });

  test('Zero emoji rule is strictly respected across all FORECAST_I18N strings', () => {
    function assertNoEmoji(obj: unknown, path = '') {
      if (typeof obj === 'string') {
        assert.equal(
          emojiRegex.test(obj),
          false,
          `Emoji found in FORECAST_I18N at ${path}: "${obj}"`
        );
      } else if (typeof obj === 'function') {
        const result = (obj as (a: number, b?: number, c?: number) => string)(4, 45, 2);
        assert.equal(
          emojiRegex.test(result),
          false,
          `Emoji found in FORECAST_I18N function at ${path}: "${result}"`
        );
      } else if (obj && typeof obj === 'object') {
        Object.entries(obj).forEach(([key, val]) => {
          assertNoEmoji(val, `${path}.${key}`);
        });
      }
    }

    assertNoEmoji(FORECAST_I18N, 'FORECAST_I18N');
  });

  test('Vietnamese translations contain correct diacritics', () => {
    assert.ok(FORECAST_I18N.HEADER.TITLE.vi.includes('DỰ BÁO'));
    assert.ok(FORECAST_I18N.HEADER.SUBTITLE.vi.includes('bão hòa'));
    assert.ok(FORECAST_I18N.SEVERITY.CONGESTED.vi === 'Kẹt xe');
    assert.ok(FORECAST_I18N.SEVERITY.CLEAR.vi === 'Thông thoáng');
  });
});
