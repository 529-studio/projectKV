import { test, describe } from 'node:test';
import assert from 'node:assert';
import {
  SATURATION_COLORS,
  getSaturationColor,
  isHotsetCongested,
  isHotsetStatusValid,
  getHotsetMarkerStyle,
  buildHotsetTooltipSection,
  parseHotsetStatusMap,
  fetchHotsetStatus,
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

  test('isHotsetStatusValid requires fresh, non-empty, non-UNKNOWN severity', () => {
    assert.strictEqual(isHotsetStatusValid(null), false);
    assert.strictEqual(isHotsetStatusValid(undefined), false);

    // Stale is invalid
    assert.strictEqual(isHotsetStatusValid({
      camera_id: 'cam-1',
      severity: 'CLEAR',
      saturation_index: 0.1,
      pcu_total: 5,
      confidence: 0.9,
      source: 'onnx',
      observed_at: new Date().toISOString(),
      stale: true,
    }), false);

    // UNKNOWN is invalid
    assert.strictEqual(isHotsetStatusValid({
      camera_id: 'cam-2',
      severity: 'UNKNOWN',
      saturation_index: 0.0,
      pcu_total: 0,
      confidence: 0.0,
      source: 'fallback',
      observed_at: new Date().toISOString(),
      stale: false,
    }), false);

    // Empty severity is invalid
    assert.strictEqual(isHotsetStatusValid({
      camera_id: 'cam-3',
      severity: '',
      saturation_index: 0.0,
      pcu_total: 0,
      confidence: 0.0,
      source: 'fallback',
      observed_at: new Date().toISOString(),
      stale: false,
    }), false);

    // Fresh CLEAR / MODERATE / CONGESTED is valid
    assert.strictEqual(isHotsetStatusValid({
      camera_id: 'cam-4',
      severity: 'CLEAR',
      saturation_index: 0.15,
      pcu_total: 10,
      confidence: 0.95,
      source: 'onnx',
      observed_at: new Date().toISOString(),
      stale: false,
    }), true);
  });

  test('getHotsetMarkerStyle handles non-hotset, fresh, stale, and unknown statuses', () => {
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

    // Stale hotset camera (pulsing suppressed AND falls back to neutral online color)
    const staleCam: HotsetCameraStatus = {
      ...congestedCam,
      stale: true,
    };
    const staleStyle = getHotsetMarkerStyle(staleCam, true);
    assert.strictEqual(staleStyle.isHotset, true);
    assert.strictEqual(staleStyle.isStale, true);
    assert.strictEqual(staleStyle.fillColor, '#ffffff'); // Neutral white, not green or red!
    assert.strictEqual(staleStyle.isCongested, false);

    const staleOfflineStyle = getHotsetMarkerStyle(staleCam, false);
    assert.strictEqual(staleOfflineStyle.fillColor, '#52525b');

    // UNKNOWN hotset camera (e.g. cold-boot / standby / fallback) must NEVER masquerade as green
    const unknownCam: HotsetCameraStatus = {
      camera_id: 'cam-3',
      severity: 'UNKNOWN',
      saturation_index: 0.0,
      pcu_total: 0.0,
      confidence: 0.0,
      source: 'fallback',
      observed_at: new Date().toISOString(),
      stale: false,
    };
    const unknownStyle = getHotsetMarkerStyle(unknownCam, true);
    assert.strictEqual(unknownStyle.isHotset, true);
    assert.strictEqual(unknownStyle.fillColor, '#ffffff'); // Neutral white, NOT green (#10b981)
    assert.strictEqual(unknownStyle.isCongested, false);
  });

  test('buildHotsetTooltipSection formats saturation percentage and PCU without emojis, and omits for UNKNOWN', () => {
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

    // UNKNOWN severity suppresses SI / PCU lines to avoid misleading users with false 0%
    const unknownCam: HotsetCameraStatus = {
      camera_id: 'cam-2',
      severity: 'UNKNOWN',
      saturation_index: 0.0,
      pcu_total: 0.0,
      confidence: 0.0,
      source: 'fallback',
      observed_at: new Date().toISOString(),
      stale: false,
    };
    assert.strictEqual(buildHotsetTooltipSection(unknownCam, 'vi'), '');
    assert.strictEqual(buildHotsetTooltipSection(unknownCam, 'en'), '');
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

  test('fetchHotsetStatus fetches, parses, and handles errors gracefully', async () => {
    const mockData = {
      total: 2,
      updated_at: '2026-10-16T15:30:00Z',
      cameras: [
        {
          camera_id: 'cam-arterial-1',
          severity: 'MODERATE',
          saturation_index: 0.45,
          pcu_total: 35.0,
          confidence: 0.95,
          source: 'onnx',
          observed_at: '2026-10-16T15:30:00Z',
          stale: false,
        },
        {
          camera_id: 'cam-arterial-2',
          severity: 'CONGESTED',
          saturation_index: 0.88,
          pcu_total: 70.0,
          confidence: 0.99,
          source: 'onnx',
          observed_at: '2026-10-16T15:30:00Z',
          stale: false,
        },
      ],
    };

    const mockFetch = (async () => ({
      ok: true,
      status: 200,
      json: async () => mockData,
    })) as unknown as typeof fetch;

    const successRes = await fetchHotsetStatus('/api/v1/cameras/hotset/status', mockFetch);
    assert.strictEqual(successRes.total, 2);
    assert.strictEqual(successRes.hotsetStatus.size, 2);
    assert.strictEqual(successRes.hotsetStatus.get('cam-arterial-2')?.severity, 'CONGESTED');

    // 503 HTTP error
    const errFetch = (async () => ({
      ok: false,
      status: 503,
      json: async () => ({}),
    })) as unknown as typeof fetch;

    const errRes = await fetchHotsetStatus('/api/v1/cameras/hotset/status', errFetch);
    assert.strictEqual(errRes.total, 0);
    assert.strictEqual(errRes.error, 'HTTP error 503');

    // Network throw
    const throwFetch = (async () => {
      throw new Error('Connection refused');
    }) as unknown as typeof fetch;

    const throwRes = await fetchHotsetStatus('/api/v1/cameras/hotset/status', throwFetch);
    assert.strictEqual(throwRes.total, 0);
    assert.strictEqual(throwRes.error, 'Connection refused');

    // Abort error (clean cancellation)
    const abortFetch = (async () => {
      const err = new Error('The user aborted a request.');
      err.name = 'AbortError';
      throw err;
    }) as unknown as typeof fetch;

    const abortRes = await fetchHotsetStatus('/api/v1/cameras/hotset/status', abortFetch);
    assert.strictEqual(abortRes.error, null);
  });
});
