'use client';
import React from 'react';
import { CAMERA_FILTER_I18N } from '../utils/cameraFilterUtils';

interface CameraFilterProps {
  hideOfflineCameras: boolean;
  onToggleHideOffline: () => void;
  visibleCount: number;
  totalCount: number;
  lang?: 'vi' | 'en';
}

export default function CameraFilter({
  hideOfflineCameras,
  onToggleHideOffline,
  visibleCount,
  totalCount,
  lang = 'vi',
}: CameraFilterProps) {
  const counterText = CAMERA_FILTER_I18N.COUNTER[lang](visibleCount, totalCount);
  const counterCompactText = CAMERA_FILTER_I18N.COUNTER_COMPACT[lang](visibleCount, totalCount);
  const buttonText = hideOfflineCameras
    ? CAMERA_FILTER_I18N.SHOW_OFFLINE_BTN[lang]
    : CAMERA_FILTER_I18N.HIDE_OFFLINE_BTN[lang];
  const buttonCompactText = hideOfflineCameras
    ? CAMERA_FILTER_I18N.SHOW_OFFLINE_BTN_COMPACT[lang]
    : CAMERA_FILTER_I18N.HIDE_OFFLINE_BTN_COMPACT[lang];
  const tooltipText = CAMERA_FILTER_I18N.TOOLTIP[lang](hideOfflineCameras);

  return (
    <div className="flex items-center gap-1.5 pointer-events-auto">
      {/* Camera Counter Badge */}
      <div
        className="bg-zinc-950/95 border border-zinc-700 text-zinc-300 px-2.5 py-1.5 rounded-lg shadow-xl backdrop-blur-md font-mono text-xs select-none flex items-center gap-2 transition-all"
        role="status"
        aria-live="polite"
      >
        <span
          className={`w-1.5 h-1.5 rounded-full shrink-0 ${
            hideOfflineCameras ? 'bg-emerald-400' : 'bg-zinc-500'
          }`}
        />
        <span className="font-semibold text-zinc-200 text-[11px] md:text-xs whitespace-nowrap">
          <span className="hidden sm:inline">{counterText}</span>
          <span className="sm:hidden">{counterCompactText}</span>
        </span>
      </div>

      {/* Offline Filter Toggle Button */}
      <button
        type="button"
        onClick={onToggleHideOffline}
        title={tooltipText}
        aria-label={buttonText}
        aria-pressed={hideOfflineCameras}
        className={`px-2.5 py-1.5 rounded-lg font-mono text-[11px] md:text-xs font-semibold border shadow-xl backdrop-blur-md transition-all cursor-pointer flex items-center gap-1 active:scale-[0.98] select-none ${
          hideOfflineCameras
            ? 'bg-zinc-900/90 border-zinc-700 text-zinc-300 hover:border-zinc-500 hover:bg-zinc-800 hover:text-white'
            : 'bg-zinc-900/90 border-zinc-600 text-zinc-100 hover:bg-zinc-800 hover:border-zinc-400'
        }`}
      >
        <span className="leading-none">
          <span className="hidden sm:inline">{buttonText}</span>
          <span className="sm:hidden">{buttonCompactText}</span>
        </span>
      </button>
    </div>
  );
}
