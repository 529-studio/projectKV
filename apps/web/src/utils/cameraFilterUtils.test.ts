import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { isCameraOffline, CAMERA_FILTER_I18N } from './cameraFilterUtils.ts';
import type { Camera } from '../hooks/useCameras.ts';

describe('Camera Filter Utilities (S7-T2 / Issue #247)', () => {
  it('isCameraOffline identifies offline camera statuses correctly', () => {
    const notImageCam: Camera = {
      id: 'cam-1',
      isOnline: false,
      status: 'NOT_IMAGE',
    };
    const noSignalCam: Camera = {
      id: 'cam-2',
      isOnline: false,
      status: 'NO_SIGNAL',
    };
    const onlineCam: Camera = {
      id: 'cam-3',
      isOnline: true,
      status: 'UP',
    };
    const undefinedStatusCam: Camera = {
      id: 'cam-4',
      isOnline: true,
    };

    assert.equal(isCameraOffline(notImageCam), true);
    assert.equal(isCameraOffline(noSignalCam), true);
    assert.equal(isCameraOffline(onlineCam), false);
    assert.equal(isCameraOffline(undefinedStatusCam), false);
    assert.equal(isCameraOffline(null), false);
    assert.equal(isCameraOffline(undefined), false);
  });

  it('CAMERA_FILTER_I18N contains bilingual translations without emojis', () => {
    const emojiRegex = /[\u{1F300}-\u{1F9FF}]|[\u{2600}-\u{26FF}]|[\u{2700}-\u{27BF}]/u;

    const counterVi = CAMERA_FILTER_I18N.COUNTER.vi(702, 837);
    const counterEn = CAMERA_FILTER_I18N.COUNTER.en(702, 837);
    const hideBtnVi = CAMERA_FILTER_I18N.HIDE_OFFLINE_BTN.vi;
    const hideBtnEn = CAMERA_FILTER_I18N.HIDE_OFFLINE_BTN.en;
    const showBtnVi = CAMERA_FILTER_I18N.SHOW_OFFLINE_BTN.vi;
    const showBtnEn = CAMERA_FILTER_I18N.SHOW_OFFLINE_BTN.en;
    const tipHiddenVi = CAMERA_FILTER_I18N.TOOLTIP.vi(true);
    const tipHiddenEn = CAMERA_FILTER_I18N.TOOLTIP.en(true);
    const tipShownVi = CAMERA_FILTER_I18N.TOOLTIP.vi(false);
    const tipShownEn = CAMERA_FILTER_I18N.TOOLTIP.en(false);

    assert.equal(counterVi, 'Đang hiển thị 702 / 837 camera');
    assert.equal(counterEn, 'Showing 702 / 837 cameras');
    assert.equal(hideBtnVi, '[Ẩn camera mất tín hiệu]');
    assert.equal(hideBtnEn, '[Hide offline cameras]');
    assert.equal(showBtnVi, '[Hiện camera mất tín hiệu]');
    assert.equal(showBtnEn, '[Show offline cameras]');

    const allStrings = [
      counterVi,
      counterEn,
      hideBtnVi,
      hideBtnEn,
      showBtnVi,
      showBtnEn,
      tipHiddenVi,
      tipHiddenEn,
      tipShownVi,
      tipShownEn,
    ];

    allStrings.forEach(str => {
      assert.equal(emojiRegex.test(str), false, `String contains emoji: ${str}`);
    });
  });

  it('filters out offline cameras accurately from a collection', () => {
    const cameras: Camera[] = [
      { id: 'c1', status: 'UP', isOnline: true },
      { id: 'c2', status: 'NOT_IMAGE', isOnline: false },
      { id: 'c3', status: 'UP', isOnline: true },
      { id: 'c4', status: 'NO_SIGNAL', isOnline: false },
      { id: 'c5', status: 'UP', isOnline: true },
    ];

    const filtered = cameras.filter(c => !isCameraOffline(c));
    assert.equal(filtered.length, 3);
    assert.deepEqual(
      filtered.map(c => c.id),
      ['c1', 'c3', 'c5']
    );
  });
});
