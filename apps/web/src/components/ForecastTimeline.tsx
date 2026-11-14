'use client';

/**
 * ForecastTimeline.tsx - Monochromatic Short-Horizon Forecast Timeline & Uncertainty Display
 * ADR-012 §D5, S10-T6 (Issue #377).
 * Follows strict 4-file pattern Step 4: UI Component, 3 mandatory UI states, 4 availability states, 48px touch targets, zero emoji.
 */

import React, { useMemo } from 'react';
import { useForecast } from '../hooks/useForecast';
import { FORECAST_I18N } from '../config/constants';
import {
  computeTimelinePoints,
  buildConfidenceBandPath,
  buildTrendPolyline,
  getSeverityStyle,
  DEFAULT_SVG_DIMENSIONS,
} from '../utils/forecastUtils';
import type { ForecastRequirements, ModelProvenance } from '../types/forecast';

export interface ForecastTimelineProps {
  cameraId: string;
  lang?: 'vi' | 'en';
  isVisible?: boolean;
  className?: string;
}

export default function ForecastTimeline({
  cameraId,
  lang = 'vi',
  isVisible = true,
  className = '',
}: ForecastTimelineProps) {
  const { data, isLoading, error, isStale, refresh } = useForecast(cameraId, {
    enabled: isVisible,
    pollIntervalMs: 60000,
  });

  // 1. LOADING SKELETON STATE (Mandatory UI State 1)
  if (isLoading && !data) {
    return (
      <section
        aria-label={FORECAST_I18N.HEADER.TITLE[lang]}
        className={`w-full bg-zinc-950 border border-zinc-800 rounded-lg p-3 sm:p-4 animate-pulse select-none ${className}`}
      >
        <div className="flex justify-between items-center mb-3">
          <div className="h-4 bg-zinc-800 rounded w-1/3" />
          <div className="h-4 bg-zinc-800 rounded w-1/5" />
        </div>
        <div className="h-28 bg-zinc-900/60 rounded mb-3 border border-zinc-850" />
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-2">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="h-14 bg-zinc-900 rounded border border-zinc-800/80" />
          ))}
        </div>
      </section>
    );
  }

  // 2. ERROR / NON-OK AVAILABILITY STATES (Mandatory UI State 2 & Fail-Closed Gating)
  // Check if error payload or data payload carries an explicit availability status
  const availability = data?.availability || error?.availability;

  if (availability === 'insufficient_history' || (!data && error?.requirements)) {
    const req = (data?.availability === 'insufficient_history'
      ? (data as unknown as { requirements?: ForecastRequirements }).requirements
      : error?.requirements) || {
      min_observations: 4,
      observed_samples: 0,
      required_window_minutes: 45,
    };
    return (
      <InsufficientHistoryPanel
        requirements={req}
        lang={lang}
        onRetry={refresh}
        className={className}
      />
    );
  }

  if (availability === 'model_unvalidated') {
    return (
      <ModelUnvalidatedPanel
        model={data?.model || error?.model}
        lang={lang}
        onRetry={refresh}
        className={className}
      />
    );
  }

  if (error && !data) {
    return (
      <section
        role="alert"
        aria-label={FORECAST_I18N.STATES.ERROR[lang]}
        className={`w-full bg-zinc-950 border border-zinc-800 rounded-lg p-3 sm:p-4 text-center ${className}`}
      >
        <div className="text-zinc-400 font-mono text-xs uppercase tracking-wider mb-1">
          [{FORECAST_I18N.STATES.ERROR[lang]}]
        </div>
        <div className="text-zinc-500 font-mono text-xs mb-3">
          {error.message || 'HTTP Error'}
        </div>
        <button
          type="button"
          onClick={refresh}
          className="min-h-[48px] min-w-[48px] px-4 py-2 bg-zinc-900 hover:bg-zinc-800 text-zinc-200 border border-zinc-750 hover:border-zinc-500 rounded text-xs font-mono tracking-wide transition-colors focus:outline-none focus:ring-2 focus:ring-zinc-400 cursor-pointer active:scale-95 inline-flex items-center justify-center"
        >
          {FORECAST_I18N.STATES.RETRY[lang]}
        </button>
      </section>
    );
  }

  // 3. EMPTY STATE (Mandatory UI State 3)
  if (!data || !data.horizons || data.horizons.length === 0) {
    return null;
  }

  // 4. SUCCESS STATE: RENDER ACTIVE FORECAST TIMELINE (ok or stale)
  return (
    <ForecastTimelineContent
      data={data}
      isStale={isStale}
      lang={lang}
      onRefresh={refresh}
      className={className}
    />
  );
}

