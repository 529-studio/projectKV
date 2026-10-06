'use client';
import { motion, AnimatePresence } from 'framer-motion';
import { Camera } from '../hooks/useCameras';
import PhuongTreeFilter, { type TrafficVerdictLike } from './PhuongTreeFilter';
import { MOBILE_SEARCH_I18N } from '../config/constants';

interface MobileCameraDrawerProps {
  cameras: Camera[];
  verdicts?: Map<string, TrafficVerdictLike>;
  floodedCameraIds?: Set<string>;
  isFloodFilterActive?: boolean;
  onToggleFloodFilter?: () => void;
  loading: boolean;
  error: Error | null;
  isOpen: boolean;
  onClose: () => void;
  onCameraClick: (cam: Camera) => void;
  selectedPhuong: string | null;
  selectedCorridor: string | null;
  onSelectPhuong: (phuong: string | null) => void;
  onSelectCorridor: (corridor: string | null) => void;
  searchTerm: string;
  onSearchChange: (term: string) => void;
  lang: 'vi' | 'en';
  onLangChange: (lang: 'vi' | 'en') => void;
  autoFocusSearch?: boolean;
}

export default function MobileCameraDrawer({
  cameras,
  verdicts,
  floodedCameraIds,
  isFloodFilterActive,
  onToggleFloodFilter,
  loading,
  error,
  isOpen,
  onClose,
  onCameraClick,
  selectedPhuong,
  selectedCorridor,
  onSelectPhuong,
  onSelectCorridor,
  searchTerm,
  onSearchChange,
  lang,
  onLangChange,
  autoFocusSearch = false,
}: MobileCameraDrawerProps) {
  const handleCameraClick = (cam: Camera) => {
    onCameraClick(cam);
    onClose();
  };

  return (
    <AnimatePresence>
      {isOpen && (
        <>
          {/* Backdrop */}
          <motion.div
            key="drawer-backdrop"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            className="fixed inset-0 z-850 bg-black/60 md:hidden"
            onClick={onClose}
          />

          {/* Drawer panel */}
          <motion.div
            key="drawer-panel"
            initial={{ y: '100%' }}
            animate={{ y: 0 }}
            exit={{ y: '100%' }}
            transition={{ duration: 0.28, ease: 'easeOut' }}
            className="fixed bottom-0 left-0 right-0 z-860 bg-zinc-950 border-t border-zinc-700 rounded-t-xl shadow-2xl flex flex-col md:hidden max-h-[85vh] h-[80vh]"
          >
            {/* Drag handle and close header */}
            <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-800 shrink-0">
              <span className="text-xs font-mono font-bold text-white tracking-wider uppercase">
                {MOBILE_SEARCH_I18N.DRAWER_TITLE[lang]}
              </span>
              <div className="w-10 h-1 rounded-full bg-zinc-600" />
              <button
                onClick={onClose}
                className="text-zinc-400 hover:text-white font-bold text-base leading-none p-1 transition-colors flex items-center gap-1 cursor-pointer"
                aria-label={lang === 'vi' ? 'Đóng và xem bản đồ' : 'Close and view map'}
              >
                <span className="text-[11px] font-mono font-normal text-zinc-400 hover:text-white mr-0.5">
                  {MOBILE_SEARCH_I18N.CLOSE_LABEL[lang]}
                </span>
                ✕
              </button>
            </div>

            {/* Tree Filter Component */}
            <div className="flex-1 min-h-0 overflow-hidden">
              <PhuongTreeFilter
                cameras={cameras}
                verdicts={verdicts}
                floodedCameraIds={floodedCameraIds}
                isFloodFilterActive={isFloodFilterActive}
                onToggleFloodFilter={onToggleFloodFilter}
                loading={loading}
                error={error}
                selectedPhuong={selectedPhuong}
                selectedCorridor={selectedCorridor}
                onSelectPhuong={onSelectPhuong}
                onSelectCorridor={onSelectCorridor}
                onCameraClick={handleCameraClick}
                searchTerm={searchTerm}
                onSearchChange={onSearchChange}
                lang={lang}
                onLangChange={onLangChange}
                autoFocusSearch={autoFocusSearch}
              />
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}
