/**
 * Contract tests for the citizen subscription utilities (S12-T4 / Issue #402).
 *
 * The payload vectors below are duplicated verbatim in
 * services/go-server/internal/telegrambot/deeplink_test.go: both sides must
 * produce identical bytes for the same target, or a deep link minted by the web
 * UI would be rejected by the bot.
 */

import assert from 'node:assert/strict';
import { describe, test } from 'node:test';

import { SUBSCRIPTION_I18N } from '../config/constants.ts';
import {
  BOT_USERNAME_PATTERN,
  CHAT_ID_PATTERN,
  MAX_START_PAYLOAD,
  buildStartPayload,
  buildTelegramDeepLink,
  describeTarget,
  filterWards,
  groupWardsByType,
  isValidChatId,
  mapSubmitError,
  normalizeBotUsername,
  normalizeSearch,
  parseStartPayload,
  slugDecode,
  slugEncode,
  type WardItem,
} from './subscriptionUtils.ts';

// --- Shared Go/TS conformance vectors (spec §2.1) --------------------------

const SLUG_VECTORS = [
  { value: 'Bến Nghé', slug: 'QuG6v24gTmdow6k' },
  { value: 'Bình Dương', slug: 'QsOsbmggRMawxqFuZw' },
  { value: 'Bà Rịa - Vũng Tàu', slug: 'QsOgIFLhu4thIC0gVsWpbmcgVMOgdQ' },
];

const PAYLOAD_VECTORS = [
  { payload: 'sub_all', value: null },
  { payload: 'sub_zone_S2h1IHbhu7FjIE7hu5lpIHRow6BuaCBUUC5IQ00', value: 'Khu vực Nội thành TP.HCM' },
  { payload: 'sub_zone_UXXhuq1uIDcgLSBOaMOgIELDqA', value: 'Quận 7 - Nhà Bè' },
  { payload: 'sub_ward_xJDDtG5nIEjGsG5nIFRodeG6rW4', value: 'Đông Hưng Thuận' },
  { payload: 'sub_cam_591c6d3ba6bfb500119400ac', value: '591c6d3ba6bfb500119400ac' },
];

const EMOJI = /\p{Extended_Pictographic}/u;
const VARIATION_SELECTOR = /\uFE0F/u;
const ZERO_WIDTH_JOINER = /\u200D/u;

describe('slugEncode / slugDecode — Go conformance', () => {
  test('encodes the shared vectors byte-for-byte', () => {
    for (const vector of SLUG_VECTORS) {
      assert.equal(slugEncode(vector.value), vector.slug, `slugEncode(${vector.value})`);
    }
  });

  test('decodes the shared vectors back to the exact names', () => {
    for (const vector of SLUG_VECTORS) {
      assert.equal(slugDecode(vector.slug), vector.value, `slugDecode(${vector.slug})`);
    }
  });

  test('round-trips ASCII, Vietnamese and emoji-free mixed text', () => {
    for (const value of ['TP.HCM', 'Phường 25', 'Cụm Cảng Cát Lái', 'a b-c_d', 'Q.12 - Hóc Môn - Củ Chi']) {
      assert.equal(slugDecode(slugEncode(value)), value, `round trip ${value}`);
    }
  });

  test('never emits "=" padding', () => {
    for (const length of [1, 2, 3, 4, 5, 6, 7, 8]) {
      assert.doesNotMatch(slugEncode('a'.repeat(length)), /=/, `length ${length}`);
    }
  });

  test('rejects non-canonical slugs exactly like Go strict decoding', () => {
    const rejected = [
      '',
      '=', // padding
      'QuG6v24gTmdow6k=',
      'QuG6v24gTmdow6', // impossible base64 length (4k+1)
      'QsOsbmggRMawxqFuZx', // non-zero trailing bits
      'QsOsbmggRMawxqFuZ', // truncated quantum with non-zero trailing bits
      'QsOgIFLhu4thIC0gVsWpbmcgVMOgdQ!', // foreign character
      '//8', // standard base64 alphabet
      '_w', // invalid UTF-8 byte 0xff
      'a b',
    ];
    for (const slug of rejected) {
      assert.equal(slugDecode(slug), null, `slugDecode(${JSON.stringify(slug)}) must be null`);
    }
  });
});

