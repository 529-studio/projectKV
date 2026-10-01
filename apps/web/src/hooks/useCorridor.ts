'use client';

import { useState, useEffect } from 'react';
import {
  fetchCorridorStatus,
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
