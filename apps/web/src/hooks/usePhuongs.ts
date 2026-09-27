import { useState, useEffect } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants';

export function usePhuongs() {
  const [phuongs, setPhuongs] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    async function fetchPhuongs() {
      try {
        const res = await fetch(getApiUrl(API_ENDPOINTS.PHUONG));
        if (!res.ok) throw new Error('Failed to fetch phuongs');
        const data = await res.json();
        setPhuongs(Array.isArray(data) ? data : []);
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setLoading(false);
      }
    }
    fetchPhuongs();
  }, []);

  return { phuongs, loading, error };
}
