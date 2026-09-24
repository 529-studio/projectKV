'use client';
import { motion, AnimatePresence } from 'framer-motion';
import { Camera } from '../hooks/useCameras';

interface MobileCameraDrawerProps {
  cameras: Camera[];
  loading: boolean;
  error: Error | null;
  isOpen: boolean;
  onClose: () => void;
  onCameraClick: (cam: Camera) => void;
  searchTerm: string;
  onSearchChange: (term: string) => void;
}

export default function MobileCameraDrawer({
  cameras,
  loading,
  error,
  isOpen,
  onClose,
  onCameraClick,
  searchTerm,
  onSearchChange,
}: MobileCameraDrawerProps) {
  const filtered = searchTerm
    ? cameras.filter(c =>
        (c.displayName || c.title || '').toLowerCase().includes(searchTerm.toLowerCase()) ||
        (c.zone || '').toLowerCase().includes(searchTerm.toLowerCase())
      )
    : cameras;

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
            className="fixed inset-0 z-[850] bg-black/50 md:hidden"
            onClick={onClose}
          />

          {/* Drawer panel */}
          <motion.div
            key="drawer-panel"
            initial={{ y: '100%' }}
            animate={{ y: 0 }}
            exit={{ y: '100%' }}
            transition={{ duration: 0.28, ease: 'easeOut' }}
            className="fixed bottom-0 left-0 right-0 z-[860] bg-zinc-950 border-t border-zinc-700 rounded-t-xl shadow-2xl flex flex-col md:hidden"
            style={{ maxHeight: '60vh' }}
          >
            {/* Drag handle */}
            <div className="flex justify-center pt-3 pb-1 shrink-0">
              <div className="w-10 h-1 rounded-full bg-zinc-600" />
            </div>

            {/* Header */}
            <div className="flex items-center justify-between px-4 py-2 border-b border-zinc-800 shrink-0">
              <span className="font-mono font-bold text-white text-xs uppercase tracking-wider">
                Danh sách camera
              </span>
              <button
                onClick={onClose}
                className="text-zinc-500 hover:text-white font-bold text-lg px-1 transition-colors"
                aria-label="Đóng danh sách camera"
              >
                ×
              </button>
            </div>

            {/* Search */}
            <div className="px-3 py-2 border-b border-zinc-800 shrink-0">
              <input
                type="text"
                placeholder="Tìm kiếm camera..."
                value={searchTerm}
                onChange={e => onSearchChange(e.target.value)}
                className="w-full bg-zinc-900 border border-zinc-700 rounded px-3 py-1.5 text-xs text-white placeholder-zinc-500 focus:outline-none focus:border-zinc-500 font-mono"
                autoComplete="off"
              />
            </div>

            {/* Camera list */}
            <div className="flex-1 overflow-y-auto p-2 space-y-1">
              {loading ? (
                <p className="text-xs text-yellow-500 font-mono p-2">Đang tải...</p>
              ) : error ? (
                <p className="text-xs text-red-500 font-mono p-2">Lỗi: {error.message}</p>
              ) : filtered.length === 0 ? (
                <p className="text-xs text-zinc-500 font-mono p-2">Không tìm thấy camera</p>
              ) : (
                filtered.map(cam => (
                  <div
                    key={cam.id}
                    onClick={() => handleCameraClick(cam)}
                    className="p-2 border border-zinc-800 bg-zinc-950 hover:bg-zinc-800 active:bg-zinc-700 cursor-pointer rounded flex flex-col gap-1 transition-colors"
                  >
                    <span className="text-xs font-bold text-white truncate">
                      {cam.displayName || cam.title}
                    </span>
                    <span className="text-[10px] text-zinc-500 truncate">{cam.zone}</span>
                  </div>
                ))
              )}
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  );
}
