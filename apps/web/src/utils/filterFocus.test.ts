import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  computeFilteredCameraIds,
  computeActiveFilterLabel,
  calculateMarkerStyle,
  OTHER_WARDS_KEY,
  OUTSIDE_HCMC_KEY,
  type FilterableCamera,
} from './phuongFormatters.ts';

describe('computeFilteredCameraIds (Issue #65 ADR-004 A3)', () => {
  const sampleCameras: FilterableCamera[] = [
    { id: 'cam-1', displayName: 'Trần Quang Khải - Hai Bà Trưng', phuong_name: 'Phường Tân Định' },
    { id: 'cam-2', displayName: 'Trần Quang Khải - Đinh Tiên Hoàng', phuong_name: 'Phường Tân Định' },
    { id: 'cam-3', displayName: 'Nguyễn Huệ - Lê Lợi', phuong_name: 'Phường Bến Nghé' },
    { id: 'cam-4', displayName: 'Cầu Sài Gòn - Dạ cầu', phuong_name: 'Phường Thạnh Mỹ Tây' },
    { id: 'cam-5', displayName: 'Nút giao Hàng Xanh 1', phuong_name: 'Phường Gia Định' },
    { id: 'cam-6', displayName: 'Camera Ngoại Thành', phuong_name: undefined },
  ];

  test('returns undefined when no filter is active', () => {
    assert.equal(computeFilteredCameraIds(sampleCameras, null, null), undefined);
  });

  test('filters cameras by Phường', () => {
    const result = computeFilteredCameraIds(sampleCameras, 'Phường Tân Định', null);
    assert.ok(result);
    assert.equal(result.size, 2);
    assert.ok(result.has('cam-1'));
    assert.ok(result.has('cam-2'));
    assert.ok(!result.has('cam-3'));
  });

  test('filters cameras in Other Wards (unmatched/outside)', () => {
    const result = computeFilteredCameraIds(sampleCameras, OTHER_WARDS_KEY, null);
    assert.ok(result);
    assert.equal(result.size, 1);
    assert.ok(result.has('cam-6'));

    const resultOutside = computeFilteredCameraIds(sampleCameras, OUTSIDE_HCMC_KEY, null);
    assert.ok(resultOutside);
    assert.equal(resultOutside.size, 1);
    assert.ok(resultOutside.has('cam-6'));
  });

  test('filters cameras by Corridor', () => {
    const result = computeFilteredCameraIds(sampleCameras, null, 'Trần Quang Khải');
    assert.ok(result);
    assert.equal(result.size, 2);
    assert.ok(result.has('cam-1'));
    assert.ok(result.has('cam-2'));
  });

  test('filters cameras by both Phường and Corridor intersection', () => {
    const result = computeFilteredCameraIds(sampleCameras, 'Phường Tân Định', 'Trần Quang Khải');
    assert.ok(result);
    assert.equal(result.size, 2);
    assert.ok(result.has('cam-1'));
    assert.ok(result.has('cam-2'));

    // Corridor not in that phuong
    const emptyResult = computeFilteredCameraIds(sampleCameras, 'Phường Bến Nghé', 'Trần Quang Khải');
    assert.ok(emptyResult);
    assert.equal(emptyResult.size, 0);
  });
});

describe('computeActiveFilterLabel (On-map filter chip)', () => {
  test('returns null when no filter is active', () => {
    assert.equal(computeActiveFilterLabel(null, null, 'vi'), null);
    assert.equal(computeActiveFilterLabel(null, null, 'en'), null);
  });

  test('formats Phường filter label in VI and EN', () => {
    assert.equal(
      computeActiveFilterLabel('Phường Tân Định', null, 'vi'),
      'Phường Tân Định'
    );
    assert.equal(
      computeActiveFilterLabel('Phường Tân Định', null, 'en'),
      'Tân Định Ward'
    );
    assert.equal(
      computeActiveFilterLabel(OTHER_WARDS_KEY, null, 'vi'),
      'Các Phường khác'
    );
    assert.equal(
      computeActiveFilterLabel(OTHER_WARDS_KEY, null, 'en'),
      'Other Wards'
    );
  });

  test('formats Phường and Corridor composite label in VI and EN', () => {
    assert.equal(
      computeActiveFilterLabel('Phường Tân Định', 'Trần Quang Khải', 'vi'),
      'Phường Tân Định → Đường Trần Quang Khải'
    );
    assert.equal(
      computeActiveFilterLabel('Phường Tân Định', 'Trần Quang Khải', 'en'),
      'Tân Định Ward → Street Trần Quang Khải'
    );
    assert.equal(
      computeActiveFilterLabel('Phường Gia Định', 'Nút giao Hàng Xanh 1', 'vi'),
      'Phường Gia Định → Nút giao Hàng Xanh 1'
    );
    assert.equal(
      computeActiveFilterLabel('Phường Gia Định', 'Nút giao Hàng Xanh 1', 'en'),
      'Gia Định Ward → Junction Hàng Xanh 1'
    );
    assert.equal(
      computeActiveFilterLabel('Phường Thạnh Mỹ Tây', 'Cầu Sài Gòn', 'en'),
      'Thạnh Mỹ Tây Ward → Bridge Sài Gòn'
    );
  });
});

describe('calculateMarkerStyle (Dimmed vs Normal styling)', () => {
  test('returns dimmed styling with 0.2 fillOpacity and 30% reduced radius', () => {
    const baseRadius = 8;
    const style = calculateMarkerStyle(true, baseRadius, 14);

    assert.equal(style.fillOpacity, 0.2);
    assert.equal(style.opacity, 0.25);
    assert.equal(style.weight, 1);
    assert.equal(style.strokeColor, '#27272a');
    // 8 * 0.7 = 5.6 -> round = 6
    assert.equal(style.radius, 6);
  });

  test('returns normal styling with full opacity and standard radius', () => {
    const baseRadius = 8;
    const styleHighZoom = calculateMarkerStyle(false, baseRadius, 14);

    assert.equal(styleHighZoom.fillOpacity, 0.95);
    assert.equal(styleHighZoom.opacity, 1.0);
    assert.equal(styleHighZoom.weight, 2);
    assert.equal(styleHighZoom.radius, 8);

    const styleLowZoom = calculateMarkerStyle(false, baseRadius, 12);
    assert.equal(styleLowZoom.weight, 1.5);
  });

  test('returns hexMacro dimmed styling with 0.25 fillOpacity and subtle stroke', () => {
    const baseRadius = 8;
    const style = calculateMarkerStyle(true, baseRadius, 12, 'hexMacro');

    assert.equal(style.fillOpacity, 0.25);
    assert.equal(style.opacity, 0.35);
    assert.equal(style.weight, 1);
    assert.equal(style.strokeColor, '#3f3f46');
    assert.equal(style.radius, 6);
  });
});
