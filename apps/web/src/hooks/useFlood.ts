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
  type UserFloodReport,
  normalizeFloodHexCell,
  deriveFloodCameraGroups,
  deriveFloodHexCounts,
  isUserReportActive,
} from '../utils/floodUtils';

export interface UseFloodReturn {
  floodVerdicts: Map<string, TrafficVerdict>;
  floodHexCells: Map<string, FloodHexCell>;
  userFloodReports: Map<string, UserFloodReport>;
  userFloodReportCount: number;
  floodedCameraIds: Set<string>;
  floodedCameraCount: number;
  wetCameraIds: Set<string>;
  wetCameraCount: number;
  dryCameraIds: Set<string>;
  dryCameraCount: number;
  floodedHexCount: number;
  wetHexCount: number;
  totalActiveHexCount: number;
  loading: boolean;
  connected: boolean;
}

export function useFlood(): UseFloodReturn {
  const [floodVerdicts, setFloodVerdicts] = useState<Map<string, TrafficVerdict>>(new Map());
  const [floodHexCells, setFloodHexCells] = useState<Map<string, FloodHexCell>>(new Map());
  const [userFloodReports, setUserFloodReports] = useState<Map<string, UserFloodReport>>(new Map());
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

        // 3. Fetch initial crowdsourced user flood reports (S8-T7)
        const userReportsPromise = fetch(getApiUrl(API_ENDPOINTS.FLOOD_USER_REPORTS))
          .then((res) => (res.ok ? res.json() : null))
          .catch((err) => {
            console.warn('Failed to fetch initial user flood reports:', err);
            return null;
          });

        const [statusData, hexData, userReportsData] = await Promise.all([statusPromise, hexPromise, userReportsPromise]);
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

        if (Array.isArray(userReportsData)) {
          const initialUserReports = new Map<string, UserFloodReport>();
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          userReportsData.forEach((item: any) => {
            if (item && item.camera_id) {
              const expiresAt = item.expires_at ? new Date(item.expires_at).getTime() : Date.now() + 900000;
              if (Date.now() < expiresAt) {
                initialUserReports.set(item.camera_id, {
                  camera_id: item.camera_id,
                  client_ip_hash: item.client_ip_hash,
                  timestamp: item.latest_at || item.timestamp || new Date().toISOString(),
                  report_count: item.report_count || 1,
                  escalated: Boolean(item.escalated),
                  ttl_seconds: 900,
                  expires_at: expiresAt,
                });
              }
            }
          });
          setUserFloodReports(initialUserReports);
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

        // S8-T7: Crowdsourced user flood report SSE broadcast listener
        es.addEventListener('user_flood_report', (event: MessageEvent) => {
          setConnected(true);
          try {
            const payload = JSON.parse(event.data);
            const id = payload.camera_id;
            if (!id) return;
            const ttlSec = payload.ttl_seconds || 900;
            const expiresAt = Date.now() + ttlSec * 1000;
            const report: UserFloodReport = {
              camera_id: id,
              client_ip_hash: payload.client_ip_hash,
              timestamp: payload.timestamp || new Date().toISOString(),
              report_count: payload.report_count || 1,
              escalated: Boolean(payload.escalated),
              ttl_seconds: ttlSec,
              expires_at: expiresAt,
            };
            setUserFloodReports((prev) => {
              const next = new Map(prev);
              next.set(id, report);
              return next;
            });
          } catch (err) {
            console.warn('Failed to parse SSE user_flood_report message:', err);
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

  // Periodic pruning of expired user flood reports (sliding 15-minute TTL)
  useEffect(() => {
    const pruneTimer = setInterval(() => {
      const now = Date.now();
      setUserFloodReports((prev) => {
        let hasExpired = false;
        for (const report of prev.values()) {
          if (!isUserReportActive(report, now)) {
            hasExpired = true;
            break;
          }
        }
        if (!hasExpired) return prev;
        const next = new Map<string, UserFloodReport>();
        for (const [id, report] of prev.entries()) {
          if (isUserReportActive(report, now)) {
            next.set(id, report);
          }
        }
        return next;
      });
    }, 15000);

    return () => clearInterval(pruneTimer);
  }, []);

  // Derive cameras with active conditions (strictly excluding stale verdicts > 3 min)
  const { floodedCameraIds, wetCameraIds, dryCameraIds } = deriveFloodCameraGroups(floodVerdicts);
  const { floodedHexCount, wetHexCount, totalHexCount } = deriveFloodHexCounts(floodHexCells);

  return {
    floodVerdicts,
    floodHexCells,
    userFloodReports,
    userFloodReportCount: userFloodReports.size,
    floodedCameraIds,
    floodedCameraCount: floodedCameraIds.size,
    wetCameraIds,
    wetCameraCount: wetCameraIds.size,
    dryCameraIds,
    dryCameraCount: dryCameraIds.size,
    floodedHexCount,
    wetHexCount,
    totalActiveHexCount: totalHexCount,
    loading,
    connected,
  };
}
