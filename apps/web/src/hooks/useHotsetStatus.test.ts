import { test, describe } from 'node:test';
import assert from 'node:assert/strict';
import { fetchHotsetStatus, type UseHotsetStatusResult } from './useHotsetStatus.ts';

describe('useHotsetStatus Poller & Data Fetcher (Issue #251 / S7-T6)', () => {
  test('fetchHotsetStatus successfully fetches and parses hotset status', async () => {
    const mockData = {
      total: 2,
      updated_at: '2026-10-16T15:30:00Z',
      cameras: [
        {
          camera_id: 'cam-arterial-1',
          severity: 'MODERATE',
          saturation_index: 0.45,
          pcu_total: 35.0,
          confidence: 0.95,
          source: 'onnx',
          observed_at: '2026-10-16T15:30:00Z',
          stale: false,
        },
        {
          camera_id: 'cam-arterial-2',
          severity: 'CONGESTED',
          saturation_index: 0.88,
          pcu_total: 70.0,
          confidence: 0.99,
          source: 'onnx',
          observed_at: '2026-10-16T15:30:00Z',
          stale: false,
        },
      ],
    };

    const mockFetch = async () => ({
      ok: true,
      status: 200,
      json: async () => mockData,
    }) as unknown as typeof fetch;

    const result = await fetchHotsetStatus(mockFetch);
    assert.equal(result.total, 2);
    assert.equal(result.updatedAt, '2026-10-16T15:30:00Z');
    assert.equal(result.hotsetStatus.size, 2);

    const cam1 = result.hotsetStatus.get('cam-arterial-1');
    assert.equal(cam1?.saturation_index, 0.45);
    assert.equal(cam1?.severity, 'MODERATE');

    const cam2 = result.hotsetStatus.get('cam-arterial-2');
    assert.equal(cam2?.saturation_index, 0.88);
    assert.equal(cam2?.severity, 'CONGESTED');
  });

  test('fetchHotsetStatus handles non-200 HTTP responses gracefully', async () => {
    const mockFetch = async () => ({
      ok: false,
      status: 503,
      json: async () => ({ error: 'Service Unavailable' }),
    }) as unknown as typeof fetch;

    const result = await fetchHotsetStatus(mockFetch);
    assert.equal(result.hotsetStatus.size, 0);
    assert.equal(result.total, 0);
    assert.equal(result.error, 'HTTP error 503');
  });

  test('fetchHotsetStatus handles network throw gracefully', async () => {
    const mockFetch = (async () => {
      throw new Error('Network timeout');
    }) as unknown as typeof fetch;

    const result = await fetchHotsetStatus(mockFetch);
    assert.equal(result.hotsetStatus.size, 0);
    assert.equal(result.total, 0);
    assert.equal(result.error, 'Network timeout');
  });

  test('fetchHotsetStatus ignores aborted requests without setting error', async () => {
    const mockFetch = (async () => {
      const err = new Error('The user aborted a request.');
      err.name = 'AbortError';
      throw err;
    }) as unknown as typeof fetch;

    const result = await fetchHotsetStatus(mockFetch);
    assert.equal(result.hotsetStatus.size, 0);
    assert.equal(result.error, null); // Abort is clean cancellation, not an error
  });
});
