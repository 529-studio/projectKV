/**
 * Tests for the shared hazard-depth view model (S14-T3 / Issue #443).
 *
 * The same view model feeds the map tooltip, the pinned dock and the mobile
 * camera list, so these assertions pin the one contract all three surfaces obey:
 * level label, centimetre figure, three-segment meter, honest unmeasured marker,
 * and a stale chip instead of a silently fresh reading.
 */

import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  buildHazardDepthHtml,
  buildHazardDepthView,
  escapeHtml,
} from './hazardDisplay.ts';
import { HAZARD_I18N } from '../config/constants.ts';

const VI_DIACRITICS = /[ăâđêôơưàáảãạằắẳẵặầấẩẫậèéẻẽẹềếểễệìíỉĩịòóỏõọồốổỗộờớởỡợùúủũụừứửữựỳýỷỹỵ]/u;
const EMOJI = /\p{Extended_Pictographic}/u;

describe('buildHazardDepthView — level label and depth', () => {
  test('a measured Level 3 renders its label, its depth and all three segments', () => {
    const view = buildHazardDepthView(
      { flood_hazard_level: 3, flood_depth_cm: 24.5, flood_depth_confidence: 'MEASURED' },
      'vi',
    );
    assert.equal(view.hasHazard, true);
    assert.equal(view.level, 3);
    assert.equal(view.label, HAZARD_I18N.LEVEL_3.vi);
    assert.equal(view.depthText, '24,5 cm');
    assert.equal(view.isUnmeasured, false);
    assert.deepEqual(view.segments, [true, true, true]);
    assert.equal(view.filledSegments, 3);
    assert.equal(view.inverted, true);
    assert.ok(Math.abs(view.meterPercent - (24.5 / 30) * 100) < 1e-9);
    assert.match(view.summary, /24,5 cm/);
    assert.match(view.summary, /Nguy cơ chết máy cao/);
  });

  test('Level 2 fills two segments and stays on the dark surface', () => {
    const view = buildHazardDepthView({ flood_hazard_level: 2, flood_depth_cm: 14.5 }, 'vi');
    assert.equal(view.label, HAZARD_I18N.LEVEL_2.vi);
    assert.deepEqual(view.segments, [true, true, false]);
    assert.equal(view.filledSegments, 2);
    assert.equal(view.inverted, false);
  });

  test('Level 1 fills one segment', () => {
    const view = buildHazardDepthView({ flood_hazard_level: 1, flood_depth_cm: 4 }, 'en');
    assert.deepEqual(view.segments, [true, false, false]);
    assert.equal(view.depthText, '4 cm');
    assert.equal(view.label, HAZARD_I18N.LEVEL_1.en);
  });

  test('a measured 0 cm is a real number, not an absence of data', () => {
    const view = buildHazardDepthView(
      {
        flood_hazard_level: 1,
        flood_depth_cm: 0,
        flood_depth_confidence: 'MEASURED',
        flood_hazard_basis: 'DEPTH_MEASURED',
      },
      'vi',
    );
    assert.equal(view.isUnmeasured, false);
    assert.equal(view.depthText, '0 cm');
    assert.equal(view.meterPercent, 0);
    assert.match(view.summary, /0 cm/);
  });

  test('the English surface uses a point decimal', () => {
    const view = buildHazardDepthView({ flood_hazard_level: 2, flood_depth_cm: 14.5 }, 'en');
    assert.equal(view.depthText, '14.5 cm');
    assert.match(view.summary, /Depth: 14\.5 cm/);
  });

  test('the VI summary uses translated wording with diacritics', () => {
    const view = buildHazardDepthView({ flood_hazard_level: 3, flood_depth_cm: 24.5 }, 'vi');
    assert.match(view.summary, /Độ sâu/);
    assert.match(view.summary, VI_DIACRITICS);
  });

  test('the meter is clamped to the 30 cm scale', () => {
    const shallow = buildHazardDepthView({ flood_hazard_level: 3, flood_depth_cm: 150 }, 'vi');
    assert.equal(shallow.meterPercent, 100);
  });
});

