import { useState, useEffect, useCallback } from 'react';
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
  isWaterway?: boolean;
  phuong_id?: string | null;
  phuong_type?: string | null;
  phuong_name?: string | null;
  road_type?: string | null;
  road_name?: string | null;
  road_key?: string | null;
  road_keys?: string[];
  status?: string;
  h3_id?: string | null;
  h3_res7?: string | null;
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
        const parsed = list.map((c: any) => {
          const isWaterway = Boolean(c.isWaterway || c.status === 'WATERWAY_MONITOR');
          return {
            ...c,
            id: c.id || c._id,
            duplicateOf: c.duplicateOf,
            isGhost: Boolean(c.isGhost),
            isWaterway,
            isOnline: (c.status === 'UP' || c.isOnline === true) && !c.isGhost && !isWaterway,
            phuong_id: c.phuong_id ?? null,
            phuong_type: c.phuong_type ?? null,
            phuong_name: c.phuong_name ?? null,
            road_type: c.road_type ?? null,
            road_name: c.road_name ?? null,
            road_key: c.road_key ?? null,
            road_keys: Array.isArray(c.road_keys) ? c.road_keys : [],
          };
        });
        setCameras(parsed);
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)));
      } finally {
        setLoading(false);
      }
    }
    fetchCameras();
  }, []);

  // markCameraGhost flips a camera to the runtime "no field signal" state after
  // the snapshot proxy reports a GHOST_CAMERA (503) response. This keeps the
  // map, sidebar, dock and modal consistent without waiting for a catalog resync.
  const markCameraGhost = useCallback((id: string) => {
    setCameras(prev =>
      prev.map(c => (c.id === id ? { ...c, isGhost: true, isOnline: false } : c))
    );
  }, []);

  return { cameras, loading, error, markCameraGhost };
}