describe('buildStartPayload', () => {
  test('emits sub_all for the city-wide scope', () => {
    assert.equal(buildStartPayload({ type: 'all', id: '' }), 'sub_all');
    assert.equal(buildStartPayload({ type: 'all', id: 'ignored' }), 'sub_all');
  });

  test('emits sub_zone_/sub_ward_/sub_cam_ with the Go slug', () => {
    assert.equal(buildStartPayload({ type: 'zone', id: 'Bình Dương' }), 'sub_zone_QsOsbmggRMawxqFuZw');
    assert.equal(buildStartPayload({ type: 'ward', id: 'Bến Nghé' }), 'sub_ward_QuG6v24gTmdow6k');
    assert.equal(
      buildStartPayload({ type: 'ward', id: 'Bà Rịa - Vũng Tàu' }),
      'sub_ward_QsOgIFLhu4thIC0gVsWpbmcgVMOgdQ',
    );
    assert.equal(
      buildStartPayload({ type: 'chokepoint', id: '591c6d3ba6bfb500119400ac' }),
      'sub_cam_591c6d3ba6bfb500119400ac',
    );
  });

  test('stays inside the 64-char Telegram bound for the whole zone taxonomy', () => {
    const zones = [
      'Bà Rịa - Vũng Tàu',
      'Bình Dương',
      'Cụm Cảng Cát Lái',
      'Quận 7 - Nhà Bè',
      'TP. Thủ Đức',
      'Bình Tân - Bình Chánh',
      'Q.12 - Hóc Môn - Củ Chi',
      'Khu vực Nội thành TP.HCM',
    ];
    for (const zone of zones) {
      const payload = buildStartPayload({ type: 'zone', id: zone });
      assert.ok(payload.length <= MAX_START_PAYLOAD, `${zone} -> ${payload.length} chars`);
      assert.deepEqual(parseStartPayload(payload), { type: 'zone', id: zone });
    }
  });

  test('accepts a 41-byte id at exactly 64 chars and refuses 42 bytes', () => {
    const atLimit = buildStartPayload({ type: 'zone', id: 'a'.repeat(41) });
    assert.equal(atLimit.length, 64);
    assert.throws(() => buildStartPayload({ type: 'zone', id: 'a'.repeat(42) }), /exceeds 64/);
    assert.throws(() => buildStartPayload({ type: 'ward', id: 'a'.repeat(42) }), /exceeds 64/);
  });

  test('throws on invalid selections instead of emitting a broken link', () => {
    assert.throws(() => buildStartPayload({ type: 'zone', id: '' }), /target id is required/);
    assert.throws(() => buildStartPayload({ type: 'ward', id: '   ' }), /target id is required/);
    assert.throws(() => buildStartPayload({ type: 'chokepoint', id: '' }), /invalid chokepoint id/);
    assert.throws(() => buildStartPayload({ type: 'chokepoint', id: 'bad id' }), /invalid chokepoint id/);
    assert.throws(() => buildStartPayload({ type: 'chokepoint', id: 'a'.repeat(41) }), /invalid chokepoint id/);
    // @ts-expect-error — runtime guard for a target type the UI cannot produce.
    assert.throws(() => buildStartPayload({ type: 'district', id: '1' }), /unknown target type/);
  });
});

