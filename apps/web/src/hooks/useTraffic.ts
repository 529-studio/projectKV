import { useState, useEffect, useCallback } from 'react';
import { API_ENDPOINTS, getApiUrl, HOTSET_CONFIG } from '../config/constants';

export interface TrafficVerdict {
  camera_id: string;
  severity: string;
  confidence: number;
  timestamp: string;
}

/**
 * Validates whether a verdict is non-stale and within the Hot Set active TTL window.
 */
export function isVerdictValid(v?: TrafficVerdict | null): v is TrafficVerdict {
  if (!v || !v.severity) return false;
  if (!v.timestamp) return false;
  const ts = new Date(v.timestamp).getTime();
  if (isNaN(ts)) return false;
  const ageMs = Date.now() - ts;
  // Allow up to 1 minute of forward clock drift and ensure age does not exceed Hot Set TTL.
  return ageMs >= -60000 && ageMs <= HOTSET_CONFIG.VERDICT_TTL_MS;
}

export function useTraffic() {
  const [verdicts, setVerdicts] = useState<Map<string, TrafficVerdict>>(new Map());

  const updateVerdict = useCallback((v: TrafficVerdict) => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    if (!v || (!v.camera_id && !(v as any).cameraId)) return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const id = v.camera_id || (v as any).cameraId;
    if (!isVerdictValid(v)) return;
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
          if (id && isVerdictValid(v)) initialMap.set(id, v);
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

    // Periodically prune verdicts that have exceeded their Hot Set TTL
    const pruneInterval = setInterval(() => {
      setVerdicts((prev) => {
        if (prev.size === 0) return prev;
        let hasExpired = false;
        for (const v of prev.values()) {
          if (!isVerdictValid(v)) {
            hasExpired = true;
            break;
          }
        }
        if (!hasExpired) return prev;
        const next = new Map<string, TrafficVerdict>();
        for (const [id, v] of prev.entries()) {
          if (isVerdictValid(v)) {
            next.set(id, v);
          }
        }
        return next;
      });
    }, 15000);

    return () => {
      clearInterval(pruneInterval);
      if (es) {
        es.close();
      }
    };
  }, [updateVerdict]);

  return { verdicts };
}
