'use client';

import React, { useState, useRef, useEffect } from 'react';
import {
  VEHICLE_PROFILES,
  type VehicleType,
} from '../types/vehicle.ts';

interface VehicleProfileSelectorProps {
  currentVehicle: VehicleType;
  onVehicleChange: (vehicle: VehicleType) => void;
  lang?: 'vi' | 'en';
}

export default function VehicleProfileSelector({
  currentVehicle,
  onVehicleChange,
  lang = 'vi',
}: VehicleProfileSelectorProps) {
  const [isOpen, setIsOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const activeProfile = VEHICLE_PROFILES[currentVehicle] || VEHICLE_PROFILES.scooter;

  // Click outside listener to close dropdown
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }
    if (isOpen) {
      document.addEventListener('mousedown', handleClickOutside);
    }
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [isOpen]);

  const vehicleKeys: VehicleType[] = ['scooter', 'motorbike', 'sedan', 'suv'];

  return (
    <div className="relative inline-block" ref={containerRef}>
      {/* Trigger Button (>= 48px touch target) */}
      <button
        type="button"
        onClick={() => setIsOpen(prev => !prev)}
        className="min-h-[48px] px-3 py-1.5 flex items-center gap-2 rounded bg-zinc-900/90 hover:bg-zinc-800/90 border border-zinc-800 hover:border-zinc-700 text-zinc-200 transition-colors text-xs font-medium focus:outline-none focus:ring-1 focus:ring-zinc-600"
        aria-haspopup="listbox"
        aria-expanded={isOpen}
        aria-label={lang === 'vi' ? 'Chọn loại phương tiện' : 'Select vehicle profile'}
        title={lang === 'vi' ? 'Chọn loại phương tiện để điều chỉnh ngưỡng cảnh báo ngập' : 'Select vehicle profile for adaptive flood thresholds'}
      >
        {/* Subtle vehicle outline icon */}
        <svg className="w-4 h-4 text-zinc-400 shrink-0" fill="none" stroke="currentColor" strokeWidth={1.8} viewBox="0 0 24 24">
          <path strokeLinecap="round" strokeLinejoin="round" d="M8 17a2 2 0 100-4 2 2 0 000 4zM16 17a2 2 0 100-4 2 2 0 000 4z" />
          <path strokeLinecap="round" strokeLinejoin="round" d="M3 9l3-4h12l3 4v6H3V9z" />
        </svg>

        <span className="font-semibold text-zinc-100 whitespace-nowrap">
          {activeProfile.shortLabel[lang]}
        </span>

        <span className="hidden sm:inline text-[11px] text-zinc-400 font-mono">
          ({activeProfile.impassableDepthCm}cm)
        </span>

        {/* Caret */}
        <svg
          className={`w-3.5 h-3.5 text-zinc-500 transition-transform ${isOpen ? 'rotate-180' : ''}`}
          fill="none"
          stroke="currentColor"
          strokeWidth={2}
          viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      {/* Dropdown Menu */}
      {isOpen && (
        <div
          role="listbox"
          className="absolute right-0 top-full mt-1.5 w-72 sm:w-80 max-w-[calc(100vw-1.5rem)] bg-zinc-950 border border-zinc-800 rounded-lg shadow-2xl z-[1200] py-1.5 font-sans overflow-hidden backdrop-blur-md"
        >
          <div className="px-3 py-2 border-b border-zinc-900 bg-zinc-900/40">
            <div className="text-[11px] font-bold uppercase tracking-wider text-zinc-400">
              {lang === 'vi' ? 'Phương tiện di chuyển' : 'Commuter Vehicle'}
            </div>
            <div className="text-[10px] text-zinc-500 mt-0.5">
              {lang === 'vi'
                ? 'Ngưỡng ngập và cảnh báo chết máy thích ứng theo xe của bạn'
                : 'Hazard and stall warnings dynamically adapt to your vehicle clearance'}
            </div>
          </div>

          <div className="divide-y divide-zinc-900">
            {vehicleKeys.map(key => {
              const p = VEHICLE_PROFILES[key];
              const isSelected = key === currentVehicle;

              return (
                <button
                  key={key}
                  role="option"
                  aria-selected={isSelected}
                  type="button"
                  onClick={() => {
                    onVehicleChange(key);
                    setIsOpen(false);
                  }}
                  className={`w-full text-left px-3 py-2.5 min-h-[48px] flex items-start gap-3 transition-colors ${
                    isSelected
                      ? 'bg-zinc-800/70 text-zinc-100'
                      : 'hover:bg-zinc-900/60 text-zinc-300'
                  }`}
                >
                  <div className="mt-0.5 shrink-0">
                    <span
                      className={`inline-block w-2.5 h-2.5 rounded-full ${
                        isSelected ? 'bg-emerald-400 ring-2 ring-emerald-500/30' : 'bg-zinc-700'
                      }`}
                    />
                  </div>

                  <div className="flex-1 min-w-0">
                    <div className="flex items-center justify-between gap-1">
                      <span className="font-semibold text-xs text-zinc-200">
                        {p.name[lang]}
                      </span>
                      <span className="text-[10px] font-mono text-zinc-400 shrink-0">
                        {lang === 'vi' ? `Ngập sâu >${p.impassableDepthCm}cm` : `Deep >${p.impassableDepthCm}cm`}
                      </span>
                    </div>

                    <div className="text-[11px] text-zinc-400 truncate mt-0.5">
                      {p.subtext[lang]}
                    </div>

                    <div className="flex items-center gap-2 mt-1 text-[10px] text-zinc-500 font-mono">
                      <span>{lang === 'vi' ? 'Gầm xe:' : 'Clearance:'} {p.groundClearanceCm}cm</span>
                      <span>•</span>
                      <span className="text-amber-400/90">
                        {lang === 'vi' ? `Cảnh báo: ≥${p.cautionDepthCm}cm` : `Caution: ≥${p.cautionDepthCm}cm`}
                      </span>
                    </div>
                  </div>
                </button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
