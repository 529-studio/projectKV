'use client';

import { useState, useEffect } from 'react';
import type { CommuteConfig, CommutePreset } from '../types/commute.ts';
import { COMMUTE_I18N } from '../types/commute.ts';
import { CORRIDOR_NAMES } from '../utils/commuteRouteUtils.ts';

interface MyRouteSetupModalProps {
  isOpen: boolean;
  onClose: () => void;
  config: CommuteConfig | null;
  presets: CommutePreset[];
  onSave: (config: CommuteConfig) => void;
  onClear: () => void;
  onSelectPreset: (preset: CommutePreset) => void;
  lang?: 'vi' | 'en';
}

export default function MyRouteSetupModal({
  isOpen,
  onClose,
  config,
  presets,
  onSave,
  onClear,
  onSelectPreset,
  lang = 'vi',
}: MyRouteSetupModalProps) {
  const [homeName, setHomeName] = useState('');
  const [workName, setWorkName] = useState('');
  const [selectedCorridors, setSelectedCorridors] = useState<string[]>([]);

  useEffect(() => {
    if (config) {
      setHomeName(config.home?.name || '');
      setWorkName(config.work?.name || '');
      setSelectedCorridors(config.selectedCorridors || []);
    } else {
      setHomeName('');
      setWorkName('');
      setSelectedCorridors(['nguyen_huu_canh']);
    }
  }, [config, isOpen]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const toggleCorridor = (key: string) => {
    setSelectedCorridors(prev =>
      prev.includes(key) ? prev.filter(k => k !== key) : [...prev, key]
    );
  };

  const handleSave = () => {
    const newConfig: CommuteConfig = {
      home: {
        name: homeName.trim() || (lang === 'vi' ? 'Nhà' : 'Home'),
        lat: config?.home?.lat || 10.795,
        lng: config?.home?.lng || 106.721,
      },
      work: {
        name: workName.trim() || (lang === 'vi' ? 'Cơ quan' : 'Work'),
        lat: config?.work?.lat || 10.776,
        lng: config?.work?.lng || 106.703,
      },
      selectedCorridors: selectedCorridors.length > 0 ? selectedCorridors : ['nguyen_huu_canh'],
      direction: config?.direction || 'to_work',
      enabled: true,
    };
    onSave(newConfig);
    onClose();
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label={COMMUTE_I18N.TITLE[lang]}
      className="fixed inset-0 z-[1100] flex items-center justify-center p-3 sm:p-4 pt-[max(1rem,env(safe-area-inset-top))] pb-[max(1rem,env(safe-area-inset-bottom))] bg-black/60 backdrop-blur-sm animate-fade-in font-sans"
    >
      <div className="w-full max-w-lg bg-zinc-950 border border-zinc-800 rounded-xl shadow-2xl overflow-hidden flex flex-col max-h-[90vh]">
        {/* HEADER */}
        <div className="px-5 py-3.5 bg-zinc-900 border-b border-zinc-800 flex items-center justify-between">
          <div>
            <h2 className="text-sm font-semibold text-zinc-100 uppercase tracking-wide">
              {COMMUTE_I18N.SETUP_ROUTE[lang]}
            </h2>
            <p className="text-xs text-zinc-400 mt-0.5">
              {COMMUTE_I18N.SUBTITLE[lang]}
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label={COMMUTE_I18N.CANCEL[lang]}
            className="min-w-[48px] min-h-[48px] flex items-center justify-center rounded-lg hover:bg-zinc-800 text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
          >
            <svg
              className="w-4 h-4"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        {/* BODY */}
        <div className="p-5 overflow-y-auto space-y-5">
          {/* QUICK PRESETS */}
          <div className="space-y-2">
            <span className="text-xs font-medium text-zinc-300 block">
              {COMMUTE_I18N.PRESETS_LABEL[lang]}
            </span>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
              {presets.map(p => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => onSelectPreset(p)}
                  className="min-h-[48px] p-2.5 rounded-lg bg-zinc-900 hover:bg-zinc-800/90 border border-zinc-800 hover:border-zinc-700 text-left transition-all cursor-pointer group flex flex-col justify-center"
                >
                  <span className="text-xs font-medium text-zinc-200 group-hover:text-cyan-400 transition-colors block">
                    {p.title[lang]}
                  </span>
                  <span className="text-[11px] text-zinc-500 block truncate mt-0.5">
                    {p.home.name} ➔ {p.work.name}
                  </span>
                </button>
              ))}
            </div>
          </div>

          <div className="border-t border-zinc-800/80 pt-4 space-y-4">
            <span className="text-xs font-medium text-zinc-300 block">
              {lang === 'vi' ? 'Tùy chỉnh lộ trình cá nhân:' : 'Custom Route Endpoints:'}
            </span>

            {/* HOME INPUT */}
            <div>
              <label htmlFor="home-endpoint-input" className="text-xs text-zinc-400 block mb-1">
                {COMMUTE_I18N.HOME[lang]} ({lang === 'vi' ? 'Tên điểm xuất phát' : 'Origin location'}):
              </label>
              <input
                id="home-endpoint-input"
                type="text"
                value={homeName}
                onChange={e => setHomeName(e.target.value)}
                placeholder={lang === 'vi' ? 'Ví dụ: Landmark 81, Bình Thạnh' : 'e.g. Landmark 81'}
                className="w-full min-h-[48px] px-3 py-2.5 text-xs rounded-lg bg-zinc-900 border border-zinc-700 text-zinc-100 placeholder-zinc-500 focus:outline-none focus:border-cyan-500 transition-colors"
              />
            </div>

            {/* WORK INPUT */}
            <div>
              <label htmlFor="work-endpoint-input" className="text-xs text-zinc-400 block mb-1">
                {COMMUTE_I18N.WORK[lang]} ({lang === 'vi' ? 'Tên nơi làm việc' : 'Destination location'}):
              </label>
              <input
                id="work-endpoint-input"
                type="text"
                value={workName}
                onChange={e => setWorkName(e.target.value)}
                placeholder={lang === 'vi' ? 'Ví dụ: Bến Nghé, Quận 1' : 'e.g. District 1 Center'}
                className="w-full min-h-[48px] px-3 py-2.5 text-xs rounded-lg bg-zinc-900 border border-zinc-700 text-zinc-100 placeholder-zinc-500 focus:outline-none focus:border-cyan-500 transition-colors"
              />
            </div>

            {/* ARTERIAL CORRIDORS SELECTION */}
            <div>
              <span className="text-xs text-zinc-400 block mb-1.5">
                {lang === 'vi' ? 'Các trục đường chính trên hành trình:' : 'Arterial corridors on journey:'}
              </span>
              <div className="grid grid-cols-2 gap-1.5 max-h-40 overflow-y-auto p-1 bg-zinc-900/60 rounded-lg border border-zinc-800">
                {Object.entries(CORRIDOR_NAMES).map(([key, name]) => {
                  const isChecked = selectedCorridors.includes(key);
                  return (
                    <label
                      key={key}
                      className={`min-h-[44px] flex items-center gap-2 px-2.5 py-2 rounded cursor-pointer text-[11px] transition-colors ${
                        isChecked ? 'bg-zinc-800 text-cyan-300' : 'text-zinc-400 hover:text-zinc-200'
                      }`}
                    >
                      <input
                        type="checkbox"
                        checked={isChecked}
                        onChange={() => toggleCorridor(key)}
                        className="rounded border-zinc-700 text-cyan-500 focus:ring-0 focus:ring-offset-0 bg-zinc-900"
                      />
                      <span className="truncate">{name}</span>
                    </label>
                  );
                })}
              </div>
            </div>
          </div>

          {/* PRIVACY GUARANTEE */}
          <div className="p-2.5 rounded-lg bg-zinc-900/40 border border-zinc-800/60 text-[11px] text-zinc-400 flex items-center gap-2">
            <svg
              className="w-4 h-4 text-zinc-500 shrink-0"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
            </svg>
            <span>{COMMUTE_I18N.ZERO_TELEMETRY[lang]}</span>
          </div>
        </div>

        {/* FOOTER ACTIONS */}
        <div className="px-5 py-3 bg-zinc-900 border-t border-zinc-800 flex items-center justify-between gap-2">
          {config ? (
            <button
              type="button"
              onClick={() => {
                onClear();
                onClose();
              }}
              className="min-h-[48px] min-w-[48px] px-4 py-2 text-xs font-medium text-rose-400 hover:text-rose-300 hover:bg-rose-950/40 rounded-lg transition-colors cursor-pointer flex items-center justify-center"
            >
              {COMMUTE_I18N.CLEAR[lang]}
            </button>
          ) : <div />}

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onClose}
              className="min-h-[48px] min-w-[48px] px-4 py-2 text-xs text-zinc-300 hover:text-white rounded-lg hover:bg-zinc-800 transition-colors cursor-pointer flex items-center justify-center"
            >
              {COMMUTE_I18N.CANCEL[lang]}
            </button>
            <button
              type="button"
              onClick={handleSave}
              className="min-h-[48px] min-w-[48px] px-5 py-2 text-xs font-semibold rounded-lg bg-cyan-600 hover:bg-cyan-500 text-white transition-colors cursor-pointer shadow flex items-center justify-center"
            >
              {COMMUTE_I18N.SAVE_CONFIG[lang]}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
