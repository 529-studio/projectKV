/**
 * forecast.ts - DTO Types for Congestion and Hourly Tidal/Rain Forecasting (ADR-012 §D5, S10-T6, S17-T2)
 * Strictly matches Go backend contract from services/go-server/internal/forecast/ and /internal/tide/
 */

export type AvailabilityStatus =
  | 'ok'
  | 'insufficient_history'
  | 'model_unvalidated'
  | 'stale';

export type FitSource = 'live_history' | 'synthetic_fixture';

export interface Interval {
  lower_95: number;
  upper_95: number;
}

export interface ConfidenceIntervals {
  saturation_index: Interval;
  speed_kmh: Interval;
}

export interface CurrentState {
  observed_at: string;
  saturation_index: number;
  severity: string;
  speed_kmh: number;
  confidence: number;
}

export interface HorizonPrediction {
  horizon_minutes: number;
  target_time: string;
  predicted_saturation_index: number;
  predicted_severity: string;
  predicted_speed_kmh: number;
  confidence: number;
  confidence_intervals: ConfidenceIntervals;
}

export interface FitWindow {
  start: string;
  end: string;
}

export interface HorizonMetric {
  horizon_minutes: number;
  mae: number;
  rmse: number;
  sample_count: number;
}

export interface ModelProvenance {
  name: string;
  version: string;
  target_mode: string;
  fit_source: FitSource;
  validated: boolean;
  fit_rows: number;
  fit_cameras: number;
  fit_window: FitWindow | null;
  fit_data_sha256: string;
  artifact_sha256: string;
  mae: number;
  metrics: HorizonMetric[];
}

export interface SourceData {
  sample_count: number;
  window_minutes: number;
  last_observed_at: string;
  age_minutes: number;
  stale: boolean;
}

export interface ForecastMultiResponse {
  camera_id: string;
  road_key: string;
  generated_at: string;
  base_time: string;
  availability: AvailabilityStatus;
  experimental: boolean;
  current: CurrentState;
  horizons: HorizonPrediction[];
  model: ModelProvenance;
  source_data: SourceData;
}

export interface ForecastRequirements {
  min_observations: number;
  observed_samples: number;
  required_window_minutes: number;
}

export interface ForecastErrorResponse {
  error: string;
  message: string;
  availability?: AvailabilityStatus;
  requirements?: ForecastRequirements;
  model?: ModelProvenance;
}

export type ForecastResult =
  | { success: true; data: ForecastMultiResponse }
  | { success: false; error: ForecastErrorResponse; status: number };

// ============================================================================
// S17-T2 / Issue #511: Hourly Tidal Surge & Convective Rain Forecast Types
// ============================================================================

export interface AffectedRoad {
  road_key: string;
  display_name: string;
  ward_district: string;
  estimated_depth_cm: number;
  hazard_level: 'CAUTION' | 'IMPASSABLE';
  hazard_label: {
    vi: string;
    en: string;
  };
}

export interface HourlyForecastItem {
  hour_offset: number;
  time: string;
  time_formatted: string;
  rain_probability_percent: number;
  rain_mm: number;
  weather_desc: {
    vi: string;
    en: string;
  };
  tide_level_phu_an_m: number;
  tide_level_nha_be_m: number;
  tide_alert_level: 'NORMAL' | 'LEVEL_1' | 'LEVEL_2' | 'LEVEL_3';
  tide_alert_label: {
    vi: string;
    en: string;
  };
  affected_roads: AffectedRoad[];
  commuter_risk: 'CLEAR' | 'CAUTION' | 'DANGER';
  commuter_risk_label: {
    vi: string;
    en: string;
  };
}

export interface CommuterAdvisory {
  peak_tide_time_window: string;
  peak_tide_level_m: number;
  peak_tide_station: string;
  peak_rain_time: string;
  peak_rain_mm: number;
  headline: {
    vi: string;
    en: string;
  };
  guidance: {
    vi: string;
    en: string;
  };
}

export interface HourlyForecastResponse {
  generated_at: string;
  advisory: CommuterAdvisory;
  timeline: HourlyForecastItem[];
}
