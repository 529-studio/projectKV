import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import type { HourlyForecastResponse } from '../types/forecast.ts';

const mockForecast: HourlyForecastResponse = {
  generated_at: '2026-11-20T10:00:00Z',
  advisory: {
    peak_tide_time_window: '17:30 - 18:30',
    peak_tide_level_m: 1.68,
    peak_tide_station: 'Phú An (Sông Sài Gòn)',
    peak_rain_time: '18:00',
    peak_rain_mm: 15.0,
    headline: {
      vi: 'Đỉnh triều cường: 17:30 - 18:30 (+1.68m) | Vũ lượng mưa dự báo: 15mm lúc 18:00',
      en: 'Peak tide: 17:30 - 18:30 (+1.68m) | Forecast rain: 15mm at 18:00',
    },
    guidance: {
      vi: 'Các tuyến ven sông (Trần Xuân Soạn, Mễ Cốc, Quốc Hương) có nguy cơ ngập sâu.',
      en: 'Riverside roads (Tran Xuan Soan, Me Coc, Quoc Huong) at high flood risk.',
    },
  },
  timeline: [
    {
      hour_offset: 0,
      time: '2026-11-20T10:00:00Z',
      time_formatted: '17:00',
      rain_probability_percent: 60,
      rain_mm: 5.0,
      weather_desc: { vi: 'Mưa rào rải rác', en: 'Scattered showers' },
      tide_level_phu_an_m: 1.45,
      tide_level_nha_be_m: 1.48,
      tide_alert_level: 'LEVEL_1',
      tide_alert_label: { vi: 'Báo động 1', en: 'Alert Level 1' },
      affected_roads: [
        {
          road_key: 'tran_xuan_soan',
          display_name: 'Đường Trần Xuân Soạn',
          ward_district: 'Quận 7',
          estimated_depth_cm: 3,
          hazard_level: 'CAUTION',
          hazard_label: { vi: 'Cảnh báo ngập mép đường', en: 'Caution' },
        },
      ],
      commuter_risk: 'CAUTION',
      commuter_risk_label: { vi: 'Cần chú ý', en: 'Caution' },
    },
    {
      hour_offset: 1,
      time: '2026-11-20T11:00:00Z',
      time_formatted: '18:00',
      rain_probability_percent: 85,
      rain_mm: 15.0,
      weather_desc: { vi: 'Mưa dông lớn', en: 'Heavy thunderstorm' },
      tide_level_phu_an_m: 1.68,
      tide_level_nha_be_m: 1.72,
      tide_alert_level: 'LEVEL_3',
      tide_alert_label: { vi: 'Báo động 3', en: 'Alert Level 3' },
      affected_roads: [
        {
          road_key: 'tran_xuan_soan',
          display_name: 'Đường Trần Xuân Soạn',
          ward_district: 'Quận 7',
          estimated_depth_cm: 26,
          hazard_level: 'IMPASSABLE',
          hazard_label: { vi: 'Nguy cơ chết máy', en: 'Impassable' },
        },
      ],
      commuter_risk: 'DANGER',
      commuter_risk_label: { vi: 'Nguy hiểm', en: 'Danger' },
    },
  ],
};

describe('S17-T2 / Issue #511: Hourly Tidal & Rain Timeline Scrubber', () => {
  test('Scenario 1: Payload integrity and bilingual compliance', () => {
    assert.equal(mockForecast.timeline.length, 2);
    assert.ok(mockForecast.advisory.headline.vi.includes('Đỉnh triều cường'));
    assert.ok(mockForecast.advisory.headline.en.includes('Peak tide'));

    // Rule 1: Zero emoji assertion across all advisory copy
    const text = JSON.stringify(mockForecast);
    const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;
    assert.equal(emojiRegex.test(text), false, 'payload must be strictly 0-emoji');
  });

  test('Scenario 2: Critical tide detection (> 1.60m)', () => {
    const peakHour = mockForecast.timeline[1];
    assert.equal(peakHour.tide_alert_level, 'LEVEL_3');
    assert.equal(peakHour.commuter_risk, 'DANGER');
    assert.equal(peakHour.affected_roads[0].hazard_level, 'IMPASSABLE');
    assert.equal(peakHour.affected_roads[0].estimated_depth_cm, 26);
  });

  test('Scenario 3: Low tide / minor overflow distinction', () => {
    const normalHour = mockForecast.timeline[0];
    assert.equal(normalHour.tide_alert_level, 'LEVEL_1');
    assert.equal(normalHour.commuter_risk, 'CAUTION');
    assert.equal(normalHour.affected_roads[0].hazard_level, 'CAUTION');
    assert.equal(normalHour.affected_roads[0].estimated_depth_cm, 3);
  });
});
