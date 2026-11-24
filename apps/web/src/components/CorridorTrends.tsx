'use client';

import { useState, useId, useMemo } from 'react';
import { useCorridorHistory } from '../hooks/useCorridorHistory.ts';
import type { HourlyHistoryItem } from '../types/corridorHistory.ts';

interface CorridorTrendsProps {
  roadKey: string;
  lang?: 'vi' | 'en';
}

export default function CorridorTrends({ roadKey, lang = 'vi' }: CorridorTrendsProps) {
  const [isExpanded, setIsExpanded] = useState(false);
  const [scrubIndex, setScrubIndex] = useState<number | null>(null);
  const { data, loading, error } = useCorridorHistory(roadKey, 24);
  const clipId = useId();

  const summaryHeadline = useMemo(() => {
    if (!data || !data.summary || !data.summary.headline) {
      return lang === 'vi' ? 'Đang tải dữ liệu triều & ngập...' : 'Loading tide & flood data...';
    }
    return data.summary.headline[lang] || data.summary.headline.vi || '';
  }, [data, lang]);

  if (loading && !data) {
    return (
      <div className="w-full bg-zinc-900/60 rounded border border-zinc-800/60 p-2 animate-pulse">
        <div className="h-4 bg-zinc-800/80 rounded w-3/4 mb-1"></div>
      </div>
    );
  }

  if (error || !data || !data.timeline || data.timeline.length === 0) {
    return null; // Gracefully degrade if no history available
  }

  const items = data.timeline;
  const count = items.length;

  // Chart dimensions in SVG viewBox coordinate space
  const svgWidth = 320;
  const svgHeight = 72;
  const paddingBottom = 16;
  const chartHeight = svgHeight - paddingBottom;
  const barWidth = Math.max(4, Math.floor((svgWidth - (count - 1) * 2) / count));
  const gap = count > 1 ? (svgWidth - count * barWidth) / (count - 1) : 0;

  // Flood depth range scaling: scale up to max(max_depth_cm, 30cm) to give visual headroom
  const maxDepthVal = Math.max(...items.map((it) => it.max_depth_cm), 30);

  // Tide elevation range scaling: 0.0m to 2.2m Saigon River / Soai Rap River range
  const minTide = 0.0;
  const maxTide = 2.2;

  // Build SVG path for smoothed tide elevation curve
  const tidePoints = items.map((it, idx) => {
    const x = idx * (barWidth + gap) + barWidth / 2;
    const clampedTide = Math.max(minTide, Math.min(maxTide, it.tide_level_m));
    const normalized = (clampedTide - minTide) / (maxTide - minTide);
    const y = chartHeight - 4 - normalized * (chartHeight - 8);
    return { x, y, tide: it.tide_level_m };
  });

  const tidePathD = tidePoints.reduce((acc, pt, idx) => {
    if (idx === 0) return `M ${pt.x} ${pt.y}`;
    return `${acc} L ${pt.x} ${pt.y}`;
  }, '');

  // 1.50m Alert Level 2/3 line y-coordinate
  const alert150Y = chartHeight - 4 - ((1.50 - minTide) / (maxTide - minTide)) * (chartHeight - 8);

  const activeItem: HourlyHistoryItem | null =
    scrubIndex !== null && scrubIndex >= 0 && scrubIndex < items.length ? items[scrubIndex] : null;

  const stationDisplayName = data.station_name
    ? data.station_name[lang] || data.station_name.vi
    : 'Sông Sài Gòn';

  const hasImpassable = items.some((it) => it.hazard_level === 'IMPASSABLE');
  const hasCaution = items.some((it) => it.hazard_level === 'CAUTION' || it.max_depth_cm >= 10);

  return (
    <div
      data-testid="corridor-trends-container"
      className="w-full bg-zinc-900/90 rounded border border-zinc-800/80 overflow-hidden font-mono text-xs transition-all duration-200"
    >
      {/* 1-Line Glanceable Summary Strip (Accessible Toggle Button >= 48px touch target) */}
      <button
        type="button"
        data-testid="corridor-trends-toggle"
        onClick={() => setIsExpanded(!isExpanded)}
        className="w-full min-h-[48px] px-3 py-2 flex items-center justify-between text-left text-zinc-300 hover:text-white hover:bg-zinc-800/40 transition-colors focus:outline-none focus:ring-1 focus:ring-cyan-500/50"
        aria-expanded={isExpanded}
        aria-label={
          isExpanded
            ? lang === 'vi'
              ? 'Thu gọn biểu đồ triều và ngập'
              : 'Collapse tide & flood chart'
            : lang === 'vi'
            ? 'Mở rộng biểu đồ triều và ngập'
            : 'Expand tide & flood chart'
        }
      >
        <div className="flex items-center gap-2 min-w-0 pr-2">
          {hasImpassable ? (
            <span className="w-2 h-2 rounded-full bg-rose-500 animate-pulse shrink-0" />
          ) : hasCaution ? (
            <span className="w-2 h-2 rounded-full bg-amber-500 shrink-0" />
          ) : (
            <span className="w-2 h-2 rounded-full bg-cyan-500 shrink-0" />
          )}
          <span className="text-[11px] font-medium truncate text-zinc-200">{summaryHeadline}</span>
        </div>

        <svg
          className={`w-4 h-4 text-zinc-400 transition-transform duration-200 shrink-0 ${
            isExpanded ? 'rotate-180' : ''
          }`}
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      {/* Expanded Spark-Ribbon Visualization (~72px compact strip) */}
      {isExpanded && (
        <div data-testid="corridor-trends-expanded" className="px-3 pb-3 pt-1 border-t border-zinc-800/60">
          {/* Header Legend and Scrubber Details */}
          <div className="flex items-center justify-between text-[10px] text-zinc-400 mb-2">
            <div className="flex items-center gap-3">
              <span className="flex items-center gap-1">
                <span className="w-2 h-2 rounded-sm bg-rose-500 inline-block" />
                {lang === 'vi' ? 'Ngập' : 'Flood'}
              </span>
              <span className="flex items-center gap-1">
                <span className="w-2.5 h-0.5 bg-cyan-400 inline-block border-b border-cyan-400 border-dashed" />
                {lang === 'vi' ? 'Triều' : 'Tide'}
              </span>
              <span className="text-zinc-500 text-[9px] hidden sm:inline">
                ({stationDisplayName})
              </span>
            </div>

            {/* Interactive Tooltip Inspector */}
            {activeItem ? (
              <div
                data-testid="corridor-trends-tooltip"
                className="text-right text-[10px] text-cyan-300 font-semibold"
              >
                <span>{activeItem.hour_label}</span> ·{' '}
                <span>
                  {activeItem.max_depth_cm > 0
                    ? `${activeItem.max_depth_cm.toFixed(0)}cm`
                    : lang === 'vi'
                    ? 'Khô ráo'
                    : 'Dry'}
                </span>{' '}
                · <span>{activeItem.tide_level_m.toFixed(2)}m</span>
              </div>
            ) : (
              <span className="text-[9px] text-zinc-500">
                {lang === 'vi' ? 'Vuốt để xem chi tiết' : 'Scrub to inspect'}
              </span>
            )}
          </div>

          {/* SVG Spark-Ribbon Chart */}
          <div className="relative w-full overflow-hidden select-none touch-none">
            <svg
              viewBox={`0 0 ${svgWidth} ${svgHeight}`}
              className="w-full h-[72px] overflow-visible"
              preserveAspectRatio="none"
              onMouseLeave={() => setScrubIndex(null)}
            >
              <defs>
                <clipPath id={clipId}>
                  <rect x="0" y="0" width={svgWidth} height={chartHeight} rx="2" />
                </clipPath>
              </defs>

              {/* Background fill */}
              <rect
                x="0"
                y="0"
                width={svgWidth}
                height={chartHeight}
                className="fill-zinc-950/60"
                rx="2"
              />

              {/* 1.50m Astronomical Tide Alert Threshold Line */}
              <line
                x1="0"
                y1={alert150Y}
                x2={svgWidth}
                y2={alert150Y}
                stroke="#f43f5e"
                strokeWidth="0.8"
                strokeDasharray="2,2"
                opacity="0.45"
              />
              <text
                x="2"
                y={alert150Y - 2}
                fill="#f43f5e"
                fontSize="7"
                opacity="0.75"
                fontFamily="monospace"
              >
                1.50m (BĐ2)
              </text>

              {/* Hourly Flood Depth Bars */}
              {items.map((it, idx) => {
                const x = idx * (barWidth + gap);
                const depth = it.max_depth_cm;
                const isDry = depth <= 0;

                // Color code according to hazard level
                let barFill = '#3f3f46'; // zinc-700
                if (it.hazard_level === 'IMPASSABLE' || depth > 20) {
                  barFill = '#f43f5e'; // rose-500
                } else if (it.hazard_level === 'CAUTION' || depth >= 10) {
                  barFill = '#f59e0b'; // amber-500
                } else if (depth > 0) {
                  barFill = '#fbbf24'; // amber-400
                }

                // Minimum 2px bar height for dry to show slot
                const barHeight = isDry
                  ? 2
                  : Math.max(3, (depth / maxDepthVal) * (chartHeight - 8));
                const y = chartHeight - barHeight;

                const isCurrent = idx === items.length - 1;
                const isHovered = scrubIndex === idx;

                return (
                  <g key={it.time}>
                    <rect
                      x={x}
                      y={y}
                      width={barWidth}
                      height={barHeight}
                      fill={barFill}
                      opacity={isHovered ? 1 : 0.85}
                      rx="1"
                    />
                    {/* Current hour marker indicator */}
                    {isCurrent && (
                      <circle
                        cx={x + barWidth / 2}
                        cy={chartHeight + 4}
                        r="2"
                        fill="#06b6d4"
                      />
                    )}
                  </g>
                );
              })}

              {/* River Tide Line Curve */}
              <path
                d={tidePathD}
                fill="none"
                stroke="#06b6d4"
                strokeWidth="1.5"
                strokeDasharray="3,2"
                opacity="0.9"
              />

              {/* Scrubber vertical guide line */}
              {scrubIndex !== null && scrubIndex >= 0 && scrubIndex < items.length && (
                <line
                  x1={scrubIndex * (barWidth + gap) + barWidth / 2}
                  y1={0}
                  x2={scrubIndex * (barWidth + gap) + barWidth / 2}
                  y2={chartHeight}
                  stroke="#38bdf8"
                  strokeWidth="1"
                  strokeDasharray="1,1"
                />
              )}

              {/* Time Axis Labels (24h span) */}
              <text x="2" y={svgHeight - 2} fill="#71717a" fontSize="8" fontFamily="monospace">
                -24h
              </text>
              <text
                x={svgWidth / 2}
                y={svgHeight - 2}
                textAnchor="middle"
                fill="#71717a"
                fontSize="8"
                fontFamily="monospace"
              >
                -12h
              </text>
              <text
                x={svgWidth - 2}
                y={svgHeight - 2}
                textAnchor="end"
                fill="#06b6d4"
                fontSize="8"
                fontWeight="bold"
                fontFamily="monospace"
              >
                {lang === 'vi' ? 'Hiện tại' : 'Now'}
              </text>

              {/* Invisible interactive hit targets for touch & mouse scrubbing (>=48px touch height) */}
              {items.map((it, idx) => {
                const x = idx * (barWidth + gap);
                return (
                  <rect
                    key={`hit-${it.time}`}
                    x={x}
                    y={0}
                    width={barWidth + gap}
                    height={svgHeight}
                    fill="transparent"
                    className="cursor-pointer"
                    onMouseEnter={() => setScrubIndex(idx)}
                    onTouchStart={() => setScrubIndex(idx)}
                    onTouchMove={() => setScrubIndex(idx)}
                  />
                );
              })}
            </svg>
          </div>
        </div>
      )}
    </div>
  );
}
