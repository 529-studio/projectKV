'use client';

/**
 * useForecast.ts - React Hook for Short-Horizon Congestion Forecasting (ADR-012 §D5, S10-T6)
 * Strict 4-file pattern Step 3: TanStack/React state management, abortable queries, 60s background refresh.
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import type { ForecastMultiResponse, ForecastErrorResponse } from '../types/forecast';
import { fetchForecast } from '../utils/forecastApi';

export interface UseForecastOptions {
  enabled?: boolean;
  pollIntervalMs?: number;
  model?: 'ridge_delta' | 'persistence' | 'historical_median';
}

export interface UseForecastReturn {
  data: ForecastMultiResponse | null;
  isLoading: boolean;
  error: ForecastErrorResponse | null;
  status: number | null;
  isStale: boolean;
  refresh: () => Promise<void>;
}

const DEFAULT_POLL_INTERVAL_MS = 60 * 1000; // 60s background refresh while open

export function useForecast(
  cameraId: string | null | undefined,
  options: UseForecastOptions = {}
): UseForecastReturn {
  const {
    enabled = true,
    pollIntervalMs = DEFAULT_POLL_INTERVAL_MS,
    model = 'ridge_delta',
  } = options;

  const [data, setData] = useState<ForecastMultiResponse | null>(null);
  const [isLoading, setIsLoading] = useState<boolean>(false);
  const [error, setError] = useState<ForecastErrorResponse | null>(null);
  const [status, setStatus] = useState<number | null>(null);

  const abortControllerRef = useRef<AbortController | null>(null);
  const isMountedRef = useRef<boolean>(true);

  const loadData = useCallback(async (isBackground = false) => {
    if (!cameraId || !cameraId.trim() || !enabled) {
      setData(null);
      setError(null);
      setStatus(null);
      setIsLoading(false);
      return;
    }

    // Cancel any previous in-flight request
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }
    const controller = new AbortController();
    abortControllerRef.current = controller;

    if (!isBackground) {
      setIsLoading(true);
    }

    try {
      const result = await fetchForecast(cameraId, {
        signal: controller.signal,
        model,
      });

      if (!isMountedRef.current) return;

      if (result.success) {
        setData(result.data);
        setError(null);
        setStatus(200);
      } else {
        // Non-200 or validation error
        setError(result.error);
        setStatus(result.status);
        // Note: if availability is returned in error envelope (e.g. 422 insufficient_history),
        // we keep data null so numeric predictions never render (ADR-012 D5)
        setData(null);
      }
    } catch (err) {
      if (!isMountedRef.current) return;
      if (err instanceof DOMException && err.name === 'AbortError') {
        // Request was cancelled; ignore
        return;
      }
      setError({
        error: 'unexpected_error',
        message: err instanceof Error ? err.message : 'Unknown forecast failure',
      });
      setStatus(0);
      setData(null);
    } finally {
      if (isMountedRef.current && !isBackground) {
        setIsLoading(false);
      }
    }
  }, [cameraId, enabled, model]);

  // Initial / dependency fetch
  useEffect(() => {
    isMountedRef.current = true;
    loadData(false);

    return () => {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
      }
    };
  }, [loadData]);

  // Polling interval while modal is open & enabled
  useEffect(() => {
    if (!enabled || !cameraId || pollIntervalMs <= 0) return;

    const interval = setInterval(() => {
      loadData(true);
    }, pollIntervalMs);

    return () => {
      clearInterval(interval);
    };
  }, [enabled, cameraId, pollIntervalMs, loadData]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  const refresh = useCallback(async () => {
    await loadData(false);
  }, [loadData]);

  const isStale = Boolean(
    data && (data.availability === 'stale' || data.source_data?.stale)
  );

  return {
    data,
    isLoading,
    error,
    status,
    isStale,
    refresh,
  };
}
