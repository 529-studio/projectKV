import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  parseSSEPayload,
  defaultLiveStreamContextValue,
  type LiveStreamEventType,
  type LiveStreamSubscriber,
} from './LiveStreamContext.ts';
interface CameraPayload {
  camera_id: string;
  severity?: string;
}

interface HexPayload {
  h3_id: string;
}

function isCameraPayload(item: unknown): item is CameraPayload {
  return typeof item === 'object' && item !== null && 'camera_id' in item && typeof item.camera_id === 'string';
}

function isHexPayload(item: unknown): item is HexPayload {
  return typeof item === 'object' && item !== null && 'h3_id' in item && typeof item.h3_id === 'string';
}

describe('LiveStreamContext pure helpers & contract', () => {
  describe('parseSSEPayload', () => {
    test('parses valid JSON object payload', () => {
      const raw = JSON.stringify({ camera_id: 'cam-01', severity: 'MODERATE' });
      const parsed = parseSSEPayload(raw);
      assert.equal(isCameraPayload(parsed), true);
      if (isCameraPayload(parsed)) {
        assert.equal(parsed.camera_id, 'cam-01');
        assert.equal(parsed.severity, 'MODERATE');
      }
    });

    test('parses valid JSON array payload', () => {
      const raw = JSON.stringify([{ camera_id: 'cam-01' }, { camera_id: 'cam-02' }]);
      const parsed = parseSSEPayload(raw);
      assert.equal(Array.isArray(parsed), true);
      if (Array.isArray(parsed)) {
        assert.equal(parsed.length, 2);
        const first = parsed[0];
        assert.equal(isCameraPayload(first), true);
        if (isCameraPayload(first)) {
          assert.equal(first.camera_id, 'cam-01');
        }
      }
    });

    test('returns null on invalid JSON without throwing', () => {
      assert.equal(parseSSEPayload('invalid {json}'), null);
      assert.equal(parseSSEPayload('{"broken: true'), null);
    });

    test('returns null on empty or whitespace strings', () => {
      assert.equal(parseSSEPayload(''), null);
      assert.equal(parseSSEPayload('   \n  \t '), null);
    });

    test('returns null on non-string input', () => {
      assert.equal(parseSSEPayload(undefined as unknown as string), null);
      assert.equal(parseSSEPayload(null as unknown as string), null);
    });
  });

  describe('defaultLiveStreamContextValue fallback contract', () => {
    test('connected defaults to false', () => {
      assert.equal(defaultLiveStreamContextValue.connected, false);
    });

    test('subscribe returns an unsubscribe function without throwing', () => {
      const unsub = defaultLiveStreamContextValue.subscribe('camera_update', () => {});
      assert.equal(typeof unsub, 'function');
      assert.doesNotThrow(() => {
        unsub();
      });
    });
  });

  describe('Subscription and dispatch mechanics', () => {
    test('dispatches parsed payload to registered subscribers and supports unsubscribe', () => {
      const subscribers = new Map<LiveStreamEventType, Set<LiveStreamSubscriber<unknown>>>([
        ['camera_update', new Set()],
        ['hex_update', new Set()],
      ]);

      const subscribe = <T = unknown>(eventType: LiveStreamEventType, handler: LiveStreamSubscriber<T>) => {
        let set = subscribers.get(eventType);
        if (!set) {
          set = new Set();
          subscribers.set(eventType, set);
        }
        const genericHandler = handler as LiveStreamSubscriber<unknown>;
        set.add(genericHandler);
        return () => {
          set?.delete(genericHandler);
        };
      };

      const receivedCamera: unknown[] = [];
      const receivedHex: unknown[] = [];

      const unsubCam = subscribe('camera_update', (data) => {
        receivedCamera.push(data);
      });
      const unsubHex = subscribe('hex_update', (data) => {
        receivedHex.push(data);
      });

      // Simulate SSE event dispatch
      const dispatch = (type: LiveStreamEventType, rawData: string) => {
        const parsed = parseSSEPayload(rawData);
        if (parsed === null) return;
        const listeners = subscribers.get(type);
        listeners?.forEach((fn) => fn(parsed));
      };

      dispatch('camera_update', JSON.stringify({ camera_id: 'cam-100' }));
      dispatch('hex_update', JSON.stringify({ h3_id: '88653' }));

      assert.equal(receivedCamera.length, 1);
      const cam0 = receivedCamera[0];
      assert.equal(isCameraPayload(cam0), true);
      if (isCameraPayload(cam0)) {
        assert.equal(cam0.camera_id, 'cam-100');
      }

      assert.equal(receivedHex.length, 1);
      const hex0 = receivedHex[0];
      assert.equal(isHexPayload(hex0), true);
      if (isHexPayload(hex0)) {
        assert.equal(hex0.h3_id, '88653');
      }

      // Unsubscribe camera
      unsubCam();

      dispatch('camera_update', JSON.stringify({ camera_id: 'cam-101' }));
      dispatch('hex_update', JSON.stringify({ h3_id: '88654' }));

      // camera should not receive new payload, hex should
      assert.equal(receivedCamera.length, 1);
      assert.equal(receivedHex.length, 2);

      unsubHex();
    });
  });
});
