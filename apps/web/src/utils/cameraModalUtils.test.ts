import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  getFloodVerifyButtonText,
  isFloodVerifyDisabled,
  calculateCooldownRemaining,
} from './cameraModalUtils.ts';
import { FLOOD_VERIFY_I18N } from '../config/constants.ts';

describe('CameraModal UI/UX & Cooldown Utilities (S6-T3 / Issue #223)', () => {
  // Unicode regex to enforce Rule 1 zero-emoji policy across all generated UI texts
  const emojiRegex = /\p{Extended_Pictographic}/u;

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
});
