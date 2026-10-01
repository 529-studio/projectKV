import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  normalizeFloodHexCell,
  getFloodHexColorAndOpacity,
  isFloodVisibleAtZoom,
  getFloodInteractivityState,
  formatFloodHexTooltip,
  getFloodSeverityLabel,
  FLOOD_I18N,
  type FloodHexCell,
} from './floodUtils.ts';

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
    // FLOODED_DEEP: deep blue with 0.26 fillOpacity
    const deep = getFloodHexColorAndOpacity('FLOODED_DEEP');
    assert.equal(deep.fillColor, '#1d4ed8');
    assert.equal(deep.fillOpacity, 0.26);
    assert.equal(deep.strokeColor, '#1e40af');
    assert.equal(deep.strokeWeight, 1.5);

    // FLOODED_SHALLOW: cyan with 0.22 fillOpacity
    const shallow = getFloodHexColorAndOpacity('FLOODED_SHALLOW');
    assert.equal(shallow.fillColor, '#0284c7');
    assert.equal(shallow.fillOpacity, 0.22);
    assert.equal(shallow.strokeColor, '#0369a1');
    assert.equal(shallow.strokeWeight, 1.2);

    // WET: slate blue with 0.15 fillOpacity
    const wet = getFloodHexColorAndOpacity('WET');
    assert.equal(wet.fillColor, '#64748b');
    assert.equal(wet.fillOpacity, 0.15);
    assert.equal(wet.strokeColor, '#475569');
    assert.equal(wet.strokeWeight, 1.0);
  });

  test('isFloodVisibleAtZoom enables macro zoom (11-13) and hides micro zoom (14+)', () => {
    assert.equal(isFloodVisibleAtZoom(10), true);
    assert.equal(isFloodVisibleAtZoom(11), true);
    assert.equal(isFloodVisibleAtZoom(12), true);
    assert.equal(isFloodVisibleAtZoom(13), true);
    assert.equal(isFloodVisibleAtZoom(14), false);
    assert.equal(isFloodVisibleAtZoom(15), false);
    assert.equal(isFloodVisibleAtZoom(18), false);
  });

  test('getFloodInteractivityState controls visibility, pointerEvents, and CSS classes across zoom levels', () => {
    // Macro zoom (<= 13)
    const macroState = getFloodInteractivityState(13);
    assert.equal(macroState.isVisible, true);
    assert.equal(macroState.pointerEvents, 'auto');
    assert.equal(macroState.className, 'hex-cell-polygon');

    // Micro zoom (>= 14)
    const microState = getFloodInteractivityState(14);
    assert.equal(microState.isVisible, false);
    assert.equal(microState.pointerEvents, 'none');
    assert.equal(microState.className, 'hex-cell-polygon hex-cell-hidden');
  });

  test('getFloodSeverityLabel provides correct Vietnamese diacritics and English labels', () => {
    assert.equal(getFloodSeverityLabel('FLOODED_DEEP', 'vi'), 'Ngập sâu (nguy hiểm)');
    assert.equal(getFloodSeverityLabel('FLOODED_DEEP', 'en'), 'Deep water (hazardous)');
    assert.equal(getFloodSeverityLabel('FLOODED_SHALLOW', 'vi'), 'Ngập nhẹ / Vũng nước');
    assert.equal(getFloodSeverityLabel('WET', 'vi'), 'Đường ướt / Mưa');
    assert.equal(getFloodSeverityLabel('UNKNOWN', 'vi'), 'Chưa xác minh');
  });

  test('formatFloodHexTooltip shows severity, camera count, and worst camera; never a ward name', () => {
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
    assert.ok(viTooltip.includes('Ngập sâu (nguy hiểm)'));
    assert.ok(viTooltip.includes('2 camera giám sát'));
    assert.ok(viTooltip.includes('Điểm ngập nặng nhất'));
    assert.ok(viTooltip.includes('Đường Ung Văn Khiêm'));
    assert.ok(!viTooltip.includes('Phường'));
    assert.ok(!viTooltip.includes('Quận'));

    const enTooltip = formatFloodHexTooltip(cell, 'en');
    assert.ok(enTooltip.includes('Deep water (hazardous)'));
    assert.ok(enTooltip.includes('2 cameras monitoring'));
    assert.ok(enTooltip.includes('Worst flood spot'));
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
    assert.equal(FLOOD_I18N.LAYER_FLOOD.vi, 'Ngập nước');
    assert.equal(FLOOD_I18N.TOOLTIP_TITLE.vi, 'Cảnh báo ngập');
    assert.equal(FLOOD_I18N.FILTER_CHIP.vi(3), '🌧️ Điểm ngập (3)');
    assert.equal(FLOOD_I18N.ACTIVE_ALERT_BADGE.vi(2), '🌧️ Vùng ngập • 2');
  });
});