/**
 * Renders the active multi-horizon timeline chart, confidence band, and metric cards.
 */
function ForecastTimelineContent({
  data,
  isStale,
  lang,
  onRefresh,
  className = '',
}: {
  data: import('../types/forecast').ForecastMultiResponse;
  isStale: boolean;
  lang: 'vi' | 'en';
  onRefresh: () => void;
  className?: string;
}) {
  const points = useMemo(() => {
    return computeTimelinePoints(
      data.current.saturation_index,
      data.current.speed_kmh,
      data.current.severity,
      data.current.confidence,
      data.horizons,
      lang,
      DEFAULT_SVG_DIMENSIONS
    );
  }, [data, lang]);

  const confidenceBandPath = useMemo(() => buildConfidenceBandPath(points), [points]);
  const trendPolyline = useMemo(() => buildTrendPolyline(points), [points]);

  const { viewWidth, viewHeight, padding } = DEFAULT_SVG_DIMENSIONS;

  return (
    <section
      role="region"
      aria-label={FORECAST_I18N.HEADER.TITLE[lang]}
      className={`w-full bg-zinc-950 border border-zinc-800 rounded-lg p-3 sm:p-4 select-none ${className}`}
    >
      {/* HEADER ROW */}
      <div className="flex flex-wrap items-center justify-between gap-2 pb-2.5 border-b border-zinc-800/80">
        <div>
          <div className="flex items-center gap-2">
            <h4 className="text-xs font-mono font-bold tracking-wider text-zinc-200 uppercase">
              {FORECAST_I18N.HEADER.TITLE[lang]}
            </h4>
            {isStale && (
              <span
                className="px-1.5 py-0.5 rounded border border-amber-600/80 bg-amber-950/40 text-amber-300 font-mono text-[10px] uppercase font-semibold"
                title={FORECAST_I18N.AVAILABILITY.STALE.DESC[lang](data.source_data?.age_minutes || 0)}
              >
                {FORECAST_I18N.AVAILABILITY.STALE.BADGE[lang]}
              </span>
            )}
            {data.model?.validated ? (
              <span className="hidden sm:inline-block px-1.5 py-0.5 rounded border border-zinc-700 bg-zinc-900 text-zinc-400 font-mono text-[10px] uppercase">
                [{FORECAST_I18N.PROVENANCE.VALIDATED[lang]}]
              </span>
            ) : (
              <span className="hidden sm:inline-block px-1.5 py-0.5 rounded border border-zinc-800 bg-zinc-900/60 text-zinc-500 font-mono text-[10px] uppercase">
                [{FORECAST_I18N.PROVENANCE.UNVALIDATED[lang]}]
              </span>
            )}
          </div>
          <p className="text-[11px] text-zinc-500 font-mono mt-0.5">
            {FORECAST_I18N.HEADER.SUBTITLE[lang]}
          </p>
        </div>

        {/* REFRESH ACTION WITH >= 48px TOUCH ERGONOMICS */}
        <button
          type="button"
          onClick={onRefresh}
          title={lang === 'vi' ? 'Làm mới dự báo' : 'Refresh forecast'}
          aria-label={lang === 'vi' ? 'Làm mới dự báo' : 'Refresh forecast'}
          className="min-h-[48px] min-w-[48px] p-2 bg-transparent hover:bg-zinc-900 text-zinc-400 hover:text-zinc-200 border border-transparent hover:border-zinc-800 rounded flex items-center justify-center transition-colors focus:outline-none focus:ring-2 focus:ring-zinc-400 cursor-pointer"
        >
          <svg
            className="w-4 h-4"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <polyline points="23 4 23 10 17 10" />
            <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
          </svg>
        </button>
      </div>

      {/* STALE BANNER IF SERIES IS DIMMED */}
      {isStale && (
        <div className="mt-2 px-2.5 py-1.5 rounded bg-zinc-900/90 border border-amber-800/40 text-amber-200/90 text-xs font-mono">
          {FORECAST_I18N.AVAILABILITY.STALE.DESC[lang](data.source_data?.age_minutes || 0)}
        </div>
      )}

      {/* SVG GRAPH: SPARKLINE + 95% CONFIDENCE BAND */}
      <div className={`mt-3 relative w-full overflow-hidden ${isStale ? 'opacity-50' : 'opacity-100'} transition-opacity`}>
        <svg
          viewBox={`0 0 ${viewWidth} ${viewHeight}`}
          className="w-full h-auto text-zinc-400 overflow-visible"
          role="img"
          aria-label={FORECAST_I18N.HEADER.TITLE[lang]}
        >
          {/* Y-Axis Horizontal Gridlines */}
          <line
            x1={padding.left}
            y1={padding.top}
            x2={viewWidth - padding.right}
            y2={padding.top}
            stroke="currentColor"
            className="text-zinc-800 stroke-[1] stroke-dasharray-2"
          />
          <text
            x={padding.left - 8}
            y={padding.top + 4}
            textAnchor="end"
            className="text-[10px] font-mono fill-zinc-500 select-none"
          >
            1.0
          </text>

          <line
            x1={padding.left}
            y1={padding.top + (viewHeight - padding.top - padding.bottom) / 2}
            x2={viewWidth - padding.right}
            y2={padding.top + (viewHeight - padding.top - padding.bottom) / 2}
            stroke="currentColor"
            className="text-zinc-850 stroke-[1] stroke-dasharray-2"
          />
          <text
            x={padding.left - 8}
            y={padding.top + (viewHeight - padding.top - padding.bottom) / 2 + 3}
            textAnchor="end"
            className="text-[10px] font-mono fill-zinc-600 select-none"
          >
            0.5
          </text>

          <line
            x1={padding.left}
            y1={viewHeight - padding.bottom}
            x2={viewWidth - padding.right}
            y2={viewHeight - padding.bottom}
            stroke="currentColor"
            className="text-zinc-800 stroke-[1]"
          />
          <text
            x={padding.left - 8}
            y={viewHeight - padding.bottom + 3}
            textAnchor="end"
            className="text-[10px] font-mono fill-zinc-500 select-none"
          >
            0.0
          </text>

          {/* 95% Confidence Band Shaded Polygon */}
          {confidenceBandPath && (
            <path
              d={confidenceBandPath}
              className="fill-zinc-600/20 text-transparent"
              aria-label={FORECAST_I18N.METRICS.CONFIDENCE_BAND[lang]}
            />
          )}

          {/* Central Trend Polyline */}
          <polyline
            points={trendPolyline}
            fill="none"
            stroke="currentColor"
            className="text-zinc-200 stroke-[2.5]"
            strokeLinecap="round"
            strokeLinejoin="round"
          />

          {/* Data Points (Dots and Labels) */}
          {points.map((p) => {
            return (
              <g key={p.index} className="group cursor-default">
                {/* Confidence Interval Bracket (Vertical line from lower to upper at horizon points) */}
                {p.minutes > 0 && (
                  <line
                    x1={p.x}
                    y1={p.yLower}
                    x2={p.x}
                    y2={p.yUpper}
                    stroke="currentColor"
                    className="text-zinc-500 stroke-[1.5] stroke-linecap-round opacity-60"
                  />
                )}

                {/* Generous invisible hit target to prevent jitter at boundary edges (Issue #456) */}
                <circle
                  cx={p.x}
                  cy={p.y}
                  r="16"
                  className="fill-transparent cursor-default pointer-events-auto"
                />

                {/* Point Center Dot with origin-centered scaling */}
                <circle
                  cx={p.x}
                  cy={p.y}
                  r="4.5"
                  style={{ transformBox: 'fill-box', transformOrigin: 'center' }}
                  className="fill-zinc-950 stroke-zinc-100 stroke-[2] transition-transform duration-150 group-hover:scale-125 pointer-events-none"
                />

                {/* SI Value Badge on Top */}
                <text
                  x={p.x}
                  y={p.y - 8}
                  textAnchor="middle"
                  className="text-[11px] font-mono font-bold fill-zinc-100 select-none drop-shadow"
                >
                  {p.si.toFixed(2)}
                </text>

                {/* X-Axis Horizon Label below */}
                <text
                  x={p.x}
                  y={viewHeight - padding.bottom + 16}
                  textAnchor="middle"
                  className="text-[10px] font-mono fill-zinc-400 select-none tracking-tight"
                >
                  {p.label}
                </text>
              </g>
            );
          })}
        </svg>
      </div>

      {/* METRIC BREAKDOWN CARDS (GRID OF 4 COLUMNS: NOW, +10M, +20M, +30M) */}
      <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 mt-3 pt-2 border-t border-zinc-850">
        {points.map((p) => {
          const sevStyle = getSeverityStyle(p.severity);
          const sevLabel =
            FORECAST_I18N.SEVERITY[p.severity as keyof typeof FORECAST_I18N.SEVERITY]?.[lang] ||
            p.severity;
          return (
            <div
              key={p.index}
              className={`p-2 rounded border bg-zinc-900/60 ${sevStyle.border} flex flex-col justify-between`}
            >
              <div className="flex justify-between items-center text-[10px] font-mono text-zinc-400 mb-1">
                <span>{p.label}</span>
                <span className={`px-1 rounded text-[9px] font-bold uppercase ${sevStyle.bg} ${sevStyle.text}`}>
                  {sevLabel}
                </span>
              </div>
              <div className="flex items-baseline justify-between mt-0.5">
                <span className="text-base font-mono font-bold text-zinc-100">
                  {p.si.toFixed(2)}
                </span>
                <span className="text-[11px] font-mono text-zinc-400">
                  {p.speedKmh.toFixed(0)} km/h
                </span>
              </div>
              {p.minutes > 0 && (
                <div className="text-[9px] font-mono text-zinc-500 mt-1 flex justify-between border-t border-zinc-800/60 pt-0.5">
                  <span>95% CI:</span>
                  <span>
                    [{p.siLower95.toFixed(2)} - {p.siUpper95.toFixed(2)}]
                  </span>
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* FOOTER: PROVENANCE METADATA */}
      {data.model && (
        <div className="mt-3 pt-2 border-t border-zinc-900 flex flex-wrap items-center justify-between text-[10px] font-mono text-zinc-500 gap-1.5">
          <div className="flex items-center gap-2">
            <span>
              {FORECAST_I18N.PROVENANCE.TITLE[lang]} {data.model.name} ({data.model.version})
            </span>
            <span>•</span>
            <span>
              {FORECAST_I18N.PROVENANCE.MAE[lang]} {data.model.mae?.toFixed(4) || '—'}
            </span>
          </div>
          {data.model.artifact_sha256 && (
            <span
              className="text-zinc-600 truncate max-w-[140px]"
              title={`Artifact SHA: ${data.model.artifact_sha256}`}
            >
              SHA: {data.model.artifact_sha256.substring(0, 8)}...
            </span>
          )}
        </div>
      )}
    </section>
  );
}

/**
 * Disabled State: Insufficient History Data (< 4 continuous samples / 45m).
 * Prevents fabricated confidence by explaining empirical data requirements (ADR-012 D5).
 */
function InsufficientHistoryPanel({
  requirements,
  lang,
  onRetry,
  className = '',
}: {
  requirements: ForecastRequirements;
  lang: 'vi' | 'en';
  onRetry: () => void;
  className?: string;
}) {
  const min = requirements.min_observations || 4;
  const windowM = requirements.required_window_minutes || 45;
  const observed = requirements.observed_samples || 0;

  return (
    <section
      role="status"
      aria-label={FORECAST_I18N.AVAILABILITY.INSUFFICIENT_HISTORY.TITLE[lang]}
      className={`w-full bg-zinc-950 border border-zinc-800 rounded-lg p-3 sm:p-4 text-center select-none ${className}`}
    >
      <div className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-zinc-900 border border-zinc-750 text-zinc-400 mb-2">
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="8" x2="12" y2="12" />
          <line x1="12" y1="16" x2="12.01" y2="16" />
        </svg>
      </div>

      <h4 className="text-xs font-mono font-bold tracking-wider text-zinc-300 uppercase mb-1">
        [{FORECAST_I18N.AVAILABILITY.INSUFFICIENT_HISTORY.TITLE[lang]}]
      </h4>
      <p className="text-xs font-mono text-zinc-400 max-w-md mx-auto mb-3">
        {FORECAST_I18N.AVAILABILITY.INSUFFICIENT_HISTORY.DESC[lang](min, windowM, observed)}
      </p>

      <div className="flex justify-center items-center gap-2">
        <button
          type="button"
          onClick={onRetry}
          className="min-h-[48px] min-w-[48px] px-4 py-2 bg-zinc-900 hover:bg-zinc-850 text-zinc-300 border border-zinc-700 hover:border-zinc-500 rounded text-xs font-mono tracking-wide transition-colors focus:outline-none focus:ring-2 focus:ring-zinc-400 cursor-pointer active:scale-95 inline-flex items-center justify-center gap-1.5"
        >
          <span>↻</span>
          <span>{FORECAST_I18N.STATES.RETRY[lang]}</span>
        </button>
      </div>
    </section>
  );
}

/**
 * Disabled State: Model Under Validation.
 * Fail-closed state when model weights are undergoing backtesting/calibration (ADR-012 D5).
 */
function ModelUnvalidatedPanel({
  model,
  lang,
  onRetry,
  className = '',
}: {
  model?: ModelProvenance;
  lang: 'vi' | 'en';
  onRetry: () => void;
  className?: string;
}) {
  return (
    <section
      role="status"
      aria-label={FORECAST_I18N.AVAILABILITY.MODEL_UNVALIDATED.TITLE[lang]}
      className={`w-full bg-zinc-950 border border-zinc-800 rounded-lg p-3 sm:p-4 text-center select-none ${className}`}
    >
      <div className="inline-flex items-center justify-center w-8 h-8 rounded-full bg-zinc-900 border border-zinc-750 text-zinc-400 mb-2">
        <svg
          className="w-4 h-4"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
        </svg>
      </div>

      <h4 className="text-xs font-mono font-bold tracking-wider text-zinc-300 uppercase mb-1">
        [{FORECAST_I18N.AVAILABILITY.MODEL_UNVALIDATED.TITLE[lang]}]
      </h4>
      <p className="text-xs font-mono text-zinc-400 max-w-md mx-auto mb-3">
        {FORECAST_I18N.AVAILABILITY.MODEL_UNVALIDATED.DESC[lang]}
      </p>

      {model && (
        <div className="text-[10px] font-mono text-zinc-500 mb-3">
          {model.name} ({model.version}) • {model.fit_source}
        </div>
      )}

      <div className="flex justify-center items-center gap-2">
        <button
          type="button"
          onClick={onRetry}
          className="min-h-[48px] min-w-[48px] px-4 py-2 bg-zinc-900 hover:bg-zinc-850 text-zinc-300 border border-zinc-700 hover:border-zinc-500 rounded text-xs font-mono tracking-wide transition-colors focus:outline-none focus:ring-2 focus:ring-zinc-400 cursor-pointer active:scale-95 inline-flex items-center justify-center gap-1.5"
        >
          <span>↻</span>
          <span>{FORECAST_I18N.STATES.RETRY[lang]}</span>
        </button>
      </div>
    </section>
  );
}