describe('parseStartPayload', () => {
  test('parses the shared payload vectors', () => {
    assert.deepEqual(parseStartPayload('sub_all'), { type: 'all', id: '' });
    assert.deepEqual(parseStartPayload(PAYLOAD_VECTORS[1].payload), {
      type: 'zone',
      id: 'Khu vực Nội thành TP.HCM',
    });
    assert.deepEqual(parseStartPayload(PAYLOAD_VECTORS[2].payload), { type: 'zone', id: 'Quận 7 - Nhà Bè' });
    assert.deepEqual(parseStartPayload(PAYLOAD_VECTORS[3].payload), { type: 'ward', id: 'Đông Hưng Thuận' });
    assert.deepEqual(parseStartPayload(PAYLOAD_VECTORS[4].payload), {
      type: 'chokepoint',
      id: '591c6d3ba6bfb500119400ac',
    });
  });

  test('matches prefixes case-insensitively but keeps slugs case-sensitive', () => {
    assert.deepEqual(parseStartPayload('SUB_ALL'), { type: 'all', id: '' });
    assert.deepEqual(parseStartPayload('Sub_Zone_QsOsbmggRMawxqFuZw'), { type: 'zone', id: 'Bình Dương' });
    assert.deepEqual(parseStartPayload('sub_WARD_QuG6v24gTmdow6k'), { type: 'ward', id: 'Bến Nghé' });
    assert.deepEqual(parseStartPayload('sub_CAM_cam-vvk-01'), { type: 'chokepoint', id: 'cam-vvk-01' });
    assert.deepEqual(parseStartPayload('  sub_all  '), { type: 'all', id: '' });
  });

  test('returns null for every malformed payload the Go decoder rejects', () => {
    const malformed = [
      '',
      '   ',
      'zone_QsOsbmggRMawxqFuZw',
      'start=sub_all',
      'sub_',
      'sub_zone_quận 7',
      'sub_zone_',
      'sub_zone_QsOsbmggRMawxqFuZw=',
      'sub_zone_QsOsbmggRMawxqFuZ',
      'sub_zone_QsOsbmggRMawxqFuZx',
      'sub_zone_QsOsbmggRMawxqFuZ!',
      'sub_zone_//8',
      'sub_zone__w',
      'sub_cam_',
      'sub_cam_bad id',
      `sub_cam_${'a'.repeat(41)}`,
      'sub_unknown_abc',
      'sub_district_Z2h1',
      `sub_zone_${'A'.repeat(56)}`, // 65 chars, above the Telegram limit
    ];
    for (const payload of malformed) {
      assert.equal(parseStartPayload(payload), null, `parseStartPayload(${payload}) must be null`);
    }
  });

  test('accepts a 40-char camera id and rejects 41', () => {
    assert.deepEqual(parseStartPayload(`sub_cam_${'a'.repeat(40)}`), {
      type: 'chokepoint',
      id: 'a'.repeat(40),
    });
    assert.equal(parseStartPayload(`sub_cam_${'a'.repeat(41)}`), null);
  });
});

describe('bot handle and deep link', () => {
  test('normalizes @prefixes and validates the handle grammar', () => {
    assert.equal(normalizeBotUsername('@WardedAlertBot'), 'WardedAlertBot');
    assert.equal(normalizeBotUsername('  WardedAlertBot  '), 'WardedAlertBot');
    assert.equal(normalizeBotUsername('warded'), 'warded');
    assert.equal(normalizeBotUsername('abc'), null); // too short (4+ chars required)
    assert.equal(normalizeBotUsername('bad-handle'), null);
    assert.equal(normalizeBotUsername('a'.repeat(32)), null); // one char too long
    assert.equal(normalizeBotUsername(''), null);
    assert.equal(normalizeBotUsername(undefined), null);
    assert.equal(normalizeBotUsername(null), null);
  });

  test('builds the t.me deep link the modal uses as its primary CTA', () => {
    assert.equal(
      buildTelegramDeepLink('@WardedAlertBot', { type: 'all', id: '' }),
      'https://t.me/WardedAlertBot?start=sub_all',
    );
    assert.equal(
      buildTelegramDeepLink('WardedAlertBot', { type: 'zone', id: 'Bình Dương' }),
      'https://t.me/WardedAlertBot?start=sub_zone_QsOsbmggRMawxqFuZw',
    );
    assert.throws(() => buildTelegramDeepLink('bad-handle', { type: 'all', id: '' }), /invalid bot username/);
  });
});

