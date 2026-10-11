import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  normalizeFloodHexCell,
  getFloodHexColorAndOpacity,
  isFloodVisibleAtZoom,
  getFloodInteractivityState,
  formatFloodHexTooltip,
  formatRadarHexTooltip,
  getFloodSeverityLabel,
  FLOOD_I18N,
  type FloodHexCell,
} from './floodUtils.ts';
import { FLOOD_VERIFY_I18N } from '../config/constants.ts';

describe('Flood Hex Overlay Utilities (Issue #106)', () => {
  const samplePoly: [number, number][] = [
    [10.776, 106.700],
    [10.780, 106.705],
    [10.775, 106.710],
  ];

  test('normalizeFloodHexCell accepts valid flood hex cell payloads', () => {
    const raw = {
      h3_id: '8765b18a1ffffff',
      resolution: 7,
      flood_severity: 'FLOODED_DEEP',
      camera_count: 2,
      worst_camera: 'cam_flood_01',
      worst_camera_name: 'Đường Nguyễn Hữu Cảnh',
      polygon: samplePoly,
    };
    const cell = normalizeFloodHexCell(raw);
    assert.ok(cell);
    assert.equal(cell.h3_id, '8765b18a1ffffff');
    assert.equal(cell.resolution, 7);
    assert.equal(cell.flood_severity, 'FLOODED_DEEP');
    assert.equal(cell.camera_count, 2);
    assert.equal(cell.worst_camera, 'cam_flood_01');
    assert.equal(cell.worst_camera_name, 'Đường Nguyễn Hữu Cảnh');
    assert.equal(cell.polygon.length, 3);
  });

  test('normalizeFloodHexCell supports camelCase property aliases', () => {
    const raw = {
      h3Id: '8765b18a2ffffff',
      resolution: 8,
      floodSeverity: 'flooded_shallow',
      cameraCount: 1,
      worstCamera: 'cam_flood_02',
      worstCameraName: 'Đường Huỳnh Tấn Phát',
      polygon: samplePoly,
    };
    const cell = normalizeFloodHexCell(raw);
    assert.ok(cell);
    assert.equal(cell.h3_id, '8765b18a2ffffff');
    assert.equal(cell.flood_severity, 'FLOODED_SHALLOW');
    assert.equal(cell.camera_count, 1);
    assert.equal(cell.worst_camera_name, 'Đường Huỳnh Tấn Phát');
  });

  test('normalizeFloodHexCell rejects invalid or malformed payloads', () => {
    assert.equal(normalizeFloodHexCell(null), null);
    assert.equal(normalizeFloodHexCell(undefined), null);
    assert.equal(normalizeFloodHexCell({}), null);
    assert.equal(normalizeFloodHexCell({ h3_id: '' }), null);
    // Fewer than 3 points in polygon
    assert.equal(normalizeFloodHexCell({ h3_id: 'abc', polygon: [[10, 106], [11, 107]] }), null);
    // Non-number coordinates
    assert.equal(normalizeFloodHexCell({ h3_id: 'abc', polygon: [[10, '106'], [11, 107], [12, 108]] }), null);
  });

  test('getFloodHexColorAndOpacity applies functional blue/cyan palette', () => {
    // FLOODED_DEEP: deep blue with 0.18 fillOpacity
    const deep = getFloodHexColorAndOpacity('FLOODED_DEEP');
    assert.equal(deep.fillColor, '#1d4ed8');
    assert.equal(deep.fillOpacity, 0.18);
    assert.equal(deep.strokeColor, '#1e40af');
    assert.equal(deep.strokeWeight, 1.0);

    // FLOODED_SHALLOW: ocean blue with 0.12 fillOpacity
    const shallow = getFloodHexColorAndOpacity('FLOODED_SHALLOW');
    assert.equal(shallow.fillColor, '#0284c7');
    assert.equal(shallow.fillOpacity, 0.12);
    assert.equal(shallow.strokeColor, '#0369a1');
    assert.equal(shallow.strokeWeight, 0.75);

    // WET: light sky blue with 0.07 fillOpacity
    const wet = getFloodHexColorAndOpacity('WET');
    assert.equal(wet.fillColor, '#38bdf8');
    assert.equal(wet.fillOpacity, 0.07);
    assert.equal(wet.strokeColor, '#38bdf8');
    assert.equal(wet.strokeWeight, 0.5);
  });

  test('isFloodVisibleAtZoom enables macro zoom (11-14) and hides micro zoom (15+)', () => {
    assert.equal(isFloodVisibleAtZoom(10), true);
    assert.equal(isFloodVisibleAtZoom(11), true);
    assert.equal(isFloodVisibleAtZoom(12), true);
    assert.equal(isFloodVisibleAtZoom(13), true);
    assert.equal(isFloodVisibleAtZoom(14), true);
    assert.equal(isFloodVisibleAtZoom(15), false);
    assert.equal(isFloodVisibleAtZoom(18), false);
  });

  test('getFloodInteractivityState controls visibility, pointerEvents, and CSS classes across zoom levels', () => {
    // Macro zoom (<= 14)
    const macroState = getFloodInteractivityState(14);
    assert.equal(macroState.isVisible, true);
    assert.equal(macroState.pointerEvents, 'auto');
    assert.equal(macroState.className, 'hex-cell-polygon');

    // Micro zoom (>= 15)
    const microState = getFloodInteractivityState(15);
    assert.equal(microState.isVisible, false);
    assert.equal(microState.pointerEvents, 'none');
    assert.equal(microState.className, 'hex-cell-polygon hex-cell-hidden');
  });

  test('getFloodSeverityLabel provides correct Vietnamese diacritics and English labels', () => {
    assert.equal(getFloodSeverityLabel('FLOODED_DEEP', 'vi'), 'Mưa to (nguy cơ ngập)');
    assert.equal(getFloodSeverityLabel('FLOODED_DEEP', 'en'), 'Heavy rain (flood risk)');
    assert.equal(getFloodSeverityLabel('FLOODED_SHALLOW', 'vi'), 'Mưa vừa');
    assert.equal(getFloodSeverityLabel('FLOODED_SHALLOW', 'en'), 'Moderate rain');
    assert.equal(getFloodSeverityLabel('WET', 'vi'), 'Mưa nhẹ');
    assert.equal(getFloodSeverityLabel('WET', 'en'), 'Light rain');
    assert.equal(getFloodSeverityLabel('UNKNOWN', 'vi'), 'Chưa xác minh');
  });

  test('formatFloodHexTooltip shows severity, camera count, and verified subtitle; never a ward name', () => {
    const cell: FloodHexCell = {
      h3_id: '8765b18a1ffffff',
      resolution: 7,
      flood_severity: 'FLOODED_DEEP',
      camera_count: 2,
      worst_camera: 'cam_flood_01',
      worst_camera_name: 'Đường Ung Văn Khiêm',
      polygon: samplePoly,
    };

    const viTooltip = formatFloodHexTooltip(cell, 'vi');
    assert.ok(viTooltip.includes('Camera xác nhận ngập'));
    assert.ok(viTooltip.includes('Xác nhận bởi camera giám sát'));
    assert.ok(viTooltip.includes('Mưa to (nguy cơ ngập)'));
    assert.ok(viTooltip.includes('2 camera giám sát'));
    assert.ok(viTooltip.includes('Điểm mưa/ngập nặng nhất'));
    assert.ok(viTooltip.includes('Đường Ung Văn Khiêm'));
    assert.ok(!viTooltip.includes('Phường'));
    assert.ok(!viTooltip.includes('Quận'));

    const enTooltip = formatFloodHexTooltip(cell, 'en');
    assert.ok(enTooltip.includes('Camera-verified flooding'));
    assert.ok(enTooltip.includes('Verified by surveillance cameras'));
    assert.ok(enTooltip.includes('Heavy rain (flood risk)'));
    assert.ok(enTooltip.includes('2 cameras monitoring'));
    assert.ok(enTooltip.includes('Worst rain/flood spot'));
  });

  test('formatRadarHexTooltip explicitly displays forecast-not-observation disclaimer', () => {
    const radarCell: FloodHexCell = {
      h3_id: '8765b18a1ffffff',
      resolution: 7,
      flood_severity: 'FLOODED_SHALLOW',
      camera_count: 0,
      worst_camera: 'radar_rainviewer',
      worst_camera_name: 'Dự báo radar thời tiết',
      polygon: samplePoly,
    };

    const viTooltip = formatRadarHexTooltip(radarCell, 'vi');
    assert.ok(viTooltip.includes('Dự báo radar'));
    assert.ok(viTooltip.includes('Dự báo — không phải quan sát thực tế'));
    assert.ok(viTooltip.includes('Mưa vừa'));
    assert.ok(viTooltip.includes('Radar thời tiết RainViewer'));

    const enTooltip = formatRadarHexTooltip(radarCell, 'en');
    assert.ok(enTooltip.includes('Radar forecast'));
    assert.ok(enTooltip.includes('Forecast — not a live observation'));
    assert.ok(enTooltip.includes('Moderate rain'));
    assert.ok(enTooltip.includes('RainViewer weather radar'));
  });

  test('honesty rule: never says "OK to go" or "safe" for unknown/unverified data', () => {
    const labelVi = getFloodSeverityLabel('UNKNOWN', 'vi');
    const labelEn = getFloodSeverityLabel('UNKNOWN', 'en');
    assert.ok(!labelVi.toLowerCase().includes('ok'));
    assert.ok(!labelVi.toLowerCase().includes('an toàn'));
    assert.ok(!labelEn.toLowerCase().includes('ok to go'));
    assert.ok(!labelEn.toLowerCase().includes('safe'));
  });

  test('FLOOD_I18N contains all required bilingual keys with Vietnamese diacritics', () => {
    assert.equal(FLOOD_I18N.LAYER_TRAFFIC.vi, 'Giao thông');
    assert.equal(FLOOD_I18N.LAYER_FLOOD.vi, 'Vùng mưa');
    assert.equal(FLOOD_I18N.LAYER_RADAR.vi, 'Dự báo radar');
    assert.equal(FLOOD_I18N.LAYER_RADAR.en, 'Radar forecast');
    assert.equal(FLOOD_I18N.LAYER_RADAR_SUBTITLE.vi, 'Dự báo — không phải quan sát thực tế');
    assert.equal(FLOOD_I18N.LAYER_RADAR_SUBTITLE.en, 'Forecast — not a live observation');
    assert.equal(FLOOD_I18N.LAYER_FLOOD_VERIFIED.vi, 'Camera xác nhận ngập');
    assert.equal(FLOOD_I18N.LAYER_FLOOD_VERIFIED.en, 'Camera-verified flooding');
    assert.equal(FLOOD_I18N.LAYER_FLOOD_VERIFIED_SUBTITLE.vi, 'Xác nhận bởi camera giám sát');
    assert.equal(FLOOD_I18N.LAYER_FLOOD_VERIFIED_SUBTITLE.en, 'Verified by surveillance cameras');
    assert.equal(FLOOD_I18N.TOOLTIP_TITLE.vi, 'Thời tiết khu vực');
    assert.equal(FLOOD_I18N.FILTER_CHIP.vi(3), 'Điểm ngập camera (3)');
    assert.equal(FLOOD_I18N.FILTER_CHIP.en(3), 'Camera flood spots (3)');
    assert.equal(FLOOD_I18N.ACTIVE_ALERT_BADGE.vi(2), 'Vùng có mưa • 2');
  });

  test('FLOOD_VERIFY_I18N has valid bilingual strings and zero emoji (Rule 1 / Issue #209)', () => {
    assert.equal(FLOOD_VERIFY_I18N.BUTTON_LABEL.vi, 'Xác minh ngập nước');
    assert.equal(FLOOD_VERIFY_I18N.BUTTON_LABEL.en, 'Verify flood status');
    assert.equal(FLOOD_VERIFY_I18N.VERIFYING.vi, 'Đang xác minh VLM...');
    assert.equal(FLOOD_VERIFY_I18N.VERIFYING.en, 'Verifying with VLM...');
    assert.equal(FLOOD_VERIFY_I18N.BADGE_PREFIX.vi, 'XÁC MINH VLM');
    assert.equal(FLOOD_VERIFY_I18N.BADGE_PREFIX.en, 'VLM VERIFIED');
    assert.equal(FLOOD_VERIFY_I18N.SEVERITY.DRY.vi, 'Khô ráo');
    assert.equal(FLOOD_VERIFY_I18N.SEVERITY.WET.vi, 'Đường ướt');
    assert.equal(FLOOD_VERIFY_I18N.SEVERITY.FLOODED_SHALLOW.vi, 'Ngập nông');
    assert.equal(FLOOD_VERIFY_I18N.SEVERITY.FLOODED_DEEP.vi, 'Ngập sâu');

    // Emoji regex to enforce Rule 1
    const emojiRegex = /\p{Extended_Pictographic}/u;
    const checkNoEmoji = (obj: Record<string, unknown>) => {
      for (const val of Object.values(obj)) {
        if (typeof val === 'string') {
          assert.ok(!emojiRegex.test(val), `Emoji detected in string: ${val}`);
        } else if (typeof val === 'object' && val !== null) {
          checkNoEmoji(val as Record<string, unknown>);
        }
      }
    };
    checkNoEmoji(FLOOD_VERIFY_I18N);
  });
});
