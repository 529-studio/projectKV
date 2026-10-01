'use client';

import { useState, useEffect, useCallback } from 'react';
import { API_ENDPOINTS, getApiUrl } from '../config/constants';
import {
  type TrafficVerdict,
  normalizeVerdict,
  isVerdictValid,
  isMockHidden,
} from '../utils/trafficUtils';
import {
  type FloodHexCell,
  normalizeFloodHexCell,
} from '../utils/floodUtils';

export interface UseFloodReturn {
  floodVerdicts: Map<string, TrafficVerdict>;
  floodHexCells: Map<string, FloodHexCell>;
  floodedCameraIds: Set<string>;
  floodedCameraCount: number;
  floodedHexCount: number;
  loading: boolean;
  connected: boolean;
}

export function useFlood(): UseFloodReturn {
  const [floodVerdicts, setFloodVerdicts] = useState<Map<string, TrafficVerdict>>(new Map());
  const [floodHexCells, setFloodHexCells] = useState<Map<string, FloodHexCell>>(new Map());
  const [loading, setLoading] = useState(true);
  const [connected, setConnected] = useState(false);
  const [appEnv, setAppEnv] = useState<string>('production');

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const updateVerdict = useCallback((raw: any) => {
    const v = normalizeVerdict(raw);
    const id = v.camera_id;
    if (!id || !isVerdictValid(v) || isMockHidden(v.source, appEnv)) {
      return;
    }

    setFloodVerdicts((prev) => {
      // If verdict has no flood severity, don't update flood state
      if (!v.flood_severity) return prev;
      const next = new Map(prev);
      next.set(id, v);
      return next;
    });
  }, [appEnv]);

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const updateFloodHexCell = useCallback((raw: any) => {
    const cell = normalizeFloodHexCell(raw);
    if (!cell) return;

    setFloodHexCells((prev) => {
      const next = new Map(prev);
      if (cell.camera_count <= 0 || cell.flood_severity === 'DRY') {
        next.delete(cell.h3_id);
      } else {
        next.set(cell.h3_id, cell);
      }
      return next;
    });
  }, []);

  useEffect(() => {
    let es: EventSource | null = null;
    let isCancelled = false;

    async function fetchInitial() {
      const startTime = Date.now();
      try {
        // 1. Fetch initial flood status
        const statusPromise = fetch(getApiUrl(API_ENDPOINTS.FLOOD_STATUS))
          .then((res) => (res.ok ? res.json() : null))
          .catch((err) => {
            console.warn('Failed to fetch initial flood status:', err);
            return null;
          });

        // 2. Fetch initial flood hex snapshot
        const hexPromise = fetch(getApiUrl(API_ENDPOINTS.FLOOD_HEX))
          .then((res) => (res.ok ? res.json() : null))
          .catch((err) => {
            console.warn('Failed to fetch initial flood hex cells:', err);
            return null;
          });

        const [statusData, hexData] = await Promise.all([statusPromise, hexPromise]);
        if (isCancelled) return;

        let currentEnv = 'production';
        if (statusData && typeof statusData === 'object') {
          if ('app_env' in statusData && typeof statusData.app_env === 'string') {
            currentEnv = statusData.app_env;
            setAppEnv(statusData.app_env);
          }
          const list = Array.isArray(statusData)
            ? statusData
            : (statusData.verdicts || []);

          const initialVerdicts = new Map<string, TrafficVerdict>();
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          list.forEach((raw: any) => {
            const v = normalizeVerdict(raw);
            const id = v.camera_id;
            if (id && isVerdictValid(v) && !isMockHidden(v.source, currentEnv) && v.flood_severity) {
              initialVerdicts.set(id, v);
            }
          });
          setFloodVerdicts(initialVerdicts);
        }

        if (Array.isArray(hexData)) {
          const initialHex = new Map<string, FloodHexCell>();
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          hexData.forEach((raw: any) => {
            const cell = normalizeFloodHexCell(raw);
            if (cell && cell.camera_count > 0 && cell.flood_severity !== 'DRY') {
              initialHex.set(cell.h3_id, cell);
            }
          });
          setFloodHexCells(initialHex);
        }
      } finally {
        const elapsed = Date.now() - startTime;
        const minDelay = 700;
        if (elapsed < minDelay) {
          await new Promise((resolve) => setTimeout(resolve, minDelay - elapsed));
        }
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
            console.warn('Failed to parse SSE camera_update message for flood:', err);
          }
        });

        es.addEventListener('flood_hex_update', (event: MessageEvent) => {
          setConnected(true);
          try {
            const payload = JSON.parse(event.data);
            if (Array.isArray(payload)) {
              payload.forEach(updateFloodHexCell);
            } else if (payload.data) {
              if (Array.isArray(payload.data)) payload.data.forEach(updateFloodHexCell);
              else updateFloodHexCell(payload.data);
            } else {
              updateFloodHexCell(payload);
            }
          } catch (err) {
            console.warn('Failed to parse SSE flood_hex_update message:', err);
          }
        });

        es.onerror = (err) => {
          setConnected(false);
          if (es?.readyState === EventSource.CLOSED) {
            console.warn('Flood SSE connection closed:', err);
          }
        };
      } catch (err) {
        console.warn('Failed to initialize Flood EventSource:', err);
      }
    }

    fetchInitial().then(() => {
      if (!isCancelled) {
        initSSE();
      }
    });

    return () => {
      isCancelled = true;
      if (es) {
        es.close();
      }
    };
  }, [updateVerdict, updateFloodHexCell]);

  // Derive cameras that have active flood conditions: WET, FLOODED_SHALLOW, FLOODED_DEEP
  const floodedCameraIds = new Set<string>();
  floodVerdicts.forEach((v) => {
    const s = (v.flood_severity || '').toUpperCase();
    if (s === 'FLOODED_DEEP' || s === 'FLOODED_SHALLOW' || s === 'WET') {
      floodedCameraIds.add(v.camera_id);
    }
  });

  return {
    floodVerdicts,
    floodHexCells,
    floodedCameraIds,
    floodedCameraCount: floodedCameraIds.size,
    floodedHexCount: floodHexCells.size,
    loading,
    connected,
  };
}