describe('isValidChatId', () => {
  test('accepts 5 to 20 digits', () => {
    assert.equal(isValidChatId('12345'), true);
    assert.equal(isValidChatId('1234567890'), true);
    assert.equal(isValidChatId('1'.repeat(20)), true);
    assert.equal(CHAT_ID_PATTERN.test('1'.repeat(20)), true);
  });

  test('rejects groups, signs, whitespace and wrong lengths', () => {
    for (const value of ['1234', '1'.repeat(21), '', '-1001234567890', '+84123456789', '12345 ', ' 12345', '12345a', '12 345']) {
      assert.equal(isValidChatId(value), false, `isValidChatId(${JSON.stringify(value)})`);
    }
  });
});

describe('normalizeSearch / filterWards', () => {
  const wards: WardItem[] = [
    { type: 'Phường', name: 'Bến Nghé' },
    { type: 'Phường', name: 'Bình Lợi' },
    { type: 'Xã', name: 'Tân Nhựt' },
    { type: 'Thị trấn', name: 'Củ Chi' },
    { type: 'Phường', name: 'Đông Hưng Thuận' },
  ];

  test('normalizeSearch lowercases and strips Vietnamese diacritics', () => {
    assert.equal(normalizeSearch('Phường Bến Nghé'), 'phuong ben nghe');
    assert.equal(normalizeSearch('ĐÔNG HƯNG THUẬN'), 'dong hung thuan');
    assert.equal(normalizeSearch('Thị trấn Củ Chi'), 'thi tran cu chi');
    assert.equal(normalizeSearch('  BÀ RỊA - VŨNG TÀU  '), 'ba ria - vung tau');
    assert.equal(normalizeSearch(''), '');
    // @ts-expect-error — non-string input must not throw.
    assert.equal(normalizeSearch(undefined), '');
  });

  test('matches diacritic-insensitively on the bare name and on type + name', () => {
    assert.deepEqual(
      filterWards(wards, 'phuong ben nghe').map((w) => w.name),
      ['Bến Nghé'],
    );
    assert.deepEqual(
      filterWards(wards, 'BEN NGHE').map((w) => w.name),
      ['Bến Nghé'],
    );
    assert.deepEqual(
      filterWards(wards, 'tan nhut').map((w) => w.name),
      ['Tân Nhựt'],
    );
    assert.deepEqual(
      filterWards(wards, 'thi tran cu chi').map((w) => w.name),
      ['Củ Chi'],
    );
    assert.deepEqual(
      filterWards(wards, 'dong hung').map((w) => w.name),
      ['Đông Hưng Thuận'],
    );
    assert.deepEqual(filterWards(wards, 'khong ton tai'), []);
  });

  test('sorts by code point when the query is empty', () => {
    assert.deepEqual(
      filterWards(wards, '').map((w) => w.name),
      ['Bình Lợi', 'Bến Nghé', 'Củ Chi', 'Tân Nhựt', 'Đông Hưng Thuận'],
    );
  });

  test('caps the result and defaults to 20', () => {
    assert.equal(filterWards(wards, '', 2).length, 2);
    const many = Array.from({ length: 30 }, (_, index) => ({
      type: 'Phường',
      name: `Phường ${String(index).padStart(2, '0')}`,
    }));
    assert.equal(filterWards(many, '').length, 20);
    assert.equal(filterWards(many, '', 0).length, 30); // 0 means "no cap"
  });

  test('groups wards by type, preserving first-seen order and input order', () => {
    const groups = groupWardsByType(wards);
    assert.deepEqual(
      groups.map((group) => group.type),
      ['Phường', 'Xã', 'Thị trấn'],
    );
    assert.deepEqual(
      groups[0].wards.map((w) => w.name),
      ['Bến Nghé', 'Bình Lợi', 'Đông Hưng Thuận'],
    );
    assert.deepEqual(
      groups[1].wards.map((w) => w.name),
      ['Tân Nhựt'],
    );
    assert.deepEqual(groupWardsByType([]), []);
  });
});

