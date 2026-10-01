import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  formatPhuongName,
  formatCorridorDisplay,
  formatRoadName,
  I18N_NAMES,
  extractCorridor,
  removeVietnameseDiacritics,
  matchesVietnameseSearch,
  ARTERIAL_CORRIDORS,
  groupCamerasByCityCorridors,
} from './phuongFormatters.ts';

describe('formatPhuongName', () => {
  test('returns original string in Vietnamese (vi) locale', () => {
    assert.equal(formatPhuongName('Phường Tân Định', 'vi'), 'Phường Tân Định');
    assert.equal(formatPhuongName('Phường 14', 'vi'), 'Phường 14');
    assert.equal(formatPhuongName('Xã Tân Nhựt', 'vi'), 'Xã Tân Nhựt');
    assert.equal(formatPhuongName('Thị trấn Củ Chi', 'vi'), 'Thị trấn Củ Chi');
  });

  test('formats named Phường into <Name> Ward in English (en) locale', () => {
    assert.equal(formatPhuongName('Phường Tân Định', 'en'), 'Tân Định Ward');
    assert.equal(formatPhuongName('Phường Bến Nghé', 'en'), 'Bến Nghé Ward');
    assert.equal(formatPhuongName('Phường Thạnh Mỹ Tây', 'en'), 'Thạnh Mỹ Tây Ward');
    assert.equal(formatPhuongName('phường An Phú Đông', 'en'), 'An Phú Đông Ward');
  });

  test('formats numbered Phường into Ward <N> in English (en) locale', () => {
    assert.equal(formatPhuongName('Phường 1', 'en'), 'Ward 1');
    assert.equal(formatPhuongName('Phường 14', 'en'), 'Ward 14');
    assert.equal(formatPhuongName('phường 05', 'en'), 'Ward 05');
  });

  test('formats Communes (Xã) into <Name> Commune in English (en) locale', () => {
    assert.equal(formatPhuongName('Xã Bình Lợi', 'en'), 'Bình Lợi Commune');
    assert.equal(formatPhuongName('Xã Tân Nhựt', 'en'), 'Tân Nhựt Commune');
  });

  test('formats Towns (Thị trấn) into <Name> Town in English (en) locale', () => {
    assert.equal(formatPhuongName('Thị trấn Cần Thạnh', 'en'), 'Cần Thạnh Town');
    assert.equal(formatPhuongName('Thị trấn Củ Chi', 'en'), 'Củ Chi Town');
  });

  test('formats prefix-less names with explicit types in vi and en', () => {
    assert.equal(formatPhuongName('Bến Nghé', 'vi', 'Phường'), 'Phường Bến Nghé');
    assert.equal(formatPhuongName('Bến Nghé', 'en', 'Phường'), 'Bến Nghé Ward');
    assert.equal(formatPhuongName('14', 'vi', 'Phường'), 'Phường 14');
    assert.equal(formatPhuongName('14', 'en', 'Phường'), 'Ward 14');
    assert.equal(formatPhuongName('Tân An Hội', 'vi', 'Xã'), 'Xã Tân An Hội');
    assert.equal(formatPhuongName('Tân An Hội', 'en', 'Xã'), 'Tân An Hội Commune');
    assert.equal(formatPhuongName('Củ Chi', 'vi', 'Thị trấn'), 'Thị trấn Củ Chi');
    assert.equal(formatPhuongName('Củ Chi', 'en', 'Thị trấn'), 'Củ Chi Town');
  });

  test('handles empty or non-matching inputs gracefully', () => {
    assert.equal(formatPhuongName('', 'en'), '');
    assert.equal(formatPhuongName('Unknown Location', 'en'), 'Unknown Location');
    assert.equal(formatPhuongName('15', 'en'), 'Ward 15');
  });
});

