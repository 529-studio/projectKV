import { useState, useEffect } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants';

export interface PhuongItem {
  type: string;
  name: string;
}

export function usePhuongs() {
  const [phuongs, setPhuongs] = useState<PhuongItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    async function fetchPhuongs() {
      try {
        const res = await fetch(getApiUrl(API_ENDPOINTS.PHUONG));
        if (!res.ok) throw new Error('Failed to fetch phuongs');
        const data = await res.json();
        const list = Array.isArray(data) ? data : [];
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const normalized: PhuongItem[] = list.map((item: any) => {
          if (typeof item === 'string') {
            return { type: 'Phường', name: item };
          }
          return {
            type: item.type || 'Phường',
            name: item.name || '',
          };
        });
        setPhuongs(normalized);
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
