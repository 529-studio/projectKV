/**
 * NotificationDrawer.test.ts — Unit and contract verification for NotificationDrawer & NotificationPromptBanner.
 * Issue #550 / Sprint 19 Task 2.
 * Strictly zero emoji per AGENTS.md Rule 1.
 */

import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { describe, test } from 'node:test';
import { fileURLToPath } from 'node:url';

import { NOTIFICATION_DRAWER_I18N } from '../config/constants.ts';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const webRoot = path.resolve(__dirname, '../..');

const EMOJI_REGEX = /\p{Extended_Pictographic}/u;

describe('NotificationDrawer & PromptBanner (Issue #550 / S19-T2)', () => {
  test('NOTIFICATION_DRAWER_I18N contains all required keys with zero emoji and bilingual translations', () => {
    const requiredKeys = [
      'DRAWER_TITLE',
      'DRAWER_DESC',
      'STATUS_TITLE',
      'STATUS_ACTIVE',
      'STATUS_INACTIVE',
      'STATUS_DENIED',
      'STATUS_UNSUPPORTED',
      'CTA_ENABLE',
      'CTA_DISABLE',
      'CTA_TEST',
      'TEST_SENT',
      'TARGET_SECTION_TITLE',
      'TARGET_MY_ROUTE',
      'TARGET_MY_ROUTE_SUB',
      'TARGET_CORRIDORS',
      'TARGET_ALL_CITY',
      'VEHICLE_SECTION_TITLE',
      'VEHICLE_SECTION_SUB',
      'HOURS_SECTION_TITLE',
      'HOURS_COMMUTE_ONLY',
      'HOURS_ALL_DAY',
      'CLOSE_DRAWER',
      'SAVE_CHANGES',
      'SAVED_SUCCESS',
      'PROMPT_TITLE',
      'PROMPT_DESC',
      'PROMPT_CTA',
      'PROMPT_DISMISS',
    ] as const;

    for (const key of requiredKeys) {
      assert.ok(key in NOTIFICATION_DRAWER_I18N, `Key ${key} must exist in NOTIFICATION_DRAWER_I18N`);
      const val = NOTIFICATION_DRAWER_I18N[key as keyof typeof NOTIFICATION_DRAWER_I18N];
      assert.ok(val.vi, `Key ${key} must have Vietnamese translation`);
      assert.ok(val.en, `Key ${key} must have English translation`);

      // Rule 1 zero emoji
      assert.equal(
        EMOJI_REGEX.test(val.vi),
        false,
        `Key ${key} [vi] must have zero emoji: "${val.vi}"`,
      );
      assert.equal(
        EMOJI_REGEX.test(val.en),
        false,
        `Key ${key} [en] must have zero emoji: "${val.en}"`,
      );
    }
  });

  test('NotificationDrawer source complies with zero emoji, accessibility, and 48px touch targets', () => {
    const drawerPath = path.join(webRoot, 'src/components/NotificationDrawer.tsx');
    const content = fs.readFileSync(drawerPath, 'utf8');

    // Rule 1: strictly zero emoji in code
    assert.equal(
      EMOJI_REGEX.test(content),
      false,
      'NotificationDrawer.tsx must have strictly zero emoji',
    );

    // Accessibility dialog semantics
    assert.match(content, /role="dialog"/, 'Must specify role="dialog"');
    assert.match(content, /aria-modal="true"/, 'Must specify aria-modal="true"');
    assert.match(content, /aria-labelledby=/, 'Must specify aria-labelledby');
    assert.match(content, /z-\[980\]/, 'NotificationDrawer must layer above z-[800] map overlays and z-[970] SubscriptionModal');

    // Kiosk touch ergonomics: minimum 48px buttons and touch labels
    assert.match(
      content,
      /min-w-\[48px\] min-h-\[48px\]/,
      'Close button must meet 48px touch target',
    );
    assert.match(
      content,
      /min-h-\[48px\]/,
      'Interactive radio labels and CTA buttons must meet min-h-[48px]',
    );

    // Web Push hook and local storage persistence
    assert.match(content, /useWebPush/, 'Must integrate useWebPush hook');
    assert.match(content, /kv_notification_preferences_v1/, 'Must persist preferences to localStorage');
    assert.match(content, /selectedCorridor/, 'Must support arterial corridor selection');
    assert.match(content, /vehicleType/, 'Must support vehicle clearance threshold selection');
  });

  test('NotificationPromptBanner source complies with zero emoji and 48px touch targets', () => {
    const bannerPath = path.join(webRoot, 'src/components/NotificationPromptBanner.tsx');
    const content = fs.readFileSync(bannerPath, 'utf8');

    // Rule 1: zero emoji
    assert.equal(
      EMOJI_REGEX.test(content),
      false,
      'NotificationPromptBanner.tsx must have strictly zero emoji',
    );

    // Semantic region
    assert.match(content, /role="region"/, 'Must specify role="region"');

    // 48px touch ergonomics
    assert.match(
      content,
      /min-h-\[48px\] min-w-\[48px\]/,
      'Dismiss button must meet 48px touch target',
    );
    assert.match(
      content,
      /min-h-\[48px\]/,
      'CTA button must meet min-h-[48px]',
    );
  });
});
