'use client';
import { motion, AnimatePresence } from 'framer-motion';

import { FUNCTIONAL_COLORS, VERDICT_I18N, HEX_I18N } from '../config/constants';
import { FLOOD_I18N } from '../utils/floodUtils';
import { WEATHER_I18N } from '../utils/weatherUtils';

interface MapLegendProps {
  isOpen: boolean;
  onToggle: () => void;
  analysisEnabled?: boolean;
  hexOverlayEnabled?: boolean;
  hexOverlayMode?: 'traffic' | 'flood';
  verdictsCount?: number;
  lang?: 'vi' | 'en';
  floodLoading?: boolean;
  trafficLoading?: boolean;
}

interface LegendContentProps {
  lang: 'vi' | 'en';
  analysisEnabled: boolean;
  hexOverlayEnabled: boolean;
  hexOverlayMode: 'traffic' | 'flood';
  verdictsCount: number;
  floodLoading?: boolean;
  trafficLoading?: boolean;
}

function LegendContent({
  lang,
  analysisEnabled,
  hexOverlayEnabled,
  hexOverlayMode,
  verdictsCount,
  floodLoading = false,
  trafficLoading = false,
}: LegendContentProps) {
  return (
    <>
      {/* Category 1: Non-Hot-Set default cameras (≥800 cameras) */}
      <div className="text-[10px] text-zinc-400 font-semibold uppercase tracking-wider pt-1">
        {lang === 'vi' ? 'Toàn mạng (Mặc định)' : 'Network Wide (Default)'}
      </div>
      <div className="flex items-center gap-2.5">
        <span
          className="w-3 h-3 rounded-full border border-black shrink-0"
          style={{ backgroundColor: FUNCTIONAL_COLORS.ONLINE_DEFAULT }}
        />
        <span>{lang === 'vi' ? 'Camera hoạt động' : 'Online camera'}</span>
      </div>
      <div className="flex items-center gap-2.5">
        <span
          className="w-3 h-3 rounded-full border border-black shrink-0"
          style={{ backgroundColor: FUNCTIONAL_COLORS.OFFLINE_DEFAULT }}
        />
        <span className="text-zinc-400">
          {lang === 'vi' ? 'Camera mất tín hiệu' : 'Offline camera'}
        </span>
      </div>

      {/* Category 2: Hot Set cameras with real-time AI verdict (Only rendered when analysisEnabled is true) */}
      {analysisEnabled && (
        <>
          <div className="flex items-center justify-between text-[10px] text-zinc-400 font-semibold uppercase tracking-wider pt-1.5 border-t border-zinc-800/80">
            <span>
              {lang === 'vi'
                ? 'Giám sát AI (Điểm trọng yếu / Đang xem)'
                : 'AI Monitoring (Hotspots / Active)'}
            </span>
            {verdictsCount === 0 && (
              <span className="flex items-center gap-1 text-amber-400 font-normal lowercase tracking-normal">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-400 animate-pulse" />
                <span>{lang === 'vi' ? 'đang tải...' : 'syncing...'}</span>
              </span>
            )}
          </div>
          <div className="flex items-center gap-2.5">
            <span
              className="w-3 h-3 rounded-full border border-black shrink-0"
              style={{ backgroundColor: FUNCTIONAL_COLORS.CLEAR }}
            />
            <span>{lang === 'vi' ? 'Thông thoáng' : 'Clear'}</span>
          </div>
          <div className="flex items-center gap-2.5">
            <span
              className="w-3 h-3 rounded-full border border-black shrink-0"
              style={{ backgroundColor: FUNCTIONAL_COLORS.MODERATE }}
            />
            <span>{lang === 'vi' ? 'Đông xe / Chậm' : 'Moderate'}</span>
          </div>
          <div className="flex items-center gap-2.5">
            <span
              className="w-3 h-3 rounded-full border border-black shrink-0"
              style={{ backgroundColor: FUNCTIONAL_COLORS.CONGESTED }}
            />
            <span>{lang === 'vi' ? 'Ùn tắc / Kẹt xe' : 'Congested'}</span>
          </div>
          <div className="flex items-center gap-2.5">
            <span
              className="w-3 h-3 rounded-full border-2 border-zinc-400 bg-transparent shrink-0"
            />
            <span className="text-zinc-400">
              {lang === 'vi' ? VERDICT_I18N.STALE_DESC.vi : VERDICT_I18N.STALE_DESC.en}
            </span>
          </div>
        </>
      )}

      {/* Category 3: H3 Hex Overlay (Only rendered when hexOverlayEnabled is true) */}
      {hexOverlayEnabled && (
        <>
          <div className="text-[10px] text-zinc-400 font-semibold uppercase tracking-wider pt-1.5 border-t border-zinc-800/80 flex items-center justify-between">
            <span>
              {hexOverlayMode === 'flood'
                ? (lang === 'vi' ? 'Khả năng có mưa (H3)' : 'Rain Forecast (H3)')
                : (lang === 'vi' ? 'Vùng tổng hợp (H3)' : 'Zone Overview (H3)')}
            </span>
            {hexOverlayMode === 'flood' && floodLoading && (
              <span className="flex items-center gap-1 text-sky-400 font-normal lowercase tracking-normal">
                <span className="w-1.5 h-1.5 rounded-full bg-sky-400 animate-pulse" />
                <span>{lang === 'vi' ? 'đang tải...' : 'syncing...'}</span>
              </span>
            )}
            {hexOverlayMode === 'traffic' && trafficLoading && (
              <span className="flex items-center gap-1 text-zinc-400 font-normal lowercase tracking-normal">
                <span className="w-1.5 h-1.5 rounded-full bg-zinc-400 animate-pulse" />
                <span>{lang === 'vi' ? 'đang tải...' : 'syncing...'}</span>
              </span>
            )}
          </div>
          {hexOverlayMode === 'flood' ? (
            <>
              <div className="flex items-center gap-2.5">
                <span
                  className="w-3.5 h-3.5 rounded-sm border border-blue-600/80 shrink-0"
                  style={{ backgroundColor: 'rgba(29, 78, 216, 0.45)' }}
                />
                <span className="text-zinc-300">{FLOOD_I18N.DEEP_WATER[lang]}</span>
              </div>
              <div className="flex items-center gap-2.5">
                <span
                  className="w-3.5 h-3.5 rounded-sm border border-sky-500/80 shrink-0"
                  style={{ backgroundColor: 'rgba(2, 132, 199, 0.4)' }}
                />
                <span className="text-zinc-300">{FLOOD_I18N.SHALLOW_WATER[lang]}</span>
              </div>
              <div className="flex items-center gap-2.5">
                <span
                  className="w-3.5 h-3.5 rounded-sm border border-slate-500/80 shrink-0"
                  style={{ backgroundColor: 'rgba(100, 116, 139, 0.3)' }}
                />
                <span className="text-zinc-300">{FLOOD_I18N.WET_ROAD[lang]}</span>
              </div>
              <div className="flex items-center gap-2.5 pt-1.5 mt-0.5 border-t border-zinc-800/80">
                <span
                  className="w-3.5 h-3.5 rounded-sm border border-sky-400/80 bg-sky-500/25 shrink-0"
                />
                <span className="text-zinc-300">{WEATHER_I18N.RADAR_LAYER[lang]}</span>
              </div>
              <div className="text-[9.5px] text-zinc-500 pt-0.5">
                {WEATHER_I18N.ATTRIBUTION[lang]}
              </div>
            </>
          ) : (
            <>
              <div className="flex items-center gap-2.5">
                <span
                  className="w-3.5 h-3.5 rounded-sm border border-emerald-500/60 shrink-0"
                  style={{ backgroundColor: 'rgba(34, 197, 94, 0.35)' }}
                />
                <span className="text-zinc-300">{HEX_I18N.ZONE_CLEAR[lang]}</span>
              </div>
              <div className="flex items-center gap-2.5">
                <span
                  className="w-3.5 h-3.5 rounded-sm border border-amber-500/60 shrink-0"
                  style={{ backgroundColor: 'rgba(234, 179, 8, 0.35)' }}
                />
                <span className="text-zinc-300">{HEX_I18N.ZONE_MODERATE[lang]}</span>
              </div>
              <div className="flex items-center gap-2.5">
                <span
                  className="w-3.5 h-3.5 rounded-sm border border-rose-500/60 shrink-0"
                  style={{ backgroundColor: 'rgba(239, 68, 68, 0.4)' }}
                />
                <span className="text-zinc-300">{HEX_I18N.ZONE_CONGESTED[lang]}</span>
              </div>
            </>
          )}
        </>
      )}
    </>
  );
}

