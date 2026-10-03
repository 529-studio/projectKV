import test from 'node:test';
import assert from 'node:assert/strict';
import { getCameraSequence } from './cameraSequence.ts';
import type { SequenceCamera } from './cameraSequence.ts';

const mockCamera = (id: string, displayName: string): SequenceCamera => ({
  id,
  displayName,
  title: displayName,
  isOnline: true,
});

test('getCameraSequence: returns empty result for null camera', () => {
  const result = getCameraSequence({
    currentCamera: null,
    allCameras: [],
    pinnedCameras: [],
    selectedCorridor: null,
    lang: 'vi',
  });
  assert.equal(result.prevCamera, null);
  assert.equal(result.nextCamera, null);
  assert.equal(result.currentIndex, -1);
  assert.equal(result.totalCount, 0);
  assert.equal(result.contextLabel, '');
});

test('getCameraSequence: prioritizes pinned cameras when current camera is in pinned list', () => {
  const cam1 = mockCamera('c1', 'Điện Biên Phủ - CMT8');
  const cam2 = mockCamera('c2', 'Nam Kỳ Khởi Nghĩa - Lý Chính Thắng');
  const cam3 = mockCamera('c3', 'Võ Văn Kiệt - Cầu Calmette');
  const pinned = [cam1, cam2, cam3];

  // Camera 2 (middle)
  const res2 = getCameraSequence({
    currentCamera: cam2,
    allCameras: pinned,
    pinnedCameras: pinned,
    selectedCorridor: null,
    lang: 'vi',
  });
  assert.equal(res2.prevCamera?.id, 'c1');
  assert.equal(res2.nextCamera?.id, 'c3');
  assert.equal(res2.currentIndex, 1);
  assert.equal(res2.totalCount, 3);
  assert.equal(res2.contextLabel, 'Ghim: 2/3');

  // Camera 1 (first)
  const res1 = getCameraSequence({
    currentCamera: cam1,
    allCameras: pinned,
    pinnedCameras: pinned,
    selectedCorridor: null,
    lang: 'en',
  });
  assert.equal(res1.prevCamera, null);
  assert.equal(res1.nextCamera?.id, 'c2');
  assert.equal(res1.contextLabel, 'Pinned: 1/3');

  // Camera 3 (last)
  const res3 = getCameraSequence({
    currentCamera: cam3,
    allCameras: pinned,
    pinnedCameras: pinned,
    selectedCorridor: null,
    lang: 'vi',
  });
  assert.equal(res3.prevCamera?.id, 'c2');
  assert.equal(res3.nextCamera, null);
  assert.equal(res3.contextLabel, 'Ghim: 3/3');
});

test('getCameraSequence: groups by street corridor for unpinned cameras on the same street', () => {
  const c1 = mockCamera('dbp-1', 'Điện Biên Phủ - Cách Mạng Tháng Tám');
  const c2 = mockCamera('dbp-2', 'Điện Biên Phủ - Đinh Tiên Hoàng');
  const c3 = mockCamera('dbp-3', 'Điện Biên Phủ - Hai Bà Trưng');
  const other = mockCamera('other-1', 'Nguyễn Thị Minh Khai - Trương Định');

  const all = [c1, c2, c3, other];

  const res = getCameraSequence({
    currentCamera: c2,
    allCameras: all,
    pinnedCameras: [],
    selectedCorridor: null,
    lang: 'vi',
  });

  assert.equal(res.prevCamera?.id, 'dbp-1');
  assert.equal(res.nextCamera?.id, 'dbp-3');
  assert.equal(res.totalCount, 3);
  assert.equal(res.currentIndex, 1);
  assert.equal(res.contextLabel, 'Điện Biên Phủ: 2/3');
});

test('getCameraSequence: handles single camera gracefully without next/prev', () => {
  const single = mockCamera('lonely-1', 'Hẻm Vắng - Nhà Số 1');
  const res = getCameraSequence({
    currentCamera: single,
    allCameras: [single],
    pinnedCameras: [],
    selectedCorridor: null,
    lang: 'vi',
  });

  assert.equal(res.prevCamera, null);
  assert.equal(res.nextCamera, null);
  assert.equal(res.totalCount, 1);
  assert.equal(res.contextLabel, '');
});
