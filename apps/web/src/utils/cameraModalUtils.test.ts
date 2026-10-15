import assert from 'node:assert/strict';
import { test, describe, beforeEach } from 'node:test';
import {
  getFloodVerifyButtonText,
  isFloodVerifyDisabled,
  calculateCooldownRemaining,
  getFloodReportButtonText,
  isFloodReportDisabled,
  calculateReportCooldownRemaining,
  getCameraReportCooldown,
  setCameraReportCooldown,
  clearCameraReportCooldowns,
  CAMERA_REPORT_MODAL_I18N,
} from './cameraModalUtils.ts';
import { FLOOD_VERIFY_I18N, FLOOD_REPORT_I18N } from '../config/constants.ts';

describe('CameraModal UI/UX & Cooldown Utilities (S6-T3 & S7-T3 / Issue #223, #248)', () => {
  // Unicode regex to enforce Rule 1 zero-emoji policy across all generated UI texts
  const emojiRegex = /\p{Extended_Pictographic}/u;

  beforeEach(() => {
    clearCameraReportCooldowns();
  });

  describe('getFloodVerifyButtonText', () => {
    test('renders idle state button text without brackets in vi and en', () => {
      const state = { isVerifying: false, cooldownRemaining: 0 };
      const viText = getFloodVerifyButtonText(state, 'vi');
      const enText = getFloodVerifyButtonText(state, 'en');

      assert.equal(viText, 'Xác minh ngập nước');
      assert.equal(enText, 'Verify flood status');

      // Requirement: Remove square brackets [...] from action buttons
      assert.ok(!viText.includes('['), 'VI text must not contain [');
      assert.ok(!viText.includes(']'), 'VI text must not contain ]');
      assert.ok(!enText.includes('['), 'EN text must not contain [');
      assert.ok(!enText.includes(']'), 'EN text must not contain ]');

      // Rule 1 zero-emoji check
      assert.ok(!emojiRegex.test(viText), 'VI text must contain no emoji');
      assert.ok(!emojiRegex.test(enText), 'EN text must contain no emoji');
    });

    test('renders in-flight verifying state button text without brackets in vi and en', () => {
      const state = { isVerifying: true, cooldownRemaining: 0 };
      const viText = getFloodVerifyButtonText(state, 'vi');
      const enText = getFloodVerifyButtonText(state, 'en');

      assert.equal(viText, 'Đang xác minh VLM...');
      assert.equal(enText, 'Verifying with VLM...');

      assert.ok(!viText.includes('['), 'VI text must not contain [');
      assert.ok(!viText.includes(']'), 'VI text must not contain ]');
      assert.ok(!enText.includes('['), 'EN text must not contain [');
      assert.ok(!enText.includes(']'), 'EN text must not contain ]');

      assert.ok(!emojiRegex.test(viText), 'VI text must contain no emoji');
      assert.ok(!emojiRegex.test(enText), 'EN text must contain no emoji');
    });

    test('renders cooldown countdown text with active seconds in vi and en', () => {
      const state15 = { isVerifying: false, cooldownRemaining: 15 };
      assert.equal(getFloodVerifyButtonText(state15, 'vi'), 'Chờ xác minh (15s)');
      assert.equal(getFloodVerifyButtonText(state15, 'en'), 'Cooldown (15s)');

      const state1 = { isVerifying: false, cooldownRemaining: 1 };
      assert.equal(getFloodVerifyButtonText(state1, 'vi'), 'Chờ xác minh (1s)');
      assert.equal(getFloodVerifyButtonText(state1, 'en'), 'Cooldown (1s)');

      // Prioritizes cooldown over in-flight if both are set
      const stateBoth = { isVerifying: true, cooldownRemaining: 14 };
      assert.equal(getFloodVerifyButtonText(stateBoth, 'vi'), 'Chờ xác minh (14s)');
      assert.equal(getFloodVerifyButtonText(stateBoth, 'en'), 'Cooldown (14s)');
    });
  });

  describe('isFloodVerifyDisabled', () => {
    test('returns false when idle with zero cooldown', () => {
      assert.equal(isFloodVerifyDisabled({ isVerifying: false, cooldownRemaining: 0 }), false);
    });

    test('returns true when request is in flight', () => {
      assert.equal(isFloodVerifyDisabled({ isVerifying: true, cooldownRemaining: 0 }), true);
    });

    test('returns true when cooldown is active', () => {
      assert.equal(isFloodVerifyDisabled({ isVerifying: false, cooldownRemaining: 15 }), true);
      assert.equal(isFloodVerifyDisabled({ isVerifying: false, cooldownRemaining: 1 }), true);
    });

    test('returns true when both in-flight and cooldown are active', () => {
      assert.equal(isFloodVerifyDisabled({ isVerifying: true, cooldownRemaining: 10 }), true);
    });
  });

  describe('calculateCooldownRemaining', () => {
    test('returns 0 when uninitialized or invalid timestamp', () => {
      assert.equal(calculateCooldownRemaining(0), 0);
      assert.equal(calculateCooldownRemaining(-1), 0);
    });

    test('returns 15 immediately after trigger (0ms elapsed)', () => {
      const now = 1000000;
      assert.equal(calculateCooldownRemaining(now, 15, now), 15);
    });

    test('returns correct ceiling seconds during cooldown countdown', () => {
      const triggerTime = 1000000;
      // 1000ms elapsed -> 14s remaining
      assert.equal(calculateCooldownRemaining(triggerTime, 15, triggerTime + 1000), 14);
      // 5500ms elapsed -> ceil((15000 - 5500)/1000) = ceil(9.5) = 10s remaining
      assert.equal(calculateCooldownRemaining(triggerTime, 15, triggerTime + 5500), 10);
      // 14500ms elapsed -> 1s remaining
      assert.equal(calculateCooldownRemaining(triggerTime, 15, triggerTime + 14500), 1);
      // 15000ms elapsed -> 0s remaining (expired)
      assert.equal(calculateCooldownRemaining(triggerTime, 15, triggerTime + 15000), 0);
      // 20000ms elapsed -> 0s remaining
      assert.equal(calculateCooldownRemaining(triggerTime, 15, triggerTime + 20000), 0);
    });
  });

  describe('FLOOD_VERIFY_I18N Constants Verification', () => {
    test('contains valid bilingual strings and zero emoji', () => {
      assert.equal(FLOOD_VERIFY_I18N.BUTTON_LABEL.vi, 'Xác minh ngập nước');
      assert.equal(FLOOD_VERIFY_I18N.BUTTON_LABEL.en, 'Verify flood status');
      assert.equal(FLOOD_VERIFY_I18N.COOLDOWN.vi(15), 'Chờ xác minh (15s)');
      assert.equal(FLOOD_VERIFY_I18N.COOLDOWN.en(15), 'Cooldown (15s)');

      const checkNoEmoji = (val: string) => {
        assert.ok(!emojiRegex.test(val), `Emoji detected in: ${val}`);
      };

      checkNoEmoji(FLOOD_VERIFY_I18N.BUTTON_LABEL.vi);
      checkNoEmoji(FLOOD_VERIFY_I18N.BUTTON_LABEL.en);
      checkNoEmoji(FLOOD_VERIFY_I18N.VERIFYING.vi);
      checkNoEmoji(FLOOD_VERIFY_I18N.VERIFYING.en);
      checkNoEmoji(FLOOD_VERIFY_I18N.COOLDOWN.vi(10));
      checkNoEmoji(FLOOD_VERIFY_I18N.COOLDOWN.en(10));
    });
  });

  describe('getFloodReportButtonText (S7-T3 / Issue #248)', () => {
    test('renders idle state button text without brackets in vi and en', () => {
      const state = { isReporting: false, cooldownRemaining: 0 };
      const viText = getFloodReportButtonText(state, 'vi');
      const enText = getFloodReportButtonText(state, 'en');

      assert.equal(viText, 'Báo cáo ngập tại đây');
      assert.equal(enText, 'Report flood here');

      assert.ok(!viText.includes('['), 'VI text must not contain [');
      assert.ok(!viText.includes(']'), 'VI text must not contain ]');
      assert.ok(!enText.includes('['), 'EN text must not contain [');
      assert.ok(!enText.includes(']'), 'EN text must not contain ]');

      assert.ok(!emojiRegex.test(viText), 'VI text must contain no emoji');
      assert.ok(!emojiRegex.test(enText), 'EN text must contain no emoji');
    });

    test('renders in-flight reporting state button text without brackets in vi and en', () => {
      const state = { isReporting: true, cooldownRemaining: 0 };
      const viText = getFloodReportButtonText(state, 'vi');
      const enText = getFloodReportButtonText(state, 'en');

      assert.equal(viText, 'Đang gửi báo cáo...');
      assert.equal(enText, 'Submitting report...');

      assert.ok(!viText.includes('['), 'VI text must not contain [');
      assert.ok(!viText.includes(']'), 'VI text must not contain ]');
      assert.ok(!enText.includes('['), 'EN text must not contain [');
      assert.ok(!enText.includes(']'), 'EN text must not contain ]');

      assert.ok(!emojiRegex.test(viText), 'VI text must contain no emoji');
      assert.ok(!emojiRegex.test(enText), 'EN text must contain no emoji');
    });

    test('renders cooldown countdown text with active 30s remaining in vi and en', () => {
      const state30 = { isReporting: false, cooldownRemaining: 30 };
      assert.equal(getFloodReportButtonText(state30, 'vi'), 'Đã báo cáo (30s)');
      assert.equal(getFloodReportButtonText(state30, 'en'), 'Reported (30s)');

      const state1 = { isReporting: false, cooldownRemaining: 1 };
      assert.equal(getFloodReportButtonText(state1, 'vi'), 'Đã báo cáo (1s)');
      assert.equal(getFloodReportButtonText(state1, 'en'), 'Reported (1s)');

      // Prioritizes cooldown over in-flight if both are set
      const stateBoth = { isReporting: true, cooldownRemaining: 25 };
      assert.equal(getFloodReportButtonText(stateBoth, 'vi'), 'Đã báo cáo (25s)');
      assert.equal(getFloodReportButtonText(stateBoth, 'en'), 'Reported (25s)');
    });
  });

  describe('isFloodReportDisabled (S7-T3 / Issue #248)', () => {
    test('returns false when idle with zero cooldown', () => {
      assert.equal(isFloodReportDisabled({ isReporting: false, cooldownRemaining: 0 }), false);
    });

    test('returns true when submission is in flight', () => {
      assert.equal(isFloodReportDisabled({ isReporting: true, cooldownRemaining: 0 }), true);
    });

    test('returns true when cooldown is active', () => {
      assert.equal(isFloodReportDisabled({ isReporting: false, cooldownRemaining: 30 }), true);
      assert.equal(isFloodReportDisabled({ isReporting: false, cooldownRemaining: 1 }), true);
    });

    test('returns true when both in-flight and cooldown are active', () => {
      assert.equal(isFloodReportDisabled({ isReporting: true, cooldownRemaining: 15 }), true);
    });
  });

  describe('calculateReportCooldownRemaining (S7-T3 / Issue #248)', () => {
    test('returns 0 when uninitialized or invalid timestamp', () => {
      assert.equal(calculateReportCooldownRemaining(0), 0);
      assert.equal(calculateReportCooldownRemaining(-1), 0);
    });

    test('returns 30 immediately after trigger (0ms elapsed)', () => {
      const now = 2000000;
      assert.equal(calculateReportCooldownRemaining(now, 30, now), 30);
    });

    test('returns correct ceiling seconds during 30s countdown', () => {
      const triggerTime = 2000000;
      // 1000ms elapsed -> 29s remaining
      assert.equal(calculateReportCooldownRemaining(triggerTime, 30, triggerTime + 1000), 29);
      // 10000ms elapsed -> 20s remaining
      assert.equal(calculateReportCooldownRemaining(triggerTime, 30, triggerTime + 10000), 20);
      // 29500ms elapsed -> 1s remaining
      assert.equal(calculateReportCooldownRemaining(triggerTime, 30, triggerTime + 29500), 1);
      // 30000ms elapsed -> 0s remaining (expired)
      assert.equal(calculateReportCooldownRemaining(triggerTime, 30, triggerTime + 30000), 0);
      // 35000ms elapsed -> 0s remaining
      assert.equal(calculateReportCooldownRemaining(triggerTime, 30, triggerTime + 35000), 0);
    });
  });

  describe('Per-Camera Report Cooldown Tracking (S7-T3 / Issue #248)', () => {
    test('isolates cooldowns across different cameras', () => {
      const now = 3000000;
      assert.equal(getCameraReportCooldown('cam-1', now), 0);
      assert.equal(getCameraReportCooldown('cam-2', now), 0);

      // Trigger cooldown for cam-1
      setCameraReportCooldown('cam-1', now);
      assert.equal(getCameraReportCooldown('cam-1', now), 30);
      assert.equal(getCameraReportCooldown('cam-2', now), 0);

      // Advance 10 seconds: cam-1 has 20s left, cam-2 still 0
      assert.equal(getCameraReportCooldown('cam-1', now + 10000), 20);
      assert.equal(getCameraReportCooldown('cam-2', now + 10000), 0);

      // Trigger cooldown for cam-2 at now + 10000
      setCameraReportCooldown('cam-2', now + 10000);
      assert.equal(getCameraReportCooldown('cam-1', now + 10000), 20);
      assert.equal(getCameraReportCooldown('cam-2', now + 10000), 30);
    });

    test('clearing cooldowns resets all cameras', () => {
      const now = 3000000;
      setCameraReportCooldown('cam-1', now);
      setCameraReportCooldown('cam-2', now);
      assert.equal(getCameraReportCooldown('cam-1', now), 30);
      assert.equal(getCameraReportCooldown('cam-2', now), 30);

      clearCameraReportCooldowns();
      assert.equal(getCameraReportCooldown('cam-1', now), 0);
      assert.equal(getCameraReportCooldown('cam-2', now), 0);
    });
  });

  describe('FLOOD_REPORT_I18N Constants Verification (S7-T3 / Issue #248)', () => {
    test('contains valid bilingual strings and zero emoji', () => {
      assert.equal(FLOOD_REPORT_I18N.BUTTON_LABEL.vi, 'Báo cáo ngập tại đây');
      assert.equal(FLOOD_REPORT_I18N.BUTTON_LABEL.en, 'Report flood here');
      assert.equal(FLOOD_REPORT_I18N.REPORTING.vi, 'Đang gửi báo cáo...');
      assert.equal(FLOOD_REPORT_I18N.REPORTING.en, 'Submitting report...');
      assert.equal(FLOOD_REPORT_I18N.COOLDOWN.vi(30), 'Đã báo cáo (30s)');
      assert.equal(FLOOD_REPORT_I18N.COOLDOWN.en(30), 'Reported (30s)');
      assert.equal(FLOOD_REPORT_I18N.SUCCESS_CHIP.vi, 'Cảm ơn bạn đã hỗ trợ ghi nhận');
      assert.equal(FLOOD_REPORT_I18N.SUCCESS_CHIP.en, 'Thank you for your report');
      assert.equal(FLOOD_REPORT_I18N.RATE_LIMITED.vi, 'Bạn đã gửi báo cáo gần đây');
      assert.equal(FLOOD_REPORT_I18N.RATE_LIMITED.en, 'You have submitted a report recently');
      assert.equal(FLOOD_REPORT_I18N.ERROR_GENERIC.vi, 'Không thể gửi báo cáo');
      assert.equal(FLOOD_REPORT_I18N.ERROR_GENERIC.en, 'Unable to submit report');

      const checkNoEmoji = (val: string) => {
        assert.ok(!emojiRegex.test(val), `Emoji detected in: ${val}`);
      };

      checkNoEmoji(FLOOD_REPORT_I18N.BUTTON_LABEL.vi);
      checkNoEmoji(FLOOD_REPORT_I18N.BUTTON_LABEL.en);
      checkNoEmoji(FLOOD_REPORT_I18N.REPORTING.vi);
      checkNoEmoji(FLOOD_REPORT_I18N.REPORTING.en);
      checkNoEmoji(FLOOD_REPORT_I18N.COOLDOWN.vi(30));
      checkNoEmoji(FLOOD_REPORT_I18N.COOLDOWN.en(30));
      checkNoEmoji(FLOOD_REPORT_I18N.SUCCESS_CHIP.vi);
      checkNoEmoji(FLOOD_REPORT_I18N.SUCCESS_CHIP.en);
      checkNoEmoji(FLOOD_REPORT_I18N.RATE_LIMITED.vi);
      checkNoEmoji(FLOOD_REPORT_I18N.RATE_LIMITED.en);
      checkNoEmoji(FLOOD_REPORT_I18N.ERROR_GENERIC.vi);
      checkNoEmoji(FLOOD_REPORT_I18N.ERROR_GENERIC.en);

      // Verify CAMERA_REPORT_MODAL_I18N aligns with FLOOD_REPORT_I18N
      assert.equal(CAMERA_REPORT_MODAL_I18N.BUTTON_LABEL.vi, FLOOD_REPORT_I18N.BUTTON_LABEL.vi);
      assert.equal(CAMERA_REPORT_MODAL_I18N.BUTTON_LABEL.en, FLOOD_REPORT_I18N.BUTTON_LABEL.en);
      assert.equal(CAMERA_REPORT_MODAL_I18N.SUCCESS_CHIP.vi, FLOOD_REPORT_I18N.SUCCESS_CHIP.vi);
      assert.equal(CAMERA_REPORT_MODAL_I18N.SUCCESS_CHIP.en, FLOOD_REPORT_I18N.SUCCESS_CHIP.en);
    });
  });
});

