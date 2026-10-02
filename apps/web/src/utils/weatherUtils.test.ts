import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import {
  WEATHER_I18N,
  getWeatherCategory,
  formatTemperature,
  formatRainProbability,
  formatHour,
} from './weatherUtils.ts';

describe('Weather Utilities (Issue #120)', () => {
  test('WEATHER_I18N contains all bilingual keys with valid Vietnamese diacritics', () => {
    assert.equal(WEATHER_I18N.RAIN_FORECAST.vi, 'Dự báo mưa & Thời tiết');
    assert.equal(WEATHER_I18N.RAIN_FORECAST.en, 'Rain & Weather Forecast');
    assert.equal(WEATHER_I18N.RAIN_CHANCE.vi, 'Xác suất mưa');
    assert.equal(WEATHER_I18N.RAIN_CHANCE.en, 'Rain chance');
    assert.equal(WEATHER_I18N.FEELS_LIKE.vi, 'Cảm giác như');
    assert.equal(WEATHER_I18N.HUMIDITY.vi, 'Độ ẩm');
    assert.equal(WEATHER_I18N.HOURLY_TITLE.vi, 'Dự báo mưa 6 giờ tới');
    assert.equal(WEATHER_I18N.RADAR_LAYER.vi, 'Radar mây mưa (Doppler)');
    assert.equal(WEATHER_I18N.ATTRIBUTION.vi, 'Dữ liệu thời tiết © Open-Meteo & RainViewer');
  });

  test('getWeatherCategory correctly maps WMO codes', () => {
    assert.equal(getWeatherCategory(0), 'sun');
    assert.equal(getWeatherCategory(1), 'cloud');
    assert.equal(getWeatherCategory(3), 'cloud');
    assert.equal(getWeatherCategory(45), 'cloud');
    assert.equal(getWeatherCategory(51), 'drizzle');
    assert.equal(getWeatherCategory(61), 'rain');
    assert.equal(getWeatherCategory(80), 'rain');
    assert.equal(getWeatherCategory(95), 'storm');
    assert.equal(getWeatherCategory(99), 'storm');
    assert.equal(getWeatherCategory(999), 'cloud');
  });

  test('formatTemperature handles valid numbers and edge cases', () => {
    assert.equal(formatTemperature(29.4), '29°C');
    assert.equal(formatTemperature(29.6), '30°C');
    assert.equal(formatTemperature(0), '0°C');
    assert.equal(formatTemperature(undefined), '--°C');
  });

  test('formatRainProbability formats percentages', () => {
    assert.equal(formatRainProbability(65), '65%');
    assert.equal(formatRainProbability(0), '0%');
    assert.equal(formatRainProbability(undefined), '0%');
  });

  test('formatHour parses time correctly', () => {
    assert.equal(formatHour('2026-10-02T15:00'), '15:00');
    assert.equal(formatHour('14:30'), '14:30');
    assert.equal(formatHour(''), '');
  });
});
