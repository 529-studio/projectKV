import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { isCameraOffline, isWaterwayCamera, CAMERA_FILTER_I18N } from './cameraFilterUtils.ts';
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
    const waterwayCam: Camera = {
      id: 'cam-5',
      isOnline: false,
      status: 'WATERWAY_MONITOR',
      isWaterway: true,
    };

    assert.equal(isCameraOffline(notImageCam), true);
    assert.equal(isCameraOffline(noSignalCam), true);
    assert.equal(isCameraOffline(waterwayCam), true);
    assert.equal(isCameraOffline(onlineCam), false);
    assert.equal(isCameraOffline(undefinedStatusCam), false);
    assert.equal(isCameraOffline(null), false);
    assert.equal(isCameraOffline(undefined), false);
  });

  it('isWaterwayCamera identifies non-road waterway cameras correctly', () => {
    const waterwayCam1: Camera = {
      id: '589d0bceb3bf7600110283d6',
      displayName: 'Cầu Sài Gòn 2 (dạ cầu)',
      status: 'WATERWAY_MONITOR',
      isWaterway: true,
      isOnline: false,
    };
    const waterwayCam2: Camera = {
      id: 'cam-w2',
      isOnline: false,
      isWaterway: true,
    };
    const roadCamUnderBridge: Camera = {
      id: '58abbee5bd82540010390ba2',
      displayName: 'Cầu Sài Gòn 5 (dạ cầu Thủ Đức)',
      status: 'UP',
      isOnline: true,
    };
    const regularCam: Camera = {
      id: 'cam-reg',
      status: 'UP',
      isOnline: true,
    };

    assert.equal(isWaterwayCamera(waterwayCam1), true);
    assert.equal(isWaterwayCamera(waterwayCam2), true);
    assert.equal(isWaterwayCamera(roadCamUnderBridge), false);
    assert.equal(isWaterwayCamera(regularCam), false);
    assert.equal(isWaterwayCamera(null), false);
    assert.equal(isWaterwayCamera(undefined), false);
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

  it('CAMERA_FILTER_I18N contains compact localization strings without emojis for mobile viewports', () => {
    const emojiRegex = /[\u{1F300}-\u{1F9FF}]|[\u{2600}-\u{26FF}]|[\u{2700}-\u{27BF}]/u;

    const counterCompactVi = CAMERA_FILTER_I18N.COUNTER_COMPACT.vi(9, 837);
    const counterCompactEn = CAMERA_FILTER_I18N.COUNTER_COMPACT.en(9, 837);
    const hideBtnCompactVi = CAMERA_FILTER_I18N.HIDE_OFFLINE_BTN_COMPACT.vi;
    const hideBtnCompactEn = CAMERA_FILTER_I18N.HIDE_OFFLINE_BTN_COMPACT.en;
    const showBtnCompactVi = CAMERA_FILTER_I18N.SHOW_OFFLINE_BTN_COMPACT.vi;
    const showBtnCompactEn = CAMERA_FILTER_I18N.SHOW_OFFLINE_BTN_COMPACT.en;

    assert.equal(counterCompactVi, '9 / 837 cam');
    assert.equal(counterCompactEn, '9 / 837 cams');
    assert.equal(hideBtnCompactVi, '[Ẩn offline]');
    assert.equal(hideBtnCompactEn, '[Hide offline]');
    assert.equal(showBtnCompactVi, '[Hiện offline]');
    assert.equal(showBtnCompactEn, '[Show offline]');

    const compactStrings = [
      counterCompactVi,
      counterCompactEn,
      hideBtnCompactVi,
      hideBtnCompactEn,
      showBtnCompactVi,
      showBtnCompactEn,
    ];

    compactStrings.forEach(str => {
      assert.equal(emojiRegex.test(str), false, `Compact string contains emoji: ${str}`);
    });
  });

  it('filters out offline and waterway cameras accurately from a collection', () => {
    const cameras: Camera[] = [
      { id: 'c1', status: 'UP', isOnline: true },
      { id: 'c2', status: 'NOT_IMAGE', isOnline: false },
      { id: 'c3', status: 'UP', isOnline: true },
      { id: 'c4', status: 'NO_SIGNAL', isOnline: false },
      { id: 'c5', status: 'UP', isOnline: true },
      { id: 'c6', status: 'WATERWAY_MONITOR', isWaterway: true, isOnline: false },
    ];

    // Regular offline filter
    const onlineOnly = cameras.filter(c => !isCameraOffline(c));
    assert.equal(onlineOnly.length, 3);
    assert.deepEqual(
      onlineOnly.map(c => c.id),
      ['c1', 'c3', 'c5']
    );

    // Map display filter: permanently excludes waterway cameras even if offline cameras are shown
    const roadCameras = cameras.filter(c => !isWaterwayCamera(c));
    assert.equal(roadCameras.length, 5);
    assert.equal(roadCameras.some(c => c.id === 'c6'), false);
  });
});
