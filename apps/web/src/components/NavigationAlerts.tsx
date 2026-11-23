'use client';

import { useState } from 'react';
import type { NavigationHazardAlert } from '../types/navigation.ts';
import { NAVIGATION_I18N } from '../types/navigation.ts';
import type { VehicleType } from '../types/vehicle.ts';
import { VEHICLE_PROFILES } from '../types/vehicle.ts';

export interface NavigationAlertsProps {
  corridorName: string;
  alerts: NavigationHazardAlert[];
  onSelectAlert?: (alert: NavigationHazardAlert) => void;
  onClose?: () => void;
  lang?: 'vi' | 'en';
  vehicleType?: VehicleType;
}

export default function NavigationAlerts({
  corridorName,
  alerts,
  onSelectAlert,
  onClose,
  lang = 'vi',
  vehicleType = 'scooter',
}: NavigationAlertsProps) {
  const [isCollapsed, setIsCollapsed] = useState(false);

  const profile = VEHICLE_PROFILES[vehicleType] || VEHICLE_PROFILES.scooter;
  const aheadAlerts = alerts.filter(a => a.isAhead);
  const hazardCount = aheadAlerts.filter(a => a.severity !== 'PASSABLE').length;

  const getVerdictStyle = (severity: string) => {
    switch (severity) {
      case 'IMPASSABLE':
        return {
          bg: 'bg-rose-950/70',
          border: 'border-rose-800/80',
          badgeBg: 'bg-rose-900/90 text-rose-200 border-rose-700',
          indicator: 'bg-rose-500 animate-pulse',
          label: NAVIGATION_I18N.IMPASSABLE_WARNING[lang],
        };
      case 'CAUTION':
        return {
          bg: 'bg-amber-950/70',
          border: 'border-amber-800/80',
          badgeBg: 'bg-amber-900/90 text-amber-200 border-amber-700',
          indicator: 'bg-amber-500',
          label: NAVIGATION_I18N.CAUTION_WARNING[lang],
        };
      case 'PASSABLE':
      default:
        return {
          bg: 'bg-zinc-900/80',
          border: 'border-zinc-800',
          badgeBg: 'bg-emerald-950/80 text-emerald-300 border-emerald-700',
          indicator: 'bg-emerald-500',
          label: NAVIGATION_I18N.PASSABLE_NOTICE[lang],
        };
    }
  };

  return (
    <section
      role="region"
      aria-label={NAVIGATION_I18N.TITLE[lang]}
      className="pointer-events-auto w-full max-w-lg mx-auto bg-zinc-950/95 border border-zinc-700/80 rounded-xl shadow-2xl backdrop-blur-md overflow-hidden text-zinc-100 transition-all font-sans"
    >
      {/* HEADER BAR */}
      <header className="px-4 py-2.5 bg-zinc-900/90 border-b border-zinc-800 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <span
            className={`w-2.5 h-2.5 rounded-full shrink-0 ${
              hazardCount > 0 ? 'bg-amber-400 animate-pulse' : 'bg-emerald-400'
            }`}
            aria-hidden="true"
          />
          <div className="min-w-0">
            <h2 className="text-xs font-semibold text-zinc-200 tracking-wide uppercase truncate">
              {NAVIGATION_I18N.TITLE[lang]}: {corridorName}
            </h2>
            <p className="text-[10px] text-zinc-400 truncate">
              {hazardCount > 0
                ? `${hazardCount} ${
                    hazardCount === 1
                      ? NAVIGATION_I18N.HAZARD_AHEAD_SINGULAR[lang]
                      : NAVIGATION_I18N.HAZARD_AHEAD_PLURAL[lang]
                  } (${profile.shortLabel[lang]})`
                : NAVIGATION_I18N.NO_HAZARDS[lang]}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-1 shrink-0">
          {/* Collapse/Expand button (>=48px touch target) */}
          <button
            type="button"
            onClick={() => setIsCollapsed(!isCollapsed)}
            aria-label={
              isCollapsed
                ? NAVIGATION_I18N.EXPAND[lang]
                : NAVIGATION_I18N.COLLAPSE[lang]
            }
            className="min-h-[48px] min-w-[48px] px-2.5 py-1.5 text-zinc-400 hover:text-zinc-100 flex items-center justify-center transition-colors rounded-lg hover:bg-zinc-800 cursor-pointer"
          >
            <svg
              className={`w-4 h-4 transition-transform duration-200 ${
                isCollapsed ? 'rotate-180' : ''
              }`}
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7" />
            </svg>
          </button>

          {/* Close button (>=48px touch target) */}
          {onClose && (
            <button
              type="button"
              onClick={onClose}
              aria-label={NAVIGATION_I18N.CLOSE[lang]}
              className="min-h-[48px] min-w-[48px] px-2.5 py-1.5 text-zinc-400 hover:text-zinc-100 flex items-center justify-center transition-colors rounded-lg hover:bg-zinc-800 cursor-pointer"
            >
              <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          )}
        </div>
      </header>

      {/* BODY CONTENT */}
      {!isCollapsed && (
        <div className="p-3 max-h-72 overflow-y-auto space-y-2 scrollbar-thin">
          {alerts.length === 0 ? (
            <div className="p-4 text-center text-xs text-zinc-400">
              {NAVIGATION_I18N.NO_HAZARDS[lang]}
            </div>
          ) : (
            alerts.map((alert) => {
              const style = getVerdictStyle(alert.severity);

              return (
                <div
                  key={alert.id}
                  className={`p-2.5 rounded-lg border transition-all ${style.bg} ${style.border} flex flex-col gap-1.5`}
                >
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <span className={`w-2 h-2 rounded-full shrink-0 ${style.indicator}`} />
                      <div className="min-w-0">
                        <span className="text-xs font-semibold text-zinc-100 block truncate">
                          {alert.name}
                        </span>
                        <span className="text-[10px] text-zinc-400 font-mono block">
                          {alert.isAhead
                            ? `${NAVIGATION_I18N.AHEAD[lang]} ${alert.distanceFormatted[lang]}`
                            : NAVIGATION_I18N.PASSED[lang]}
                        </span>
                      </div>
                    </div>

                    {/* Passability badge */}
                    <span
                      className={`px-2 py-0.5 text-[10px] font-bold tracking-wider rounded border shrink-0 ${style.badgeBg}`}
                    >
                      {style.label}
                    </span>
                  </div>

                  {/* Water depth and clearance metrics if flooded */}
                  {alert.depthCm && alert.depthCm > 0 ? (
                    <div className="text-[11px] flex items-center justify-between text-zinc-300 bg-zinc-900/60 px-2 py-1 rounded border border-zinc-800/80">
                      <span>
                        Mực nước: <strong className="text-zinc-100">{alert.depthCm} cm</strong>
                      </span>
                      {alert.excessDepthCm !== null && alert.excessDepthCm !== undefined && alert.excessDepthCm > 0 && (
                        <span className="text-rose-400 font-semibold text-[10px]">
                          +{alert.excessDepthCm} cm {lang === 'vi' ? 'vượt gầm' : 'over limit'}
                        </span>
                      )}
                    </div>
                  ) : null}

                  {/* Actionable Advice & Map Pan CTA */}
                  <div className="flex items-center justify-between gap-2 pt-1 border-t border-zinc-800/60">
                    <p className="text-[10px] text-zinc-300 leading-snug truncate">
                      {alert.actionableAdvice[lang]}
                    </p>

                    {onSelectAlert && (
                      <button
                        type="button"
                        onClick={() => onSelectAlert(alert)}
                        aria-label={`${NAVIGATION_I18N.VIEW_ON_MAP[lang]}: ${alert.name}`}
                        className="min-h-[48px] min-w-[48px] px-3 py-1.5 text-[11px] font-medium text-cyan-400 hover:text-cyan-300 hover:underline shrink-0 flex items-center justify-center transition-colors cursor-pointer"
                      >
                        {NAVIGATION_I18N.VIEW_ON_MAP[lang]}
                      </button>
                    )}
                  </div>
                </div>
              );
            })
          )}
        </div>
      )}
    </section>
  );
}
