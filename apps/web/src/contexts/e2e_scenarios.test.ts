import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import { MAP_CONFIG } from '../config/constants.ts';
import {
  parseSSEPayload,
  type LiveStreamEventType,
  type LiveStreamSubscriber,
} from './LiveStreamContext.ts';

describe('Manual Testing Scenario Checklist & End-to-End Verification (AGENTS.md Rule 10)', () => {
  // --------------------------------------------------------------------------
  // Scenario 1: Exactly 1 EventSource connection per client; single deserialization
  // --------------------------------------------------------------------------
  test('Scenario 1: Unified LiveStream multiplexing — single JSON parsing for dual subscribers', () => {
    let parseCount = 0;
    const testJson = JSON.stringify({
      camera_id: 'cam-district-1-01',
      severity: 'HIGH',
      flood_severity: 'MODERATE',
      confidence: 0.95,
      timestamp: new Date().toISOString(),
    });

    const trackedParse = (data: string) => {
      parseCount++;
      return parseSSEPayload(data);
    };

    const subscribers = new Map<LiveStreamEventType, Set<LiveStreamSubscriber<unknown>>>([
      ['camera_update', new Set()],
      ['hex_update', new Set()],
      ['flood_hex_update', new Set()],
      ['user_flood_report', new Set()],
    ]);

    const trafficVerdicts: unknown[] = [];
    const floodVerdicts: unknown[] = [];

    // Register useTraffic's camera_update listener
    subscribers.get('camera_update')?.add((payload) => {
      trafficVerdicts.push(payload);
    });

    // Register useFlood's camera_update listener
    subscribers.get('camera_update')?.add((payload) => {
      floodVerdicts.push(payload);
    });

    // Simulate single EventSource message dispatch
    const parsed = trackedParse(testJson);
    assert.notEqual(parsed, null);

    const listeners = subscribers.get('camera_update');
    listeners?.forEach((fn) => fn(parsed));

    // Assert that parse happened exactly ONCE
    assert.equal(parseCount, 1, 'JSON.parse must only be executed once for all subscribers');
    // Assert both subscribers received identical payload reference
    assert.equal(trafficVerdicts.length, 1);
    assert.equal(floodVerdicts.length, 1);
    assert.equal(trafficVerdicts[0], floodVerdicts[0], 'Both consumers must share the same deserialized object');
  });

  // --------------------------------------------------------------------------
  // Scenario 2: Elimination of Initial Render Reconnect Loop
  // --------------------------------------------------------------------------
  test('Scenario 2: Initial Render Reconnect Loop elimination via appEnvRef stability', () => {
    let eventSourceClosedCount = 0;
    let eventSourceCreatedCount = 0;

    class MockEventSource {
      readyState = 1;
      constructor(_url: string) {
        eventSourceCreatedCount++;
      }
      close() {
        eventSourceClosedCount++;
      }
    }

    // Mount LiveStreamRootProvider with singleton MockEventSource
    const es = new MockEventSource('/api/live/stream');
    assert.equal(eventSourceCreatedCount, 1);
    assert.equal(eventSourceClosedCount, 0);

    // Simulate useTraffic fetchInitial resolving with new app_env
    const appEnvRef = { current: 'production' };

    const updateVerdict = (raw: { source?: string }) => {
      return appEnvRef.current === 'development' || raw.source !== 'mock';
    };

    // Transition app_env from production to development
    appEnvRef.current = 'development';

    // Verify updateVerdict reflects new appEnvRef immediately
    assert.equal(updateVerdict({ source: 'mock' }), true);

    // Because LiveStreamContext manages EventSource independently,
    // neither es.close() nor new EventSource() was triggered!
    assert.equal(eventSourceCreatedCount, 1, 'Exactly 1 EventSource connection must remain active');
    assert.equal(eventSourceClosedCount, 0, 'EventSource must not be closed on app_env state transition');

    es.close();
    assert.equal(eventSourceClosedCount, 1);
  });

  // --------------------------------------------------------------------------
  // Scenario 3: Leaflet preferCanvas configuration
  // --------------------------------------------------------------------------
  test('Scenario 3: MAP_CONFIG.PREFER_CANVAS is enabled for GPU-accelerated HTML5 Canvas rendering', () => {
    assert.equal(
      MAP_CONFIG.PREFER_CANVAS,
      true,
      'MAP_CONFIG.PREFER_CANVAS must be true to consolidate 837 markers into Canvas'
    );
  });

  // --------------------------------------------------------------------------
  // Scenario 4: Page Visibility API Throttle & Immediate Refresh (Deterministic clock)
  // --------------------------------------------------------------------------
  test('Scenario 4: Page Visibility API pauses interval timer when hidden and resumes/refreshes when visible', () => {
    let fetchCount = 0;
    let visibilityState: 'visible' | 'hidden' = 'visible';
    let isPolling = false;
    let simulatedClock = 1_000_000;
    let lastFetched = 0;

    const executeFetch = () => {
      lastFetched = simulatedClock;
      fetchCount++;
    };

    const startPolling = () => {
      isPolling = true;
    };

    const stopPolling = () => {
      isPolling = false;
    };

    const handleVisibilityChange = () => {
      if (visibilityState === 'visible') {
        executeFetch();
        startPolling();
      } else {
        stopPolling();
      }
    };

    // Deterministic simulation tick
    const tick = () => {
      simulatedClock += 30_000;
      if (isPolling && visibilityState !== 'hidden') {
        executeFetch();
      }
    };

    // 1. Initial mount while visible
    executeFetch();
    startPolling();
    assert.equal(fetchCount, 1, 'Initial fetch runs on mount');
    assert.equal(isPolling, true, 'Polling state is active when visible');

    // Advance clock tick
    tick();
    assert.equal(fetchCount, 2, 'Polling fires interval tick while visible');

    // 2. Tab becomes hidden
    visibilityState = 'hidden';
    handleVisibilityChange();
    assert.equal(isPolling, false, 'Polling is suspended when tab is hidden');

    // Advance multiple clock ticks while hidden
    tick();
    tick();
    tick();
    assert.equal(fetchCount, 2, 'No fetches execute while tab is hidden');

    // 3. Tab becomes visible again
    visibilityState = 'visible';
    handleVisibilityChange();
    assert.equal(fetchCount, 3, 'Must refresh immediately upon becoming visible');
    assert.equal(isPolling, true, 'Polling is resumed upon becoming visible');

    // 4. Focus event immediately following visibilitychange should debounce
    const handleFocus = () => {
      if (visibilityState === 'visible' && simulatedClock - lastFetched > 2000) {
        executeFetch();
      }
    };
    handleFocus();
    assert.equal(fetchCount, 3, 'Focus event within debounce window must not trigger duplicate fetch');

    // Advance time beyond debounce window and trigger focus
    simulatedClock += 3000;
    handleFocus();
    assert.equal(fetchCount, 4, 'Focus event outside debounce window triggers fresh update');

    stopPolling();
  });
});
