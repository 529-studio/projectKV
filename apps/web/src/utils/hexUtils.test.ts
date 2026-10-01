import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  normalizeHexCell,
  getHexColorAndOpacity,
  isHexVisibleAtZoom,
  formatHexTooltip,
  HEX_I18N,
  type HexCell,
} from './hexUtils.ts';

describe('Hex Overlay Utilities (Issue #74)', () => {
  const samplePoly: [number, number][] = [
    [10.776, 106.700],
    [10.780, 106.705],
    [10.775, 106.710],
  ];

  test('normalizeHexCell accepts valid hex cell payloads', () => {
    const raw = {
      h3_id: '8765b18a1ffffff',
      resolution: 7,
      severity: 'CONGESTED',
      camera_count: 5,
      worst_camera: 'cam_001',
      worst_camera_name: 'Ngã tư Hàng Xanh',
      polygon: samplePoly,
    };
    const cell = normalizeHexCell(raw);
    assert.ok(cell);
    assert.equal(cell.h3_id, '8765b18a1ffffff');
    assert.equal(cell.resolution, 7);
    assert.equal(cell.severity, 'CONGESTED');
    assert.equal(cell.camera_count, 5);
    assert.equal(cell.worst_camera, 'cam_001');
    assert.equal(cell.worst_camera_name, 'Ngã tư Hàng Xanh');
    assert.equal(cell.polygon.length, 3);
  });

  test('normalizeHexCell supports camelCase property aliases', () => {
    const raw = {
      h3Id: '8765b18a2ffffff',
      resolution: 9,
      severity: 'moderate',
      cameraCount: 3,
      worstCamera: 'cam_002',
      worstCameraName: 'Cầu Sài Gòn',
      polygon: samplePoly,
    };
    const cell = normalizeHexCell(raw);
    assert.ok(cell);
    assert.equal(cell.h3_id, '8765b18a2ffffff');
    assert.equal(cell.severity, 'MODERATE');
    assert.equal(cell.camera_count, 3);
    assert.equal(cell.worst_camera_name, 'Cầu Sài Gòn');
  });

  test('normalizeHexCell rejects invalid or malformed payloads', () => {
    assert.equal(normalizeHexCell(null), null);
    assert.equal(normalizeHexCell(undefined), null);
    assert.equal(normalizeHexCell({}), null);
    assert.equal(normalizeHexCell({ h3_id: '' }), null);
    // Fewer than 3 points in polygon
    assert.equal(normalizeHexCell({ h3_id: 'abc', polygon: [[10, 106], [11, 107]] }), null);
    // Non-number coordinates
    assert.equal(normalizeHexCell({ h3_id: 'abc', polygon: [[10, '106'], [11, 107], [12, 108]] }), null);
  });

  test('getHexColorAndOpacity applies approved low-opacity tints per ADR-004 §D11', () => {
    // CLEAR / DRY: green with 0.18 fillOpacity
    const clear = getHexColorAndOpacity('CLEAR');
    assert.equal(clear.fillColor, '#22c55e');
    assert.equal(clear.fillOpacity, 0.18);

    const dry = getHexColorAndOpacity('DRY');
    assert.equal(dry.fillColor, '#22c55e');
    assert.equal(dry.fillOpacity, 0.18);

    // MODERATE / WET: yellow with 0.18 fillOpacity
    const mod = getHexColorAndOpacity('MODERATE');
    assert.equal(mod.fillColor, '#eab308');
    assert.equal(mod.fillOpacity, 0.18);

    const wet = getHexColorAndOpacity('WET');
    assert.equal(wet.fillColor, '#eab308');
    assert.equal(wet.fillOpacity, 0.18);

    // HEAVY / CONGESTED / FLOOD: red with 0.22 fillOpacity
    const heavy = getHexColorAndOpacity('HEAVY');
    assert.equal(heavy.fillColor, '#ef4444');
    assert.equal(heavy.fillOpacity, 0.22);

    const congested = getHexColorAndOpacity('CONGESTED');
    assert.equal(congested.fillColor, '#ef4444');
    assert.equal(congested.fillOpacity, 0.22);

    const floodDeep = getHexColorAndOpacity('FLOODED_DEEP');
    assert.equal(floodDeep.fillColor, '#ef4444');
    assert.equal(floodDeep.fillOpacity, 0.22);
  });

  test('isHexVisibleAtZoom enables macro zoom (11-13) and hides micro zoom (14+)', () => {
    assert.equal(isHexVisibleAtZoom(10), true);
    assert.equal(isHexVisibleAtZoom(11), true);
    assert.equal(isHexVisibleAtZoom(12), true);
    assert.equal(isHexVisibleAtZoom(13), true);
    assert.equal(isHexVisibleAtZoom(14), false);
    assert.equal(isHexVisibleAtZoom(15), false);
    assert.equal(isHexVisibleAtZoom(18), false);
  });

  test('formatHexTooltip shows camera count and worst camera, never a ward name', () => {
    const cell: HexCell = {
      h3_id: '8765b18a1ffffff',
      resolution: 7,
      severity: 'CONGESTED',
      camera_count: 4,
      worst_camera: 'cam_529',
      worst_camera_name: 'Vòng xoay Dân Chủ',
      polygon: samplePoly,
    };

    // Vietnamese
    const viTooltip = formatHexTooltip(cell, 'vi');
    assert.match(viTooltip, /4 camera đang giám sát/);
    assert.match(viTooltip, /Tình trạng nặng nhất:/);
    assert.match(viTooltip, /Vòng xoay Dân Chủ/);
    assert.doesNotMatch(viTooltip, /Phường/);
    assert.doesNotMatch(viTooltip, /Xã/);

    // English
    const enTooltip = formatHexTooltip(cell, 'en');
    assert.match(enTooltip, /4 cameras monitoring/);
    assert.match(enTooltip, /Worst status:/);
    assert.match(enTooltip, /Vòng xoay Dân Chủ/);
    assert.doesNotMatch(enTooltip, /Ward/);
    assert.doesNotMatch(enTooltip, /Commune/);

    // Single camera pluralization in English
    const singleCell: HexCell = { ...cell, camera_count: 1 };
    assert.match(formatHexTooltip(singleCell, 'en'), /1 camera monitoring/);
  });

  test('HEX_I18N contains all required strings with proper Vietnamese diacritics', () => {
    assert.equal(HEX_I18N.TOGGLE.vi, 'Vùng tổng hợp');
    assert.equal(HEX_I18N.TOGGLE.en, 'Zone Overview');

    assert.equal(HEX_I18N.WORST_CAMERA.vi, 'Tình trạng nặng nhất');
    assert.equal(HEX_I18N.WORST_CAMERA.en, 'Worst status');

    assert.equal(HEX_I18N.ZONE_CLEAR.vi, 'Thông thoáng (vùng)');
    assert.equal(HEX_I18N.ZONE_CLEAR.en, 'Clear (zone)');

    assert.equal(HEX_I18N.ZONE_MODERATE.vi, 'Đông xe (vùng)');
    assert.equal(HEX_I18N.ZONE_MODERATE.en, 'Moderate (zone)');

    assert.equal(HEX_I18N.ZONE_CONGESTED.vi, 'Ùn tắc (vùng)');
    assert.equal(HEX_I18N.ZONE_CONGESTED.en, 'Congested (zone)');
  });
});
