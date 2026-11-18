'use client';

import { useState, useEffect } from 'react';
import {
  fetchCorridorStatus,
  fetchCorridorStatusSummary,
  getCachedCorridorStatus,
  type CorridorStatus,
  type CameraVerdict,
} from '../utils/corridorUtils';

export type { CorridorStatus, CameraVerdict };

/**
 * React hook for on-demand corridor status querying with 30s cache.
 */
export function useCorridor(roadKey: string | null) {
  const [status, setStatus] = useState<CorridorStatus | null>(() =>
    roadKey ? getCachedCorridorStatus(roadKey) : null
  );
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    // If we're not filtering by a specific road, we might want the summary
    if (!roadKey) {
      setStatus(null);
      setLoading(false);
      setError(null);
      return;
    }

    const cached = getCachedCorridorStatus(roadKey);
    if (cached) {
      setStatus(cached);
      setLoading(false);
      setError(null);
      return;
    }

    let active = true;
    setLoading(true);
    setError(null);

    fetchCorridorStatus(roadKey)
      .then((data) => {
        if (active) {
          setStatus(data);
          setLoading(false);
        }
      })
      .catch((err) => {
        if (active) {
          setError(err instanceof Error ? err : new Error(String(err)));
          setLoading(false);
        }
      });

    return () => {
      active = false;
    };
  }, [roadKey]);

  return { status, loading, error };
}

/**
 * Hook to fetch all corridor statuses in one batch.
 */
export function useCorridorSummary() {
  const [data, setData] = useState<Record<string, CorridorStatus> | null>(null);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);

    fetchCorridorStatusSummary()
      .then((summary) => {
        if (active) {
          setData(summary);
          setLoading(false);
        }
      })
      .catch((err) => {
        if (active) {
          setError(err instanceof Error ? err : new Error(String(err)));
          setLoading(false);
        }
      });

    return () => {
      active = false;
    };
  }, []);

  return { data, loading, error };
}
