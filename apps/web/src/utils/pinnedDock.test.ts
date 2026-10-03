import test from 'node:test';
import assert from 'node:assert/strict';
import { PINNED_DOCK_I18N, PINNED_DOCK_CAP } from '../config/constants.ts';

test('PINNED_DOCK_I18N contains all required keys and correct Vietnamese diacritics', () => {
  // 1. Verify all required keys exist
  assert.ok(PINNED_DOCK_I18N.TITLE, 'TITLE should exist');
  assert.ok(PINNED_DOCK_I18N.COLLAPSED_PILL, 'COLLAPSED_PILL should exist');
  assert.ok(PINNED_DOCK_I18N.CLEAR_ALL, 'CLEAR_ALL should exist');
  assert.ok(PINNED_DOCK_I18N.COLLAPSE, 'COLLAPSE should exist');
  assert.ok(PINNED_DOCK_I18N.EXPAND, 'EXPAND should exist');
  assert.ok(PINNED_DOCK_I18N.UNPIN, 'UNPIN should exist');
  assert.ok(PINNED_DOCK_I18N.SIZE_PRESETS, 'SIZE_PRESETS should exist');
  assert.ok(PINNED_DOCK_I18N.VIEW_MODE, 'VIEW_MODE should exist');
  assert.ok(PINNED_DOCK_I18N.FULL_WIDTH_GRID, 'FULL_WIDTH_GRID should exist');
  assert.ok(PINNED_DOCK_I18N.PIN_CAP_REACHED, 'PIN_CAP_REACHED should exist');
  assert.ok(PINNED_DOCK_I18N.PIN_LIMIT_BADGE, 'PIN_LIMIT_BADGE should exist');
  assert.ok(PINNED_DOCK_I18N.NO_ANALYSIS, 'NO_ANALYSIS should exist');
  assert.ok(PINNED_DOCK_I18N.ONLINE, 'ONLINE should exist');
  assert.ok(PINNED_DOCK_I18N.OFFLINE, 'OFFLINE should exist');
  assert.ok(PINNED_DOCK_I18N.STALE, 'STALE should exist');
  assert.ok(PINNED_DOCK_I18N.JUST_NOW, 'JUST_NOW should exist');
  assert.ok(PINNED_DOCK_I18N.SECONDS_AGO, 'SECONDS_AGO should exist');
  assert.ok(PINNED_DOCK_I18N.MOVE_LEFT, 'MOVE_LEFT should exist');
  assert.ok(PINNED_DOCK_I18N.MOVE_RIGHT, 'MOVE_RIGHT should exist');
  assert.ok(PINNED_DOCK_I18N.DRAG_TO_REORDER, 'DRAG_TO_REORDER should exist');

  // 2. Verify Vietnamese strings with proper diacritics
  assert.equal(PINNED_DOCK_I18N.TITLE.vi, 'DOCK THEO DÕI');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.vi(1), '📌 Đang theo dõi: 1 cam');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.vi(3), '📌 Đang theo dõi: 3 cam');
  assert.equal(PINNED_DOCK_I18N.CLEAR_ALL.vi, '[XÓA HẾT]');
  assert.equal(PINNED_DOCK_I18N.COLLAPSE.vi, 'Thu gọn');
  assert.equal(PINNED_DOCK_I18N.EXPAND.vi, 'Mở rộng');
  assert.equal(PINNED_DOCK_I18N.UNPIN.vi, 'Bỏ ghim');
  assert.equal(PINNED_DOCK_I18N.SIZE_PRESETS.S.vi, 'Nhỏ');
  assert.equal(PINNED_DOCK_I18N.SIZE_PRESETS.M.vi, 'Vừa');
  assert.equal(PINNED_DOCK_I18N.SIZE_PRESETS.L.vi, 'Lớn');
  assert.equal(PINNED_DOCK_I18N.VIEW_MODE.STRIP.vi, 'Dải');
  assert.equal(PINNED_DOCK_I18N.VIEW_MODE.GRID.vi, 'Lưới');
  assert.equal(PINNED_DOCK_I18N.FULL_WIDTH_GRID.vi, 'Lưới toàn chiều rộng');
  assert.equal(PINNED_DOCK_I18N.PIN_CAP_REACHED.vi, 'Đã đạt giới hạn ghim (tối đa 12 camera)');
  assert.equal(PINNED_DOCK_I18N.PIN_LIMIT_BADGE.vi, 'ĐÃ ĐẠT GIỚI HẠN');
  assert.equal(PINNED_DOCK_I18N.NO_ANALYSIS.vi, 'Chưa phân tích');
  assert.equal(PINNED_DOCK_I18N.ONLINE.vi, 'Trực tuyến');
  assert.equal(PINNED_DOCK_I18N.OFFLINE.vi, 'Mất kết nối');
  assert.equal(PINNED_DOCK_I18N.STALE.vi, 'Cũ');
  assert.equal(PINNED_DOCK_I18N.JUST_NOW.vi, 'Vừa xong');
  assert.equal(PINNED_DOCK_I18N.SECONDS_AGO.vi(15), '15s trước');
  assert.equal(PINNED_DOCK_I18N.MOVE_LEFT.vi, 'Di chuyển sang trái');
  assert.equal(PINNED_DOCK_I18N.MOVE_RIGHT.vi, 'Di chuyển sang phải');
  assert.equal(PINNED_DOCK_I18N.DRAG_TO_REORDER.vi, 'Kéo để sắp xếp lại');

  // 3. Verify English strings and pluralization
  assert.equal(PINNED_DOCK_I18N.TITLE.en, 'MONITORING DOCK');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.en(1), '📌 Monitoring: 1 cam');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.en(2), '📌 Monitoring: 2 cams');
  assert.equal(PINNED_DOCK_I18N.CLEAR_ALL.en, '[CLEAR ALL]');
  assert.equal(PINNED_DOCK_I18N.COLLAPSE.en, 'Collapse');
  assert.equal(PINNED_DOCK_I18N.EXPAND.en, 'Expand');
  assert.equal(PINNED_DOCK_I18N.UNPIN.en, 'Unpin');
  assert.equal(PINNED_DOCK_I18N.SIZE_PRESETS.S.en, 'Small');
  assert.equal(PINNED_DOCK_I18N.SIZE_PRESETS.M.en, 'Medium');
  assert.equal(PINNED_DOCK_I18N.SIZE_PRESETS.L.en, 'Large');
  assert.equal(PINNED_DOCK_I18N.VIEW_MODE.STRIP.en, 'Strip');
  assert.equal(PINNED_DOCK_I18N.VIEW_MODE.GRID.en, 'Grid');
  assert.equal(PINNED_DOCK_I18N.FULL_WIDTH_GRID.en, 'Full-width grid');
  assert.equal(PINNED_DOCK_I18N.PIN_CAP_REACHED.en, 'Pin limit reached (maximum 12 cameras)');
  assert.equal(PINNED_DOCK_I18N.PIN_LIMIT_BADGE.en, 'PIN CAP REACHED');
  assert.equal(PINNED_DOCK_I18N.NO_ANALYSIS.en, 'No analysis');
  assert.equal(PINNED_DOCK_I18N.ONLINE.en, 'Online');
  assert.equal(PINNED_DOCK_I18N.OFFLINE.en, 'Offline');
  assert.equal(PINNED_DOCK_I18N.STALE.en, 'Stale');
  assert.equal(PINNED_DOCK_I18N.JUST_NOW.en, 'Just now');
  assert.equal(PINNED_DOCK_I18N.SECONDS_AGO.en(15), '15s ago');
  assert.equal(PINNED_DOCK_I18N.MOVE_LEFT.en, 'Move left');
  assert.equal(PINNED_DOCK_I18N.MOVE_RIGHT.en, 'Move right');
  assert.equal(PINNED_DOCK_I18N.DRAG_TO_REORDER.en, 'Drag to reorder');
});

test('PINNED_DOCK_CAP is exactly 12 per ADR-004 §A4/S5', () => {
  assert.equal(PINNED_DOCK_CAP, 12);
});

test('Reordering helper function moves items correctly', () => {
  const reorder = <T>(list: T[], fromIndex: number, toIndex: number): T[] => {
    if (fromIndex < 0 || fromIndex >= list.length || toIndex < 0 || toIndex >= list.length) {
      return list;
    }
    const result = [...list];
    const [removed] = result.splice(fromIndex, 1);
    result.splice(toIndex, 0, removed);
    return result;
  };

  const initial = ['cam-1', 'cam-2', 'cam-3', 'cam-4'];
  
  // Move cam-1 to index 2
  const reordered1 = reorder(initial, 0, 2);
  assert.deepEqual(reordered1, ['cam-2', 'cam-3', 'cam-1', 'cam-4']);

  // Move cam-4 left to index 1
  const reordered2 = reorder(initial, 3, 1);
  assert.deepEqual(reordered2, ['cam-1', 'cam-4', 'cam-2', 'cam-3']);

  // Out of bounds checks
  assert.deepEqual(reorder(initial, -1, 2), initial);
  assert.deepEqual(reorder(initial, 1, 99), initial);
});
