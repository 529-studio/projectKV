/**
 * Citizen subscription utilities (S12-T4 / Issue #402, spec §5.1).
 *
 * Pure helpers with no Buffer, btoa or atob: the browser and the Node test
 * runner must produce byte-for-byte the same deep-link payloads as the Go
 * implementation in services/go-server/internal/telegrambot/deeplink.go.
 *
 * Grammar (spec §2.1):
 *   payload := "sub_" target
 *   target  := "all" | "zone_" slug | "ward_" slug | "cam_" id
 *   slug    := base64url(UTF-8(id)) without "=", at most 64 chars in total
 */

import { SUBSCRIPTION_I18N } from '../config/constants.ts';

export type TargetType = 'all' | 'zone' | 'ward' | 'chokepoint';
export type Lang = 'vi' | 'en';

export interface TargetSelection {
  type: TargetType;
  id: string;
}

export interface WardItem {
  type: string;
  name: string;
}

export interface WardGroup {
  type: string;
  wards: WardItem[];
}

export const MAX_START_PAYLOAD = 64;
export const PAYLOAD_PREFIX = 'sub_';
export const CHAT_ID_PATTERN = /^[0-9]{5,20}$/;
export const BOT_USERNAME_PATTERN = /^[A-Za-z][A-Za-z0-9_]{3,30}$/;
/** Camera ids are `[A-Za-z0-9_-]{1,40}` — same pattern as the Go encoder. */
export const CHOKEPOINT_ID_PATTERN = /^[A-Za-z0-9_-]{1,40}$/;

const B64URL_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
const B64URL_VALUES = new Map<string, number>(
  Array.from(B64URL_ALPHABET).map((char, index) => [char, index]),
);

/** Encodes a string as unpadded base64url over its UTF-8 bytes. */
export function slugEncode(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let out = '';
  for (let i = 0; i < bytes.length; i += 3) {
    const b0 = bytes[i];
    const has1 = i + 1 < bytes.length;
    const has2 = i + 2 < bytes.length;
    const b1 = has1 ? bytes[i + 1] : 0;
    const b2 = has2 ? bytes[i + 2] : 0;
    out += B64URL_ALPHABET[b0 >> 2];
    out += B64URL_ALPHABET[((b0 & 0x03) << 4) | (b1 >> 4)];
    if (has1) {
      out += B64URL_ALPHABET[((b1 & 0x0f) << 2) | (b2 >> 6)];
    }
    if (has2) {
      out += B64URL_ALPHABET[b2 & 0x3f];
    }
  }
  return out;
}

/**
 * Decodes an unpadded base64url slug back to a string, or null when the slug is
 * not canonical. Padding, foreign characters, non-zero trailing bits and
 * invalid UTF-8 are rejected — exactly like Go's RawURLEncoding.Strict().
 */
export function slugDecode(slug: string): string | null {
  if (typeof slug !== 'string' || slug.length === 0 || slug.length % 4 === 1) {
    return null;
  }

  const values: number[] = [];
  for (const char of slug) {
    const value = B64URL_VALUES.get(char);
    if (value === undefined) {
      return null;
    }
    values.push(value);
  }

  const remainder = values.length % 4;
  const last = values[values.length - 1];
  if (remainder === 2 && (last & 0x0f) !== 0) {
    return null;
  }
  if (remainder === 3 && (last & 0x03) !== 0) {
    return null;
  }

  const bytes: number[] = [];
  for (let i = 0; i < values.length; i += 4) {
    const c0 = values[i];
    const c1 = i + 1 < values.length ? values[i + 1] : 0;
    const c2 = i + 2 < values.length ? values[i + 2] : 0;
    const c3 = i + 3 < values.length ? values[i + 3] : 0;
    bytes.push((c0 << 2) | (c1 >> 4));
    if (i + 2 < values.length) {
      bytes.push(((c1 & 0x0f) << 4) | (c2 >> 2));
    }
    if (i + 3 < values.length) {
      bytes.push(((c2 & 0x03) << 6) | c3);
    }
  }

  try {
    const decoded = new TextDecoder('utf-8', { fatal: true }).decode(new Uint8Array(bytes));
    return decoded.length === 0 ? null : decoded;
  } catch {
    return null;
  }
}

/**
 * Builds the Telegram `start` payload for a selection.
 * Throws when the selection is invalid or the payload would exceed 64 chars.
 */
export function buildStartPayload(sel: TargetSelection): string {
  if (!sel || typeof sel.type !== 'string') {
    throw new Error('invalid target selection');
  }

  let payload: string;
  switch (sel.type) {
    case 'all':
      payload = `${PAYLOAD_PREFIX}all`;
      break;
    case 'zone':
    case 'ward': {
      if (typeof sel.id !== 'string' || sel.id.trim() === '') {
        throw new Error('target id is required');
      }
      payload = `${PAYLOAD_PREFIX}${sel.type}_${slugEncode(sel.id)}`;
      break;
    }
    case 'chokepoint': {
      if (typeof sel.id !== 'string' || !CHOKEPOINT_ID_PATTERN.test(sel.id)) {
        throw new Error('invalid chokepoint id');
      }
      payload = `${PAYLOAD_PREFIX}cam_${sel.id}`;
      break;
    }
    default:
      throw new Error('unknown target type');
  }

  if (payload.length > MAX_START_PAYLOAD) {
    throw new Error(`start payload exceeds ${MAX_START_PAYLOAD} characters`);
  }
  return payload;
}

/**
 * Parses a `start` payload into a selection, or null when it is malformed.
 * Prefixes are matched case-insensitively; slugs stay case-sensitive.
 */
