import { useState, useEffect } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants';

export interface Camera {
  id: string;
  title?: string;
  displayName?: string;
  zone?: string;
  latitude?: number;
  longitude?: number;
  isOnline: boolean;
  duplicateOf?: string;
  isGhost?: boolean;
  phuong_id?: string | null;
  phuong_type?: string | null;
  phuong_name?: string | null;
  road_type?: string | null;
  road_name?: string | null;
}

export function useCameras() {
  const [cameras, setCameras] = useState<Camera[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    async function fetchCameras() {
      try {
        const res = await fetch(getApiUrl(API_ENDPOINTS.CAMERAS));
        if (!res.ok) throw new Error('Failed to fetch cameras');
        const data = await res.json();
        const list = Array.isArray(data) ? data : (data.cameras || []);
        
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const parsed = list.map((c: any) => ({
          ...c,
          id: c.id || c._id,
          duplicateOf: c.duplicateOf,
          isGhost: Boolean(c.isGhost),
          isOnline: (c.status === 'UP' || c.isOnline === true) && !c.isGhost,
          phuong_id: c.phuong_id ?? null,
          phuong_type: c.phuong_type ?? null,
          phuong_name: c.phuong_name ?? null,
          road_type: c.road_type ?? null,
          road_name: c.road_name ?? null,
        }));
        setCameras(parsed);
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setLoading(false);
      }
    }
    fetchCameras();
  }, []);

  return { cameras, loading, error };
}