describe('formatCorridorDisplay', () => {
  test('handles bridges (Cầu)', () => {
    assert.deepEqual(formatCorridorDisplay('Cầu Sài Gòn', 'vi'), {
      prefix: 'Cầu',
      name: 'Sài Gòn',
    });
    assert.deepEqual(formatCorridorDisplay('Cầu Sài Gòn', 'en'), {
      prefix: 'Bridge',
      name: 'Sài Gòn',
    });
  });

  test('handles overpasses (Cầu vượt)', () => {
    assert.deepEqual(formatCorridorDisplay('Cầu vượt Sóng Thần 1', 'vi'), {
      prefix: 'Cầu vượt',
      name: 'Sóng Thần 1',
    });
    assert.deepEqual(formatCorridorDisplay('Cầu vượt Sóng Thần 1', 'en'), {
      prefix: 'Overpass',
      name: 'Sóng Thần 1',
    });
  });

  test('handles junctions (Nút giao)', () => {
    assert.deepEqual(formatCorridorDisplay('Nút giao Hàng Xanh 1 (Viện Máy tính)', 'vi'), {
      prefix: 'Nút giao',
      name: 'Hàng Xanh 1 (Viện Máy tính)',
    });
    assert.deepEqual(formatCorridorDisplay('Nút giao Hàng Xanh 1 (Viện Máy tính)', 'en'), {
      prefix: 'Junction',
      name: 'Hàng Xanh 1 (Viện Máy tính)',
    });
  });

  test('handles roundabouts (Vòng xoay)', () => {
    assert.deepEqual(formatCorridorDisplay('Vòng xoay Phú Lâm', 'vi'), {
      prefix: 'Vòng xoay',
      name: 'Phú Lâm',
    });
    assert.deepEqual(formatCorridorDisplay('Vòng xoay Phú Lâm', 'en'), {
      prefix: 'Roundabout',
      name: 'Phú Lâm',
    });
  });

  test('handles expressways (Cao tốc)', () => {
    assert.deepEqual(formatCorridorDisplay('Cao tốc LTDG', 'vi'), {
      prefix: 'Cao tốc',
      name: 'LTDG',
    });
    assert.deepEqual(formatCorridorDisplay('Cao tốc LTDG', 'en'), {
      prefix: 'Expressway',
      name: 'LTDG',
    });
  });

  test('handles national routes (Quốc lộ / QL)', () => {
    assert.deepEqual(formatCorridorDisplay('Quốc Lộ 13', 'vi'), {
      prefix: 'Quốc lộ',
      name: '13',
    });
    assert.deepEqual(formatCorridorDisplay('Quốc Lộ 13', 'en'), {
      prefix: 'National Route',
      name: '13',
    });
    assert.deepEqual(formatCorridorDisplay('QL1', 'en'), {
      prefix: 'National Route',
      name: '1',
    });
  });

  test('handles tunnels (Đường hầm / Hầm)', () => {
    assert.deepEqual(formatCorridorDisplay('Đường hầm (Đầu hầm Tp Thủ Đức)', 'vi'), {
      prefix: 'Đường hầm',
      name: '(Đầu hầm Tp Thủ Đức)',
    });
    assert.deepEqual(formatCorridorDisplay('Đường hầm (Đầu hầm Tp Thủ Đức)', 'en'), {
      prefix: 'Tunnel',
      name: '(Đầu hầm Tp Thủ Đức)',
    });
  });

  test('handles numbered streets (Đường số)', () => {
    assert.deepEqual(formatCorridorDisplay('Đường số 1', 'vi'), {
      prefix: 'Đường',
      name: 'số 1',
    });
    assert.deepEqual(formatCorridorDisplay('Đường số 1', 'en'), {
      prefix: 'Street',
      name: 'No. 1',
    });
  });

  test('handles standard streets without prefix', () => {
    assert.deepEqual(formatCorridorDisplay('Trần Quang Khải', 'vi'), {
      prefix: 'Đường',
      name: 'Trần Quang Khải',
    });
    assert.deepEqual(formatCorridorDisplay('Trần Quang Khải', 'en'), {
      prefix: 'Street',
      name: 'Trần Quang Khải',
    });
  });

  test('handles fallback/other category', () => {
    assert.deepEqual(formatCorridorDisplay('Khác', 'vi'), {
      prefix: '',
      name: 'Khác',
    });
    assert.deepEqual(formatCorridorDisplay('Khác', 'en'), {
      prefix: '',
      name: 'Other',
    });
  });
});

