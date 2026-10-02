import test from 'node:test';
import assert from 'node:assert/strict';
import { PINNED_DOCK_I18N } from '../config/constants.ts';

test('PINNED_DOCK_I18N contains all required keys and correct Vietnamese diacritics', () => {
  // 1. Verify all required keys exist
  assert.ok(PINNED_DOCK_I18N.TITLE, 'TITLE should exist');
  assert.ok(PINNED_DOCK_I18N.COLLAPSED_PILL, 'COLLAPSED_PILL should exist');
  assert.ok(PINNED_DOCK_I18N.CLEAR_ALL, 'CLEAR_ALL should exist');
  assert.ok(PINNED_DOCK_I18N.COLLAPSE, 'COLLAPSE should exist');
  assert.ok(PINNED_DOCK_I18N.EXPAND, 'EXPAND should exist');
  assert.ok(PINNED_DOCK_I18N.UNPIN, 'UNPIN should exist');

  // 2. Verify Vietnamese strings with proper diacritics
  assert.equal(PINNED_DOCK_I18N.TITLE.vi, 'DOCK THEO DÕI');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.vi(1), '📌 Đang theo dõi: 1 cam');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.vi(3), '📌 Đang theo dõi: 3 cam');
  assert.equal(PINNED_DOCK_I18N.CLEAR_ALL.vi, '[XÓA HẾT]');
  assert.equal(PINNED_DOCK_I18N.COLLAPSE.vi, 'Thu gọn');
  assert.equal(PINNED_DOCK_I18N.EXPAND.vi, 'Mở rộng');
  assert.equal(PINNED_DOCK_I18N.UNPIN.vi, 'Bỏ ghim');

  // 3. Verify English strings and pluralization
  assert.equal(PINNED_DOCK_I18N.TITLE.en, 'MONITORING DOCK');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.en(1), '📌 Monitoring: 1 cam');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.en(2), '📌 Monitoring: 2 cams');
  assert.equal(PINNED_DOCK_I18N.CLEAR_ALL.en, '[CLEAR ALL]');
  assert.equal(PINNED_DOCK_I18N.COLLAPSE.en, 'Collapse');
  assert.equal(PINNED_DOCK_I18N.EXPAND.en, 'Expand');
  assert.equal(PINNED_DOCK_I18N.UNPIN.en, 'Unpin');
});
