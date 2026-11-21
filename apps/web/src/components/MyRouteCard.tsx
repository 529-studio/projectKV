'use client';

import { useState } from 'react';
import type { CommuteConfig, CommuteEvaluation } from '../types/commute.ts';
import { COMMUTE_I18N } from '../types/commute.ts';
import type { VehicleType } from '../types/vehicle.ts';
import { VEHICLE_PROFILES } from '../types/vehicle.ts';
import { formatCommuteDuration, formatCommuteDelay } from '../utils/commuteRouteUtils.ts';
import { normalizeBotUsername } from '../utils/subscriptionUtils.ts';

interface MyRouteCardProps {
  config: CommuteConfig;
  evaluation: CommuteEvaluation | null;
  onClose: () => void;
  onOpenSetup: () => void;
  onToggleDirection: () => void;
  onSelectDetour?: (detourCorridorKey: string) => void;
  onSubscribeRoute?: (corridorKey: string) => void;
  lang?: 'vi' | 'en';
  vehicleType?: VehicleType;
}

export default function MyRouteCard({
  config,
  evaluation,
  onClose,
  onOpenSetup,
  onToggleDirection,
  onSelectDetour,
  onSubscribeRoute,
  lang = 'vi',
  vehicleType = 'scooter',
}: MyRouteCardProps) {
  const [isCollapsed, setIsCollapsed] = useState(false);

  const vehicleProfile = VEHICLE_PROFILES[vehicleType] || VEHICLE_PROFILES.scooter;
  const isToWork = config.direction === 'to_work';
  const originName = isToWork ? config.home?.name || 'Nhà' : config.work?.name || 'Cơ quan';
  const destName = isToWork ? config.work?.name || 'Cơ quan' : config.home?.name || 'Nhà';

  const verdict = evaluation?.passability || 'UNKNOWN';

  const getVerdictBadge = () => {
    switch (verdict) {
      case 'PASSABLE':
        return {
          label: lang === 'vi' ? 'THÔNG THOÁNG' : 'PASSABLE',
          subLabel: lang === 'vi' ? 'Hành trình an toàn' : 'Safe commute',
          bg: 'bg-emerald-950/80',
          border: 'border-emerald-600',
          text: 'text-emerald-300',
          indicator: 'bg-emerald-500',
        };
      case 'CAUTION':
        return {
          label: lang === 'vi' ? 'CẢNH BÁO XE GẦM THẤP' : 'CAUTION (LOW CLEARANCE)',
          subLabel: lang === 'vi' ? 'Ngập nhẹ hoặc ùn tắc' : 'Minor flood or slow traffic',
          bg: 'bg-amber-950/80',
          border: 'border-amber-600',
          text: 'text-amber-300',
          indicator: 'bg-amber-500',
        };
      case 'IMPASSABLE':
        return {
          label: lang === 'vi' ? 'NGUY CƠ CHẾT MÁY' : 'IMPASSABLE (STALL RISK)',
          subLabel: lang === 'vi' ? 'Ngập sâu vượt gầm xe' : 'Deep water exceeds clearance',
          bg: 'bg-rose-950/80',
          border: 'border-rose-600',
          text: 'text-rose-300',
          indicator: 'bg-rose-500 animate-pulse',
        };
      case 'UNKNOWN':
      default:
        return {
          label: lang === 'vi' ? 'CHƯA ĐỦ DỮ LIỆU' : 'UNKNOWN STATUS',
          subLabel: lang === 'vi' ? 'Đang cập nhật giám sát' : 'Awaiting sensor sync',
          bg: 'bg-zinc-900',
          border: 'border-zinc-700',
          text: 'text-zinc-400',
          indicator: 'bg-zinc-500',
        };
    }
  };

  const badge = getVerdictBadge();

  // Telegram alert deep-link
  const primaryCorridor = config.selectedCorridors[0] || 'vo_van_kiet';
  const botUsername =
    normalizeBotUsername(process.env.NEXT_PUBLIC_TELEGRAM_BOT_USERNAME) || 'WardedBot';
  const telegramDeepLink = `https://t.me/${botUsername}?start=sub_corridor_${primaryCorridor}`;

  return (
    <section
      role="region"
      aria-label={COMMUTE_I18N.TITLE[lang]}
      className="pointer-events-auto w-full max-w-lg mx-auto bg-zinc-950/95 border border-zinc-700/80 rounded-xl shadow-2xl backdrop-blur-md overflow-hidden text-zinc-100 transition-all font-sans"
    >
      {/* HEADER BAR */}
      <header className="px-4 py-2.5 bg-zinc-900/90 border-b border-zinc-800 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <span className="w-2.5 h-2.5 rounded-full bg-cyan-400 shrink-0" aria-hidden="true" />
          <h2 className="text-xs font-semibold text-zinc-200 tracking-wide uppercase truncate">
            {COMMUTE_I18N.TITLE[lang]}
          </h2>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          {/* DIRECTION SWITCH BUTTON (>=48px touch target) */}
          <button
            type="button"
            onClick={onToggleDirection}
            aria-label={COMMUTE_I18N.SWITCH_DIRECTION[lang]}
            title={isToWork ? COMMUTE_I18N.DIRECTION_TO_HOME[lang] : COMMUTE_I18N.DIRECTION_TO_WORK[lang]}
            className="min-h-[48px] min-w-[48px] px-3 py-1.5 text-xs rounded-lg bg-zinc-800 hover:bg-zinc-700 border border-zinc-700 text-zinc-300 hover:text-zinc-100 flex items-center justify-center gap-1.5 transition-colors cursor-pointer"
          >
            <svg
              className="w-3.5 h-3.5 text-zinc-400"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M7 16V4m0 0L3 8m4-4l4 4m6 4v12m0 0l4-4m-4 4l-4-4" />
            </svg>
            <span>{isToWork ? (lang === 'vi' ? 'Đi làm' : 'To work') : (lang === 'vi' ? 'Về nhà' : 'To home')}</span>
          </button>

          {/* SETUP / CHANGE ROUTE (>=48px touch target) */}
          <button
            type="button"
            onClick={onOpenSetup}
            aria-label={COMMUTE_I18N.CHANGE_ROUTE[lang]}
            title={COMMUTE_I18N.CHANGE_ROUTE[lang]}
            className="min-w-[48px] min-h-[48px] flex items-center justify-center rounded-lg hover:bg-zinc-800 text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
          >
            <svg
              className="w-4 h-4"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
              <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
          </button>

          {/* COLLAPSE TOGGLE (>=48px touch target) */}
          <button
            type="button"
            onClick={() => setIsCollapsed(prev => !prev)}
            aria-label={isCollapsed ? (lang === 'vi' ? 'Mở rộng thẻ lộ trình' : 'Expand commute card') : (lang === 'vi' ? 'Thu gọn thẻ lộ trình' : 'Collapse commute card')}
            className="min-w-[48px] min-h-[48px] flex items-center justify-center rounded-lg hover:bg-zinc-800 text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
          >
            <svg
              className={`w-4 h-4 transition-transform duration-200 ${isCollapsed ? 'rotate-180' : ''}`}
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
          </button>

          {/* CLOSE BUTTON (>=48px touch target) */}
          <button
            type="button"
            onClick={onClose}
            aria-label={lang === 'vi' ? 'Đóng thẻ lộ trình' : 'Close commute card'}
            className="min-w-[48px] min-h-[48px] flex items-center justify-center rounded-lg hover:bg-zinc-800 text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
          >
            <svg
              className="w-4 h-4"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>
      </header>

      {/* BODY CONTENT */}
      {!isCollapsed && (
        <div className="p-3.5 space-y-3">
          {/* ORIGIN ➔ DESTINATION GLANCE */}
          <div className="flex items-center justify-between text-xs text-zinc-300 pb-1 border-b border-zinc-800/80">
            <div className="truncate max-w-[45%] font-medium text-zinc-200" title={originName}>
              <span className="text-[10px] text-zinc-500 uppercase block">{isToWork ? COMMUTE_I18N.HOME[lang] : COMMUTE_I18N.WORK[lang]}</span>
              {originName}
            </div>
            <div className="shrink-0 px-2 text-zinc-500" aria-hidden="true">➔</div>
            <div className="truncate max-w-[45%] text-right font-medium text-zinc-200" title={destName}>
              <span className="text-[10px] text-zinc-500 uppercase block">{isToWork ? COMMUTE_I18N.WORK[lang] : COMMUTE_I18N.HOME[lang]}</span>
              {destName}
            </div>
          </div>

          {/* MAIN 3-SECOND PASSABILITY VERDICT BADGE */}
          <div className={`p-3 rounded-lg border ${badge.border} ${badge.bg} flex items-center justify-between gap-3`}>
            <div className="flex items-center gap-2.5">
              <span className={`w-3 h-3 rounded-full ${badge.indicator} shrink-0`} aria-hidden="true" />
              <div>
                <span className={`text-sm font-bold tracking-tight block ${badge.text}`}>
                  {badge.label}
                </span>
                <span className="text-[11px] text-zinc-400 block">
                  {badge.subLabel}
                </span>
              </div>
            </div>

            {/* VEHICLE PROFILE BADGE */}
            <div className="text-right shrink-0">
              <span className="inline-block px-2 py-0.5 rounded text-[10px] font-medium bg-zinc-900 border border-zinc-700 text-zinc-300">
                {vehicleProfile.name[lang]} (Gầm {vehicleProfile.groundClearanceCm} cm)
              </span>
            </div>
          </div>

          {/* STATS ROW: TIME, DELAY, DISTANCE, RAIN */}
          <div className="grid grid-cols-3 gap-2 text-center">
            <div className="p-2 rounded bg-zinc-900/70 border border-zinc-800">
              <span className="text-[10px] uppercase tracking-wider text-zinc-500 block">Thời gian</span>
              <span className="text-xs font-semibold text-zinc-100">
                {evaluation ? formatCommuteDuration(evaluation.estimated_seconds, lang) : '--'}
              </span>
              <span className="text-[10px] text-amber-400 block">
                {evaluation ? formatCommuteDelay(evaluation.delay_seconds, lang) : ''}
              </span>
            </div>

            <div className="p-2 rounded bg-zinc-900/70 border border-zinc-800">
              <span className="text-[10px] uppercase tracking-wider text-zinc-500 block">Cự ly</span>
              <span className="text-xs font-semibold text-zinc-100">
                {evaluation ? `${evaluation.total_distance_km} km` : '--'}
              </span>
              <span className="text-[10px] text-zinc-400 block">
                {config.selectedCorridors.length} trục đường
              </span>
            </div>

            <div className="p-2 rounded bg-zinc-900/70 border border-zinc-800">
              <span className="text-[10px] uppercase tracking-wider text-zinc-500 block">Thời tiết</span>
              <span className="text-xs font-semibold text-zinc-100">
                {evaluation && evaluation.rain_probability_pct > 0 ? `Mưa ${evaluation.rain_probability_pct}%` : COMMUTE_I18N.NO_RAIN[lang]}
              </span>
              <span className="text-[10px] text-cyan-400 block truncate">
                {evaluation ? evaluation.rain_summary[lang] : ''}
              </span>
            </div>
          </div>

          {/* BOTTLENECK CALLOUT (IF DANGER OR HEAVY CONGESTION) */}
          {evaluation?.worst_bottleneck && (
            <div className="p-2.5 rounded-lg bg-rose-950/40 border border-rose-800/80 text-xs flex items-start gap-2">
              <svg
                className="w-4 h-4 text-rose-400 shrink-0 mt-0.5"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                viewBox="0 0 24 24"
                aria-hidden="true"
              >
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
              <div className="space-y-0.5">
                <span className="font-semibold text-rose-300 block">
                  {COMMUTE_I18N.BOTTLENECK_TITLE[lang]}
                </span>
                <p className="text-rose-200/90 text-[11px] leading-relaxed">
                  {evaluation.worst_bottleneck.description[lang]}
                </p>
              </div>
            </div>
          )}

          {/* DETOUR RECOMMENDATION (IF DETOUR AVAILABLE) */}
          {evaluation?.detour && (
            <div className="p-2.5 rounded-lg bg-emerald-950/40 border border-emerald-800/80 text-xs flex items-center justify-between gap-2">
              <div className="space-y-0.5 min-w-0">
                <span className="font-semibold text-emerald-300 block">
                  {COMMUTE_I18N.DETOUR_AVAILABLE[lang]}
                </span>
                <p className="text-emerald-200/90 text-[11px] truncate">
                  {evaluation.detour.reason[lang]}
                </p>
              </div>

              {onSelectDetour && (
                <button
                  type="button"
                  onClick={() => onSelectDetour(evaluation.detour!.detour_corridor_key)}
                  className="min-h-[48px] px-3.5 py-2 text-xs font-semibold rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white shrink-0 transition-colors cursor-pointer shadow flex items-center justify-center"
                >
                  {COMMUTE_I18N.VIEW_DETOUR[lang]}
                </button>
              )}
            </div>
          )}

          {/* BOTTOM ACTIONS: TELEGRAM ALERT & ZERO TELEMETRY */}
          <div className="pt-1 border-t border-zinc-800 flex items-center justify-between text-[11px] text-zinc-400">
            <a
              href={telegramDeepLink}
              target="_blank"
              rel="noopener noreferrer"
              onClick={e => {
                if (onSubscribeRoute) {
                  e.preventDefault();
                  onSubscribeRoute(primaryCorridor);
                }
              }}
              className="min-h-[48px] inline-flex items-center gap-1.5 text-cyan-400 hover:text-cyan-300 font-medium transition-colors py-2 cursor-pointer"
            >
              <svg className="w-3.5 h-3.5 fill-current" viewBox="0 0 24 24" aria-hidden="true">
                <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm4.64 6.8c-.15 1.58-.8 5.42-1.13 7.19-.14.75-.42 1-.68 1.03-.58.05-1.02-.38-1.58-.75-.88-.58-1.38-.94-2.23-1.5-.99-.65-.35-1.01.22-1.59.15-.15 2.71-2.48 2.76-2.69a.2.2 0 00-.05-.18c-.06-.05-.14-.03-.21-.02-.09.02-1.49.95-4.22 2.79-.4.27-.76.41-1.08.4-.36-.01-1.04-.2-1.55-.37-.63-.2-1.12-.31-1.08-.66.02-.18.27-.36.75-.55 2.92-1.27 4.86-2.11 5.83-2.51 2.78-1.16 3.35-1.36 3.73-1.36.08 0 .27.02.39.12.1.08.13.19.14.27-.01.06.01.24 0 .38z" />
              </svg>
              <span>{COMMUTE_I18N.SUBSCRIBE_TELEGRAM[lang]}</span>
            </a>

            <span className="text-[10px] text-zinc-500 truncate" title={COMMUTE_I18N.ZERO_TELEMETRY[lang]}>
              100% Private (localStorage)
            </span>
          </div>
        </div>
      )}
    </section>
  );
}
