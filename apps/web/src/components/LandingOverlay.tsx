'use client';
import { motion, AnimatePresence } from 'framer-motion';
import { useState } from 'react';

export default function LandingOverlay() {
  const [isVisible, setIsVisible] = useState(true);

  return (
    <AnimatePresence>
      {isVisible && (
        <motion.div
          initial={{ opacity: 1, backdropFilter: 'blur(12px)' }}
          exit={{ opacity: 0, backdropFilter: 'blur(0px)' }}
          transition={{ duration: 0.6, ease: 'easeInOut' }}
          className="fixed inset-0 z-[9999] bg-black/85 flex items-center justify-center pointer-events-auto"
        >
          <motion.div 
            initial={{ y: 20, opacity: 0 }}
            animate={{ y: 0, opacity: 1 }}
            transition={{ delay: 0.2, duration: 0.5 }}
            className="text-center font-mono text-white p-6 max-w-2xl mx-auto"
          >
            <h1 className="text-4xl md:text-5xl font-extrabold tracking-tight mb-4">GIAO THÔNG TP.HCM</h1>
            <p className="text-zinc-400 text-lg mb-8">
              Giám sát camera thời gian thực. Phân tích ùn tắc bằng AI với độ trễ 0s.
            </p>
            <button 
              onClick={() => setIsVisible(false)}
              className="bg-white text-black font-bold py-3 px-8 rounded hover:scale-105 transition-transform duration-200"
            >
              Truy cập Hệ thống
            </button>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
