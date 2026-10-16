import { test, describe } from 'node:test';
import assert from 'node:assert';
import {
  SATURATION_COLORS,
  getSaturationColor,
  isHotsetCongested,
  getHotsetMarkerStyle,
  buildHotsetTooltipSection,
  parseHotsetStatusMap,
  type HotsetCameraStatus,
  type HotsetStatusResponse,
} from './hotsetUtils.ts';
import { HOTSET_LEGEND_I18N, API_ENDPOINTS } from '../config/constants.ts';

describe('Hot Set Saturation Utilities (Issue #251 / S7-T6)', () => {
  test('SATURATION_COLORS matches exact design specifications', () => {
    assert.strictEqual(SATURATION_COLORS.CLEAR, '#10b981');
    assert.strictEqual(SATURATION_COLORS.MODERATE, '#f59e0b');
    assert.strictEqual(SATURATION_COLORS.CONGESTED, '#ef4444');
  });

  test('API_ENDPOINTS contains HOTSET_STATUS', () => {
    assert.strictEqual(API_ENDPOINTS.HOTSET_STATUS, '/api/v1/cameras/hotset/status');
  });

  test('HOTSET_LEGEND_I18N provides exact bilingual strings without emojis', () => {
    assert.strictEqual(HOTSET_LEGEND_I18N.CLEAR.vi, '[Xanh: Thông thoáng]');
    assert.strictEqual(HOTSET_LEGEND_I18N.MODERATE.vi, '[Vàng: Đông xe]');
    assert.strictEqual(HOTSET_LEGEND_I18N.CONGESTED.vi, '[Đỏ: Ùn ứ]');

    assert.strictEqual(HOTSET_LEGEND_I18N.CLEAR.en, '[Green: Clear]');
    assert.strictEqual(HOTSET_LEGEND_I18N.MODERATE.en, '[Yellow: Moderate]');
    assert.strictEqual(HOTSET_LEGEND_I18N.CONGESTED.en, '[Red: Congested]');
  });

  test('getSaturationColor maps saturation index thresholds accurately', () => {
    // SI < 0.30 -> CLEAR (#10b981)
    assert.strictEqual(getSaturationColor(0.0), '#10b981');
    assert.strictEqual(getSaturationColor(0.15), '#10b981');
    assert.strictEqual(getSaturationColor(0.299), '#10b981');

    // 0.30 <= SI < 0.60 -> MODERATE (#f59e0b)
    assert.strictEqual(getSaturationColor(0.30), '#f59e0b');
    assert.strictEqual(getSaturationColor(0.45), '#f59e0b');
    assert.strictEqual(getSaturationColor(0.599), '#f59e0b');

    // SI >= 0.60 -> CONGESTED (#ef4444)
    assert.strictEqual(getSaturationColor(0.60), '#ef4444');
    assert.strictEqual(getSaturationColor(0.85), '#ef4444');
    assert.strictEqual(getSaturationColor(1.20), '#ef4444');
  });

  test('isHotsetCongested identifies severe arterial congestion (SI >= 0.60)', () => {
    assert.strictEqual(isHotsetCongested(0.1), false);
    assert.strictEqual(isHotsetCongested(0.59), false);
    assert.strictEqual(isHotsetCongested(0.60), true);
    assert.strictEqual(isHotsetCongested(0.95), true);
  });

  test('getHotsetMarkerStyle handles non-hotset, fresh, and stale statuses', () => {
    // Non-hotset fallback to online/offline default
    const nonHotOnline = getHotsetMarkerStyle(null, true);
    assert.strictEqual(nonHotOnline.isHotset, false);
    assert.strictEqual(nonHotOnline.fillColor, '#ffffff');
    assert.strictEqual(nonHotOnline.isCongested, false);
    assert.strictEqual(nonHotOnline.isStale, false);

    const nonHotOffline = getHotsetMarkerStyle(null, false);
    assert.strictEqual(nonHotOffline.fillColor, '#52525b');

    // Fresh clear hotset camera
    const clearCam: HotsetCameraStatus = {
      camera_id: 'cam-1',
      severity: 'CLEAR',
      saturation_index: 0.22,
      pcu_total: 12.5,
      confidence: 0.95,
      source: 'onnx',
      observed_at: new Date().toISOString(),
      stale: false,
    };
    const clearStyle = getHotsetMarkerStyle(clearCam, true);
    assert.strictEqual(clearStyle.isHotset, true);
    assert.strictEqual(clearStyle.fillColor, '#10b981');
    assert.strictEqual(clearStyle.isCongested, false);
    assert.strictEqual(clearStyle.isStale, false);

    // Fresh congested hotset camera (pulsing enabled)
    const congestedCam: HotsetCameraStatus = {
      camera_id: 'cam-2',
      severity: 'CONGESTED',
      saturation_index: 0.78,
      pcu_total: 58.0,
      confidence: 0.98,
      source: 'onnx',
      observed_at: new Date().toISOString(),
      stale: false,
    };
    const congestedStyle = getHotsetMarkerStyle(congestedCam, true);
    assert.strictEqual(congestedStyle.isHotset, true);
    assert.strictEqual(congestedStyle.fillColor, '#ef4444');
    assert.strictEqual(congestedStyle.isCongested, true);
    assert.strictEqual(congestedStyle.isStale, false);

    // Stale hotset camera (pulsing suppressed for safety)
    const staleCam: HotsetCameraStatus = {
      ...congestedCam,
      stale: true,
    };
    const staleStyle = getHotsetMarkerStyle(staleCam, true);
    assert.strictEqual(staleStyle.isHotset, true);
    assert.strictEqual(staleStyle.isStale, true);
    assert.strictEqual(staleStyle.isCongested, false);
  });

  test('buildHotsetTooltipSection formats saturation percentage and PCU without emojis', () => {
    const cam: HotsetCameraStatus = {
      camera_id: 'cam-1',
      severity: 'CONGESTED',
      saturation_index: 0.654,
      pcu_total: 45.2,
      confidence: 0.98,
      source: 'onnx',
      observed_at: new Date().toISOString(),
      stale: false,
    };

    const viTooltip = buildHotsetTooltipSection(cam, 'vi');
    assert.match(viTooltip, /Độ bão hòa \(SI\):.*65%/);
    assert.match(viTooltip, /PCU:.*45/);
    assert.doesNotMatch(viTooltip, /[\u{1F300}-\u{1FAFF}]/u); // Zero emoji rule

    const enTooltip = buildHotsetTooltipSection(cam, 'en');
    assert.match(enTooltip, /Saturation \(SI\):.*65%/);
    assert.match(enTooltip, /PCU:.*45/);
  });

  test('parseHotsetStatusMap converts raw API response into camera_id Map', () => {
    const raw: HotsetStatusResponse = {
      total: 2,
      updated_at: '2026-10-16T15:00:00Z',
      cameras: [
        {
          camera_id: 'cam-1',
          severity: 'CLEAR',
          saturation_index: 0.15,
          pcu_total: 8,
          confidence: 0.9,
          source: 'onnx',
          observed_at: '2026-10-16T15:00:00Z',
          stale: false,
        },
        {
          camera_id: 'cam-2',
          severity: 'CONGESTED',
          saturation_index: 0.72,
          pcu_total: 62,
          confidence: 0.99,
          source: 'onnx',
          observed_at: '2026-10-16T15:00:00Z',
          stale: false,
        },
      ],
    };

    const map = parseHotsetStatusMap(raw);
    assert.strictEqual(map.size, 2);
    assert.strictEqual(map.get('cam-1')?.saturation_index, 0.15);
    assert.strictEqual(map.get('cam-2')?.severity, 'CONGESTED');

    // Handles empty / malformed response gracefully
    assert.strictEqual(parseHotsetStatusMap(null).size, 0);
    assert.strictEqual(parseHotsetStatusMap(undefined).size, 0);
  });
});
