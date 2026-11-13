'use client';

import { useState, useEffect, useCallback, useRef } from 'react';
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
import {
  useLiveStream,
  useCameraUpdate,
  useFloodHexUpdate,
  useUserFloodReport,
} from '../contexts/LiveStreamContext.ts';

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
  const { connected } = useLiveStream();

  const appEnvRef = useRef<string>('production');

  const updateVerdict = useCallback((raw: unknown) => {
    if (!raw || typeof raw !== 'object') return;
    const v = normalizeVerdict(raw);
    const id = v.camera_id;
    if (!id || !isVerdictValid(v) || isMockHidden(v.source, appEnvRef.current)) {
      return;
    }

    setFloodVerdicts((prev) => {
      // If verdict has no flood severity, don't update flood state
      if (!v.flood_severity) return prev;
      const next = new Map(prev);
      next.set(id, v);
      return next;
    });
  }, []);

  const updateFloodHexCell = useCallback((raw: unknown) => {
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

  // Subscribe to consolidated live SSE stream
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

  useFloodHexUpdate(
    useCallback(
      (payload: unknown) => {
        if (Array.isArray(payload)) {
          payload.forEach(updateFloodHexCell);
        } else if (payload && typeof payload === 'object' && 'data' in payload) {
          const innerData = payload.data;
          if (Array.isArray(innerData)) innerData.forEach(updateFloodHexCell);
          else updateFloodHexCell(innerData);
        } else if (payload) {
          updateFloodHexCell(payload);
        }
      },
      [updateFloodHexCell]
    )
  );

  useUserFloodReport(
    useCallback((payload: unknown) => {
      if (!payload || typeof payload !== 'object') return;
      if (!('camera_id' in payload) || typeof payload.camera_id !== 'string') return;
      const id = payload.camera_id;
      const ttlSec = ('ttl_seconds' in payload && typeof payload.ttl_seconds === 'number')
        ? payload.ttl_seconds
        : 900;
      const expiresAt = Date.now() + ttlSec * 1000;
      const clientIpHash = ('client_ip_hash' in payload && typeof payload.client_ip_hash === 'string')
        ? payload.client_ip_hash
        : undefined;
      const timestamp = ('timestamp' in payload && typeof payload.timestamp === 'string')
        ? payload.timestamp
        : new Date().toISOString();
      const reportCount = ('report_count' in payload && typeof payload.report_count === 'number')
        ? payload.report_count
        : 1;
      const escalated = ('escalated' in payload && Boolean(payload.escalated)) || false;

      const report: UserFloodReport = {
        camera_id: id,
        client_ip_hash: clientIpHash,
        timestamp,
        report_count: reportCount,
        escalated,
        ttl_seconds: ttlSec,
        expires_at: expiresAt,
      };
      setUserFloodReports((prev) => {
        const next = new Map(prev);
        next.set(id, report);
        return next;
      });
    }, [])
  );

  useEffect(() => {
    let isCancelled = false;

    async function fetchInitial() {
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

        const [statusData, hexData, userReportsData] = await Promise.all([
          statusPromise,
          hexPromise,
          userReportsPromise,
        ]);
        if (isCancelled) return;

        let currentEnv = 'production';
        if (statusData && typeof statusData === 'object') {
          if ('app_env' in statusData && typeof statusData.app_env === 'string') {
            currentEnv = statusData.app_env;
            appEnvRef.current = statusData.app_env;
          }
          const list = Array.isArray(statusData)
            ? statusData
            : (statusData.verdicts || []);

          const initialVerdicts = new Map<string, TrafficVerdict>();
          list.forEach((raw: unknown) => {
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
          hexData.forEach((raw: unknown) => {
            const cell = normalizeFloodHexCell(raw);
            if (cell && cell.camera_count > 0 && cell.flood_severity !== 'DRY') {
              initialHex.set(cell.h3_id, cell);
            }
          });
          setFloodHexCells(initialHex);
        }

        if (Array.isArray(userReportsData)) {
          const initialUserReports = new Map<string, UserFloodReport>();
          userReportsData.forEach((item: unknown) => {
            if (item && typeof item === 'object' && 'camera_id' in item && typeof item.camera_id === 'string') {
              const expiresAt = ('expires_at' in item && typeof item.expires_at === 'string')
                ? new Date(item.expires_at).getTime()
                : Date.now() + 900000;
              if (Date.now() < expiresAt) {
                const clientIpHash = ('client_ip_hash' in item && typeof item.client_ip_hash === 'string')
                  ? item.client_ip_hash
                  : undefined;
                const timestamp = ('latest_at' in item && typeof item.latest_at === 'string')
                  ? item.latest_at
                  : ('timestamp' in item && typeof item.timestamp === 'string')
                    ? item.timestamp
                    : new Date().toISOString();
                const reportCount = ('report_count' in item && typeof item.report_count === 'number')
                  ? item.report_count
                  : 1;
                const escalated = Boolean('escalated' in item && item.escalated);

                initialUserReports.set(item.camera_id, {
                  camera_id: item.camera_id,
                  client_ip_hash: clientIpHash,
                  timestamp,
                  report_count: reportCount,
                  escalated,
                  ttl_seconds: 900,
                  expires_at: expiresAt,
                });
              }
            }
          });
          setUserFloodReports(initialUserReports);
        }
      } finally {
        // S14-T3 / Issue #443: the artificial 700 ms floor was removed. The map
        // must paint the first flood state as soon as the three requests settle;
        // a deliberate delay on an emergency surface is a defect, not polish.
        if (!isCancelled) {
          setLoading(false);
        }
      }
    }

    fetchInitial();

    return () => {
      isCancelled = true;
    };
  }, []);

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
    }, 30000);

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