export function parseStartPayload(payload: string): TargetSelection | null {
  if (typeof payload !== 'string') {
    return null;
  }
  const trimmed = payload.trim();
  if (trimmed.length === 0 || trimmed.length > MAX_START_PAYLOAD) {
    return null;
  }
  if (!/^sub_/i.test(trimmed)) {
    return null;
  }

  const rest = trimmed.slice(PAYLOAD_PREFIX.length);
  const lower = rest.toLowerCase();

  if (lower === 'all') {
    return { type: 'all', id: '' };
  }
  if (lower.startsWith('zone_')) {
    const id = slugDecode(rest.slice('zone_'.length));
    return id === null ? null : { type: 'zone', id };
  }
  if (lower.startsWith('ward_')) {
    const id = slugDecode(rest.slice('ward_'.length));
    return id === null ? null : { type: 'ward', id };
  }
  if (lower.startsWith('cam_')) {
    const id = rest.slice('cam_'.length);
    return CHOKEPOINT_ID_PATTERN.test(id) ? { type: 'chokepoint', id } : null;
  }
  return null;
}

/** Normalizes a public bot handle (strips `@`) or returns null when invalid. */
export function normalizeBotUsername(raw?: string | null): string | null {
  if (typeof raw !== 'string') {
    return null;
  }
  const value = raw.trim().replace(/^@/, '');
  return BOT_USERNAME_PATTERN.test(value) ? value : null;
}

/** Builds the `https://t.me/<bot>?start=<payload>` deep link. */
export function buildTelegramDeepLink(username: string, sel: TargetSelection): string {
  const bot = normalizeBotUsername(username);
  if (!bot) {
    throw new Error('invalid bot username');
  }
  return `https://t.me/${bot}?start=${buildStartPayload(sel)}`;
}

/** Web users are private chats: 5 to 20 digits, never a `-100…` group id. */
export function isValidChatId(value: string): boolean {
  return typeof value === 'string' && CHAT_ID_PATTERN.test(value);
}

/** Lowercase + strip diacritics so `phuong ben nghe` matches `Phường Bến Nghé`. */
export function normalizeSearch(value: string): string {
  if (typeof value !== 'string') {
    return '';
  }
  return value
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .replace(/đ/g, 'd')
    .replace(/Đ/g, 'D')
    .toLowerCase()
    .trim();
}

function compareByCodePoint(a: string, b: string): number {
  const left = Array.from(a);
  const right = Array.from(b);
  const shared = Math.min(left.length, right.length);
  for (let i = 0; i < shared; i += 1) {
    const diff = left[i].codePointAt(0)! - right[i].codePointAt(0)!;
    if (diff !== 0) {
      return diff;
    }
  }
  return left.length - right.length;
}

/**
 * Diacritic-insensitive ward search. Matches the bare name as well as the
 * "type + name" form, sorts by code point and caps the result (default 20).
 */
export function filterWards(wards: WardItem[], q: string, limit = 20): WardItem[] {
  if (!Array.isArray(wards)) {
    return [];
  }
  const needle = normalizeSearch(q);
  const matches = wards.filter((ward) => {
    if (!ward || typeof ward.name !== 'string') {
      return false;
    }
    if (needle === '') {
      return true;
    }
    const haystack = normalizeSearch(`${ward.type ?? ''} ${ward.name}`);
    return haystack.includes(needle);
  });

  const sorted = [...matches].sort((a, b) => compareByCodePoint(a.name, b.name));
  return limit > 0 && sorted.length > limit ? sorted.slice(0, limit) : sorted;
}

/** Groups wards by their administrative type, preserving first-seen order. */
export function groupWardsByType(wards: WardItem[]): WardGroup[] {
  if (!Array.isArray(wards)) {
    return [];
  }
  const groups = new Map<string, WardItem[]>();
  for (const ward of wards) {
    if (!ward || typeof ward.name !== 'string') {
      continue;
    }
    const type = typeof ward.type === 'string' ? ward.type : '';
    const bucket = groups.get(type);
    if (bucket) {
      bucket.push(ward);
    } else {
      groups.set(type, [ward]);
    }
  }
  return Array.from(groups, ([type, items]) => ({ type, wards: items }));
}

/** Human-readable label for a selection in the requested language. */
export function describeTarget(sel: TargetSelection, lang: Lang): string {
  const copy = SUBSCRIPTION_I18N;
  switch (sel.type) {
    case 'all':
      return copy.SCOPE_ALL[lang];
    case 'zone':
      return `${copy.SCOPE_ZONE[lang]}: ${sel.id}`;
    case 'ward':
      return `${copy.SCOPE_WARD[lang]}: ${sel.id}`;
    case 'chokepoint':
      return `${copy.SCOPE_CHOKEPOINT[lang]}: ${sel.id}`;
    default:
      return sel.id;
  }
}

function extractErrorMessage(body: unknown): string {
  if (body && typeof body === 'object' && 'error' in body) {
    const value = (body as { error?: unknown }).error;
    return typeof value === 'string' ? value : '';
  }
  return '';
}

/**
 * Maps a submit failure to citizen-facing copy (spec §5.1, E15):
 * 429 -> rate limit, 400 -> field error or unknown target, everything else and
 * a network failure (status 0) -> network copy.
 */
export function mapSubmitError(status: number, body: unknown, lang: Lang): string {
  const copy = SUBSCRIPTION_I18N;
  if (status === 429) {
    return copy.ERROR_RATE_LIMIT[lang];
  }
  if (status === 400) {
    const message = extractErrorMessage(body);
    if (/chat[_\s-]?id/i.test(message)) {
      return copy.CHAT_ID_INVALID[lang];
    }
    return copy.ERROR_UNKNOWN_TARGET[lang];
  }
  return copy.ERROR_NETWORK[lang];
}
