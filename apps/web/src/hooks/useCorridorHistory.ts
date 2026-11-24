'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import type { CorridorHistoryResponse } from '../types/corridorHistory.ts';
import { getApiUrl, API_ENDPOINTS } from '../config/constants.ts';

const HISTORY_CACHE_TTL_MS = 60_000; // 1 minute client cache

interface CacheEntry {
  data: CorridorHistoryResponse;
  timestamp: number;
}

const historyCache = new Map<string, CacheEntry>();

export function getCachedCorridorHistory(roadKey: string, hours = 24): CorridorHistoryResponse | null {
  const cacheKey = `${roadKey}:${hours}`;
  const entry = historyCache.get(cacheKey);
  if (!entry) return null;
  if (Date.now() - entry.timestamp > HISTORY_CACHE_TTL_MS) {
    historyCache.delete(cacheKey);
    return null;
  }
  return entry.data;
}

export function clearCorridorHistoryCache(): void {
  historyCache.clear();
}

export interface UseCorridorHistoryResult {
  data: CorridorHistoryResponse | null;
  loading: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

/**
 * React hook for fetching corridor historical flood depth and tidal inundation trend.
 */
export function useCorridorHistory(roadKey: string | null, hours = 24): UseCorridorHistoryResult {
  const [data, setData] = useState<CorridorHistoryResponse | null>(() =>
    roadKey ? getCachedCorridorHistory(roadKey, hours) : null
  );
  const [loading, setLoading] = useState<boolean>(() =>
    roadKey ? getCachedCorridorHistory(roadKey, hours) === null : false
  );
  const [error, setError] = useState<Error | null>(null);

  const activeRoadKeyRef = useRef(roadKey);
  activeRoadKeyRef.current = roadKey;

  const fetchHistory = useCallback(async () => {
    if (!roadKey) {
      setData(null);
      setLoading(false);
      setError(null);
      return;
    }

    const cached = getCachedCorridorHistory(roadKey, hours);
    if (cached) {
      setData(cached);
      setLoading(false);
      setError(null);
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const endpoint = API_ENDPOINTS.ROAD_HISTORY(roadKey, hours);
      const url = getApiUrl(endpoint);
      const res = await fetch(url, {
        headers: { Accept: 'application/json' },
      });

      if (!res.ok) {
        throw new Error(`Failed to fetch corridor history: ${res.status}`);
      }

      const json: CorridorHistoryResponse = await res.json();
      const cacheKey = `${roadKey}:${hours}`;
      historyCache.set(cacheKey, { data: json, timestamp: Date.now() });

      if (activeRoadKeyRef.current === roadKey) {
        setData(json);
        setLoading(false);
      }
    } catch (err) {
      if (activeRoadKeyRef.current === roadKey) {
        setError(err instanceof Error ? err : new Error(String(err)));
        setLoading(false);
      }
    }
  }, [roadKey, hours]);

  useEffect(() => {
    fetchHistory();
  }, [fetchHistory]);

  return { data, loading, error, refetch: fetchHistory };
}
