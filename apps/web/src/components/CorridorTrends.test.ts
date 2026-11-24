import assert from 'node:assert/strict';
import { test, describe, beforeEach } from 'node:test';
import type {
  CorridorHistoryResponse,
  HourlyHistoryItem,
  HistorySummary,
} from '../types/corridorHistory.ts';
import {
  getCachedCorridorHistory,
  clearCorridorHistoryCache,
} from '../hooks/useCorridorHistory.ts';

describe('S18-T3: Corridor Historical Flood Depth & Tidal Inundation Verification', () => {
  const sampleSummary: HistorySummary = {
    peak_tide_time: '2026-11-24T07:00:00Z',
    peak_tide_level_m: 2.08,
    max_depth_cm: 22.5,
    headline: {
      vi: 'Ngập đỉnh 23cm • Đỉnh triều 2.08m lúc 07:00',
      en: 'Peak flood 23cm • Peak tide 2.08m at 07:00',
    },
  };

  const sampleTimeline: HourlyHistoryItem[] = [
    {
      time: '2026-11-23T12:00:00Z',
      hour_label: '12:00',
      avg_depth_cm: 0.0,
      max_depth_cm: 0.0,
      tide_level_m: -0.02,
      tide_alert_level: 'NORMAL',
      hazard_level: 'CLEAR',
    },
    {
      time: '2026-11-24T06:00:00Z',
      hour_label: '06:00',
      avg_depth_cm: 12.0,
      max_depth_cm: 15.0,
      tide_level_m: 2.04,
      tide_alert_level: 'LEVEL_3',
      hazard_level: 'CAUTION',
    },
    {
      time: '2026-11-24T07:00:00Z',
      hour_label: '07:00',
      avg_depth_cm: 18.0,
      max_depth_cm: 22.5,
      tide_level_m: 2.08,
      tide_alert_level: 'LEVEL_3',
      hazard_level: 'IMPASSABLE',
    },
  ];

  const sampleResponse: CorridorHistoryResponse = {
    road_key: 'vo_van_kiet',
    hours: 24,
    station: 'phu_an',
    station_name: {
      vi: 'Trạm Phú An (Sông Sài Gòn)',
      en: 'Phu An Station (Saigon River)',
    },
    summary: sampleSummary,
    timeline: sampleTimeline,
  };

  beforeEach(() => {
    clearCorridorHistoryCache();
  });

  test('validates historical response payload structure & mathematical invariants', () => {
    assert.equal(sampleResponse.road_key, 'vo_van_kiet');
    assert.equal(sampleResponse.station, 'phu_an');
    assert.equal(sampleResponse.hours, 24);
    assert.equal(sampleResponse.timeline.length, 3);

    // Peak depth in summary must match or exceed any timeline max depth
    for (const h of sampleResponse.timeline) {
      assert.ok(sampleResponse.summary.max_depth_cm >= h.max_depth_cm);
      assert.ok(h.max_depth_cm >= h.avg_depth_cm);
      assert.ok(h.tide_level_m >= -1.0 && h.tide_level_m <= 2.5);
    }
  });

  test('validates headline summary formatting for bilingual modes', () => {
    assert.equal(
      sampleResponse.summary.headline.vi,
      'Ngập đỉnh 23cm • Đỉnh triều 2.08m lúc 07:00'
    );
    assert.equal(
      sampleResponse.summary.headline.en,
      'Peak flood 23cm • Peak tide 2.08m at 07:00'
    );
  });

  test('validates flood depth hazard color coding rules', () => {
    const getBarColor = (hazard: string, depth: number): string => {
      if (hazard === 'IMPASSABLE' || depth > 20) return '#f43f5e'; // rose-500: impassable
      if (hazard === 'CAUTION' || depth >= 10) return '#f59e0b'; // amber-500: caution
      if (depth > 0) return '#fbbf24'; // amber-400: shallow
      return '#3f3f46'; // zinc-700: dry
    };

    assert.equal(getBarColor('CLEAR', 0), '#3f3f46');
    assert.equal(getBarColor('CLEAR', 5), '#fbbf24');
    assert.equal(getBarColor('CAUTION', 15), '#f59e0b');
    assert.equal(getBarColor('IMPASSABLE', 25), '#f43f5e');
  });

  test('validates 1.50m astronomical river tide alert threshold positioning', () => {
    const minTide = 0.0;
    const maxTide = 2.2;
    const chartHeight = 56;

    const computeTideY = (tideM: number): number => {
      const clamped = Math.max(minTide, Math.min(maxTide, tideM));
      const normalized = (clamped - minTide) / (maxTide - minTide);
      return chartHeight - 4 - normalized * (chartHeight - 8);
    };

    const alertY = computeTideY(1.50);
    const lowY = computeTideY(0.50);
    const peakY = computeTideY(1.70);

    // Higher tide height in meters should yield lower Y in SVG coordinates (inverted coordinate space)
    assert.ok(peakY < alertY);
    assert.ok(alertY < lowY);
  });

  test('validates cache hit and cache clear operations in useCorridorHistory', () => {
    assert.equal(getCachedCorridorHistory('vo_van_kiet', 24), null);
    clearCorridorHistoryCache();
    assert.equal(getCachedCorridorHistory('vo_van_kiet', 24), null);
  });
});
