'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants.ts';
import type { CorridorRouteResponse, CorridorRouteSummary } from '../types/corridor.ts';

export const CORRIDOR_ROUTE_CACHE_TTL_MS = 30_000;

const routeCache = new Map<string, { data: CorridorRouteResponse; timestamp: number }>();
let overviewCache: { data: CorridorRouteSummary[]; timestamp: number } | null = null;

export function getCachedCorridorRoute(roadKey: string): CorridorRouteResponse | null {
  const normKey = roadKey.trim().toLowerCase();
  const entry = routeCache.get(normKey);
  if (!entry) return null;
  if (Date.now() - entry.timestamp > CORRIDOR_ROUTE_CACHE_TTL_MS) {
    routeCache.delete(normKey);
    return null;
  }
  return entry.data;
}

export function setCachedCorridorRoute(roadKey: string, data: CorridorRouteResponse): void {
  const normKey = roadKey.trim().toLowerCase();
  routeCache.set(normKey, { data, timestamp: Date.now() });
}

export function clearCorridorRouteCache(): void {
  routeCache.clear();
  overviewCache = null;
}

/**
 * Fetches GeoJSON route and cumulative hazard summary for a corridor road key.
 */
export async function fetchCorridorRoute(roadKey: string): Promise<CorridorRouteResponse | null> {
  const normKey = roadKey.trim().toLowerCase();
  if (!normKey) return null;

  const cached = getCachedCorridorRoute(normKey);
  if (cached) return cached;

  try {
    const url = getApiUrl(API_ENDPOINTS.ROAD_ROUTE(normKey));
    const res = await fetch(url);
    if (!res.ok) {
      if (res.status === 404) return null;
      throw new Error(`Failed to fetch route for ${normKey}: ${res.status}`);
    }
    const data: CorridorRouteResponse = await res.json();
    setCachedCorridorRoute(normKey, data);
    return data;
  } catch (err) {
    console.warn(`[useCorridorRoute] Error fetching route for ${normKey}:`, err);
    return null;
  }
}

/**
 * Fetches high-level status across all 10 canonical commuter arterial corridors.
 */
export async function fetchCorridorsOverview(): Promise<CorridorRouteSummary[]> {
  if (overviewCache && Date.now() - overviewCache.timestamp <= CORRIDOR_ROUTE_CACHE_TTL_MS) {
    return overviewCache.data;
  }

  try {
    const url = getApiUrl(API_ENDPOINTS.CORRIDORS_OVERVIEW);
    const res = await fetch(url);
    if (!res.ok) {
      throw new Error(`Failed to fetch corridors overview: ${res.status}`);
    }
    const data: CorridorRouteSummary[] = await res.json();
    overviewCache = { data, timestamp: Date.now() };
    return data;
  } catch (err) {
    console.warn('[useCorridorRoute] Error fetching corridors overview:', err);
    return [];
  }
}

/**
 * React hook for fetching and caching corridor GeoJSON route and metrics.
 */
export function useCorridorRoute(roadKey: string | null) {
  const [route, setRoute] = useState<CorridorRouteResponse | null>(() =>
    roadKey ? getCachedCorridorRoute(roadKey) : null
  );
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<Error | null>(null);
  const activeRoadKeyRef = useRef<string | null>(roadKey);

  useEffect(() => {
    activeRoadKeyRef.current = roadKey;
    if (!roadKey) {
      setRoute(null);
      setLoading(false);
      setError(null);
      return;
    }

    const cached = getCachedCorridorRoute(roadKey);
    if (cached) {
      setRoute(cached);
      setLoading(false);
      setError(null);
      return;
    }

    let isMounted = true;
    setLoading(true);
    setError(null);

    fetchCorridorRoute(roadKey)
      .then((data) => {
        if (!isMounted || activeRoadKeyRef.current !== roadKey) return;
        setRoute(data);
        setLoading(false);
      })
      .catch((err) => {
        if (!isMounted || activeRoadKeyRef.current !== roadKey) return;
        setError(err instanceof Error ? err : new Error(String(err)));
        setLoading(false);
      });

    return () => {
      isMounted = false;
    };
  }, [roadKey]);

  const refetch = useCallback(() => {
    if (!roadKey) return;
    const normKey = roadKey.trim().toLowerCase();
    routeCache.delete(normKey);
    setLoading(true);
    fetchCorridorRoute(normKey)
      .then((data) => {
        setRoute(data);
        setLoading(false);
      })
      .catch((err) => {
        setError(err instanceof Error ? err : new Error(String(err)));
        setLoading(false);
      });
  }, [roadKey]);

  return { route, loading, error, refetch };
}

/**
 * React hook for prefetching all canonical corridors overview.
 */
export function useCorridorsOverview() {
  const [overviews, setOverviews] = useState<CorridorRouteSummary[]>(() =>
    overviewCache ? overviewCache.data : []
  );
  const [loading, setLoading] = useState<boolean>(!overviewCache);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    let isMounted = true;
    if (overviewCache && Date.now() - overviewCache.timestamp <= CORRIDOR_ROUTE_CACHE_TTL_MS) {
      setOverviews(overviewCache.data);
      setLoading(false);
      return;
    }

    setLoading(true);
    fetchCorridorsOverview()
      .then((data) => {
        if (!isMounted) return;
        setOverviews(data);
        setLoading(false);
      })
      .catch((err) => {
        if (!isMounted) return;
        setError(err instanceof Error ? err : new Error(String(err)));
        setLoading(false);
      });

    return () => {
      isMounted = false;
    };
  }, []);

  return { overviews, loading, error };
}
