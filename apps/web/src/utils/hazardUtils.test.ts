/**
 * Tests for the physical hazard block helpers (Issue #401 / Sprint 12 Task 3).
 *
 * Contract under test: `docs/sprints/s12_t3_spec.md` §2.3 (bands), §6.1 (helpers),
 * §6.3 (`HAZARD_I18N`) and §7 (edge cases E1-E5, E8, E12, E16, E17).
 *
 * Written against the specification, not an implementation: `hazardUtils.ts` ships
 * as a skeleton, so a red run with "is not implemented yet" is the expected TDD
 * state. The catalog assertions already run against production data and pin the
 * bilingual, emoji-free wording (AC15).
 */

import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  levelFromDepthCm,
  resolveHazard,
  formatDepthCm,
  getHazardBadgeStyle,
  type HazardLevel,
  type VerdictLike,
} from './hazardUtils.ts';
import { HAZARD_I18N } from '../config/constants.ts';
import { normalizeVerdict } from './trafficUtils.ts';

const VI_DIACRITICS = /[ăâđêôơưàáảãạằắẳẵặầấẩẫậèéẻẽẹềếểễệìíỉĩịòóỏõọồốổỗộờớởỡợùúủũụừứửữựỳýỷỹỵ]/u;
const EMOJI = /\p{Extended_Pictographic}/u;
const VARIATION_SELECTOR = /\uFE0F/u;
const HUE_TOKENS = /\b(?:red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose)-\d{2,3}\b/;

type HazardLeaf = {
  readonly vi: string | ((arg: string | number) => string);
  readonly en: string | ((arg: string | number) => string);
};

function renderLeaf(name: string, leaf: HazardLeaf, lang: 'vi' | 'en'): string {
  const value = leaf[lang];
  if (typeof value === 'string') return value;
  return value(name === 'LEVEL_ARIA' ? 3 : '14,5 cm');
}

const CATALOG = Object.entries(HAZARD_I18N as unknown as Record<string, HazardLeaf>);

describe('levelFromDepthCm — physical bands (spec §2.3, AC3, E2, E16)', () => {
  test('classifies the exact boundary table', () => {
    const cases: Array<[number, HazardLevel]> = [
      [0, 1],
      [9.99, 1],
      [10.0, 2],
      [19.99, 2],
      [20.0, 3],
      [20.01, 3],
      [150.0, 3],
    ];
    for (const [depth, expected] of cases) {
      assert.equal(levelFromDepthCm(depth), expected, `depth ${depth} cm`);
    }
  });

  test('20.0 cm is inclusive into Level 3 (the exhaust outlet sits at the waterline)', () => {
    assert.equal(levelFromDepthCm(20.0), 3);
    assert.equal(levelFromDepthCm(19.999999), 2);
  });

  test('10.0 cm is inclusive into Level 2', () => {
    assert.equal(levelFromDepthCm(10.0), 2);
    assert.equal(levelFromDepthCm(9.999999), 1);
  });

  test('never rounds before classifying', () => {
    // Rounding to one decimal first would turn 19.96 cm into a stall warning.
    assert.equal(levelFromDepthCm(9.949), 1);
    assert.equal(levelFromDepthCm(19.949), 2);
    assert.equal(levelFromDepthCm(20.049), 3);
  });

  test('a measured dry road is Level 1', () => {
    assert.equal(levelFromDepthCm(0), 1);
  });
});

