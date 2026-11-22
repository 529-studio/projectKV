import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { NAVIGATION_I18N } from '../types/navigation.ts';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const webRoot = path.resolve(__dirname, '../..');

describe('S18-T1 / Issue #523: NavigationAlerts Component & Ergonomics Verification', () => {
  const componentPath = path.join(webRoot, 'src/components/NavigationAlerts.tsx');
  const componentContent = fs.readFileSync(componentPath, 'utf8');

  test('Scenario 1: Kiosk Ergonomics - Enforces minimum 48px touch targets on all interactive buttons', () => {
    // Assert all buttons have min-h-[48px] and min-w-[48px]
    const buttonMatches = componentContent.match(/<button[\s\S]*?<\/button>/g) || [];
    assert.ok(buttonMatches.length >= 2, 'Must contain interactive buttons');

    for (const btn of buttonMatches) {
      assert.match(btn, /min-h-\[48px\]/, 'All interactive buttons must enforce min-h-[48px]');
      assert.match(btn, /min-w-\[48px\]/, 'All interactive buttons must enforce min-w-[48px]');
      assert.match(btn, /cursor-pointer/, 'All buttons must show cursor-pointer');
    }
  });

  test('Scenario 2: Accessibility & Semantic Structure', () => {
    assert.match(componentContent, /role="region"/, 'Component container must declare role="region"');
    assert.match(componentContent, /aria-label=\{NAVIGATION_I18N\.TITLE\[lang\]\}/, 'Region must have accessible aria-label');
    assert.match(componentContent, /aria-hidden="true"/, 'Decorative icons must have aria-hidden');
  });

  test('Scenario 3: AGENTS.md Rule 1 - Zero emoji in UI and i18n constants', () => {
    // Emoji regex checking component source and navigation i18n
    const emojiRegex = /[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u;

    assert.ok(!emojiRegex.test(componentContent), 'NavigationAlerts.tsx must not contain emojis');

    const i18nJson = JSON.stringify(NAVIGATION_I18N);
    assert.ok(!emojiRegex.test(i18nJson), 'NAVIGATION_I18N must not contain emojis');
  });

  test('Scenario 4: Bilingual i18n Dictionary Parity', () => {
    for (const [key, val] of Object.entries(NAVIGATION_I18N)) {
      assert.ok(val.vi, `NAVIGATION_I18N.${key} must have Vietnamese translation`);
      assert.ok(val.en, `NAVIGATION_I18N.${key} must have English translation`);
      assert.ok(typeof val.vi === 'string' && val.vi.trim().length > 0);
      assert.ok(typeof val.en === 'string' && val.en.trim().length > 0);
    }
  });

  test('Scenario 5: Dynamic Vehicle Profile & Stall Risk Visual States', () => {
    // Verified classes for severity levels
    assert.match(componentContent, /bg-rose-950\/70/, 'IMPASSABLE alerts must use deep rose background');
    assert.match(componentContent, /bg-amber-950\/70/, 'CAUTION alerts must use amber background');
    assert.match(componentContent, /bg-emerald-950\/80/, 'PASSABLE alerts must use emerald badge');
    assert.match(componentContent, /animate-pulse/, 'IMPASSABLE alerts must pulse warning indicator');
  });
});
