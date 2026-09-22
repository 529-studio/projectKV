import { useState, useEffect, useCallback } from 'react';
import { API_ENDPOINTS } from '../config/constants';

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
    let ws: WebSocket;
    let reconnectTimeout: NodeJS.Timeout;

    async function fetchInitial() {
      try {
        const res = await fetch(`http://localhost:3000${API_ENDPOINTS.TRAFFIC_STATUS}`);
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

    function initWs() {
      ws = new WebSocket(`ws://localhost:3000${API_ENDPOINTS.WS_LIVE}`);
      ws.onmessage = (event) => {
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
          console.warn('Failed to parse WS message:', err);
        }
      };
      ws.onclose = () => {
        reconnectTimeout = setTimeout(initWs, 5000);
      };
    }

    fetchInitial().then(() => initWs());

    return () => {
      if (ws) ws.close();
      clearTimeout(reconnectTimeout);
    };
  }, [updateVerdict]);

  return { verdicts };
}
