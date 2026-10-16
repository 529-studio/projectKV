import { useState, useEffect, useCallback, useRef } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants';
import {
  fetchHotsetStatus,
  type HotsetCameraStatus,
} from '../utils/hotsetUtils';

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

    const endpointUrl = getApiUrl(API_ENDPOINTS.HOTSET_STATUS);
    const result = await fetchHotsetStatus(endpointUrl, fetch, controller.signal);

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
