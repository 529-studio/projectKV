'use client';

import { useState, useEffect, useCallback } from 'react';
import type { HourlyForecastResponse } from '../types/forecast.ts';

const HOURLY_FORECAST_CACHE_TTL_MS = 60_000; // 1 minute client cache

interface UseHourlyForecastResult {
  forecast: HourlyForecastResponse | null;
  loading: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

export function useHourlyForecast(): UseHourlyForecastResult {
  const [forecast, setForecast] = useState<HourlyForecastResponse | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);

  const fetchForecast = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const base = (typeof process !== 'undefined' && process.env?.NEXT_PUBLIC_API_URL) || '';
      const res = await fetch(`${base}/api/forecast/hourly`, {
        headers: { Accept: 'application/json' },
      });

      if (!res.ok) {
        throw new Error(`Failed to fetch hourly forecast: ${res.status}`);
      }

      const data: HourlyForecastResponse = await res.json();
      setForecast(data);
    } catch (err) {
      console.warn('[useHourlyForecast] Fetch failed:', err);
      setError(err instanceof Error ? err : new Error(String(err)));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchForecast();
    const interval = setInterval(fetchForecast, HOURLY_FORECAST_CACHE_TTL_MS);
    return () => clearInterval(interval);
  }, [fetchForecast]);

  return { forecast, loading, error, refetch: fetchForecast };
}
