'use client';
import { motion, AnimatePresence } from 'framer-motion';

interface MapLegendProps {
  isOpen: boolean;
  onToggle: () => void;
}

export default function MapLegend({ isOpen, onToggle }: MapLegendProps) {
  return (
    <div className="absolute top-3 right-3 z-[800] bg-zinc-950/95 border border-zinc-700 rounded-md font-mono shadow-2xl backdrop-blur-md pointer-events-auto select-none min-w-[210px]">
      {/* Header row — always visible */}
      <button
        onClick={onToggle}
        className="w-full flex items-center justify-between px-3 py-2 hover:bg-zinc-900/60 transition-colors rounded-md focus:outline-none"
        aria-expanded={isOpen}
        aria-label="Đóng/mở chú giải bản đồ"
      >
        <span className="font-bold text-white tracking-wider text-xs uppercase">
          CHÚ GIẢI BẢN ĐỒ
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
            <div className="flex flex-col gap-2.5 text-zinc-200 text-xs px-3 pb-3 pt-1 border-t border-zinc-800">
              <div className="flex items-center gap-2.5">
                <span className="w-3 h-3 rounded-full bg-white shrink-0" />
                <span>Camera hoạt động</span>
              </div>
              <div className="flex items-center gap-2.5">
                <span className="w-3 h-3 rounded-full bg-zinc-600 shrink-0" />
                <span className="text-zinc-400">Camera mất tín hiệu</span>
              </div>
              <div className="flex items-center gap-2.5">
                <span className="w-3 h-3 rounded-full bg-green-500 shrink-0" />
                <span>Giao thông thông thoáng</span>
              </div>
              <div className="flex items-center gap-2.5">
                <span className="w-3 h-3 rounded-full bg-yellow-500 shrink-0" />
                <span>Đông xe / Chậm</span>
              </div>
              <div className="flex items-center gap-2.5">
                <span className="w-3 h-3 rounded-full bg-red-500 shrink-0" />
                <span>Ùn tắc / Kẹt xe</span>
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
