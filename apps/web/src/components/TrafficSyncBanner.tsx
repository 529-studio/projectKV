'use client';
import { useState, useEffect, useRef } from 'react';
import { motion, AnimatePresence } from 'framer-motion';

export interface TrafficSyncBannerProps {
  analysisEnabled: boolean;
  verdictsCount: number;
  lang?: 'vi' | 'en';
}

export default function TrafficSyncBanner({
  analysisEnabled,
  verdictsCount,
  lang = 'vi',
}: TrafficSyncBannerProps) {
  // If verdicts are already present on mount, do not show any banner
  const initialHadVerdicts = useRef(verdictsCount > 0);
  const [dismissed, setDismissed] = useState(initialHadVerdicts.current);
  const [showSyncedSuccess, setShowSyncedSuccess] = useState(false);
  const prevCountRef = useRef(verdictsCount);

  useEffect(() => {
    // When verdicts arrive for the first time while syncing
    if (prevCountRef.current === 0 && verdictsCount > 0 && !dismissed) {
      setShowSyncedSuccess(true);
      const timer = setTimeout(() => {
        setDismissed(true);
      }, 3500);
      return () => clearTimeout(timer);
    }
    prevCountRef.current = verdictsCount;
  }, [verdictsCount, dismissed]);

  if (!analysisEnabled || dismissed) {
    return null;
  }

  return (
    <div className="fixed top-16 md:top-3.5 left-1/2 -translate-x-1/2 z-[850] pointer-events-none w-auto max-w-[calc(100vw-32px)] sm:max-w-md">
      <AnimatePresence>
        <motion.div
          initial={{ opacity: 0, y: -10, scale: 0.96 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={{ opacity: 0, y: -10, scale: 0.96 }}
          transition={{ duration: 0.25, ease: 'easeOut' }}
          className="pointer-events-auto flex items-start sm:items-center gap-2.5 px-3.5 py-2 rounded-lg bg-zinc-950/95 border border-zinc-800 text-zinc-300 font-mono text-xs shadow-2xl backdrop-blur-md select-none"
          role="status"
          aria-live="polite"
        >
          {/* Status Indicator Dot */}
          <div className="pt-0.5 sm:pt-0 shrink-0">
            {showSyncedSuccess ? (
              <span className="flex h-2 w-2 relative">
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-400" />
              </span>
            ) : (
              <span className="flex h-2 w-2 relative">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-2 w-2 bg-amber-400" />
              </span>
            )}
          </div>

          {/* Text Content */}
          <div className="flex-1 min-w-0 pr-1">
            {showSyncedSuccess ? (
              <div className="text-zinc-200 font-semibold truncate">
                {lang === 'vi'
                  ? `✓ Đã sẵn sàng phân tích AI (${verdictsCount} camera)`
                  : `✓ AI traffic data ready (${verdictsCount} cameras)`}
              </div>
            ) : (
              <>
                <div className="text-zinc-200 font-semibold leading-tight">
                  {lang === 'vi'
                    ? 'Đang kết nối phân tích giao thông AI...'
                    : 'Connecting AI traffic analysis...'}
                </div>
                <div className="text-[11px] text-zinc-400 mt-0.5 leading-snug">
                  {lang === 'vi'
                    ? 'Bản đồ sẵn sàng — bạn có thể bấm vào camera bất kỳ để xem ngay.'
                    : 'Map ready — click any camera to view live stream.'}
                </div>
              </>
            )}
          </div>

          {/* Dismiss Button */}
          <button
            type="button"
            onClick={() => setDismissed(true)}
            className="text-zinc-500 hover:text-white transition-colors p-1 -mr-1 -mt-1 sm:mt-0 rounded focus:outline-none shrink-0"
            aria-label={lang === 'vi' ? 'Đóng thông báo' : 'Dismiss notice'}
            title={lang === 'vi' ? 'Đóng thông báo' : 'Dismiss notice'}
          >
            <span className="text-sm font-bold leading-none block">✕</span>
          </button>
        </motion.div>
      </AnimatePresence>
    </div>
  );
}