describe('resolveHazard — published level, rollout fallback, pre-contract (AC2, AC14, E12)', () => {
  test('prefers the published level over a re-derivation', () => {
    const hazard = resolveHazard({ flood_hazard_level: 3, flood_depth_cm: 8.0 });
    assert.ok(hazard);
    assert.equal(hazard.level, 3);
    assert.equal(hazard.depthCm, 8.0);
    assert.equal(hazard.isUnmeasured, false);
  });

  test('derives the level from a measured depth when the level is missing (rolling deploy)', () => {
    const hazard = resolveHazard({ flood_depth_cm: 14.5 });
    assert.ok(hazard);
    assert.equal(hazard.level, 2);
    assert.equal(hazard.basis, 'DEPTH_MEASURED');
    assert.equal(hazard.depthConfidence, 'INFERRED');
    assert.equal(hazard.depthCm, 14.5);
    assert.equal(hazard.isUnmeasured, false);
  });

  test('derives Level 3 at exactly 20 cm', () => {
    const hazard = resolveHazard({ flood_depth_cm: 20.0 });
    assert.ok(hazard);
    assert.equal(hazard.level, 3);
  });

  test('a measured dry 0 cm stays Level 1 with a visible number (E3)', () => {
    const hazard = resolveHazard({
      flood_hazard_level: 1,
      flood_depth_cm: 0,
      flood_depth_confidence: 'MEASURED',
      flood_hazard_basis: 'DEPTH_MEASURED',
    });
    assert.ok(hazard);
    assert.equal(hazard.level, 1);
    assert.equal(hazard.depthCm, 0);
    assert.equal(hazard.isUnmeasured, false);
    assert.equal(hazard.depthConfidence, 'MEASURED');
  });

  test('an avoidance escalation publishes Level 3 with no centimetre figure (AC10/E4)', () => {
    const hazard = resolveHazard({
      flood_hazard_level: 3,
      flood_hazard_basis: 'AVOIDANCE_INFERRED',
      flood_depth_confidence: 'UNMEASURED',
      flood_depth_cm: null,
    });
    assert.ok(hazard);
    assert.equal(hazard.level, 3);
    assert.equal(hazard.basis, 'AVOIDANCE_INFERRED');
    assert.equal(hazard.depthCm, null);
    assert.equal(hazard.isUnmeasured, true);
  });

  test('returns null for a pre-contract verdict so the legacy badge keeps rendering (AC14)', () => {
    const legacyVerdict = {
      camera_id: 'cam_dbp_01',
      severity: 'CLEAR',
      flood_severity: 'FLOODED_SHALLOW',
      confidence: 0.82,
      timestamp: '2026-11-14T09:12:30Z',
      observed_at: '2026-11-14T09:12:30Z',
    };
    assert.equal(resolveHazard(legacyVerdict), null);
  });

  test('never manufactures a level out of water_ratio', () => {
    // The defect this issue names: an area ratio is not a depth.
    const hazard = resolveHazard({ water_ratio: 0.42, flood_severity: 'FLOODED_DEEP' });
    assert.equal(hazard, null);
  });

  test('returns null when both the level and the depth are absent or null', () => {
    assert.equal(resolveHazard({}), null);
    assert.equal(resolveHazard({ flood_hazard_level: null, flood_depth_cm: null }), null);
    assert.equal(resolveHazard(null), null);
    assert.equal(resolveHazard(undefined), null);
  });

  test('a non-finite depth is not a measurement', () => {
    assert.equal(resolveHazard({ flood_depth_cm: Number.NaN }), null);
    assert.equal(resolveHazard({ flood_depth_cm: Number.POSITIVE_INFINITY }), null);
  });

  test('an out-of-range level is ignored in favour of the measured depth', () => {
    // A level of 0 or 4 is not in the vocabulary; the depth still is.
    const fromDepth = resolveHazard({ flood_hazard_level: 4, flood_depth_cm: 14.5 });
    assert.ok(fromDepth);
    assert.equal(fromDepth.level, 2);

    assert.equal(resolveHazard({ flood_hazard_level: 0 }), null);
  });

  test('unknown basis and confidence strings are not surfaced to the UI', () => {
    const hazard = resolveHazard({
      flood_hazard_level: 3,
      flood_hazard_basis: 'SOMETHING_ELSE',
      flood_depth_confidence: 'PROBABLY',
      flood_depth_cm: null,
    });
    assert.ok(hazard);
    assert.equal(hazard.basis, 'NONE');
    assert.equal(hazard.depthConfidence, 'UNMEASURED');
    assert.equal(hazard.isUnmeasured, true);
  });

  test('an unmeasured depth is reported as unmeasured even with a published level', () => {
    const hazard = resolveHazard({ flood_hazard_level: 2, flood_depth_cm: null });
    assert.ok(hazard);
    assert.equal(hazard.level, 2);
    assert.equal(hazard.depthCm, null);
    assert.equal(hazard.isUnmeasured, true);
  });
});

describe('formatDepthCm — VI comma, EN point, one decimal (AC1, E17)', () => {
  test('uses a comma as the decimal separator in Vietnamese', () => {
    assert.equal(formatDepthCm(14.5, 'vi'), '14,5 cm');
  });

  test('uses a point as the decimal separator in English', () => {
    assert.equal(formatDepthCm(14.5, 'en'), '14.5 cm');
  });

  test('prints a measured zero rather than hiding it', () => {
    assert.equal(formatDepthCm(0, 'vi'), '0 cm');
    assert.equal(formatDepthCm(0, 'en'), '0 cm');
  });

  test('drops a trailing zero decimal', () => {
    assert.equal(formatDepthCm(20, 'vi'), '20 cm');
    assert.equal(formatDepthCm(150, 'en'), '150 cm');
  });

  test('rounds to a single decimal', () => {
    assert.equal(formatDepthCm(2.25, 'vi'), '2,3 cm');
    assert.equal(formatDepthCm(19.94, 'en'), '19.9 cm');
  });

  test('never inserts thousands separators', () => {
    assert.equal(formatDepthCm(1000, 'en'), '1000 cm');
  });

  test('returns null when there is no credible number to show', () => {
    assert.equal(formatDepthCm(null, 'vi'), null);
    assert.equal(formatDepthCm(undefined, 'en'), null);
    assert.equal(formatDepthCm(Number.NaN, 'vi'), null);
    assert.equal(formatDepthCm(Number.POSITIVE_INFINITY, 'en'), null);
    assert.equal(formatDepthCm(-1, 'vi'), null);
  });
});