export default function MapLegend({
  isOpen,
  onToggle,
  analysisEnabled = false,
  hexOverlayEnabled = false,
  hexOverlayMode = 'flood',
  verdictsCount = 0,
  lang = 'vi',
  floodLoading = false,
  trafficLoading = false,
}: MapLegendProps) {
  return (
    <>
      {/* DESKTOP VIEW (md+): Collapsible card */}
      <div className="hidden md:block z-[800] bg-zinc-950/95 border border-zinc-700 rounded-md font-mono shadow-2xl backdrop-blur-md pointer-events-auto select-none min-w-[230px]">
        {/* Header row — always visible */}
        <button
          onClick={onToggle}
          className="w-full flex items-center justify-between px-3 py-2 hover:bg-zinc-900/60 transition-colors rounded-md focus:outline-none cursor-pointer"
          aria-expanded={isOpen}
          aria-label={lang === 'vi' ? 'Đóng/mở chú giải bản đồ' : 'Toggle map legend'}
        >
          <span className="font-bold text-white tracking-wider text-xs uppercase">
            {lang === 'vi' ? 'CHÚ GIẢI BẢN ĐỒ' : 'MAP LEGEND'}
          </span>
          <span className="text-zinc-400 text-xs ml-3 shrink-0">
            {isOpen ? '▾' : '▸'}
          </span>
        </button>

        {/* Collapsible body */}
        <AnimatePresence initial={false}>
          {isOpen && (
            <motion.div
              key="legend-body-desktop"
              initial={{ height: 0, opacity: 0 }}
              animate={{ height: 'auto', opacity: 1 }}
              exit={{ height: 0, opacity: 0 }}
              transition={{ duration: 0.22, ease: 'easeInOut' }}
              className="overflow-hidden"
            >
              <div className="flex flex-col gap-2 text-zinc-200 text-xs px-3 pb-3 pt-1 border-t border-zinc-800">
                <LegendContent
                  lang={lang}
                  analysisEnabled={analysisEnabled}
                  hexOverlayEnabled={hexOverlayEnabled}
                  hexOverlayMode={hexOverlayMode}
                  verdictsCount={verdictsCount}
                  floodLoading={floodLoading}
                  trafficLoading={trafficLoading}
                />
              </div>
            </motion.div>
          )}
        </AnimatePresence>
      </div>

      {/* MOBILE VIEW (<md): Trigger button + Bottom sheet modal */}
      <div className="md:hidden">
        {/* Trigger Button */}
        <div className="z-[800] pointer-events-auto select-none">
          <button
            onClick={onToggle}
            className="w-[34px] h-[34px] flex items-center justify-center rounded border border-zinc-700 bg-zinc-950/95 text-zinc-300 hover:text-white shadow-2xl backdrop-blur-md focus:outline-none active:bg-zinc-900 cursor-pointer transition-colors"
            aria-expanded={isOpen}
            aria-label={lang === 'vi' ? 'Chú giải bản đồ' : 'Map legend'}
            title={lang === 'vi' ? 'Chú giải bản đồ' : 'Map legend'}
          >
            <svg
              className="w-4 h-4 text-zinc-300"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              aria-hidden="true"
            >
              <circle cx="12" cy="12" r="10" />
              <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3" />
              <path d="M12 17h.01" />
            </svg>
          </button>
        </div>

        {/* Bottom Sheet Drawer & Backdrop */}
        <AnimatePresence>
          {isOpen && (
            <>
              {/* Backdrop */}
              <motion.div
                key="legend-backdrop-mobile"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.2 }}
                onClick={onToggle}
                className="fixed inset-0 z-[850] bg-black/60 backdrop-blur-xs pointer-events-auto"
                aria-hidden="true"
              />

              {/* Bottom Sheet Drawer */}
              <motion.div
                key="legend-sheet-mobile"
                initial={{ y: '100%' }}
                animate={{ y: 0 }}
                exit={{ y: '100%' }}
                transition={{ duration: 0.25, ease: 'easeOut' }}
                className="fixed bottom-0 left-0 right-0 z-[860] bg-zinc-950 border-t border-zinc-700 rounded-t-xl shadow-2xl flex flex-col max-h-[75vh] font-mono pointer-events-auto select-none pb-6"
              >
                {/* Drag handle */}
                <div className="pt-2.5 pb-1 flex justify-center shrink-0">
                  <div className="w-10 h-1 rounded-full bg-zinc-600" />
                </div>

                {/* Header */}
                <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-800 shrink-0">
                  <span className="font-bold text-white tracking-wider text-xs uppercase">
                    {lang === 'vi' ? 'CHÚ GIẢI BẢN ĐỒ' : 'MAP LEGEND'}
                  </span>
                  <button
                    onClick={onToggle}
                    className="text-zinc-400 hover:text-white font-bold text-base leading-none p-1 transition-colors cursor-pointer"
                    aria-label={lang === 'vi' ? 'Đóng chú giải' : 'Close legend'}
                  >
                    ✕
                  </button>
                </div>

                {/* Body Content */}
                <div className="overflow-y-auto px-4 py-3 flex flex-col gap-2.5 text-xs text-zinc-200">
                  <LegendContent
                    lang={lang}
                    analysisEnabled={analysisEnabled}
                    hexOverlayEnabled={hexOverlayEnabled}
                    hexOverlayMode={hexOverlayMode}
                    verdictsCount={verdictsCount}
                    floodLoading={floodLoading}
                    trafficLoading={trafficLoading}
                  />
                </div>
              </motion.div>
            </>
          )}
        </AnimatePresence>
      </div>
    </>
  );
}