describe('describeTarget', () => {
  test('renders both languages with the selected scope', () => {
    assert.equal(describeTarget({ type: 'all', id: '' }, 'vi'), 'Toàn thành phố');
    assert.equal(describeTarget({ type: 'all', id: '' }, 'en'), 'City-wide');
    assert.equal(describeTarget({ type: 'zone', id: 'Bình Dương' }, 'vi'), 'Khu vực: Bình Dương');
    assert.equal(describeTarget({ type: 'zone', id: 'Bình Dương' }, 'en'), 'Zone: Bình Dương');
    assert.equal(describeTarget({ type: 'ward', id: 'Bến Nghé' }, 'vi'), 'Phường / Xã: Bến Nghé');
    assert.equal(describeTarget({ type: 'chokepoint', id: 'cam-1' }, 'en'), 'Chokepoint: cam-1');
  });
});

describe('mapSubmitError', () => {
  test('maps rate limiting, validation and network failures', () => {
    assert.equal(mapSubmitError(429, null, 'vi'), SUBSCRIPTION_I18N.ERROR_RATE_LIMIT.vi);
    assert.equal(mapSubmitError(429, null, 'en'), SUBSCRIPTION_I18N.ERROR_RATE_LIMIT.en);
    assert.equal(
      mapSubmitError(400, { error: 'chat_id is required' }, 'vi'),
      SUBSCRIPTION_I18N.CHAT_ID_INVALID.vi,
    );
    assert.equal(
      mapSubmitError(400, { error: 'invalid zone target_id', valid_zones: ['Bình Dương'] }, 'en'),
      SUBSCRIPTION_I18N.ERROR_UNKNOWN_TARGET.en,
    );
    assert.equal(mapSubmitError(400, {}, 'en'), SUBSCRIPTION_I18N.ERROR_UNKNOWN_TARGET.en);
    assert.equal(mapSubmitError(500, { error: 'boom' }, 'vi'), SUBSCRIPTION_I18N.ERROR_NETWORK.vi);
    assert.equal(mapSubmitError(0, null, 'en'), SUBSCRIPTION_I18N.ERROR_NETWORK.en);
    assert.equal(mapSubmitError(201, null, 'vi'), SUBSCRIPTION_I18N.ERROR_NETWORK.vi);
  });
});

