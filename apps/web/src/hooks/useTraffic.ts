import { useState, useEffect, useCallback } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants';

export interface TrafficVerdict {
  camera_id: string;
  severity: string;
  confidence: number;
  timestamp: string;
}

export function useTraffic() {
  const [verdicts, setVerdicts] = useState<Map<string, TrafficVerdict>>(new Map());

  const updateVerdict = useCallback((v: TrafficVerdict) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    if (!v || (!v.camera_id && !(v as any).cameraId)) return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const id = v.camera_id || (v as any).cameraId;
    setVerdicts((prev) => {
      const next = new Map(prev);
      next.set(id, v);
      return next;
    });
  }, []);

  useEffect(() => {
    let es: EventSource | null = null;

    async function fetchInitial() {
      try {
        const res = await fetch(getApiUrl(API_ENDPOINTS.TRAFFIC_STATUS));
        if (!res.ok) return;
        const data = await res.json();
        const list = Array.isArray(data) ? data : (data.verdicts || []);
        const initialMap = new Map<string, TrafficVerdict>();
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        list.forEach((v: any) => {
          const id = v.camera_id || v.cameraId;
          if (id) initialMap.set(id, v);
        });
        setVerdicts(initialMap);
      } catch (err) {
        console.warn('Failed to fetch initial traffic status:', err);
      }
    }

    function initSSE() {
      if (typeof window === 'undefined' || typeof EventSource === 'undefined') {
        return;
      }

      try {
        es = new EventSource(getApiUrl(API_ENDPOINTS.SSE_LIVE));

        es.addEventListener('camera_update', (event: MessageEvent) => {
          try {
            const payload = JSON.parse(event.data);
            if (Array.isArray(payload)) {
              payload.forEach(updateVerdict);
            } else if (payload.data) {
              if (Array.isArray(payload.data)) payload.data.forEach(updateVerdict);
              else updateVerdict(payload.data);
            } else {
              updateVerdict(payload);
            }
          } catch (err) {
            console.warn('Failed to parse SSE camera_update message:', err);
          }
        });

        es.onerror = (err) => {
          // EventSource automatically attempts to reconnect on transient errors per the SSE spec.
          if (es?.readyState === EventSource.CLOSED) {
            console.warn('SSE connection closed:', err);
          }
        };
      } catch (err) {
        console.warn('Failed to initialize EventSource:', err);
      }
    }

    fetchInitial().then(() => initSSE());

    return () => {
      if (es) {
        es.close();
      }
    };
  }, [updateVerdict]);

  return { verdicts };
}