describe('search & diacritics', () => {
  test('strips Vietnamese diacritics correctly', () => {
    assert.equal(removeVietnameseDiacritics('Phường Tân Định'), 'Phuong Tan Dinh');
    assert.equal(removeVietnameseDiacritics('Đường Đinh Tiên Hoàng'), 'Duong Dinh Tien Hoang');
  });

  test('matches search queries with diacritics, unaccented, and English tokens', () => {
    const target = 'Trần Quang Khải - Hai Bà Trưng Trần Quang Khải Street Trần Quang Khải Phường Tân Định Tân Định Ward';

    // Exact Vietnamese accented search
    assert.ok(matchesVietnameseSearch(target, 'Tân Định'));
    assert.ok(matchesVietnameseSearch(target, 'Trần Quang Khải'));

    // Unaccented search
    assert.ok(matchesVietnameseSearch(target, 'Tan Dinh'));
    assert.ok(matchesVietnameseSearch(target, 'Hai Ba Trung'));

    // English search
    assert.ok(matchesVietnameseSearch(target, 'Ward'));
    assert.ok(matchesVietnameseSearch(target, 'Tan Dinh Ward'));
    assert.ok(matchesVietnameseSearch(target, 'Street'));
  });
});

describe('extractCorridor', () => {
  test('extracts primary street name by splitting delimiters', () => {
    assert.equal(extractCorridor('Trần Quang Khải - Hai Bà Trưng'), 'Trần Quang Khải');
    assert.equal(extractCorridor('Nguyễn Văn Trỗi – Hoàng Văn Thụ'), 'Nguyễn Văn Trỗi');
    assert.equal(extractCorridor('Điện Biên Phủ — Pasteur'), 'Điện Biên Phủ');
    assert.equal(extractCorridor('Võ Văn Kiệt − Ký Con'), 'Võ Văn Kiệt');
  });

  test('falls back to title or Khác when displayName is absent', () => {
    assert.equal(extractCorridor(undefined, 'Cầu Sài Gòn - Hướng Q2'), 'Cầu Sài Gòn');
    assert.equal(extractCorridor('', ''), 'Khác');
    assert.equal(extractCorridor(undefined, undefined), 'Khác');
  });
});

describe('formatRoadName & I18N_NAMES', () => {
  test('contains all 6 required i18n keys for vi and en', () => {
    assert.equal(I18N_NAMES.vi.ward_type_phuong, 'Phường');
    assert.equal(I18N_NAMES.en.ward_type_phuong, 'Ward');
    assert.equal(I18N_NAMES.vi.ward_type_xa, 'Xã');
    assert.equal(I18N_NAMES.en.ward_type_xa, 'Commune');
    assert.equal(I18N_NAMES.vi.ward_type_thi_tran, 'Thị trấn');
    assert.equal(I18N_NAMES.en.ward_type_thi_tran, 'Town');
    assert.equal(I18N_NAMES.vi.road_type_duong, 'Đường');
    assert.equal(I18N_NAMES.en.road_type_duong, 'Street');
    assert.equal(I18N_NAMES.vi.road_type_quoc_lo, 'Quốc lộ');
    assert.equal(I18N_NAMES.en.road_type_quoc_lo, 'National Route');
    assert.equal(I18N_NAMES.vi.road_type_xa_lo, 'Xa lộ');
    assert.equal(I18N_NAMES.en.road_type_xa_lo, 'Highway');
  });

  test('formats full road names with localization', () => {
    assert.equal(formatRoadName('Trần Quang Khải', 'vi', 'Đường'), 'Đường Trần Quang Khải');
    assert.equal(formatRoadName('Trần Quang Khải', 'en', 'Đường'), 'Trần Quang Khải Street');
    assert.equal(formatRoadName('13', 'vi', 'Quốc lộ'), 'Quốc lộ 13');
    assert.equal(formatRoadName('13', 'en', 'Quốc lộ'), 'National Route 13');
    assert.equal(formatRoadName('Hà Nội', 'vi', 'Xa lộ'), 'Xa lộ Hà Nội');
    assert.equal(formatRoadName('Hà Nội', 'en', 'Xa lộ'), 'Hà Nội Highway');
  });
});

