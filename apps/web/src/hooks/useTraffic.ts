import { useState, useEffect, useCallback, useRef } from 'react';
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
import {
  useLiveStream,
  useCameraUpdate,
  useHexUpdate,
} from '../contexts/LiveStreamContext.ts';

// Re-export pure utilities so existing imports of isVerdictValid from this module still work.
export type { TrafficVerdict, HexCell };
export { isVerdictValid, isVerdictStale, isMockHidden, normalizeVerdict, getSeverityLabel, normalizeHexCell };

export function useTraffic() {
  const [verdicts, setVerdicts] = useState<Map<string, TrafficVerdict>>(new Map());
  const [hexCells, setHexCells] = useState<Map<string, HexCell>>(new Map());
  const [analysisEnabled, setAnalysisEnabled] = useState(false);
  const [appEnv, setAppEnv] = useState<string>('production');
  const [loading, setLoading] = useState(true);
  const { connected } = useLiveStream();

  const appEnvRef = useRef<string>('production');

  const updateVerdict = useCallback((raw: unknown) => {
    if (!raw || typeof raw !== 'object') return;
    const v = normalizeVerdict(raw);
    const id = v.camera_id;
    if (!id || !isVerdictValid(v)) return;
    if (isMockHidden(v.source, appEnvRef.current)) return;
    setVerdicts((prev) => {
      const existing = prev.get(id);
      if (
        existing &&
        existing.observed_at !== undefined &&
        v.observed_at !== undefined &&
        existing.observed_at >= v.observed_at
      ) {
        return prev;
      }
      const next = new Map(prev);
      next.set(id, v);
      return next;
    });
  }, []);

  const updateHexCell = useCallback((raw: unknown) => {
    const cell = normalizeHexCell(raw);
    if (!cell) return;
    setHexCells((prev) => {
      const next = new Map(prev);
      next.set(cell.h3_id, cell);
      return next;
    });
  }, []);

  useCameraUpdate(
    useCallback(
      (payload: unknown) => {
        if (Array.isArray(payload)) {
          payload.forEach(updateVerdict);
        } else if (payload && typeof payload === 'object' && 'data' in payload) {
          const innerData = payload.data;
          if (Array.isArray(innerData)) innerData.forEach(updateVerdict);
          else updateVerdict(innerData);
        } else if (payload) {
          updateVerdict(payload);
        }
      },
      [updateVerdict]
    )
  );

  useHexUpdate(
    useCallback(
      (payload: unknown) => {
        if (Array.isArray(payload)) {
          payload.forEach(updateHexCell);
        } else if (payload && typeof payload === 'object' && 'data' in payload) {
          const innerData = payload.data;
          if (Array.isArray(innerData)) innerData.forEach(updateHexCell);
          else updateHexCell(innerData);
        } else if (payload) {
          updateHexCell(payload);
        }
      },
      [updateHexCell]
    )
  );

  useEffect(() => {
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
            appEnvRef.current = data.app_env;
            setAppEnv(data.app_env);
          }
        }

        const list = Array.isArray(data) ? data : (data.verdicts || []);
        const initialMap = new Map<string, TrafficVerdict>();
        list.forEach((raw: unknown) => {
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

    fetchInitial();

    // Periodically update staleness and prune expired verdicts
    const pruneInterval = setInterval(() => {
      setVerdicts((prev) => {
        if (prev.size === 0) return prev;
        let changed = false;
        const next = new Map<string, TrafficVerdict>();
        for (const [id, v] of prev.entries()) {
          if (!isVerdictValid(v) || isMockHidden(v.source, appEnvRef.current)) {
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
    };
  }, []);

  return { verdicts, hexCells, analysisEnabled, appEnv, loading, connected };
}
