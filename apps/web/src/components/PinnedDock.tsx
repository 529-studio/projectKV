'use client';

import { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Camera } from '../hooks/useCameras';
import { API_ENDPOINTS, getApiUrl, PINNED_DOCK_I18N } from '../config/constants';

interface PinnedDockProps {
  pinnedCameras: Camera[];
  onUnpin: (id: string) => void;
  onClear: () => void;
  onCameraClick?: (cam: Camera) => void;
  lang?: 'vi' | 'en';
}

export default function PinnedDock({
  pinnedCameras,
  onUnpin,
  onClear,
  onCameraClick,
  lang = 'vi',
}: PinnedDockProps) {
  const [isMobileExpanded, setIsMobileExpanded] = useState(false);

  if (pinnedCameras.length === 0) return null;

  const handleCardClick = (cam: Camera) => {
    if (onCameraClick) {
      onCameraClick(cam);
      setIsMobileExpanded(false);
    }
  };

  const handleUnpin = (e: React.MouseEvent, camId: string) => {
    e.stopPropagation();
    onUnpin(camId);
  };

  return (
    <>
      {/* =======================================================
          DESKTOP VIEW (md+): Standard horizontal bottom dock
         ======================================================= */}
      <div className="hidden md:block absolute bottom-0 left-0 right-0 z-[800] bg-zinc-950/95 border-t border-zinc-800 shadow-2xl backdrop-blur-md">
        <div className="flex items-center justify-between px-3 py-1.5 bg-zinc-900 border-b border-zinc-800 font-mono text-xs select-none">
          <div className="flex items-center gap-2">
            <span className="font-bold text-white tracking-wide">
              {PINNED_DOCK_I18N.TITLE[lang]}
            </span>
            <span className="bg-zinc-950 border border-zinc-700 text-zinc-300 px-1.5 py-0.5 rounded">
              [{pinnedCameras.length} {lang === 'vi' ? 'CAM' : pinnedCameras.length === 1 ? 'CAM' : 'CAMS'}]
            </span>
          </div>
          <button
            type="button"
            onClick={onClear}
            className="px-2 py-0.5 bg-zinc-950 border border-zinc-700 hover:border-zinc-400 text-zinc-300 hover:text-white rounded transition cursor-pointer"
          >
            {PINNED_DOCK_I18N.CLEAR_ALL[lang]}
          </button>
        </div>
        <div className="flex gap-2 p-2 overflow-x-auto h-[120px]">
          {pinnedCameras.map(cam => (
            <div
              key={cam.id}
              onClick={() => handleCardClick(cam)}
              className="relative min-w-[160px] h-full bg-black border border-zinc-800 hover:border-zinc-600 rounded group flex-shrink-0 cursor-pointer overflow-hidden transition-colors"
            >
              <img
                src={`${getApiUrl(API_ENDPOINTS.SNAPSHOT(cam.id))}?t=${Date.now()}`}
                className="w-full h-full object-cover rounded opacity-85 group-hover:opacity-100 transition-opacity"
                alt={cam.title}
                loading="lazy"
              />
              <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/95 via-black/70 to-transparent p-2 text-[10px] font-mono text-white truncate">
                {cam.displayName || cam.title}
              </div>
              <button
                type="button"
                onClick={(e) => handleUnpin(e, cam.id)}
                className="absolute top-1 right-1 w-5 h-5 bg-red-500/80 hover:bg-red-500 text-white rounded flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity cursor-pointer text-xs"
                title={PINNED_DOCK_I18N.UNPIN[lang]}
                aria-label={`${PINNED_DOCK_I18N.UNPIN[lang]}: ${cam.displayName || cam.title}`}
              >
                ×
              </button>
            </div>
          ))}
        </div>
      </div>

      {/* =======================================================
          MOBILE VIEW (<md): Floating Collapsed Pill + Slide-up Drawer
         ======================================================= */}
      <div className="md:hidden">
        {/* 1. Collapsed Floating Pill (shown when not expanded) */}
        {!isMobileExpanded && (
          <motion.div
            initial={{ opacity: 0, y: 10, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 10, scale: 0.95 }}
            className="fixed bottom-4 left-1/2 -translate-x-1/2 z-[820] pointer-events-auto select-none"
          >
            <button
              type="button"
              onClick={() => setIsMobileExpanded(true)}
              className="px-3.5 py-1.5 rounded-full bg-zinc-950/95 border border-zinc-700 hover:border-zinc-500 text-zinc-200 font-mono text-xs font-semibold shadow-2xl backdrop-blur-md flex items-center gap-2 cursor-pointer active:scale-95 transition-all"
              aria-label={PINNED_DOCK_I18N.COLLAPSED_PILL[lang](pinnedCameras.length)}
            >
              <span>{PINNED_DOCK_I18N.COLLAPSED_PILL[lang](pinnedCameras.length)}</span>
              <span className="text-zinc-400 text-xs">▴</span>
            </button>
          </motion.div>
        )}

        {/* 2. Expanded Bottom Sheet Drawer */}
        <AnimatePresence>
          {isMobileExpanded && (
            <>
              {/* Backdrop */}
              <motion.div
                key="pinned-backdrop-mobile"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.2 }}
                className="fixed inset-0 z-[840] bg-black/60 md:hidden"
                onClick={() => setIsMobileExpanded(false)}
              />

              {/* Drawer Sheet */}
              <motion.div
                key="pinned-sheet-mobile"
                initial={{ y: '100%' }}
                animate={{ y: 0 }}
                exit={{ y: '100%' }}
                transition={{ duration: 0.26, ease: 'easeOut' }}
                className="fixed bottom-0 left-0 right-0 z-[850] bg-zinc-950 border-t border-zinc-700 rounded-t-xl shadow-2xl flex flex-col md:hidden pb-3 max-h-[50vh]"
              >
                {/* Drag handle */}
                <div className="pt-2 pb-1 flex justify-center shrink-0">
                  <div className="w-10 h-1 rounded-full bg-zinc-600" />
                </div>

                {/* Header */}
                <div className="flex items-center justify-between px-3.5 py-2 border-b border-zinc-800 shrink-0 font-mono text-xs">
                  <div className="flex items-center gap-2">
                    <span className="font-bold text-white tracking-wide">
                      {PINNED_DOCK_I18N.TITLE[lang]}
                    </span>
                    <span className="bg-zinc-900 border border-zinc-700 text-zinc-300 px-1.5 py-0.5 rounded text-[10px]">
                      [{pinnedCameras.length}]
                    </span>
                  </div>

                  <div className="flex items-center gap-2">
                    <button
                      type="button"
                      onClick={onClear}
                      className="px-2 py-0.5 bg-zinc-900 border border-zinc-700 hover:border-zinc-400 text-zinc-400 hover:text-white rounded text-[10.5px] transition cursor-pointer"
                    >
                      {PINNED_DOCK_I18N.CLEAR_ALL[lang]}
                    </button>
                    <button
                      type="button"
                      onClick={() => setIsMobileExpanded(false)}
                      className="text-zinc-400 hover:text-white font-bold text-base leading-none p-1 transition-colors flex items-center gap-1 cursor-pointer"
                      aria-label={PINNED_DOCK_I18N.COLLAPSE[lang]}
                    >
                      <span className="text-[11px] font-normal text-zinc-400 mr-0.5">
                        {PINNED_DOCK_I18N.COLLAPSE[lang]}
                      </span>
                      ▾
                    </button>
                  </div>
                </div>

                {/* Horizontal Scroll Track */}
                <div className="flex gap-2.5 p-3 overflow-x-auto h-[140px] items-center">
                  {pinnedCameras.map(cam => (
                    <div
                      key={cam.id}
                      onClick={() => handleCardClick(cam)}
                      className="relative min-w-[165px] h-full bg-black border border-zinc-800 active:border-zinc-500 rounded group flex-shrink-0 cursor-pointer overflow-hidden shadow-lg transition-colors"
                    >
                      <img
                        src={`${getApiUrl(API_ENDPOINTS.SNAPSHOT(cam.id))}?t=${Date.now()}`}
                        className="w-full h-full object-cover rounded opacity-90 active:opacity-100 transition-opacity"
                        alt={cam.title}
                        loading="lazy"
                      />
                      <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/95 via-black/75 to-transparent p-2 text-[10px] font-mono text-white truncate">
                        {cam.displayName || cam.title}
                      </div>
                      <button
                        type="button"
                        onClick={(e) => handleUnpin(e, cam.id)}
                        className="absolute top-1.5 right-1.5 w-6 h-6 bg-red-600/90 active:bg-red-500 text-white rounded flex items-center justify-center font-bold text-sm cursor-pointer shadow-md"
                        title={PINNED_DOCK_I18N.UNPIN[lang]}
                        aria-label={`${PINNED_DOCK_I18N.UNPIN[lang]}: ${cam.displayName || cam.title}`}
                      >
                        ×
                      </button>
                    </div>
                  ))}
                </div>
              </motion.div>
            </>
          )}
        </AnimatePresence>
      </div>
    </>
  );
}
