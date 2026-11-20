'use client';

import React, { useState } from 'react';
import type { HourlyForecastResponse, HourlyForecastItem } from '../types/forecast.ts';

interface HourlyTimelineScrubberProps {
  forecast: HourlyForecastResponse | null;
  selectedHour: number;
  onSelectHour: (hour: number) => void;
  lang?: 'vi' | 'en';
}

export default function HourlyTimelineScrubber({
  forecast,
  selectedHour,
  onSelectHour,
  lang = 'vi',
}: HourlyTimelineScrubberProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  if (!forecast || !forecast.timeline || forecast.timeline.length === 0) {
    return null;
  }

  const { advisory, timeline } = forecast;
  const activeItem: HourlyForecastItem =
    timeline.find(t => t.hour_offset === selectedHour) || timeline[0];

  return (
    <div
      className="w-full max-w-4xl mx-auto px-2 pointer-events-auto select-none font-mono"
      role="region"
      aria-label={lang === 'vi' ? 'Thanh trượt dự báo ngập triều & mưa theo giờ' : 'Hourly tide & rain timeline scrubber'}
    >
      <div className="bg-zinc-950/95 border border-zinc-800 rounded-lg shadow-2xl backdrop-blur-md overflow-hidden transition-all duration-200">
        {/* Compact Advisory Bar */}
        <div className="px-3 py-2 border-b border-zinc-800/80 flex items-center justify-between gap-2 text-xs">
          <div className="flex items-center gap-2 min-w-0 flex-1">
            <span
              className={`w-2 h-2 rounded-full shrink-0 ${
                activeItem.commuter_risk === 'DANGER'
                  ? 'bg-rose-500 animate-pulse'
                  : activeItem.commuter_risk === 'CAUTION'
                  ? 'bg-amber-400'
                  : 'bg-emerald-400'
              }`}
            />
            <div className="truncate">
              <span className="font-semibold text-zinc-100">
                {advisory.headline[lang]}
              </span>
              <span className="hidden sm:inline text-zinc-400 text-[11px] ml-2">
                {advisory.guidance[lang]}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-1.5 shrink-0">
            {selectedHour > 0 && (
              <button
                type="button"
                onClick={() => onSelectHour(0)}
                className="px-2 py-1 min-h-[36px] text-[11px] rounded bg-zinc-800 hover:bg-zinc-700 text-zinc-200 transition-colors cursor-pointer"
                aria-label={lang === 'vi' ? 'Trở về hiện tại' : 'Reset to now'}
                title={lang === 'vi' ? 'Trở về mốc thời gian hiện tại' : 'Reset to current time'}
              >
                {lang === 'vi' ? 'Về hiện tại' : 'Reset'}
              </button>
            )}

            <button
              type="button"
              onClick={() => setIsExpanded(prev => !prev)}
              className="p-1 min-w-[36px] min-h-[36px] flex items-center justify-center rounded hover:bg-zinc-800 text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
              aria-label={
                isExpanded
                  ? lang === 'vi'
                    ? 'Thu gọn chi tiết'
                    : 'Collapse details'
                  : lang === 'vi'
                  ? 'Mở rộng chi tiết'
                  : 'Expand details'
              }
              title={
                isExpanded
                  ? lang === 'vi'
                    ? 'Thu gọn danh sách tuyến ngập'
                    : 'Collapse affected roads'
                  : lang === 'vi'
                  ? 'Xem danh sách tuyến đường ngập triều'
                  : 'View tidal flooded roads'
              }
            >
              <svg
                className={`w-4 h-4 transition-transform duration-200 ${isExpanded ? 'rotate-180' : ''}`}
                fill="none"
                stroke="currentColor"
                strokeWidth={2}
                viewBox="0 0 24 24"
              >
                <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
              </svg>
            </button>
          </div>
        </div>

        {/* Horizontal Timeline Scrubber Buttons */}
        <div
          role="tablist"
          aria-label={lang === 'vi' ? 'Chọn khung giờ dự báo' : 'Select forecast hour'}
          className="flex items-stretch overflow-x-auto no-scrollbar divide-x divide-zinc-900 bg-zinc-900/30 p-1 gap-1"
        >
          {timeline.map(item => {
            const isSelected = item.hour_offset === selectedHour;
            const hasTideAlert = item.tide_alert_level === 'LEVEL_2' || item.tide_alert_level === 'LEVEL_3';
            const hasHeavyRain = item.rain_probability_percent >= 50 || item.rain_mm >= 5.0;

            return (
              <button
                key={item.hour_offset}
                role="tab"
                aria-selected={isSelected}
                tabIndex={isSelected ? 0 : -1}
                type="button"
                onClick={() => onSelectHour(item.hour_offset)}
                className={`flex-1 min-w-[84px] sm:min-w-[100px] min-h-[52px] py-1.5 px-2 rounded flex flex-col items-center justify-between text-center transition-all cursor-pointer focus:outline-none focus:ring-1 focus:ring-zinc-600 ${
                  isSelected
                    ? 'bg-zinc-800 border border-zinc-600 shadow-md text-white'
                    : 'hover:bg-zinc-900/80 text-zinc-400 hover:text-zinc-200'
                }`}
              >
                {/* Hour label */}
                <div className="flex items-center gap-1">
                  <span className={`text-xs font-bold ${isSelected ? 'text-zinc-100' : 'text-zinc-300'}`}>
                    {item.hour_offset === 0
                      ? lang === 'vi' ? 'Hiện tại' : 'Now'
                      : `+${item.hour_offset}h`}
                  </span>
                  <span className="text-[10px] text-zinc-500 font-sans">
                    {item.time_formatted}
                  </span>
                </div>

                {/* Tide Gauge Indicator */}
                <div className="flex items-center gap-1 text-[10px] mt-0.5">
                  <span
                    className={`font-semibold ${
                      hasTideAlert
                        ? 'text-rose-400'
                        : item.tide_alert_level === 'LEVEL_1'
                        ? 'text-amber-400'
                        : 'text-zinc-400'
                    }`}
                  >
                    {item.tide_level_phu_an_m.toFixed(2)}m
                  </span>
                  {hasTideAlert && (
                    <span className="w-1.5 h-1.5 rounded-full bg-rose-500 shrink-0" />
                  )}
                </div>

                {/* Rain metric */}
                <div className="text-[10px] text-zinc-500 mt-0.5 flex items-center gap-1">
                  <span className={hasHeavyRain ? 'text-sky-300 font-medium' : 'text-zinc-500'}>
                    {item.rain_probability_percent}%
                  </span>
                  {item.rain_mm > 0 && (
                    <span className="text-sky-400 font-medium">
                      ({item.rain_mm}mm)
                    </span>
                  )}
                </div>
              </button>
            );
          })}
        </div>

        {/* Expandable Affected Roads List for Active Hour */}
        {isExpanded && (
          <div className="p-3 border-t border-zinc-800 bg-zinc-900/50 text-xs">
            <div className="flex items-center justify-between mb-2">
              <span className="font-bold text-zinc-200">
                {lang === 'vi'
                  ? `Dự báo ngập lúc ${activeItem.time_formatted} (+${activeItem.hour_offset}h)`
                  : `Flood Forecast at ${activeItem.time_formatted} (+${activeItem.hour_offset}h)`}
              </span>
              <span className="text-[11px] text-zinc-400">
                {lang === 'vi' ? 'Triều Phú An:' : 'Phu An Tide:'}{' '}
                <strong className="text-zinc-200">{activeItem.tide_level_phu_an_m}m</strong>{' '}
                ({activeItem.tide_alert_label[lang]})
              </span>
            </div>

            {activeItem.affected_roads.length === 0 ? (
              <div className="py-2 text-zinc-500 text-center italic">
                {lang === 'vi'
                  ? 'Mực nước dưới ngưỡng tràn, không có điểm ngập triều lớn trên các trục chính.'
                  : 'Water level below curb thresholds, no major tidal flooding on main arterials.'}
              </div>
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 mt-2">
                {activeItem.affected_roads.map(road => (
                  <div
                    key={road.road_key}
                    className="p-2 rounded bg-zinc-950/80 border border-zinc-800 flex items-start justify-between gap-2"
                  >
                    <div>
                      <div className="font-semibold text-zinc-200">
                        {road.display_name}
                      </div>
                      <div className="text-[10px] text-zinc-500 mt-0.5">
                        {road.ward_district}
                      </div>
                    </div>
                    <div className="text-right shrink-0">
                      <span
                        className={`inline-block px-1.5 py-0.5 rounded text-[10px] font-bold ${
                          road.hazard_level === 'IMPASSABLE'
                            ? 'bg-rose-950 text-rose-300 border border-rose-800'
                            : 'bg-amber-950 text-amber-300 border border-amber-800'
                        }`}
                      >
                        +{road.estimated_depth_cm}cm
                      </span>
                      <div className="text-[10px] text-zinc-400 mt-0.5">
                        {road.hazard_label[lang]}
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
