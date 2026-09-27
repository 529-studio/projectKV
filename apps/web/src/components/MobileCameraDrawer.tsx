'use client';
import { motion, AnimatePresence } from 'framer-motion';
import { Camera } from '../hooks/useCameras';
import PhuongTreeFilter from './PhuongTreeFilter';

interface MobileCameraDrawerProps {
  cameras: Camera[];
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
}

export default function MobileCameraDrawer({
  cameras,
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
            className="fixed inset-0 z-[850] bg-black/60 md:hidden"
            onClick={onClose}
          />

          {/* Drawer panel */}
          <motion.div
            key="drawer-panel"
            initial={{ y: '100%' }}
            animate={{ y: 0 }}
            exit={{ y: '100%' }}
            transition={{ duration: 0.28, ease: 'easeOut' }}
            className="fixed bottom-0 left-0 right-0 z-[860] bg-zinc-950 border-t border-zinc-700 rounded-t-xl shadow-2xl flex flex-col md:hidden max-h-[85vh] h-[80vh]"
          >
            {/* Drag handle and close header */}
            <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-800 shrink-0">
              <div className="w-6" />
              <div className="w-10 h-1 rounded-full bg-zinc-600" />
              <button
                onClick={onClose}
                className="text-zinc-400 hover:text-white font-bold text-lg leading-none p-1 transition-colors"
                aria-label="Đóng danh sách camera"
              >
                ✕
              </button>
            </div>

            {/* Tree Filter Component */}
            <div className="flex-1 min-h-0 overflow-hidden">
              <PhuongTreeFilter
                cameras={cameras}
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
              />
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}
