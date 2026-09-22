import { useState, useEffect } from 'react';
import { API_ENDPOINTS } from '../config/constants';

export interface Camera {
  id: string;
  title?: string;
  displayName?: string;
  zone?: string;
  latitude?: number;
  longitude?: number;
  isOnline: boolean;
}

export function useCameras() {
  const [cameras, setCameras] = useState<Camera[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    async function fetchCameras() {
      try {
        const res = await fetch(`http://localhost:3000${API_ENDPOINTS.CAMERAS}`);
        if (!res.ok) throw new Error('Failed to fetch cameras');
        const data = await res.json();
        const list = Array.isArray(data) ? data : (data.cameras || []);
        
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const parsed = list.map((c: any) => ({
          ...c,
          id: c.id || c._id,
          isOnline: c.status === 'UP' || c.isOnline === true,
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
