/**
 * forecast.ts - DTO Types for Short-Horizon Congestion Forecasting (ADR-012 §D5, S10-T6)
 * Strictly matches Go backend contract from services/go-server/internal/forecast/model.go
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
