/**
 * Contract and ergonomics tests for SubscriptionModal Web Push & Telegram integration (Issue #525 / S18-T2).
 * Strictly zero emoji per AGENTS.md Rule 1.
 */

import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { describe, test } from 'node:test';
import { fileURLToPath } from 'node:url';

import { SUBSCRIPTION_I18N } from '../config/constants.ts';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const webRoot = path.resolve(__dirname, '../..');

const EMOJI_REGEX = /\p{Extended_Pictographic}/u;

describe('SubscriptionModal Web Push integration & ergonomics (Issue #525)', () => {
  test('SUBSCRIPTION_I18N contains all required Web Push keys with zero emoji and full diacritics', () => {
    const requiredKeys = [
      'TAB_TELEGRAM',
      'TAB_WEBPUSH',
      'WEBPUSH_TITLE',
      'WEBPUSH_DESC',
      'WEBPUSH_CTA_ENABLE',
      'WEBPUSH_CTA_DISABLE',
      'WEBPUSH_CTA_TEST',
      'WEBPUSH_STATUS_ACTIVE',
      'WEBPUSH_STATUS_DENIED',
      'WEBPUSH_STATUS_UNSUPPORTED',
      'WEBPUSH_PRIVACY_NOTE',
    ] as const;

    for (const key of requiredKeys) {
      assert.ok(key in SUBSCRIPTION_I18N, `Key ${key} must exist in SUBSCRIPTION_I18N`);
      const val = SUBSCRIPTION_I18N[key as keyof typeof SUBSCRIPTION_I18N];
      assert.ok(val.vi, `Key ${key} must have Vietnamese translation`);
      assert.ok(val.en, `Key ${key} must have English translation`);

      // Rule 1 zero-emoji validation
      assert.equal(
        EMOJI_REGEX.test(val.vi),
        false,
        `Key ${key} [vi] must not contain emoji: "${val.vi}"`,
      );
      assert.equal(
        EMOJI_REGEX.test(val.en),
        false,
        `Key ${key} [en] must not contain emoji: "${val.en}"`,
      );
    }
  });

  test('SubscriptionModal source meets kiosk 48px touch target ergonomics', () => {
    const modalPath = path.join(webRoot, 'src/components/SubscriptionModal.tsx');
    const content = fs.readFileSync(modalPath, 'utf8');

    // Rule 1 zero-emoji
    assert.equal(
      EMOJI_REGEX.test(content),
      false,
      'SubscriptionModal.tsx must have strictly zero emoji',
    );

    // Kiosk touch target rule: buttons meet min-h-[48px] and min-w-[48px]
    assert.match(
      content,
      /TAB_WEBPUSH/,
      'Must render TAB_WEBPUSH toggle',
    );
    assert.match(
      content,
      /TAB_TELEGRAM/,
      'Must render TAB_TELEGRAM toggle',
    );

    // Matches tab button touch target
    const tabTouchTargetRegex = /role="tab"[\s\S]*?min-h-\[48px\][\s\S]*?min-w-\[48px\]/;
    assert.match(
      content,
      tabTouchTargetRegex,
      'Channel tab buttons must have min-h-[48px] and min-w-[48px]',
    );

    // Matches Web Push CTA touch target
    assert.match(
      content,
      /WEBPUSH_CTA_ENABLE/,
      'Must reference WEBPUSH_CTA_ENABLE',
    );
    assert.match(
      content,
      /WEBPUSH_CTA_DISABLE/,
      'Must reference WEBPUSH_CTA_DISABLE',
    );
    assert.match(
      content,
      /WEBPUSH_CTA_TEST/,
      'Must reference WEBPUSH_CTA_TEST',
    );
  });
});
