import test from 'node:test';
import assert from 'node:assert/strict';
import { PINNED_DOCK_I18N, PINNED_DOCK_CAP } from '../config/constants.ts';
import {
  getDockSeverity,
  getDockSeverityLabel,
  getDockSeverityVisual,
  reorderPinnedCameras,
} from './pinnedDock.ts';
import type { TrafficVerdict } from './trafficUtils.ts';

test('PINNED_DOCK_I18N contains all required keys and correct Vietnamese diacritics without emojis', () => {
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
  assert.ok(PINNED_DOCK_I18N.SEVERITY, 'SEVERITY should exist');
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
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.vi(1), 'Đang theo dõi: 1 cam');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.vi(3), 'Đang theo dõi: 3 cam');
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
  assert.equal(PINNED_DOCK_I18N.SEVERITY.CLEAR.vi, 'Thông thoáng');
  assert.equal(PINNED_DOCK_I18N.SEVERITY.MODERATE.vi, 'Vừa phải');
  assert.equal(PINNED_DOCK_I18N.SEVERITY.HEAVY.vi, 'Đông đúc');
  assert.equal(PINNED_DOCK_I18N.SEVERITY.CONGESTED.vi, 'Tắc nghẽn');
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
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.en(1), 'Monitoring: 1 cam');
  assert.equal(PINNED_DOCK_I18N.COLLAPSED_PILL.en(2), 'Monitoring: 2 cams');
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
  assert.equal(PINNED_DOCK_I18N.SEVERITY.CLEAR.en, 'Clear');
  assert.equal(PINNED_DOCK_I18N.SEVERITY.MODERATE.en, 'Moderate');
  assert.equal(PINNED_DOCK_I18N.SEVERITY.HEAVY.en, 'Heavy');
  assert.equal(PINNED_DOCK_I18N.SEVERITY.CONGESTED.en, 'Congested');
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

test('getDockSeverity provenance gating and staleness rules (Issue #140 / ADR-004 §3 A5, §9 D10)', () => {
  const freshTime = new Date().toISOString();
  const ancientTime = new Date(Date.now() - 300 * 1000).toISOString(); // 5 minutes ago (exceeds 180s TTL)

  // 1. Fresh real verdicts with valid analyzer sources
  const clearVerdict: TrafficVerdict = {
    camera_id: 'cam-1',
    severity: 'CLEAR',
    confidence: 0.95,
    source: 'onnx',
    timestamp: freshTime,
  };
  assert.equal(getDockSeverity(clearVerdict, 'production'), 'CLEAR');

  const moderateVerdict: TrafficVerdict = {
    camera_id: 'cam-2',
    severity: 'MODERATE',
    confidence: 0.88,
    source: 'vlm',
    timestamp: freshTime,
  };
  assert.equal(getDockSeverity(moderateVerdict, 'production'), 'MODERATE');

  const heavyVerdict: TrafficVerdict = {
    camera_id: 'cam-3',
    severity: 'HEAVY',
    confidence: 0.92,
    source: 'onnx',
    timestamp: freshTime,
  };
  assert.equal(getDockSeverity(heavyVerdict, 'production'), 'HEAVY');

  const congestedVerdict: TrafficVerdict = {
    camera_id: 'cam-4',
    severity: 'CONGESTED',
    confidence: 0.97,
    source: 'onnx',
    timestamp: freshTime,
  };
  assert.equal(getDockSeverity(congestedVerdict, 'production'), 'CONGESTED');

  // Case insensitivity
  assert.equal(getDockSeverity({ ...clearVerdict, severity: 'clear' }, 'production'), 'CLEAR');
  assert.equal(getDockSeverity({ ...congestedVerdict, severity: 'congested' }, 'production'), 'CONGESTED');

  // 2. Stale verdicts must fall back to null (gray "no analysis")
  assert.equal(getDockSeverity({ ...clearVerdict, stale: true }, 'production'), null);
  assert.equal(getDockSeverity({ ...clearVerdict, timestamp: ancientTime }, 'production'), null);

  // 3. Standby dummy responses must always be rejected, even in dev mode
  const standbyVerdict: TrafficVerdict = {
    camera_id: 'cam-dummy',
    severity: 'CLEAR',
    confidence: 0.0,
    source: 'standby_dummy',
    timestamp: freshTime,
  };
  assert.equal(getDockSeverity(standbyVerdict, 'production'), null);
  assert.equal(getDockSeverity(standbyVerdict, 'dev'), null);

  // 4. Mock source verdicts outside dev must be rejected
  const mockVerdict: TrafficVerdict = {
    camera_id: 'cam-mock',
    severity: 'CONGESTED',
    confidence: 0.99,
    source: 'mock',
    timestamp: freshTime,
  };
  assert.equal(getDockSeverity(mockVerdict, 'production'), null);
  assert.equal(getDockSeverity(mockVerdict, 'staging'), null);
  assert.equal(getDockSeverity(mockVerdict, undefined), null);
  // Allowed in dev
  assert.equal(getDockSeverity(mockVerdict, 'dev'), 'CONGESTED');

  // 5. Zero-confidence verdicts must be rejected
  assert.equal(getDockSeverity({ ...clearVerdict, confidence: 0 }, 'production'), null);
  assert.equal(getDockSeverity({ ...clearVerdict, confidence: -0.1 }, 'production'), null);

  // 6. Unknown or invalid severities must be rejected
  assert.equal(getDockSeverity({ ...clearVerdict, severity: 'UNKNOWN' }, 'production'), null);
  assert.equal(getDockSeverity({ ...clearVerdict, severity: '' }, 'production'), null);

  // 7. Null/undefined verdicts
  assert.equal(getDockSeverity(null, 'production'), null);
  assert.equal(getDockSeverity(undefined, 'production'), null);
});

