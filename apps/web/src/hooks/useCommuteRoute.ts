/**
 * Hook for Commuter Route Management, Local Storage Persistence & Real-Time Evaluation.
 * Enforces S17-T4 / Issue #512.
 */

'use client';

import { useState, useEffect, useCallback, useMemo } from 'react';
import type { CommuteConfig, CommuteEvaluation, CommutePreset } from '../types/commute.ts';
import type { VehicleType } from '../types/vehicle.ts';
import type { WeatherData } from '../utils/weatherUtils.ts';
import type { CorridorRouteSummary } from '../types/corridor.ts';
import {
  getStoredCommuteConfig,
  setStoredCommuteConfig,
  clearStoredCommuteConfig,
  COMMUTE_PRESETS,
} from '../utils/commuteStorage.ts';
import { evaluateCommuteRoute } from '../utils/commuteRouteUtils.ts';
import { getApiUrl, API_ENDPOINTS } from '../config/constants.ts';

interface UseCommuteRouteProps {
  vehicleType: VehicleType;
  weather?: WeatherData | null;
  corridorSummaries?: Record<string, CorridorRouteSummary> | CorridorRouteSummary[];
}

export function useCommuteRoute({
  vehicleType,
  weather,
  corridorSummaries = {},
}: UseCommuteRouteProps) {
  const [config, setConfig] = useState<CommuteConfig | null>(null);
  const [isSetupOpen, setIsSetupOpen] = useState(false);
  const [isDismissed, setIsDismissed] = useState(false);
  const [apiEvaluation, setApiEvaluation] = useState<CommuteEvaluation | null>(null);

  // Initialize from client-side localStorage
  useEffect(() => {
    const stored = getStoredCommuteConfig();
    if (stored) {
      setConfig(stored);
    }
  }, []);

  const saveConfig = useCallback((newConfig: CommuteConfig) => {
    setConfig(newConfig);
    setStoredCommuteConfig(newConfig);
    setIsDismissed(false);
  }, []);

  const clearConfig = useCallback(() => {
    setConfig(null);
    clearStoredCommuteConfig();
    setIsDismissed(true);
    setApiEvaluation(null);
  }, []);

  const toggleDirection = useCallback(() => {
    setConfig(prev => {
      if (!prev) return null;
      const nextDir = prev.direction === 'to_home' ? 'to_work' : 'to_home';
      const updated: CommuteConfig = {
        ...prev,
        direction: nextDir,
      };
      setStoredCommuteConfig(updated);
      return updated;
    });
  }, []);

  const selectPreset = useCallback((preset: CommutePreset) => {
    const newConfig: CommuteConfig = {
      home: preset.home,
      work: preset.work,
      selectedCorridors: preset.corridors,
      direction: 'to_work',
      enabled: true,
    };
    saveConfig(newConfig);
    setIsSetupOpen(false);
  }, [saveConfig]);

  // Query Go backend `/api/corridors/commute-eval` when corridors are selected
  const activeCorridors = useMemo(() => {
    return config?.selectedCorridors || [];
  }, [config]);

  useEffect(() => {
    if (!config || activeCorridors.length === 0) {
      setApiEvaluation(null);
      return;
    }

    let isMounted = true;
    const fetchApiEvaluation = async () => {
      try {
        const query = new URLSearchParams({
          corridors: activeCorridors.join(','),
          vehicle: vehicleType,
        });
        const res = await fetch(`${getApiUrl(API_ENDPOINTS.COMMUTE_EVAL)}?${query.toString()}`);
        if (!res.ok) return;
        const data = await res.json();
        if (isMounted && data) {
          // Adapt backend response to frontend CommuteEvaluation structure
          const adapted: CommuteEvaluation = {
            passability: data.passability_verdict,
            status_color: data.status_color,
            worst_bottleneck: data.worst_bottleneck
              ? {
                  ...data.worst_bottleneck,
                  description: {
                    vi: data.worst_bottleneck.description || '',
                    en: data.worst_bottleneck.description || '',
                  },
                }
              : null,
            total_distance_km: data.total_distance_km,
            free_flow_seconds: data.free_flow_seconds,
            estimated_seconds: data.estimated_seconds,
            delay_seconds: data.delay_seconds,
            rain_probability_pct: weather ? Math.round(weather.precipitation_probability_percent) : 0,
            rain_summary: {
              vi: weather && weather.precipitation_probability_percent >= 60 ? `Khả năng mưa ${Math.round(weather.precipitation_probability_percent)}%` : 'Khô ráo',
              en: weather && weather.precipitation_probability_percent >= 60 ? `Rain chance ${Math.round(weather.precipitation_probability_percent)}%` : 'Dry',
            },
            corridors: activeCorridors,
            corridor_summaries: data.corridors || [],
            detour: data.detour
              ? {
                  ...data.detour,
                  reason: {
                    vi: data.detour.reason || '',
                    en: data.detour.reason || '',
                  },
                }
              : null,
            vehicle: vehicleType,
          };
          setApiEvaluation(adapted);
        }
      } catch {
        // Fall back to client-side evaluation
      }
    };

    fetchApiEvaluation();

    return () => {
      isMounted = false;
    };
  }, [config, activeCorridors, vehicleType, weather]);

  // Client-side instant evaluation fallback
  const clientEvaluation = useMemo(() => {
    if (!config || activeCorridors.length === 0) return null;
    return evaluateCommuteRoute({
      corridorKeys: activeCorridors,
      vehicleType,
      corridorSummaries,
      weather,
    });
  }, [config, activeCorridors, vehicleType, corridorSummaries, weather]);

  const evaluation = apiEvaluation || clientEvaluation;

  return {
    config,
    evaluation,
    isSetupOpen,
    setIsSetupOpen,
    isDismissed,
    setIsDismissed,
    saveConfig,
    clearConfig,
    toggleDirection,
    selectPreset,
    presets: COMMUTE_PRESETS,
  };
}
