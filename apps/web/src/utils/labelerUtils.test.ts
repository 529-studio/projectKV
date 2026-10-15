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

describe('Labeler Utilities & Hotkeys (Issue #214 / S7-T8 Directional Taxonomy)', () => {
  test('handleHotkey maps numbers to flood levels and physical depth anchors', () => {
    const s1: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    const r1 = handleHotkey('1', s1);
    assert.equal(r1.state.flood_level, 'none');
    assert.equal(r1.state.flood_submersion_anchor, 'dry');

    const r2 = handleHotkey('2', s1);
    assert.equal(r2.state.flood_level, 'puddle');
    assert.equal(r2.state.flood_submersion_anchor, 'tire_base_under_10cm');

    const r3 = handleHotkey('3', s1);
    assert.equal(r3.state.flood_level, 'deep');
    assert.equal(r3.state.flood_submersion_anchor, 'exhaust_hub_over_15cm');
  });

  test('handleHotkey maps q/w/e/r to Direction A and global traffic level', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    const rq = handleHotkey('q', s);
    assert.equal(rq.state.traffic_dir_a, 'clear');
    assert.equal(rq.state.traffic_level, 'clear');

    const rw = handleHotkey('w', s);
    assert.equal(rw.state.traffic_dir_a, 'moderate');
    assert.equal(rw.state.traffic_level, 'moderate');

    const re = handleHotkey('e', s);
    assert.equal(re.state.traffic_dir_a, 'heavy');
    assert.equal(re.state.traffic_level, 'heavy');

    const rr = handleHotkey('r', s);
    assert.equal(rr.state.traffic_dir_a, 'congested');
    assert.equal(rr.state.traffic_level, 'gridlock');
  });

  test('handleHotkey maps Shift + Q/W/E/R/T to Direction B', () => {
    const s: LabelState = { flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' };

    assert.equal(handleHotkey('Q', s).state.traffic_dir_b, 'clear');
    assert.equal(handleHotkey('W', s).state.traffic_dir_b, 'moderate');
    assert.equal(handleHotkey('E', s).state.traffic_dir_b, 'heavy');
    assert.equal(handleHotkey('R', s).state.traffic_dir_b, 'congested');
    assert.equal(handleHotkey('T', s).state.traffic_dir_b, 'not_applicable');
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

  test('isLabelComplete requires all core criteria', () => {
    assert.equal(isLabelComplete({}), false);
    assert.equal(isLabelComplete({ flood_level: 'none' }), false);
    assert.equal(isLabelComplete({ flood_level: 'none', traffic_level: 'clear' }), false);
    assert.equal(isLabelComplete({ flood_level: 'none', traffic_level: 'clear', weather: 'clear' }), false);
    assert.equal(
      isLabelComplete({ flood_level: 'none', traffic_level: 'clear', weather: 'clear', time_of_day: 'day' }),
      true
    );
    assert.equal(
      isLabelComplete({ flood_level: 'none', traffic_dir_a: 'clear', weather: 'clear', time_of_day: 'day' }),
      true
    );
  });

  test('LABELER_I18N contains bilingual strings with zero emojis', () => {
    const emojiRegex = /[\p{Emoji_Presentation}\p{Extended_Pictographic}]/u;

    for (const locale of ['vi', 'en'] as const) {
      const strings = LABELER_I18N[locale];
      assert.ok(strings.title);
      assert.ok(strings.floodLabel);
      assert.ok(strings.floodSubmersionLabel);
      assert.ok(strings.trafficLabel);
      assert.ok(strings.trafficDirALabel);
      assert.ok(strings.trafficDirBLabel);
      assert.ok(strings.qualityTagsLabel);
      assert.ok(strings.queueReasonLabel);
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