describe('getHazardBadgeStyle — monochrome tokens, Level 3 inverted (AC5)', () => {
  test('level 1 recedes into the dark surface', () => {
    assert.deepEqual(getHazardBadgeStyle(1), {
      container: 'border-zinc-700 bg-zinc-900/80 text-zinc-300',
      inverted: false,
    });
  });

  test('level 2 lifts the surface', () => {
    assert.deepEqual(getHazardBadgeStyle(2), {
      container: 'border-zinc-400 bg-zinc-800 text-zinc-100',
      inverted: false,
    });
  });

  test('level 3 inverts for maximum contrast', () => {
    assert.deepEqual(getHazardBadgeStyle(3), {
      container: 'border-zinc-100 bg-zinc-100 text-zinc-950',
      inverted: true,
    });
  });

  test('a pre-contract verdict gets no hazard container', () => {
    assert.deepEqual(getHazardBadgeStyle(null), { container: '', inverted: false });
  });

  test('only Level 3 inverts', () => {
    assert.equal(getHazardBadgeStyle(1).inverted, false);
    assert.equal(getHazardBadgeStyle(2).inverted, false);
    assert.equal(getHazardBadgeStyle(3).inverted, true);
  });

  test('every level is visually distinct and free of hue tokens', () => {
    const containers = [1, 2, 3].map((level) => getHazardBadgeStyle(level as HazardLevel).container);
    assert.equal(new Set(containers).size, 3, 'levels must not share a container style');
    for (const container of containers) {
      assert.doesNotMatch(container, HUE_TOKENS, `hue token leaked into "${container}"`);
      assert.doesNotMatch(container, EMOJI);
    }
  });
});

