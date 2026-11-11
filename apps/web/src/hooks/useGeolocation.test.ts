/**
 * Unit tests for Issue #400 (S12-T2) — `useGeolocation.ts` pure helpers.
 * Normative source: `docs/sprints/s12_t2_spec.md` §4.2, §5 E1–E4, §7.1.
 *
 * No DOM and no React renderer: only the exported pure mappers are exercised
 * here. The hook state machine itself is covered by the §7.2 manual scenarios
 * (M1–M14) on live services.
 */
import { test, describe } from 'node:test';
import assert from 'node:assert/strict';

import {
  mapGeolocationErrorCode,
  normalizeGeolocationPosition,
  type GeolocationFix,
  type GeolocationErrorCode,
} from './useGeolocation.ts';

const TIMESTAMP = 1_700_000_000_000;

/** Raw `GeolocationPosition`-shaped input, typed from the public signature. */
type RawPosition = Parameters<typeof normalizeGeolocationPosition>[0];

function rawPosition(
  coords: Partial<{ latitude: number; longitude: number; accuracy: number }>,
  timestamp?: number,
): RawPosition {
  return {
    coords,
    timestamp: timestamp ?? TIMESTAMP,
  } as unknown as RawPosition;
}

function nullPosition(): RawPosition {
  return null as unknown as RawPosition;
}

// ---------------------------------------------------------------------------
// mapGeolocationErrorCode (spec §4.2)
// ---------------------------------------------------------------------------

describe('mapGeolocationErrorCode (spec §4.2)', () => {
  test('maps GeolocationPositionError code 1 to PERMISSION_DENIED', () => {
    const expected: GeolocationErrorCode = 'PERMISSION_DENIED';
    assert.equal(mapGeolocationErrorCode(1), expected);
  });

  test('maps GeolocationPositionError code 2 to POSITION_UNAVAILABLE', () => {
    assert.equal(mapGeolocationErrorCode(2), 'POSITION_UNAVAILABLE');
  });

  test('maps GeolocationPositionError code 3 to TIMEOUT', () => {
    assert.equal(mapGeolocationErrorCode(3), 'TIMEOUT');
  });

  test('maps unknown codes to POSITION_UNAVAILABLE (never throws)', () => {
    for (const code of [0, 4, 99, -1, Number.NaN]) {
      assert.equal(mapGeolocationErrorCode(code), 'POSITION_UNAVAILABLE', `code ${code}`);
    }
  });

  test('is a pure function: repeated calls return the same code', () => {
    const first = mapGeolocationErrorCode(1);
    const second = mapGeolocationErrorCode(1);
    assert.equal(first, second);
    assert.equal(mapGeolocationErrorCode(3), 'TIMEOUT');
  });
});

// ---------------------------------------------------------------------------
// normalizeGeolocationPosition (spec §4.2, E4)
// ---------------------------------------------------------------------------

describe('normalizeGeolocationPosition (spec §4.2, E4)', () => {
  test('normalizes a valid browser position', () => {
    const fix: GeolocationFix | null = normalizeGeolocationPosition(
      rawPosition({ latitude: 10.7769, longitude: 106.7009, accuracy: 18 }),
    );

    assert.ok(fix, 'expected a normalized fix');
    assert.deepEqual(fix, {
      latitude: 10.7769,
      longitude: 106.7009,
      accuracy: 18,
      timestamp: TIMESTAMP,
    });
  });

  test('accepts the legal coordinate extremes', () => {
    assert.deepEqual(normalizeGeolocationPosition(rawPosition({ latitude: 90, longitude: 180, accuracy: 5 })), {
      latitude: 90,
      longitude: 180,
      accuracy: 5,
      timestamp: TIMESTAMP,
    });
    assert.deepEqual(normalizeGeolocationPosition(rawPosition({ latitude: -90, longitude: -180, accuracy: 5 })), {
      latitude: -90,
      longitude: -180,
      accuracy: 5,
      timestamp: TIMESTAMP,
    });
    assert.deepEqual(normalizeGeolocationPosition(rawPosition({ latitude: 0, longitude: 0, accuracy: 5 })), {
      latitude: 0,
      longitude: 0,
      accuracy: 5,
      timestamp: TIMESTAMP,
    });
  });

  test('returns null for null or undefined input', () => {
    assert.equal(normalizeGeolocationPosition(nullPosition()), null);
    assert.equal(normalizeGeolocationPosition(undefined as unknown as RawPosition), null);
  });

  test('returns null for NaN coordinates', () => {
    assert.equal(normalizeGeolocationPosition(rawPosition({ latitude: Number.NaN, longitude: 106.7009, accuracy: 10 })), null);
    assert.equal(normalizeGeolocationPosition(rawPosition({ latitude: 10.7769, longitude: Number.NaN, accuracy: 10 })), null);
  });

  test('returns null for out-of-bounds or non-finite coordinates', () => {
    assert.equal(normalizeGeolocationPosition(rawPosition({ latitude: 91, longitude: 106.7009, accuracy: 10 })), null);
    assert.equal(normalizeGeolocationPosition(rawPosition({ latitude: -91, longitude: 106.7009, accuracy: 10 })), null);
    assert.equal(normalizeGeolocationPosition(rawPosition({ latitude: 10.7769, longitude: 181, accuracy: 10 })), null);
    assert.equal(normalizeGeolocationPosition(rawPosition({ latitude: 10.7769, longitude: -181, accuracy: 10 })), null);
    assert.equal(
      normalizeGeolocationPosition(rawPosition({ latitude: Number.POSITIVE_INFINITY, longitude: 106.7009, accuracy: 10 })),
      null,
    );
  });

  test('returns null when accuracy is missing', () => {
    assert.equal(normalizeGeolocationPosition(rawPosition({ latitude: 10.7769, longitude: 106.7009 })), null);
  });

  test('returns null when accuracy is not a usable number', () => {
    assert.equal(
      normalizeGeolocationPosition(rawPosition({ latitude: 10.7769, longitude: 106.7009, accuracy: Number.NaN })),
      null,
    );
    assert.equal(
      normalizeGeolocationPosition(rawPosition({ latitude: 10.7769, longitude: 106.7009, accuracy: Number.POSITIVE_INFINITY })),
      null,
    );
  });

  test('returns null when coords are missing entirely', () => {
    assert.equal(normalizeGeolocationPosition({} as unknown as RawPosition), null);
  });

  test('does not mutate the raw browser position', () => {
    const raw = rawPosition({ latitude: 10.7769, longitude: 106.7009, accuracy: 30 });
    const snapshot = JSON.parse(JSON.stringify(raw));
    normalizeGeolocationPosition(raw);
    assert.deepEqual(JSON.parse(JSON.stringify(raw)), snapshot);
  });
});
