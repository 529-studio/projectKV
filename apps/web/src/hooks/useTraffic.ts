import { useState, useEffect, useCallback } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants';
import {
  type TrafficVerdict,
  isVerdictValid,
  isVerdictStale,
  isMockHidden,
  normalizeVerdict,
  getSeverityLabel,
} from '../utils/trafficUtils';
import { type HexCell, normalizeHexCell } from '../utils/hexUtils';

// Re-export pure utilities so existing imports of isVerdictValid from this module still work.
export type { TrafficVerdict, HexCell };
export { isVerdictValid, isVerdictStale, isMockHidden, normalizeVerdict, getSeverityLabel, normalizeHexCell };

export function useTraffic() {
  const [verdicts, setVerdicts] = useState<Map<string, TrafficVerdict>>(new Map());
  const [hexCells, setHexCells] = useState<Map<string, HexCell>>(new Map());
  const [analysisEnabled, setAnalysisEnabled] = useState(false);
  const [appEnv, setAppEnv] = useState<string>('production');
  const [loading, setLoading] = useState(true);
  const [connected, setConnected] = useState(false);

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const updateVerdict = useCallback((raw: any) => {
    if (!raw || (!raw.camera_id && !raw.cameraId)) return;
    const v = normalizeVerdict(raw);
    const id = v.camera_id;
    if (!isVerdictValid(v)) return;
    if (isMockHidden(v.source, appEnv)) return;
    setVerdicts((prev) => {
      const next = new Map(prev);
      next.set(id, v);
      return next;
    });
  }, [appEnv]);

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const updateHexCell = useCallback((raw: any) => {
    const cell = normalizeHexCell(raw);
    if (!cell) return;
    setHexCells((prev) => {
      const next = new Map(prev);
      next.set(cell.h3_id, cell);
      return next;
    });
  }, []);

  useEffect(() => {
    let es: EventSource | null = null;
    let isCancelled = false;

    async function fetchInitial() {
      try {
        const res = await fetch(getApiUrl(API_ENDPOINTS.TRAFFIC_STATUS));
        if (!res.ok) return;
        const data = await res.json();
        if (isCancelled) return;

        let currentEnv = 'production';
        if (typeof data === 'object' && data !== null) {
          if ('analysis_enabled' in data) {
            setAnalysisEnabled(Boolean(data.analysis_enabled));
          }
          if ('app_env' in data && typeof data.app_env === 'string') {
            currentEnv = data.app_env;
            setAppEnv(data.app_env);
          }
        }

        const list = Array.isArray(data) ? data : (data.verdicts || []);
        const initialMap = new Map<string, TrafficVerdict>();
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        list.forEach((raw: any) => {
          const v = normalizeVerdict(raw);
          const id = v.camera_id;
          if (id && isVerdictValid(v) && !isMockHidden(v.source, currentEnv)) {
            initialMap.set(id, v);
          }
        });
        setVerdicts(initialMap);
      } catch (err) {
        console.warn('Failed to fetch initial traffic status:', err);
      } finally {
        if (!isCancelled) {
          setLoading(false);
        }
      }
    }

    function initSSE() {
      if (typeof window === 'undefined' || typeof EventSource === 'undefined') {
        return;
      }

      try {
        es = new EventSource(getApiUrl(API_ENDPOINTS.SSE_LIVE));

        es.onopen = () => {
          setConnected(true);
        };

        es.addEventListener('camera_update', (event: MessageEvent) => {
          setConnected(true);
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

        es.addEventListener('hex_update', (event: MessageEvent) => {
          setConnected(true);
          try {
            const payload = JSON.parse(event.data);
            if (Array.isArray(payload)) {
              payload.forEach(updateHexCell);
            } else if (payload.data) {
              if (Array.isArray(payload.data)) payload.data.forEach(updateHexCell);
              else updateHexCell(payload.data);
            } else {
              updateHexCell(payload);
            }
          } catch (err) {
            console.warn('Failed to parse SSE hex_update message:', err);
          }
        });

        es.onerror = (err) => {
          setConnected(false);
          // EventSource automatically attempts to reconnect on transient errors per the SSE spec.
          if (es?.readyState === EventSource.CLOSED) {
            console.warn('SSE connection closed:', err);
          }
        };
      } catch (err) {
        console.warn('Failed to initialize EventSource:', err);
      }
    }

    fetchInitial().then(() => {
      if (!isCancelled) {
        initSSE();
      }
    });

    // Periodically update staleness and prune expired verdicts
    const pruneInterval = setInterval(() => {
      setVerdicts((prev) => {
        if (prev.size === 0) return prev;
        let changed = false;
        const next = new Map<string, TrafficVerdict>();
        for (const [id, v] of prev.entries()) {
          if (!isVerdictValid(v) || isMockHidden(v.source, appEnv)) {
            changed = true;
            continue;
          }
          const stale = isVerdictStale(v);
          if (stale !== v.stale) {
            changed = true;
            next.set(id, { ...v, stale });
          } else {
            next.set(id, v);
          }
        }
        return changed ? next : prev;
      });
    }, 15000);

    return () => {
      isCancelled = true;
      clearInterval(pruneInterval);
      if (es) {
        es.close();
      }
    };
  }, [updateVerdict, updateHexCell, appEnv]);

  return { verdicts, hexCells, analysisEnabled, appEnv, loading, connected };
}