describe('buildHazardDepthView — honesty (AC2/AC10/AC14)', () => {
  test('an avoidance-inferred Level 3 publishes the level but no centimetres', () => {
    const view = buildHazardDepthView(
      {
        flood_hazard_level: 3,
        flood_hazard_basis: 'AVOIDANCE_INFERRED',
        flood_depth_confidence: 'UNMEASURED',
        flood_depth_cm: null,
      },
      'vi',
    );
    assert.equal(view.level, 3);
    assert.equal(view.depthText, null);
    assert.equal(view.isUnmeasured, true);
    assert.equal(view.meterPercent, 0);
    assert.equal(view.meterAriaLabel, null);
    assert.equal(view.summary, `${HAZARD_I18N.LEVEL_3.vi} · ${HAZARD_I18N.DEPTH_UNMEASURED.vi}`);
    assert.doesNotMatch(view.summary, /\d+(,\d+)?\s*cm/);
  });

  test('a pre-contract verdict has no hazard view and keeps the legacy badge (AC14)', () => {
    const view = buildHazardDepthView(
      { flood_severity: 'FLOODED_SHALLOW', water_ratio: 0.42 },
      'vi',
    );
    assert.equal(view.hasHazard, false);
    assert.equal(view.level, null);
    assert.equal(view.label, null);
    assert.equal(view.summary, '');
    assert.equal(buildHazardDepthHtml(view, 'vi'), '');
  });

  test('water_ratio is never treated as a depth', () => {
    const view = buildHazardDepthView({ water_ratio: 0.6, flood_severity: 'FLOODED_DEEP' }, 'vi');
    assert.equal(view.hasHazard, false);
  });

  test('a null/undefined verdict has no hazard view', () => {
    assert.equal(buildHazardDepthView(null, 'vi').hasHazard, false);
    assert.equal(buildHazardDepthView(undefined, 'en').hasHazard, false);
  });

  test('a stale verdict is labelled stale, never presented as fresh', () => {
    const view = buildHazardDepthView(
      { flood_hazard_level: 2, flood_depth_cm: 14.5 },
      'vi',
      { stale: true },
    );
    assert.equal(view.isStale, true);
    assert.match(view.summary, /Dữ liệu cũ/);
  });

  test('stale defaults to false when the caller does not know the age', () => {
    const view = buildHazardDepthView({ flood_hazard_level: 2, flood_depth_cm: 14.5 }, 'vi');
    assert.equal(view.isStale, false);
    assert.doesNotMatch(view.summary, /Dữ liệu cũ/);
  });
});

describe('accessibility labels (AC15)', () => {
  test('the level and the meter are announced separately', () => {
    const view = buildHazardDepthView({ flood_hazard_level: 3, flood_depth_cm: 24.5 }, 'vi');
    assert.equal(view.levelAriaLabel, HAZARD_I18N.LEVEL_ARIA.vi(3));
    assert.equal(view.meterAriaLabel, HAZARD_I18N.METER_ARIA.vi('24,5 cm'));
  });

  test('an unmeasured hazard omits the meter announcement entirely', () => {
    const view = buildHazardDepthView({ flood_hazard_level: 3, flood_depth_cm: null }, 'en');
    assert.equal(view.meterAriaLabel, null);
    assert.equal(view.levelAriaLabel, HAZARD_I18N.LEVEL_ARIA.en(3));
  });
});

describe('buildHazardDepthHtml — tooltip block', () => {
  test('renders the label, the depth, three segments and no stray markup', () => {
    const view = buildHazardDepthView({ flood_hazard_level: 3, flood_depth_cm: 24.5 }, 'vi');
    const html = buildHazardDepthHtml(view, 'vi');
    assert.match(html, /Nguy cơ chết máy cao/);
    assert.match(html, /Độ sâu: 24,5 cm/);
    assert.equal((html.match(/border-radius:1px/g) ?? []).length, 3);
    assert.doesNotMatch(html, /NaN|undefined|\$\{/);
    assert.match(html, /aria-label=/);
  });

  test('renders the unmeasured sentence and no centimetre figure', () => {
    const view = buildHazardDepthView(
      { flood_hazard_level: 3, flood_hazard_basis: 'AVOIDANCE_INFERRED', flood_depth_cm: null },
      'en',
    );
    const html = buildHazardDepthHtml(view, 'en');
    assert.match(html, /Depth not measurable/);
    assert.doesNotMatch(html, /Depth:/);
    assert.doesNotMatch(html, /\d+(\.\d+)?\s?cm/);
  });

  test('renders a stale chip only when stale', () => {
    const fresh = buildHazardDepthView({ flood_hazard_level: 2, flood_depth_cm: 14.5 }, 'vi');
    assert.doesNotMatch(buildHazardDepthHtml(fresh, 'vi'), /Dữ liệu cũ/);
    const stale = buildHazardDepthView({ flood_hazard_level: 2, flood_depth_cm: 14.5 }, 'vi', {
      stale: true,
    });
    assert.match(buildHazardDepthHtml(stale, 'vi'), /Dữ liệu cũ/);
  });

  test('the block stays emoji-free', () => {
    for (const level of [1, 2, 3]) {
      const view = buildHazardDepthView({ flood_hazard_level: level, flood_depth_cm: 12 }, 'vi');
      assert.doesNotMatch(buildHazardDepthHtml(view, 'vi'), EMOJI);
    }
  });
});

describe('escapeHtml', () => {
  test('escapes the five characters that can break a tooltip', () => {
    assert.equal(escapeHtml(`<b>&"'`), '&lt;b&gt;&amp;&quot;&#39;');
  });

  test('leaves ordinary Vietnamese copy untouched', () => {
    assert.equal(escapeHtml('Ngập sâu 24,5 cm'), 'Ngập sâu 24,5 cm');
  });
});