describe('groupCamerasByCityCorridors & ARTERIAL_CORRIDORS', () => {
  test('ARTERIAL_CORRIDORS contains high-priority commuter routes', () => {
    assert.ok(ARTERIAL_CORRIDORS.includes('Điện Biên Phủ'));
    assert.ok(ARTERIAL_CORRIDORS.includes('Cộng Hòa'));
    assert.ok(ARTERIAL_CORRIDORS.includes('Xa Lộ Hà Nội'));
    assert.ok(ARTERIAL_CORRIDORS.includes('Võ Văn Kiệt'));
  });

  test('groups cameras across wards by corridor name', () => {
    const cameras = [
      { id: 'cam1', displayName: 'Điện Biên Phủ - Pasteur', phuong_name: 'Phường Võ Thị Sáu' },
      { id: 'cam2', displayName: 'Điện Biên Phủ - Đinh Tiên Hoàng', phuong_name: 'Phường Đa Kao' },
      { id: 'cam3', displayName: 'Cộng Hòa - Ấp Bắc', phuong_name: 'Phường 13' },
    ];

    const groups = groupCamerasByCityCorridors(cameras);
    assert.equal(groups.length, 2);

    const dbp = groups.find(g => g.name === 'Điện Biên Phủ');
    assert.ok(dbp);
    assert.equal(dbp.totalCameras, 2);
    assert.equal(dbp.cameras.length, 2);

    const ch = groups.find(g => g.name === 'Cộng Hòa');
    assert.ok(ch);
    assert.equal(ch.totalCameras, 1);
  });

  test('prioritizes corridors with active congestion to top', () => {
    const cameras = [
      { id: 'cam1', displayName: 'Cộng Hòa - Ấp Bắc' },
      { id: 'cam2', displayName: 'Điện Biên Phủ - Pasteur' },
      { id: 'cam3', displayName: 'Xa Lộ Hà Nội - Cầu Rạch Chiếc' },
    ];

    const verdicts = new Map([
      ['cam1', { severity: 'CONGESTED', stale: false }],
      ['cam2', { severity: 'CLEAR', stale: false }],
      ['cam3', { severity: 'MODERATE', stale: false }],
    ]);

    const groups = groupCamerasByCityCorridors(cameras, verdicts);
    assert.equal(groups[0].name, 'Cộng Hòa'); // Congested first
    assert.equal(groups[0].congestedCount, 1);

    assert.equal(groups[1].name, 'Xa Lộ Hà Nội'); // Moderate second
    assert.equal(groups[1].moderateCount, 1);

    assert.equal(groups[2].name, 'Điện Biên Phủ'); // Clear third
    assert.equal(groups[2].congestedCount, 0);
  });

  test('filters corridors and cameras by search term', () => {
    const cameras = [
      { id: 'cam1', displayName: 'Cộng Hòa - Ấp Bắc' },
      { id: 'cam2', displayName: 'Điện Biên Phủ - Pasteur' },
    ];

    const searchCorridor = groupCamerasByCityCorridors(cameras, undefined, 'Cong Hoa');
    assert.equal(searchCorridor.length, 1);
    assert.equal(searchCorridor[0].name, 'Cộng Hòa');

    const searchCrossStreet = groupCamerasByCityCorridors(cameras, undefined, 'Pasteur');
    assert.equal(searchCrossStreet.length, 1);
    assert.equal(searchCrossStreet[0].name, 'Điện Biên Phủ');
  });
});