describe('SUBSCRIPTION_I18N catalog', () => {
  // The 31 keys of spec §5.4. (The spec text says "30 keys"; the list it
  // enumerates contains 31, which is what the modal actually needs.)
  const REQUIRED_KEYS = [
    'TITLE',
    'SUBTITLE',
    'SCOPE_ALL',
    'SCOPE_ZONE',
    'SCOPE_WARD',
    'SCOPE_CHOKEPOINT',
    'ZONE_LABEL',
    'WARD_LABEL',
    'CHOKEPOINT_LABEL',
    'ZONE_PLACEHOLDER',
    'SEARCH_WARD',
    'SEARCH_CHOKEPOINT',
    'TELEGRAM_CTA',
    'TELEGRAM_HINT',
    'BOT_UNAVAILABLE',
    'MANUAL_TOGGLE',
    'CHAT_ID_LABEL',
    'CHAT_ID_HINT',
    'CHAT_ID_INVALID',
    'SUBMIT',
    'SUBMITTING',
    'SUCCESS_TITLE',
    'SUCCESS_BODY',
    'ALREADY_HINT',
    'ERROR_NETWORK',
    'ERROR_RATE_LIMIT',
    'ERROR_UNKNOWN_TARGET',
    'RETRY',
    'CLOSE',
    'PRIVACY_NOTE',
    'OPEN_TELEGRAM',
  ];

  test('declares every key the modal needs', () => {
    assert.deepEqual(Object.keys(SUBSCRIPTION_I18N).sort(), [...REQUIRED_KEYS].sort());
  });

  test('every key ships both vi and en as non-empty strings', () => {
    for (const key of REQUIRED_KEYS) {
      const entry = SUBSCRIPTION_I18N[key as keyof typeof SUBSCRIPTION_I18N] as {
        vi?: unknown;
        en?: unknown;
      };
      assert.equal(typeof entry, 'object', `${key} must be an object`);
      assert.equal(typeof entry.vi, 'string', `${key}.vi must be a string`);
      assert.equal(typeof entry.en, 'string', `${key}.en must be a string`);
      assert.ok((entry.vi as string).trim().length > 0, `${key}.vi must not be empty`);
      assert.ok((entry.en as string).trim().length > 0, `${key}.en must not be empty`);
      assert.notEqual(entry.vi, entry.en, `${key} must actually be translated`);
    }
  });

  test('Vietnamese copy keeps full diacritics', () => {
    assert.equal(SUBSCRIPTION_I18N.TITLE.vi, 'Đăng ký nhận cảnh báo');
    assert.equal(SUBSCRIPTION_I18N.TELEGRAM_CTA.vi, 'Mở Telegram để đăng ký');
    assert.equal(SUBSCRIPTION_I18N.TELEGRAM_CTA.en, 'Open Telegram to subscribe');
    assert.equal(
      SUBSCRIPTION_I18N.PRIVACY_NOTE.vi,
      'Chúng tôi chỉ lưu Chat ID và phạm vi bạn chọn. Gửi /stop để huỷ bất cứ lúc nào.',
    );
    assert.equal(
      SUBSCRIPTION_I18N.PRIVACY_NOTE.en,
      'We store only your chat ID and chosen scope. Send /stop to unsubscribe at any time.',
    );

    // Diacritics must survive: a de-accented VI string would be a copy bug.
    for (const key of ['TITLE', 'SUBTITLE', 'SCOPE_WARD', 'CHAT_ID_INVALID', 'PRIVACY_NOTE', 'ALREADY_HINT'] as const) {
      const vi = SUBSCRIPTION_I18N[key].vi;
      assert.notEqual(normalizeSearch(vi), vi.toLowerCase(), `${key}.vi lost its diacritics`);
    }
  });

  test('every string is emoji-free and free of emoji controls (Rule 1)', () => {
    for (const [key, entry] of Object.entries(SUBSCRIPTION_I18N)) {
      for (const [lang, text] of Object.entries(entry as Record<string, string>)) {
        assert.doesNotMatch(text, EMOJI, `${key}.${lang} contains an emoji`);
        assert.doesNotMatch(text, VARIATION_SELECTOR, `${key}.${lang} contains a variation selector`);
        assert.doesNotMatch(text, ZERO_WIDTH_JOINER, `${key}.${lang} contains a zero-width joiner`);
      }
    }
  });

  test('describeTarget output is emoji-free in both languages', () => {
    const selections = [
      { type: 'all' as const, id: '' },
      { type: 'zone' as const, id: 'Bình Dương' },
      { type: 'ward' as const, id: 'Bến Nghé' },
      { type: 'chokepoint' as const, id: 'cam-1' },
    ];
    for (const sel of selections) {
      for (const lang of ['vi', 'en'] as const) {
        assert.doesNotMatch(describeTarget(sel, lang), EMOJI, `${sel.type}/${lang}`);
      }
    }
  });

  test('bot username pattern matches the Go validator', () => {
    assert.equal(BOT_USERNAME_PATTERN.test('WardedAlertBot'), true);
    assert.equal(BOT_USERNAME_PATTERN.test('warded_alert_bot'), true);
    assert.equal(BOT_USERNAME_PATTERN.test('abc'), false);
    assert.equal(BOT_USERNAME_PATTERN.test('1abc'), false);
    assert.equal(BOT_USERNAME_PATTERN.test('a'.repeat(32)), false);
  });
});
