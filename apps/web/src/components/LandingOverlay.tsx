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
            <div className="flex items-center justify-center gap-4 mb-4">
              <img src="/wared-icon.png" alt="Warded Logo" className="w-12 h-12 md:w-16 md:h-16 object-contain" />
              <h1 className="text-4xl md:text-5xl font-extrabold tracking-tight">Warded</h1>
            </div>
            <p className="text-zinc-400 text-lg mb-8">
              Giúp đoạn đường của bạn đỡ chông gai hơn mỗi ngày.
            </p>
            <button 
              onClick={() => setIsVisible(false)}
              className="bg-white text-black font-bold py-3 px-8 rounded hover:scale-105 transition-transform duration-200 cursor-pointer"
            >
              Truy cập Hệ thống
            </button>
            <div className="mt-8 text-xs text-zinc-500 font-mono">
              Developed by{' '}
              <a
                href="https://thdatvu.vercel.app/"
                target="_blank"
                rel="noopener noreferrer"
                className="text-zinc-300 hover:text-white underline underline-offset-4 transition-colors font-medium"
              >
                ThDat Vu
              </a>
              {' and '}
              <a
                href="https://github.com/giangdq202"
                target="_blank"
                rel="noopener noreferrer"
                className="text-zinc-300 hover:text-white underline underline-offset-4 transition-colors font-medium"
              >
                Dam Quang Giang
              </a>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  );
}
