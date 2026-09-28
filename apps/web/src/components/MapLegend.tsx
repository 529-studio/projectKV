'use client';
import { motion, AnimatePresence } from 'framer-motion';

import { FUNCTIONAL_COLORS } from '../config/constants';

interface MapLegendProps {
  isOpen: boolean;
  onToggle: () => void;
  analysisEnabled?: boolean;
  lang?: 'vi' | 'en';
}

export default function MapLegend({
  isOpen,
  onToggle,
  analysisEnabled = false,
  lang = 'vi',
}: MapLegendProps) {
  return (
    <div className="absolute top-3 right-3 z-[800] bg-zinc-950/95 border border-zinc-700 rounded-md font-mono shadow-2xl backdrop-blur-md pointer-events-auto select-none min-w-[230px]">
      {/* Header row — always visible */}
      <button
        onClick={onToggle}
        className="w-full flex items-center justify-between px-3 py-2 hover:bg-zinc-900/60 transition-colors rounded-md focus:outline-none"
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
            key="legend-body"
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ duration: 0.22, ease: 'easeInOut' }}
            className="overflow-hidden"
          >
            <div className="flex flex-col gap-2 text-zinc-200 text-xs px-3 pb-3 pt-1 border-t border-zinc-800">
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
                  <div className="text-[10px] text-zinc-400 font-semibold uppercase tracking-wider pt-1.5 border-t border-zinc-800/80">
                    {lang === 'vi'
                      ? 'Giám sát AI (Điểm trọng yếu / Đang xem)'
                      : 'AI Monitoring (Hotspots / Active)'}
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
                </>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
