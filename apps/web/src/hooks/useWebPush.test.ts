/**
 * Unit tests for `useWebPush.ts` pure helpers (S18-T2 / Issue #525).
 * Strictly zero emoji per AGENTS.md Rule 1.
 */

import assert from 'node:assert/strict';
import { describe, test } from 'node:test';

import {
  checkPushSupport,
  mapPushPermission,
  urlBase64ToUint8Array,
} from './useWebPush.ts';

describe('useWebPush pure utilities (Issue #525)', () => {
  describe('urlBase64ToUint8Array', () => {
    test('correctly decodes standard Base64URL string without padding', () => {
      // "hello" in base64url is "aGVsbG8"
      const result = urlBase64ToUint8Array('aGVsbG8');
      const expected = new Uint8Array([104, 101, 108, 108, 111]); // "hello"
      assert.deepEqual(result, expected);
    });

    test('handles url-safe characters - and _', () => {
      // 0xfb, 0xff in hex -> binary -> base64 "+/8=" -> base64url "-_8"
      const input = '-_8';
      const result = urlBase64ToUint8Array(input);
      assert.equal(result.length, 2);
      assert.equal(result[0], 0xfb);
      assert.equal(result[1], 0xff);
    });

    test('decodes standard 65-byte uncompressed P-256 public key length', () => {
      // 65 bytes in base64url
      const real65Bytes = Buffer.alloc(65, 0x04);
      const b64Url = real65Bytes.toString('base64url');
      const result = urlBase64ToUint8Array(b64Url);
      assert.equal(result.length, 65);
      assert.equal(result[0], 0x04);
    });
  });

  describe('checkPushSupport', () => {
    test('returns true when all APIs are present', () => {
      const mockNav = { serviceWorker: {} };
      const mockWin = { PushManager: {}, Notification: {} };
      assert.equal(checkPushSupport(mockNav as never, mockWin as never), true);
    });

    test('returns false when serviceWorker is missing', () => {
      const mockNav = {};
      const mockWin = { PushManager: {}, Notification: {} };
      assert.equal(checkPushSupport(mockNav as never, mockWin as never), false);
    });

    test('returns false when PushManager is missing', () => {
      const mockNav = { serviceWorker: {} };
      const mockWin = { Notification: {} };
      assert.equal(checkPushSupport(mockNav as never, mockWin as never), false);
    });

    test('returns false when Notification is missing', () => {
      const mockNav = { serviceWorker: {} };
      const mockWin = { PushManager: {} };
      assert.equal(checkPushSupport(mockNav as never, mockWin as never), false);
    });

    test('returns false when nav or win is null', () => {
      assert.equal(checkPushSupport(null, null), false);
    });
  });

  describe('mapPushPermission', () => {
    test('returns unsupported when supported is false', () => {
      assert.equal(mapPushPermission('granted', false), 'unsupported');
    });

    test('maps granted correctly', () => {
      assert.equal(mapPushPermission('granted', true), 'granted');
    });

    test('maps denied correctly', () => {
      assert.equal(mapPushPermission('denied', true), 'denied');
    });

    test('maps default correctly', () => {
      assert.equal(mapPushPermission('default', true), 'default');
    });

    test('maps null or undefined to default when supported', () => {
      assert.equal(mapPushPermission(null, true), 'default');
      assert.equal(mapPushPermission(undefined, true), 'default');
    });
  });
});
