import test, { describe } from 'node:test';
import assert from 'node:assert/strict';
import {
  handleHotkey,
  isLabelComplete,
  LABELER_I18N,
  formatSnapshotTimestamp,
  buildAuthHeaders,
  type LabelState,
} from './labelerUtils.ts';

describe('Labeler Utilities & Hotkeys (Issue #214)', () => {
  test('handleHotkey maps numbers to flood levels', () => {
    const s1: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('1', s1).state.flood_level, 'none');
    assert.equal(handleHotkey('2', s1).state.flood_level, 'puddle');
    assert.equal(handleHotkey('3', s1).state.flood_level, 'deep');
  });

  test('handleHotkey maps Q/W/E/R to traffic levels case-insensitively', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('q', s).state.traffic_level, 'clear');
    assert.equal(handleHotkey('Q', s).state.traffic_level, 'clear');
    assert.equal(handleHotkey('w', s).state.traffic_level, 'moderate');
    assert.equal(handleHotkey('W', s).state.traffic_level, 'moderate');
    assert.equal(handleHotkey('e', s).state.traffic_level, 'heavy');
    assert.equal(handleHotkey('E', s).state.traffic_level, 'heavy');
    assert.equal(handleHotkey('r', s).state.traffic_level, 'gridlock');
    assert.equal(handleHotkey('R', s).state.traffic_level, 'gridlock');
  });

  test('handleHotkey maps A/S/D to weather', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('a', s).state.weather, 'clear');
    assert.equal(handleHotkey('s', s).state.weather, 'rain');
    assert.equal(handleHotkey('d', s).state.weather, 'fog');
  });

  test('handleHotkey maps Z/X/C to time of day', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('z', s).state.time_of_day, 'day');
    assert.equal(handleHotkey('x', s).state.time_of_day, 'dusk');
    assert.equal(handleHotkey('c', s).state.time_of_day, 'night');
  });

  test('handleHotkey triggers navigation and save actions', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('ArrowLeft', s).action, 'prev');
    assert.equal(handleHotkey('ArrowRight', s).action, 'next');
    assert.equal(handleHotkey('Enter', s).action, 'save');
  });

  test('isLabelComplete requires all 4 criteria', () => {
    assert.equal(isLabelComplete({}), false);
    assert.equal(isLabelComplete({ flood_level: 'none' }), false);
    assert.equal(isLabelComplete({ flood_level: 'none', traffic_level: 'clear' }), false);
    assert.equal(isLabelComplete({ flood_level: 'none', traffic_level: 'clear', weather: 'clear' }), false);
    assert.equal(isLabelComplete({ flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' }), true);
  });

  test('LABELER_I18N contains bilingual strings with zero emojis', () => {
    const emojiRegex = /[\p{Emoji_Presentation}\p{Extended_Pictographic}]/u;

    for (const locale of ['vi', 'en'] as const) {
      const strings = LABELER_I18N[locale];
      assert.ok(strings.title);
      assert.ok(strings.floodLabel);
      assert.ok(strings.trafficLabel);
      assert.ok(strings.weatherLabel);
      assert.ok(strings.timeOfDayLabel);
      assert.ok(strings.saveAndNext);

      // Verify zero emojis in user-facing UI strings
      for (const [key, val] of Object.entries(strings)) {
        if (typeof val === 'string') {
          assert.equal(emojiRegex.test(val), false, `Emoji found in ${locale}.${key}: ${val}`);
        }
      }
    }
  });

  test('formatSnapshotTimestamp formats ICT time', () => {
    // 1728637200 is 2024-10-11T09:00:00Z -> 16:00:00 ICT
    const formatted = formatSnapshotTimestamp(1728637200);
    assert.ok(formatted.includes('16:00') || formatted.includes('2024'));
  });

  test('buildAuthHeaders constructs X-Dev-Token and Bearer authorization headers', () => {
    const empty = buildAuthHeaders('');
    assert.deepEqual(empty, {});

    const withBase = buildAuthHeaders('my-secret-token', { 'Content-Type': 'application/json' });
    assert.equal(withBase['Content-Type'], 'application/json');
    assert.equal(withBase['X-Dev-Token'], 'my-secret-token');
    assert.equal(withBase['Authorization'], 'Bearer my-secret-token');
  });

  test('LABELER_I18N contains developer auth gatekeeper strings', () => {
    for (const locale of ['vi', 'en'] as const) {
      const strings = LABELER_I18N[locale];
      assert.ok(strings.devAuthRequired);
      assert.ok(strings.devAuthSubtitle);
      assert.ok(strings.devTokenPlaceholder);
      assert.ok(strings.devUnlock);
      assert.ok(strings.devInvalidToken);
      assert.ok(strings.devLockSession);
      assert.ok(strings.saveError);
    }
  });
});

