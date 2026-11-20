'use client';

import { useState } from 'react';
import type { CorridorRouteSummary } from '../types/corridor.ts';
import type { Camera } from '../hooks/useCameras.ts';
import { normalizeBotUsername } from '../utils/subscriptionUtils.ts';
import { getApiUrl, API_ENDPOINTS } from '../config/constants.ts';

interface CorridorOverviewCardProps {
  summary: CorridorRouteSummary | null;
  corridorCameras?: Camera[];
  onClose: () => void;
  onCameraClick?: (camera: Camera) => void;
  lang?: 'vi' | 'en';
  showPolyline?: boolean;
  onTogglePolyline?: () => void;
}

export default function CorridorOverviewCard({
  summary,
  corridorCameras = [],
  onClose,
  onCameraClick,
  lang = 'vi',
  showPolyline = true,
  onTogglePolyline,
}: CorridorOverviewCardProps) {
  const [isCollapsed, setIsCollapsed] = useState(false);

  if (!summary) return null;

  const estMinutes = Math.round(summary.estimated_seconds / 60);
  const freeFlowMinutes = Math.round(summary.free_flow_seconds / 60);
  const delayMinutes = Math.round(summary.delay_seconds / 60);

  // Verdict translation and visual badge styling
  const getVerdictBadge = () => {
    switch (summary.passability_verdict) {
      case 'PASSABLE':
        return {
          label: lang === 'vi' ? 'THÔNG THOÁNG' : 'PASSABLE',
          bg: 'bg-emerald-950/80',
          border: 'border-emerald-600',
          text: 'text-emerald-300',
          indicator: 'bg-emerald-500',
        };
      case 'CAUTION':
        return {
          label: lang === 'vi' ? 'CẢNH BÁO XE GẦM THẤP' : 'CAUTION (LOW CLEARANCE)',
          bg: 'bg-amber-950/80',
          border: 'border-amber-600',
          text: 'text-amber-300',
          indicator: 'bg-amber-500',
        };
      case 'IMPASSABLE':
        return {
          label: lang === 'vi' ? 'NGUY CƠ CHẾT MÁY / THỦY KÍCH' : 'IMPASSABLE (DEEP FLOOD)',
          bg: 'bg-rose-950/80',
          border: 'border-rose-600',
          text: 'text-rose-300',
          indicator: 'bg-rose-500 animate-pulse',
        };
      case 'UNKNOWN':
      default:
        return {
          label: lang === 'vi' ? 'CHƯA ĐỦ DỮ LIỆU' : 'UNKNOWN',
          bg: 'bg-zinc-900',
          border: 'border-zinc-700',
          text: 'text-zinc-400',
          indicator: 'bg-zinc-500',
        };
    }
  };

  const badge = getVerdictBadge();

  // Telegram deep link construction
  const botUsername =
    normalizeBotUsername(process.env.NEXT_PUBLIC_TELEGRAM_BOT_USERNAME) || 'WardedBot';
  const telegramDeepLink = `https://t.me/${botUsername}?start=sub_corridor_${summary.road_key}`;

  // Take first 4-5 corridor cameras for checkpoint strip
  const checkpointCams = corridorCameras.slice(0, 5);

  return (
    <section
      aria-label={lang === 'vi' ? 'Tổng quan trục đường' : 'Corridor Overview'}
      className="relative pointer-events-auto w-full sm:w-[380px] bg-zinc-950/95 backdrop-blur-md border border-zinc-800 rounded-lg shadow-2xl font-mono text-white transition-all duration-200"
    >
      {/* Header bar */}
      <div className="flex items-center justify-between p-3 border-b border-zinc-800/80">
        <div className="flex-1 min-w-0 pr-2">
          <div className="flex items-center gap-2">
            <span className={`w-2.5 h-2.5 rounded-full ${badge.indicator} shrink-0`} />
            <h2 className="text-sm font-bold text-zinc-100 truncate tracking-wide">
              {summary.display_name}
            </h2>
          </div>
          <p className="text-[11px] text-zinc-400 truncate mt-0.5">{summary.description}</p>
        </div>

        <div className="flex items-center gap-1 shrink-0">
          {/* Route line visibility toggle (>=48px touch target) */}
          {onTogglePolyline && (
            <button
              type="button"
              onClick={onTogglePolyline}
              className={`min-w-[48px] min-h-[48px] flex items-center justify-center rounded transition-colors ${
                showPolyline
                  ? 'text-emerald-400 hover:text-emerald-300 hover:bg-zinc-800/50'
                  : 'text-zinc-500 hover:text-zinc-300 hover:bg-zinc-800/50'
              }`}
              aria-label={
                showPolyline
                  ? lang === 'vi'
                    ? 'Tắt đường kẻ lộ trình'
                    : 'Hide route line'
                  : lang === 'vi'
                  ? 'Bật đường kẻ lộ trình'
                  : 'Show route line'
              }
              title={
                showPolyline
                  ? lang === 'vi'
                    ? 'Tắt đường kẻ lộ trình'
                    : 'Hide route line'
                  : lang === 'vi'
                  ? 'Bật đường kẻ lộ trình'
                  : 'Show route line'
              }
            >
              {showPolyline ? (
                <svg className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth={2} viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                  <path strokeLinecap="round" strokeLinejoin="round" d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                </svg>
              ) : (
                <svg className="w-4 h-4" fill="none" stroke="currentColor" strokeWidth={2} viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l18 18" />
                </svg>
              )}
            </button>
          )}

          {/* Collapse/Expand Toggle (>=48px touch target) */}
          <button
            type="button"
            onClick={() => setIsCollapsed(!isCollapsed)}
            className="min-w-[48px] min-h-[48px] flex items-center justify-center text-zinc-400 hover:text-white transition-colors"
            aria-label={
              isCollapsed
                ? lang === 'vi'
                  ? 'Mở rộng chi tiết trục đường'
                  : 'Expand corridor card'
                : lang === 'vi'
                ? 'Thu gọn chi tiết trục đường'
                : 'Collapse corridor card'
            }
          >
            <svg
              className={`w-4 h-4 transition-transform duration-200 ${
                isCollapsed ? 'rotate-180' : ''
              }`}
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M19 9l-7 7-7-7"
              />
            </svg>
          </button>

          {/* Close button (>=48px touch target) */}
          <button
            type="button"
            onClick={onClose}
            className="min-w-[48px] min-h-[48px] flex items-center justify-center text-zinc-400 hover:text-white hover:bg-zinc-800/50 rounded transition-colors"
            aria-label={lang === 'vi' ? 'Đóng tổng quan trục đường' : 'Close corridor overview'}
          >
            <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M6 18L18 6M6 6l12 12"
              />
            </svg>
          </button>
        </div>
      </div>

      {/* Body content */}
      {!isCollapsed && (
        <div className="p-3 space-y-3">
          {/* Passability Verdict Badge */}
          <div
            className={`flex items-center justify-between px-3 py-2 rounded border ${badge.bg} ${badge.border}`}
          >
            <span className="text-[11px] text-zinc-300 font-medium">
              {lang === 'vi' ? 'Khả năng lưu thông:' : 'Passability:'}
            </span>
            <span className={`text-xs font-bold tracking-wider ${badge.text}`}>
              {badge.label}
            </span>
          </div>

          {/* Key Metrics Grid */}
          <div className="grid grid-cols-2 gap-2 text-xs">
            {/* Travel Duration */}
            <div className="bg-zinc-900/80 p-2 rounded border border-zinc-800/60">
              <span className="text-[10px] text-zinc-400 block mb-0.5">
                {lang === 'vi' ? 'Thời gian di chuyển:' : 'Est. Travel Time:'}
              </span>
              <div className="flex items-baseline gap-1">
                <span className="text-base font-bold text-white">{estMinutes}m</span>
                <span className="text-[11px] text-zinc-500">({freeFlowMinutes}m std)</span>
              </div>
              {delayMinutes > 0 && (
                <span className="text-[10px] font-semibold text-amber-400 block mt-0.5">
                  +{delayMinutes} {lang === 'vi' ? 'phút chậm trễ' : 'min delay'}
                </span>
              )}
            </div>

            {/* Flood Depth */}
            <div className="bg-zinc-900/80 p-2 rounded border border-zinc-800/60">
              <span className="text-[10px] text-zinc-400 block mb-0.5">
                {lang === 'vi' ? 'Mực nước ngập đỉnh:' : 'Peak Flood Depth:'}
              </span>
              <div className="flex items-baseline gap-1">
                {summary.max_depth_cm && summary.max_depth_cm > 0 ? (
                  <span className="text-base font-bold text-rose-400">
                    {summary.max_depth_cm.toFixed(0)} cm
                  </span>
                ) : (
                  <span className="text-base font-semibold text-emerald-400">
                    {lang === 'vi' ? 'Khô ráo' : 'Dry'}
                  </span>
                )}
              </div>
              <span className="text-[10px] text-zinc-500 block mt-0.5">
                {summary.length_km.toFixed(1)} km · {summary.coverage_pct.toFixed(0)}%{' '}
                {lang === 'vi' ? 'phủ sóng' : 'coverage'}
              </span>
            </div>
          </div>

          {/* Camera Checkpoint Strip */}
          {checkpointCams.length > 0 && (
            <div>
              <div className="flex items-center justify-between text-[11px] text-zinc-400 mb-1.5">
                <span>{lang === 'vi' ? 'Điểm quan sát camera:' : 'Corridor Checkpoints:'}</span>
                <span className="text-zinc-500 text-[10px]">
                  {summary.verified_cameras}/{summary.total_cameras} {lang === 'vi' ? 'xác thực' : 'verified'}
                </span>
              </div>
              <div className="flex gap-1.5 overflow-x-auto pb-1 scrollbar-thin">
                {checkpointCams.map((cam) => {
                  const snapshotUrl = getApiUrl(API_ENDPOINTS.SNAPSHOT(cam.id));
                  return (
                    <button
                      key={cam.id}
                      type="button"
                      onClick={() => onCameraClick?.(cam)}
                      className="relative min-w-[72px] h-[52px] rounded border border-zinc-800 bg-zinc-900 overflow-hidden shrink-0 group focus:outline-none focus:ring-1 focus:ring-white cursor-pointer"
                      title={cam.displayName || cam.title}
                    >
                      <img
                        src={snapshotUrl}
                        alt={cam.displayName || cam.title || 'Camera checkpoint'}
                        className="w-full h-full object-cover group-hover:scale-105 transition-transform"
                        loading="lazy"
                      />
                      <div className="absolute inset-x-0 bottom-0 bg-zinc-950/80 px-1 py-0.5 text-[9px] text-zinc-200 truncate">
                        {(cam.displayName || cam.title || '').split(' - ')[1] || 'Camera'}
                      </div>
                    </button>
                  );
                })}
              </div>
            </div>
          )}

          {/* Telegram Alert CTA Button (>=48px touch target) */}
          <a
            href={telegramDeepLink}
            target="_blank"
            rel="noopener noreferrer"
            className="w-full min-h-[48px] px-4 py-2.5 rounded bg-zinc-900 hover:bg-zinc-800 border border-zinc-700 hover:border-zinc-500 text-xs font-semibold text-zinc-200 hover:text-white flex items-center justify-center gap-2 transition-colors cursor-pointer"
          >
            <svg
              className="w-4 h-4 text-sky-400 shrink-0"
              fill="currentColor"
              viewBox="0 0 24 24"
            >
              <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm4.64 6.8c-.15 1.58-.8 5.42-1.13 7.19-.14.75-.42 1-.68 1.03-.58.05-1.02-.38-1.58-.75-.88-.58-1.38-.94-2.23-1.5-.99-.65-.35-1.01.22-1.59.15-.15 2.71-2.48 2.76-2.69a.2.2 0 00-.05-.18c-.06-.05-.14-.03-.21-.02-.09.02-1.49.95-4.22 2.79-.4.27-.76.41-1.08.4-.36-.01-1.04-.2-1.55-.37-.63-.2-1.12-.31-1.08-.66.02-.18.27-.36.74-.55 2.92-1.27 4.86-2.11 5.83-2.52 2.77-1.18 3.35-1.39 3.73-1.39.08 0 .27.02.39.12.1.08.13.19.14.27-.01.06.01.24 0 .37z" />
            </svg>
            <span>
              {lang === 'vi'
                ? 'Nhận cảnh báo trục đường qua Telegram'
                : 'Get corridor alerts via Telegram'}
            </span>
          </a>
        </div>
      )}
    </section>
  );
}
