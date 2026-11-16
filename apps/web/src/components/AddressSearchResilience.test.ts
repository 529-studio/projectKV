import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  sanitizeAddressSearchQuery,
  matchesVietnameseSearch,
  groupCamerasByCityCorridors,
  formatStreetDisplayName,
  type FilterableCamera,
} from '../utils/phuongFormatters.ts';

/**
 * Empirical Automated Verification for Issue #484 / S15-T7:
 * Resilient Address Search & House Number Query Sanitizer
 */
describe('S15-T7: Resilient Address Search & House Number Verification', () => {
  const sampleCameras: FilterableCamera[] = [
    {
      id: 'cam-cmt8-1',
      displayName: 'Cách Mạng Tháng Tám - Phạm Văn Hai',
      title: 'Cách Mạng Tháng Tám - Phạm Văn Hai',
      phuong_name: 'Phường 5',
    },
    {
      id: 'cam-cmt8-2',
      displayName: 'Cách Mạng Tháng Tám - Tô Hiến Thành',
      title: 'Cách Mạng Tháng Tám - Tô Hiến Thành',
      phuong_name: 'Phường 13',
    },
    {
      id: 'cam-nt-1',
      displayName: 'Nguyễn Trãi - Lê Lai',
      title: 'Nguyễn Trãi - Lê Lai',
      phuong_name: 'Phường Bến Thành',
    },
    {
      id: 'cam-32-1',
      displayName: 'Ba Tháng Hai - Sư Vạn Hạnh',
      title: 'Ba Tháng Hai - Sư Vạn Hạnh',
      phuong_name: 'Phường 12',
    },
    {
      id: 'cam-ch-1',
      displayName: 'Cộng Hòa - Ấp Bắc',
      title: 'Cộng Hòa - Ấp Bắc',
      phuong_name: 'Phường 13',
    },
  ];

  test('Scenario 1: Input "34/55/67 cách mạng tháng tám" returns CMT8 cameras and identifies house number', () => {
    const query = '34/55/67 cách mạng tháng tám';
    const parsed = sanitizeAddressSearchQuery(query);

    assert.equal(parsed.isHouseNumberDetected, true);
    assert.equal(parsed.houseNumber, '34/55/67');
    assert.equal(parsed.cleanStreetQuery, 'cách mạng tháng tám');

    // UI formatted corridor name
    const formatted = formatStreetDisplayName(parsed.cleanStreetQuery);
    assert.equal(formatted, 'Cách Mạng Tháng Tám');

    // Verify cameras along corridor are matched
    const filtered = groupCamerasByCityCorridors(sampleCameras, undefined, query);
    assert.equal(filtered.length, 1);
    assert.equal(filtered[0].name, 'Cách Mạng Tháng Tám');
    assert.equal(filtered[0].cameras.length, 2);
  });

  test('Scenario 2: Input "Số 123 Nguyễn Trãi" returns Nguyễn Trãi cameras', () => {
    const query = 'Số 123 Nguyễn Trãi';
    const parsed = sanitizeAddressSearchQuery(query);

    assert.equal(parsed.isHouseNumberDetected, true);
    assert.equal(parsed.houseNumber, '123');
    assert.equal(parsed.cleanStreetQuery, 'Nguyễn Trãi');

    const filtered = groupCamerasByCityCorridors(sampleCameras, undefined, query);
    assert.equal(filtered.length, 1);
    assert.equal(filtered[0].name, 'Nguyễn Trãi');
    assert.equal(filtered[0].cameras[0].id, 'cam-nt-1');
  });

  test('Scenario 3: Input "Đường 3/2" preserves corridor without false-positive house stripping', () => {
    const query = 'Đường 3/2';
    const parsed = sanitizeAddressSearchQuery(query);

    assert.equal(parsed.isHouseNumberDetected, false);
    assert.equal(parsed.cleanStreetQuery, 'Đường 3/2');

    const filtered = groupCamerasByCityCorridors(sampleCameras, undefined, query);
    assert.equal(filtered.length, 1);
    assert.equal(filtered[0].name, 'Ba Tháng Hai');
  });

  test('Scenario 4: Address with specific ward token filters to matching camera', () => {
    const query = '34/55/67 CMT8, Phường 5';
    const parsed = sanitizeAddressSearchQuery(query);

    assert.equal(parsed.isHouseNumberDetected, true);
    assert.equal(parsed.cleanStreetQuery, 'CMT8');
    assert.equal(parsed.detectedWard, 'Phường 5');

    const targetP5 = 'Cách Mạng Tháng Tám - Phạm Văn Hai Phường 5';
    const targetP13 = 'Cách Mạng Tháng Tám - Tô Hiến Thành Phường 13';

    assert.equal(matchesVietnameseSearch(targetP5, query), true);
    assert.equal(matchesVietnameseSearch(targetP13, query), false);
  });

  test('Scenario 5: Empty search input unmounts hint and restores all corridors', () => {
    const query = '';
    const parsed = sanitizeAddressSearchQuery(query);

    assert.equal(parsed.isHouseNumberDetected, false);
    assert.equal(parsed.cleanStreetQuery, '');

    const filtered = groupCamerasByCityCorridors(sampleCameras, undefined, query);
    // All 4 distinct corridors should be returned
    assert.equal(filtered.length, 4);
  });
});