describe('HAZARD_I18N — bilingual, monochrome, honest (AC15, AC16)', () => {
  test('every entry is a frozen vi/en pair and nothing else', () => {
    assert.ok(Object.isFrozen(HAZARD_I18N));
    assert.ok(CATALOG.length > 0);
    for (const [name, leaf] of CATALOG) {
      assert.deepEqual(Object.keys(leaf).sort(), ['en', 'vi'], `${name} must be a vi/en pair`);
    }
  });

  test('pins the three level labels and their advice (AC4)', () => {
    assert.equal(HAZARD_I18N.LEVEL_1.vi, 'Bấp bênh / Bắn nước');
    assert.equal(HAZARD_I18N.LEVEL_1.en, 'Splash zone — low risk');
    assert.equal(HAZARD_I18N.LEVEL_2.vi, 'Cảnh báo xe ga gầm thấp');
    assert.equal(HAZARD_I18N.LEVEL_2.en, 'Caution — low-clearance scooters');
    assert.equal(HAZARD_I18N.LEVEL_3.vi, 'Nguy cơ chết máy cao');
    assert.equal(HAZARD_I18N.LEVEL_3.en, 'High stall risk — do not enter');
  });

  test('contains zero emoji or variation selectors in any language', () => {
    for (const [name, leaf] of CATALOG) {
      for (const lang of ['vi', 'en'] as const) {
        const text = renderLeaf(name, leaf, lang);
        assert.doesNotMatch(text, EMOJI, `${name}.${lang} contains an emoji`);
        assert.doesNotMatch(text, VARIATION_SELECTOR, `${name}.${lang} contains a variation selector`);
      }
    }
  });

  test('every string is non-empty and translated', () => {
    for (const [name, leaf] of CATALOG) {
      const vi = renderLeaf(name, leaf, 'vi');
      const en = renderLeaf(name, leaf, 'en');
      assert.ok(vi.trim().length > 0, `${name}.vi is empty`);
      assert.ok(en.trim().length > 0, `${name}.en is empty`);
      assert.notEqual(vi, en, `${name} is not translated`);
    }
  });

  test('Vietnamese copy carries full diacritics (default language)', () => {
    for (const [name, leaf] of CATALOG) {
      assert.match(renderLeaf(name, leaf, 'vi'), VI_DIACRITICS, `${name}.vi lost its diacritics`);
    }
  });

  test('the avoidance sentence never claims a depth (AC10)', () => {
    for (const lang of ['vi', 'en'] as const) {
      assert.doesNotMatch(
        HAZARD_I18N.AVOIDANCE_ADVICE[lang],
        /\d/,
        'avoidance proves a blocked lane, not a centimetre value',
      );
    }
  });

  test('depth advice quotes the band it describes', () => {
    assert.match(HAZARD_I18N.LEVEL_1_ADVICE.vi, /10/);
    assert.match(HAZARD_I18N.LEVEL_1_ADVICE.en, /10/);
    assert.match(HAZARD_I18N.LEVEL_3_ADVICE.vi, /20/);
    assert.match(HAZARD_I18N.LEVEL_3_ADVICE.en, /20/);
  });

  test('the unmeasured copy offers no number', () => {
    assert.equal(HAZARD_I18N.DEPTH_UNMEASURED.vi, 'Chưa đo được độ sâu');
    assert.equal(HAZARD_I18N.DEPTH_UNMEASURED.en, 'Depth not measurable');
  });

  test('each calibration reference is named in the rider’s language (AC2)', () => {
    assert.match(HAZARD_I18N.REF_MOTORCYCLE.vi, /110/);
    assert.match(HAZARD_I18N.REF_MOTORCYCLE.en, /110/);
    assert.match(HAZARD_I18N.REF_CURB.vi, /15/);
    assert.match(HAZARD_I18N.REF_CURB.en, /15/);
    assert.notEqual(HAZARD_I18N.REF_OVERRIDE.vi, HAZARD_I18N.REF_OVERRIDE.en);
  });

  test('the stale marker is bilingual (E14)', () => {
    assert.equal(HAZARD_I18N.STALE.vi, 'Dữ liệu cũ');
    assert.equal(HAZARD_I18N.STALE.en, 'Stale data');
  });

  test('aria labels take the same argument in both languages', () => {
    assert.equal(HAZARD_I18N.METER_ARIA.vi('14,5 cm'), 'Độ sâu nước 14,5 cm, thang từ 0 đến 30 cm');
    assert.equal(HAZARD_I18N.METER_ARIA.en('14.5 cm'), 'Water depth 14.5 cm, scale 0 to 30 cm');
    assert.equal(HAZARD_I18N.LEVEL_ARIA.vi(3), 'Mức nguy hiểm 3 trên 3');
    assert.equal(HAZARD_I18N.LEVEL_ARIA.en(3), 'Hazard level 3 of 3');
  });

  test('the level count is announced for every level', () => {
    for (const level of [1, 2, 3]) {
      assert.match(HAZARD_I18N.LEVEL_ARIA.vi(level), new RegExp(`${level} trên 3`));
      assert.match(HAZARD_I18N.LEVEL_ARIA.en(level), new RegExp(`${level} of 3`));
    }
  });
});

describe('normalizeVerdict — camelCase aliases for the hazard fields (§6.1)', () => {
  test('maps camelCase payload keys onto the snake_case contract', () => {
    const normalized = normalizeVerdict({
      camera_id: 'cam_dbp_01',
      severity: 'CLEAR',
      confidence: 0.8,
      timestamp: '2026-11-14T09:12:30Z',
      floodDepthCm: 14.5,
      floodHazardLevel: 2,
      floodHazardBasis: 'DEPTH_MEASURED',
      floodDepthConfidence: 'MEASURED',
      floodCalibrationRef: 'CURB_15CM',
    }) as unknown as Record<string, unknown>;

    assert.equal(normalized.flood_depth_cm, 14.5);
    assert.equal(normalized.flood_hazard_level, 2);
    assert.equal(normalized.flood_hazard_basis, 'DEPTH_MEASURED');
    assert.equal(normalized.flood_depth_confidence, 'MEASURED');
    assert.equal(normalized.flood_calibration_ref, 'CURB_15CM');
  });

  test('leaves the snake_case payload untouched', () => {
    const normalized = normalizeVerdict({
      camera_id: 'cam_dbp_01',
      severity: 'CLEAR',
      confidence: 0.8,
      timestamp: '2026-11-14T09:12:30Z',
      flood_depth_cm: 14.5,
      flood_hazard_level: 2,
    });

    assert.equal(normalized.flood_depth_cm, 14.5);
    assert.equal(normalized.flood_hazard_level, 2);
  });

  test('a normalized camelCase payload resolves to the same hazard block', () => {
    const verdict = normalizeVerdict({
      camera_id: 'cam_dbp_01',
      severity: 'CLEAR',
      confidence: 0.8,
      timestamp: '2026-11-14T09:12:30Z',
      floodDepthCm: 14.5,
    }) as VerdictLike;

    const hazard = resolveHazard(verdict);
    assert.ok(hazard);
    assert.equal(hazard.level, 2);
    assert.equal(hazard.depthCm, 14.5);
  });
});
