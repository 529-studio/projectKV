'use client';

import { useState, useRef, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import {
  WeatherData,
  WEATHER_I18N,
  formatTemperature,
  formatRainProbability,
  formatHour,
  getWeatherCategory,
} from '../utils/weatherUtils';

interface WeatherWidgetProps {
  weather: WeatherData | null;
  loading?: boolean;
  lang?: 'vi' | 'en';
  className?: string;
}

export default function WeatherWidget({
  weather,
  loading = false,
  lang = 'vi',
  className = '',
}: WeatherWidgetProps) {
  const [isOpen, setIsOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  // Close dropdown on click outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent | TouchEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }
    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
      document.addEventListener('touchstart', handleClickOutside);
    }
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
      document.removeEventListener('touchstart', handleClickOutside);
    };
  }, [isOpen]);

  if (loading && !weather) {
    return (
      <div className={`flex items-center gap-1.5 px-3 py-1.5 rounded-full bg-zinc-950/90 border border-zinc-800 text-zinc-400 font-mono text-xs select-none backdrop-blur-xs ${className}`}>
        <span className="w-2 h-2 rounded-full bg-sky-400 animate-pulse" />
        <span className="text-[11px]">{WEATHER_I18N.LOADING[lang]}</span>
      </div>
    );
  }

  if (!weather) return null;

  const category = getWeatherCategory(weather.weather_code);
  const desc = lang === 'vi' ? weather.weather_desc_vi : weather.weather_desc_en;
  const hourly = weather.hourly_forecast ? weather.hourly_forecast.slice(0, 6) : [];

  return (
    <div ref={containerRef} className={`relative select-none font-mono text-xs ${className}`}>
      {/* TRIGGER BADGE (Compact & Sleek) */}
      <button
        type="button"
        onClick={() => setIsOpen(prev => !prev)}
        className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg bg-zinc-950/95 hover:bg-zinc-900 border border-zinc-800 hover:border-zinc-700 text-zinc-200 transition-all cursor-pointer shadow-2xl backdrop-blur-md focus:outline-none text-[11px]"
        title={WEATHER_I18N.RAIN_FORECAST[lang]}
        aria-expanded={isOpen}
      >
        {/* Weather Category SVG Icon */}
        <span className="text-sky-400 shrink-0">
          {category === 'sun' && (
            <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="12" cy="12" r="5" />
              <line x1="12" y1="1" x2="12" y2="3" />
              <line x1="12" y1="21" x2="12" y2="23" />
              <line x1="4.22" y1="4.22" x2="5.64" y2="5.64" />
              <line x1="18.36" y1="18.36" x2="19.78" y2="19.78" />
              <line x1="1" y1="12" x2="3" y2="12" />
              <line x1="21" y1="12" x2="23" y2="12" />
              <line x1="4.22" y1="19.78" x2="5.64" y2="18.36" />
              <line x1="18.36" y1="5.64" x2="19.78" y2="4.22" />
            </svg>
          )}
          {category === 'cloud' && (
            <svg className="w-3.5 h-3.5 text-zinc-300" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z" />
            </svg>
          )}
          {(category === 'rain' || category === 'drizzle') && (
            <svg className="w-3.5 h-3.5 text-sky-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <line x1="16" y1="13" x2="16" y2="21" />
              <line x1="8" y1="13" x2="8" y2="21" />
              <line x1="12" y1="15" x2="12" y2="23" />
              <path d="M20 16.58A5 5 0 0 0 18 7h-1.26A8 8 0 1 0 4 15.25" />
            </svg>
          )}
          {category === 'storm' && (
            <svg className="w-3.5 h-3.5 text-amber-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M19 16.9A5 5 0 0 0 18 7h-1.26a8 8 0 1 0-11.62 9" />
              <polyline points="13 11 9 17 15 17 11 23" />
            </svg>
          )}
        </span>

        {/* Temperature */}
        <span className="font-semibold text-white tracking-tight">
          {formatTemperature(weather.temperature_c)}
        </span>

        {/* Rain Chance Tag */}
        {weather.precipitation_probability_percent > 0 && (
          <span className="px-1.5 py-0.2 rounded text-[10px] font-bold bg-sky-950 text-sky-300 border border-sky-800/80">
            {formatRainProbability(weather.precipitation_probability_percent)}
          </span>
        )}
      </button>

      {/* POPOVER DROPDOWN */}
      <AnimatePresence>
        {isOpen && (
          <motion.div
            initial={{ opacity: 0, y: 6, scale: 0.96 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 4, scale: 0.96 }}
            transition={{ duration: 0.18 }}
            className="absolute top-full mt-2 right-0 w-72 p-3.5 rounded-xl bg-zinc-950 border border-zinc-800 shadow-2xl z-[900] text-zinc-300 font-mono text-xs"
          >
            {/* Header */}
            <div className="flex items-center justify-between pb-2 mb-2 border-b border-zinc-800">
              <div className="flex flex-col">
                <span className="font-bold text-white text-[11px] uppercase tracking-wider">
                  {WEATHER_I18N.RAIN_FORECAST[lang]}
                </span>
                {desc && (
                  <span className="text-[10px] text-zinc-400 capitalize">
                    {desc}
                  </span>
                )}
              </div>
              <button
                type="button"
                onClick={() => setIsOpen(false)}
                className="text-zinc-500 hover:text-white p-0.5 transition-colors cursor-pointer"
                aria-label="Close"
              >
                ✕
              </button>
            </div>

            {/* Current Metrics Grid */}
            <div className="grid grid-cols-2 gap-2 pb-3 mb-3 border-b border-zinc-800/80 text-[11px]">
              <div>
                <span className="text-zinc-500 block text-[10px]">{WEATHER_I18N.FEELS_LIKE[lang]}</span>
                <span className="font-semibold text-zinc-200">{formatTemperature(weather.apparent_temperature_c)}</span>
              </div>
              <div>
                <span className="text-zinc-500 block text-[10px]">{WEATHER_I18N.HUMIDITY[lang]}</span>
                <span className="font-semibold text-zinc-200">{weather.humidity_percent}%</span>
              </div>
              <div>
                <span className="text-zinc-500 block text-[10px]">{WEATHER_I18N.RAIN_CHANCE[lang]}</span>
                <span className="font-bold text-sky-400">{formatRainProbability(weather.precipitation_probability_percent)}</span>
              </div>
              <div>
                <span className="text-zinc-500 block text-[10px]">{WEATHER_I18N.PRECIPITATION[lang]}</span>
                <span className="font-semibold text-zinc-200">{weather.rain_mm.toFixed(1)} mm</span>
              </div>
            </div>

            {/* Hourly Forecast (Next 6h) */}
            {hourly.length > 0 && (
              <div className="mb-2">
                <span className="text-[10px] text-zinc-400 uppercase tracking-wider font-semibold block mb-2">
                  {WEATHER_I18N.HOURLY_TITLE[lang]}
                </span>
                <div className="grid grid-cols-6 gap-1 text-center">
                  {hourly.map((h, idx) => (
                    <div key={idx} className="flex flex-col items-center">
                      <span className="text-[9px] text-zinc-400">{formatHour(h.time)}</span>
                      {/* Bar indicator */}
                      <div className="w-2.5 h-12 bg-zinc-900 rounded-sm my-1 flex flex-col justify-end overflow-hidden">
                        <div
                          className="w-full bg-sky-500 transition-all rounded-xs"
                          style={{ height: `${Math.min(100, Math.max(8, h.precipitation_probability_percent))}%` }}
                        />
                      </div>
                      <span className="text-[9.5px] font-bold text-sky-400">
                        {h.precipitation_probability_percent}%
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Attribution */}
            <div className="pt-2 mt-1 border-t border-zinc-900 text-[9px] text-zinc-500 text-center">
              {WEATHER_I18N.ATTRIBUTION[lang]}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
