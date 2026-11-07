/**
 * forecastUtils.ts - Pure geometric & formatting utilities for ForecastTimeline (S10-T6)
 * Zero external runtime dependencies: enables clean Node test execution and Next.js compilation.
 */

import type { AvailabilityStatus, HorizonPrediction, ForecastMultiResponse } from '../types/forecast';

export interface TimelinePoint {
  index: number;
  label: string;
  minutes: number;
  si: number;
  speedKmh: number;
  severity: string;
  confidence: number;
  siLower95: number;
  siUpper95: number;
  x: number;
  y: number;
  yUpper: number;
  yLower: number;
}

export interface SvgDimensions {
  viewWidth: number;
  viewHeight: number;
  padding: {
    top: number;
    right: number;
    bottom: number;
    left: number;
  };
}

export const DEFAULT_SVG_DIMENSIONS: SvgDimensions = {
  viewWidth: 420,
  viewHeight: 140,
  padding: {
    top: 20,
    right: 32,
    bottom: 28,
    left: 44,
  },
};

/**
 * Validates that the parsed JSON conforms to ForecastMultiResponse structure.
 */
export function isValidForecastResponse(data: unknown): data is ForecastMultiResponse {
  if (!data || typeof data !== 'object') return false;
  const obj = data as Partial<ForecastMultiResponse>;

  if (typeof obj.camera_id !== 'string') return false;
  if (typeof obj.availability !== 'string') return false;
  if (!['ok', 'insufficient_history', 'model_unvalidated', 'stale'].includes(obj.availability)) {
    return false;
  }
  if (!Array.isArray(obj.horizons)) return false;
  if (!obj.current || typeof obj.current !== 'object') return false;
  if (!obj.model || typeof obj.model !== 'object') return false;

  return true;
}

/**
 * Clamps a number between min and max.
 */
export function clamp(val: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, val));
}

/**
 * Converts a Saturation Index [0.0 - 1.0] to an SVG Y coordinate.
 */
export function siToSvgY(si: number, dims: SvgDimensions = DEFAULT_SVG_DIMENSIONS): number {
  const { viewHeight, padding } = dims;
  const plotHeight = viewHeight - padding.top - padding.bottom;
  const clampedSi = clamp(si, 0, 1);
  return padding.top + plotHeight * (1 - clampedSi);
}

/**
 * Computes SVG X and Y coordinates for current and multi-horizon points.
 */
export function computeTimelinePoints(
  currentSi: number,
  currentSpeed: number,
  currentSeverity: string,
  currentConfidence: number,
  horizons: HorizonPrediction[],
  lang: 'vi' | 'en' = 'vi',
  dims: SvgDimensions = DEFAULT_SVG_DIMENSIONS
): TimelinePoint[] {
  const { viewWidth, padding } = dims;
  const plotWidth = viewWidth - padding.left - padding.right;
  const totalSteps = 3; // Now, +10m, +20m, +30m

  const points: TimelinePoint[] = [];

  const nowLabel = lang === 'vi' ? 'Hiện tại' : 'Now';
  const formatMinutes = (m: number) => (lang === 'vi' ? `Sau ${m} phút` : `+${m} min`);

  // Point 0: Current (Now)
  const x0 = padding.left;
  const y0 = siToSvgY(currentSi, dims);
  points.push({
    index: 0,
    label: nowLabel,
    minutes: 0,
    si: currentSi,
    speedKmh: currentSpeed,
    severity: currentSeverity,
    confidence: currentConfidence,
    siLower95: currentSi,
    siUpper95: currentSi,
    x: x0,
    y: y0,
    yUpper: y0,
    yLower: y0,
  });

  // Points 1..3: Horizons (10m, 20m, 30m)
  horizons.forEach((h, i) => {
    const step = i + 1;
    const x = padding.left + (step / totalSteps) * plotWidth;
    const y = siToSvgY(h.predicted_saturation_index, dims);
    const lower = h.confidence_intervals?.saturation_index?.lower_95 ?? h.predicted_saturation_index;
    const upper = h.confidence_intervals?.saturation_index?.upper_95 ?? h.predicted_saturation_index;

    points.push({
      index: step,
      label: formatMinutes(h.horizon_minutes),
      minutes: h.horizon_minutes,
      si: h.predicted_saturation_index,
      speedKmh: h.predicted_speed_kmh,
      severity: h.predicted_severity,
      confidence: h.confidence,
      siLower95: lower,
      siUpper95: upper,
      x,
      y,
      yUpper: siToSvgY(upper, dims),
      yLower: siToSvgY(lower, dims),
    });
  });

  return points;
}

/**
 * Builds SVG SVG path string for the 95% Confidence Interval band polygon.
 */
export function buildConfidenceBandPath(points: TimelinePoint[]): string {
  if (points.length < 2) return '';

  // Top edge (from left to right)
  const topPoints = points.map((p) => `${p.x.toFixed(1)},${p.yUpper.toFixed(1)}`);

  // Bottom edge (from right to left)
  const bottomPoints = [...points].reverse().map((p) => `${p.x.toFixed(1)},${p.yLower.toFixed(1)}`);

  return `M ${topPoints.join(' L ')} L ${bottomPoints.join(' L ')} Z`;
}

/**
 * Builds SVG polyline points string for the central trend line.
 */
export function buildTrendPolyline(points: TimelinePoint[]): string {
  return points.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ');
}

/**
 * Determines whether the availability state allows rendering numeric predictions.
 * Fail-closed rule (ADR-012 D5): only 'ok' and 'stale' (dimmed) are renderable.
 */
export function isPredictionRenderable(availability: AvailabilityStatus): boolean {
  return availability === 'ok' || availability === 'stale';
}

/**
 * Returns monochromatic tactical styling classes for severity chips.
 */
export function getSeverityStyle(severity: string): {
  border: string;
  bg: string;
  text: string;
} {
  const norm = (severity || '').toUpperCase().trim();
  switch (norm) {
    case 'CLEAR':
      return {
        border: 'border-zinc-600',
        bg: 'bg-zinc-900',
        text: 'text-zinc-300',
      };
    case 'MODERATE':
      return {
        border: 'border-amber-600/80',
        bg: 'bg-amber-950/40',
        text: 'text-amber-300',
      };
    case 'HEAVY':
      return {
        border: 'border-orange-600/80',
        bg: 'bg-orange-950/40',
        text: 'text-orange-300',
      };
    case 'CONGESTED':
      return {
        border: 'border-rose-600/90',
        bg: 'bg-rose-950/50',
        text: 'text-rose-200',
      };
    default:
      return {
        border: 'border-zinc-700',
        bg: 'bg-zinc-900',
        text: 'text-zinc-400',
      };
  }
}