test('getDockSeverityLabel localizes severities accurately in vi and en', () => {
  assert.equal(getDockSeverityLabel('CLEAR', 'vi'), 'Thông thoáng');
  assert.equal(getDockSeverityLabel('CLEAR', 'en'), 'Clear');

  assert.equal(getDockSeverityLabel('MODERATE', 'vi'), 'Vừa phải');
  assert.equal(getDockSeverityLabel('MODERATE', 'en'), 'Moderate');

  assert.equal(getDockSeverityLabel('HEAVY', 'vi'), 'Đông đúc');
  assert.equal(getDockSeverityLabel('HEAVY', 'en'), 'Heavy');

  assert.equal(getDockSeverityLabel('CONGESTED', 'vi'), 'Tắc nghẽn');
  assert.equal(getDockSeverityLabel('CONGESTED', 'en'), 'Congested');

  assert.equal(getDockSeverityLabel(null, 'vi'), 'Chưa phân tích');
  assert.equal(getDockSeverityLabel(null, 'en'), 'No analysis');
  assert.equal(getDockSeverityLabel(undefined, 'vi'), 'Chưa phân tích');
});

test('getDockSeverityVisual provides monochromatic functional accents and distinguishes CONGESTED', () => {
  const clearStyle = getDockSeverityVisual('CLEAR');
  assert.match(clearStyle.borderClass, /border-emerald/);
  assert.equal(clearStyle.textClass, 'text-emerald-400');
  assert.equal(clearStyle.dotClass, 'bg-emerald-500');
  assert.equal(clearStyle.isPulsing, false);

  const modStyle = getDockSeverityVisual('MODERATE');
  assert.match(modStyle.borderClass, /border-yellow/);
  assert.equal(modStyle.textClass, 'text-yellow-400');
  assert.equal(modStyle.dotClass, 'bg-yellow-400');
  assert.equal(modStyle.isPulsing, false);

  const heavyStyle = getDockSeverityVisual('HEAVY');
  assert.match(heavyStyle.borderClass, /border-red/);
  assert.equal(heavyStyle.textClass, 'text-red-400');
  assert.equal(heavyStyle.dotClass, 'bg-red-500');
  assert.equal(heavyStyle.isPulsing, false);

  // CONGESTED must be distinguished via pulsing and bold border/ring
  const congestedStyle = getDockSeverityVisual('CONGESTED');
  assert.match(congestedStyle.borderClass, /border-red/);
  assert.match(congestedStyle.borderClass, /ring-1/);
  assert.equal(congestedStyle.textClass, 'text-red-400');
  assert.equal(congestedStyle.isPulsing, true);

  // Default / fallback
  const fallbackStyle = getDockSeverityVisual(null);
  assert.match(fallbackStyle.borderClass, /border-zinc/);
  assert.equal(fallbackStyle.textClass, 'text-zinc-400');
  assert.equal(fallbackStyle.isPulsing, false);
});

test('reorderPinnedCameras moves items correctly and immutably', () => {
  const initial = ['cam-1', 'cam-2', 'cam-3', 'cam-4'];

  // Move cam-1 to index 2
  const reordered1 = reorderPinnedCameras(initial, 0, 2);
  assert.deepEqual(reordered1, ['cam-2', 'cam-3', 'cam-1', 'cam-4']);
  // Initial array remained unchanged
  assert.deepEqual(initial, ['cam-1', 'cam-2', 'cam-3', 'cam-4']);

  // Move cam-4 left to index 1
  const reordered2 = reorderPinnedCameras(initial, 3, 1);
  assert.deepEqual(reordered2, ['cam-1', 'cam-4', 'cam-2', 'cam-3']);

  // Out of bounds checks
  assert.deepEqual(reorderPinnedCameras(initial, -1, 2), initial);
  assert.deepEqual(reorderPinnedCameras(initial, 1, 99), initial);
});
