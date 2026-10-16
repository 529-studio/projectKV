import { useState, useEffect, useCallback, useRef } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants.ts';
import {
  parseHotsetStatusMap,
  type HotsetCameraStatus,
  type HotsetStatusResponse,
} from '../utils/hotsetUtils.ts';

export const DEFAULT_HOTSET_POLL_INTERVAL_MS = 30000;

export interface UseHotsetStatusOptions {
  pollIntervalMs?: number;
  enabled?: boolean;
}

export interface UseHotsetStatusResult {
  hotsetStatus: Map<string, HotsetCameraStatus>;
  total: number;
  updatedAt: string | null;
  loading: boolean;
  error: string | null;
  refresh: () => Promise<void>;
}

/**
 * Pure fetcher function for hotset status endpoint.
 * Isolated from React state for testing and headless execution.
 */
export async function fetchHotsetStatus(
  customFetch: typeof fetch = fetch,
  signal?: AbortSignal
): Promise<{
  hotsetStatus: Map<string, HotsetCameraStatus>;
  total: number;
  updatedAt: string | null;
  error: string | null;
}> {
  try {
    const res = await customFetch(getApiUrl(API_ENDPOINTS.HOTSET_STATUS), {
      signal,
    });

    if (!res.ok) {
      return {
        hotsetStatus: new Map(),
        total: 0,
        updatedAt: null,
        error: `HTTP error ${res.status}`,
      };
    }

    const data: HotsetStatusResponse = await res.json();
    const map = parseHotsetStatusMap(data);

    return {
      hotsetStatus: map,
      total: data?.total ?? map.size,
      updatedAt: data?.updated_at ?? null,
      error: null,
    };
  } catch (err: unknown) {
    if (err instanceof Error && err.name === 'AbortError') {
      return {
        hotsetStatus: new Map(),
        total: 0,
        updatedAt: null,
        error: null,
      };
    }
    const message = err instanceof Error ? err.message : 'Unknown error';
    return {
      hotsetStatus: new Map(),
      total: 0,
      updatedAt: null,
      error: message,
    };
  }
}

/**
 * React hook to poll GET /api/v1/cameras/hotset/status every 30 seconds.
 * Provides real-time arterial saturation indexes for the 50 Hot Set cameras.
 */
export function useHotsetStatus(options: UseHotsetStatusOptions = {}): UseHotsetStatusResult {
  const { pollIntervalMs = DEFAULT_HOTSET_POLL_INTERVAL_MS, enabled = true } = options;

  const [hotsetStatus, setHotsetStatus] = useState<Map<string, HotsetCameraStatus>>(new Map());
  const [total, setTotal] = useState<number>(0);
  const [updatedAt, setUpdatedAt] = useState<string | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);

  const abortControllerRef = useRef<AbortController | null>(null);

  const executeFetch = useCallback(async () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }
    const controller = new AbortController();
    abortControllerRef.current = controller;

    const result = await fetchHotsetStatus(fetch, controller.signal);

    if (!controller.signal.aborted) {
      if (!result.error) {
        setHotsetStatus(result.hotsetStatus);
        setTotal(result.total);
        setUpdatedAt(result.updatedAt);
        setError(null);
      } else {
        setError(result.error);
      }
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) {
      setLoading(false);
      return;
    }

    let isCancelled = false;

    executeFetch();

    const intervalId = setInterval(() => {
      if (!isCancelled) {
        executeFetch();
      }
    }, pollIntervalMs);

    return () => {
      isCancelled = true;
      clearInterval(intervalId);
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
        abortControllerRef.current = null;
      }
    };
  }, [enabled, pollIntervalMs, executeFetch]);

  return {
    hotsetStatus,
    total,
    updatedAt,
    loading,
    error,
    refresh: executeFetch,
  };
}
