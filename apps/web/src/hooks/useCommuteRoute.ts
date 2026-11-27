/**
 * Hook for Commuter Route Management, Local Storage Persistence & Real-Time Evaluation.
 * Enforces S17-T4 / Issue #512.
 */

'use client';

import { useState, useEffect, useCallback, useMemo } from 'react';
import type { CommuteConfig, CommuteEvaluation, CommutePreset, DetourSuggestion } from '../types/commute.ts';
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

interface RawApiDetour {
  original_corridor_key: string;
  original_corridor_name?: string;
  detour_corridor_key: string;
  detour_corridor_name: string;
  detour_path?: string[];
  detour_path_names?: string[];
  passability_verdict: DetourSuggestion['passability_verdict'];
  max_depth_cm?: number | null;
  delay_seconds: number;
  estimated_delta_seconds?: number;
  is_dry_route?: boolean;
  reason?: string;
}

function adaptApiDetour(raw: RawApiDetour): DetourSuggestion {
  return {
    original_corridor_key: raw.original_corridor_key,
    original_corridor_name: raw.original_corridor_name,
    detour_corridor_key: raw.detour_corridor_key,
    detour_corridor_name: raw.detour_corridor_name,
    detour_path: raw.detour_path,
    detour_path_names: raw.detour_path_names,
    passability_verdict: raw.passability_verdict,
    max_depth_cm: raw.max_depth_cm,
    delay_seconds: raw.delay_seconds,
    estimated_delta_seconds: raw.estimated_delta_seconds,
    is_dry_route: raw.is_dry_route ?? raw.passability_verdict === 'PASSABLE',
    reason: {
      vi: raw.reason || '',
      en: raw.reason || '',
    },
  };
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
            detour: data.detour ? adaptApiDetour(data.detour) : null,
            detours: Array.isArray(data.detours)
              ? data.detours.map(adaptApiDetour)
              : data.detour
                ? [adaptApiDetour(data.detour)]
                : [],
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
