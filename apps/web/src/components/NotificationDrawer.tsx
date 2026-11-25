'use client';
/**
 * NotificationDrawer.tsx — Monochromatic, accessible slide-over drawer for citizen
 * flood-alert preferences, Web Push subscriptions, route targeting, and vehicle threshold selection.
 * Issue #550 / Sprint 19 Task 2.
 * Strictly zero emoji per AGENTS.md Rule 1. Every interactive target >= 48px.
 */

import { useCallback, useEffect, useState } from 'react';
import { NOTIFICATION_DRAWER_I18N } from '../config/constants';
import { useWebPush } from '../hooks/useWebPush';
import type { VehicleType } from '../types/vehicle';
import { VEHICLE_PROFILES } from '../types/vehicle';
import { ARTERIAL_CORRIDORS } from '../utils/phuongFormatters';

export interface NotificationPreferences {
  targetType: 'my_route' | 'corridor' | 'all';
  selectedCorridor: string;
  vehicleType: VehicleType;
  alertHours: 'commute_only' | 'all_day';
}

export const PREFERENCES_STORAGE_KEY = 'kv_notification_preferences_v1';

export const DEFAULT_PREFERENCES: NotificationPreferences = {
  targetType: 'my_route',
  selectedCorridor: 'nguyen_huu_canh',
  vehicleType: 'scooter',
  alertHours: 'commute_only',
};

export interface NotificationDrawerProps {
  isOpen: boolean;
  onClose: () => void;
  lang?: 'vi' | 'en';
  initialCorridorKey?: string;
}

export default function NotificationDrawer({
  isOpen,
  onClose,
  lang = 'vi',
  initialCorridorKey,
}: NotificationDrawerProps) {
  const t = NOTIFICATION_DRAWER_I18N;
  const webPush = useWebPush();

  const [prefs, setPrefs] = useState<NotificationPreferences>(() => {
    if (typeof window === 'undefined') return DEFAULT_PREFERENCES;
    try {
      const stored = localStorage.getItem(PREFERENCES_STORAGE_KEY);
      if (stored) {
        const parsed = JSON.parse(stored);
        return {
          ...DEFAULT_PREFERENCES,
          ...parsed,
          selectedCorridor: initialCorridorKey || parsed.selectedCorridor || DEFAULT_PREFERENCES.selectedCorridor,
        };
      }
    } catch {
      // Fallback on corrupt JSON
    }
    return {
      ...DEFAULT_PREFERENCES,
      selectedCorridor: initialCorridorKey || DEFAULT_PREFERENCES.selectedCorridor,
    };
  });

  const [savedNotice, setSavedNotice] = useState(false);
  const [testSentNotice, setTestSentNotice] = useState(false);

  // Sync initial corridor if prop updates
  useEffect(() => {
    if (initialCorridorKey) {
      setPrefs(prev => ({
        ...prev,
        targetType: 'corridor',
        selectedCorridor: initialCorridorKey,
      }));
    }
  }, [initialCorridorKey]);

  // Handle ESC key to dismiss
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) {
        onClose();
      }
    };
    if (isOpen) {
      window.addEventListener('keydown', handleKeyDown);
    }
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  const handleSave = useCallback(() => {
    try {
      localStorage.setItem(PREFERENCES_STORAGE_KEY, JSON.stringify(prefs));
      setSavedNotice(true);
      setTimeout(() => setSavedNotice(false), 3000);
    } catch {
      // Ignore storage quota errors
    }
  }, [prefs]);

  const handleTogglePush = async () => {
    if (webPush.isSubscribed) {
      await webPush.unsubscribe();
    } else {
      const targetScope = prefs.targetType === 'corridor' ? 'corridor' : 'all';
      const targetId = prefs.targetType === 'corridor' ? prefs.selectedCorridor : '';
      await webPush.subscribe(targetScope, targetId);
    }
  };

  const handleSendTest = async () => {
    const ok = await webPush.sendTestNotification();
    if (ok) {
      setTestSentNotice(true);
      setTimeout(() => setTestSentNotice(false), 3500);
    }
  };

  if (!isOpen) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="notification-drawer-title"
      className="fixed inset-0 z-[980] overflow-hidden"
    >
      {/* Monochromatic backdrop */}
      <div
        className="fixed inset-0 bg-black/75 transition-opacity"
        onClick={onClose}
        aria-hidden="true"
      />

      <div className="fixed inset-y-0 right-0 max-w-full flex pl-10">
        <aside className="w-screen max-w-md bg-zinc-900 border-l border-zinc-800 text-zinc-100 flex flex-col shadow-2xl">
          {/* HEADER */}
          <header className="p-4 border-b border-zinc-800 flex items-center justify-between">
            <div>
              <h2 id="notification-drawer-title" className="text-base font-semibold text-zinc-100 tracking-tight">
                {t.DRAWER_TITLE[lang]}
              </h2>
              <p className="text-xs text-zinc-400 mt-0.5 leading-relaxed">
                {t.DRAWER_DESC[lang]}
              </p>
            </div>
            <button
              type="button"
              onClick={onClose}
              aria-label={t.CLOSE_DRAWER[lang]}
              className="min-w-[48px] min-h-[48px] flex items-center justify-center rounded-lg hover:bg-zinc-800 text-zinc-400 hover:text-zinc-200 transition-colors cursor-pointer"
            >
              <svg className="w-5 h-5" fill="none" stroke="currentColor" strokeWidth="2" viewBox="0 0 24 24" aria-hidden="true">
                <path strokeLinecap="round" strokeLinejoin="round" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </header>

          {/* SCROLLABLE BODY */}
          <div className="flex-1 overflow-y-auto p-4 space-y-6 text-sm">
            {/* SECTION 1: WEB PUSH DELIVERY STATUS */}
            <section className="space-y-3">
              <h3 className="text-xs uppercase tracking-wider font-semibold text-zinc-400">
                {t.STATUS_TITLE[lang]}
              </h3>

              <div className="p-3.5 rounded-lg bg-zinc-950/70 border border-zinc-800 space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2.5">
                    <span
                      className={`w-2.5 h-2.5 rounded-full ${
                        webPush.isSubscribed
                          ? 'bg-emerald-500 shadow-[0_0_8px_rgba(16,185,129,0.5)]'
                          : webPush.permission === 'denied'
                          ? 'bg-rose-500'
                          : 'bg-zinc-600'
                      }`}
                      aria-hidden="true"
                    />
                    <span className="text-xs font-medium text-zinc-200">
                      {webPush.isSubscribed
                        ? t.STATUS_ACTIVE[lang]
                        : webPush.permission === 'denied'
                        ? t.STATUS_DENIED[lang]
                        : !webPush.isSupported
                        ? t.STATUS_UNSUPPORTED[lang]
                        : t.STATUS_INACTIVE[lang]}
                    </span>
                  </div>

                  {webPush.isSupported && (
                    <button
                      type="button"
                      disabled={webPush.loading || webPush.permission === 'denied'}
                      onClick={handleTogglePush}
                      className={`min-h-[48px] px-4 py-2 text-xs font-semibold rounded-lg transition-colors cursor-pointer flex items-center justify-center ${
                        webPush.isSubscribed
                          ? 'bg-zinc-800 hover:bg-zinc-700 text-zinc-300 border border-zinc-700'
                          : 'bg-cyan-600 hover:bg-cyan-500 text-white'
                      } ${webPush.loading ? 'opacity-50 cursor-not-allowed' : ''}`}
                    >
                      {webPush.isSubscribed ? t.CTA_DISABLE[lang] : t.CTA_ENABLE[lang]}
                    </button>
                  )}
                </div>

                {/* TEST NOTIFICATION TRIGGER */}
                {webPush.isSubscribed && (
                  <div className="pt-2 border-t border-zinc-800/80 flex items-center justify-between">
                    <span className="text-[11px] text-zinc-400">
                      {testSentNotice
                        ? t.TEST_SENT[lang]
                        : lang === 'vi'
                        ? 'Kiểm tra hoạt động chuông thông báo:'
                        : 'Verify browser alert delivery:'}
                    </span>
                    <button
                      type="button"
                      disabled={webPush.loading}
                      onClick={handleSendTest}
                      className="min-h-[48px] min-w-[48px] px-3 py-2 text-xs text-zinc-300 hover:text-white bg-zinc-800 hover:bg-zinc-700 rounded-lg transition-colors cursor-pointer flex items-center justify-center"
                    >
                      {t.CTA_TEST[lang]}
                    </button>
                  </div>
                )}
              </div>
            </section>

            {/* SECTION 2: ALERT TRACKING SCOPE */}
            <section className="space-y-3">
              <h3 className="text-xs uppercase tracking-wider font-semibold text-zinc-400">
                {t.TARGET_SECTION_TITLE[lang]}
              </h3>

              <div className="space-y-2">
                {/* OPTION A: MY ROUTE */}
                <label className="min-h-[48px] p-3 rounded-lg border border-zinc-800 bg-zinc-950/40 hover:bg-zinc-800/40 cursor-pointer flex items-start gap-3 transition-colors">
                  <input
                    type="radio"
                    name="targetScope"
                    value="my_route"
                    checked={prefs.targetType === 'my_route'}
                    onChange={() => setPrefs(p => ({ ...p, targetType: 'my_route' }))}
                    className="mt-1 h-4 w-4 text-cyan-500 focus:ring-cyan-400 bg-zinc-900 border-zinc-700"
                  />
                  <div>
                    <span className="font-medium text-zinc-200 block text-xs">
                      {t.TARGET_MY_ROUTE[lang]}
                    </span>
                    <span className="text-[11px] text-zinc-400 block mt-0.5 leading-relaxed">
                      {t.TARGET_MY_ROUTE_SUB[lang]}
                    </span>
                  </div>
                </label>

                {/* OPTION B: ARTERIAL CORRIDORS */}
                <label className="min-h-[48px] p-3 rounded-lg border border-zinc-800 bg-zinc-950/40 hover:bg-zinc-800/40 cursor-pointer flex items-start gap-3 transition-colors">
                  <input
                    type="radio"
                    name="targetScope"
                    value="corridor"
                    checked={prefs.targetType === 'corridor'}
                    onChange={() => setPrefs(p => ({ ...p, targetType: 'corridor' }))}
                    className="mt-1 h-4 w-4 text-cyan-500 focus:ring-cyan-400 bg-zinc-900 border-zinc-700"
                  />
                  <div className="flex-1">
                    <span className="font-medium text-zinc-200 block text-xs">
                      {t.TARGET_CORRIDORS[lang]}
                    </span>
                    {prefs.targetType === 'corridor' && (
                      <select
                        value={prefs.selectedCorridor}
                        onChange={e => setPrefs(p => ({ ...p, selectedCorridor: e.target.value }))}
                        className="min-h-[48px] mt-2 w-full bg-zinc-900 border border-zinc-700 rounded-lg p-2.5 text-xs text-zinc-200 focus:outline-none focus:border-cyan-500"
                      >
                        {ARTERIAL_CORRIDORS.map(corridorName => {
                          const valKey = corridorName.toLowerCase().replace(/\s+/g, '_');
                          return (
                            <option key={corridorName} value={valKey}>
                              {corridorName}
                            </option>
                          );
                        })}
                      </select>
                    )}
                  </div>
                </label>

                {/* OPTION C: ALL CITY HOTSPOTS */}
                <label className="min-h-[48px] p-3 rounded-lg border border-zinc-800 bg-zinc-950/40 hover:bg-zinc-800/40 cursor-pointer flex items-start gap-3 transition-colors">
                  <input
                    type="radio"
                    name="targetScope"
                    value="all"
                    checked={prefs.targetType === 'all'}
                    onChange={() => setPrefs(p => ({ ...p, targetType: 'all' }))}
                    className="mt-1 h-4 w-4 text-cyan-500 focus:ring-cyan-400 bg-zinc-900 border-zinc-700"
                  />
                  <div>
                    <span className="font-medium text-zinc-200 block text-xs">
                      {t.TARGET_ALL_CITY[lang]}
                    </span>
                  </div>
                </label>
              </div>
            </section>

            {/* SECTION 3: VEHICLE CLEARANCE THRESHOLD */}
            <section className="space-y-3">
              <div>
                <h3 className="text-xs uppercase tracking-wider font-semibold text-zinc-400">
                  {t.VEHICLE_SECTION_TITLE[lang]}
                </h3>
                <p className="text-[11px] text-zinc-500 mt-0.5">
                  {t.VEHICLE_SECTION_SUB[lang]}
                </p>
              </div>

              <div className="grid grid-cols-2 gap-2">
                {(['scooter', 'motorbike', 'sedan', 'suv'] as VehicleType[]).map(vType => {
                  const prof = VEHICLE_PROFILES[vType];
                  const isSelected = prefs.vehicleType === vType;
                  return (
                    <button
                      key={vType}
                      type="button"
                      onClick={() => setPrefs(p => ({ ...p, vehicleType: vType }))}
                      className={`min-h-[48px] p-3 rounded-lg border text-left transition-colors cursor-pointer flex flex-col justify-between ${
                        isSelected
                          ? 'border-cyan-500 bg-cyan-950/30 text-white'
                          : 'border-zinc-800 bg-zinc-950/40 hover:bg-zinc-800/40 text-zinc-300'
                      }`}
                    >
                      <span className="text-xs font-semibold block">{prof.name[lang]}</span>
                      <span className="text-[10px] text-zinc-400 mt-1 block">
                        {lang === 'vi' ? 'Nguy cơ: ' : 'Threshold: '}&gt;={prof.cautionDepthCm} cm
                      </span>
                    </button>
                  );
                })}
              </div>
            </section>

            {/* SECTION 4: COMMUTE HOURS FILTER */}
            <section className="space-y-3">
              <h3 className="text-xs uppercase tracking-wider font-semibold text-zinc-400">
                {t.HOURS_SECTION_TITLE[lang]}
              </h3>

              <div className="space-y-2">
                <label className="min-h-[48px] p-3 rounded-lg border border-zinc-800 bg-zinc-950/40 hover:bg-zinc-800/40 cursor-pointer flex items-center gap-3 transition-colors">
                  <input
                    type="radio"
                    name="alertHours"
                    value="commute_only"
                    checked={prefs.alertHours === 'commute_only'}
                    onChange={() => setPrefs(p => ({ ...p, alertHours: 'commute_only' }))}
                    className="h-4 w-4 text-cyan-500 focus:ring-cyan-400 bg-zinc-900 border-zinc-700"
                  />
                  <span className="text-xs font-medium text-zinc-200">
                    {t.HOURS_COMMUTE_ONLY[lang]}
                  </span>
                </label>

                <label className="min-h-[48px] p-3 rounded-lg border border-zinc-800 bg-zinc-950/40 hover:bg-zinc-800/40 cursor-pointer flex items-center gap-3 transition-colors">
                  <input
                    type="radio"
                    name="alertHours"
                    value="all_day"
                    checked={prefs.alertHours === 'all_day'}
                    onChange={() => setPrefs(p => ({ ...p, alertHours: 'all_day' }))}
                    className="h-4 w-4 text-cyan-500 focus:ring-cyan-400 bg-zinc-900 border-zinc-700"
                  />
                  <span className="text-xs font-medium text-zinc-200">
                    {t.HOURS_ALL_DAY[lang]}
                  </span>
                </label>
              </div>
            </section>
          </div>

          {/* FOOTER ACTIONS */}
          <footer className="p-4 border-t border-zinc-800 bg-zinc-950/80 flex items-center justify-between gap-3">
            <span className="text-xs text-emerald-400 font-medium">
              {savedNotice && t.SAVED_SUCCESS[lang]}
            </span>

            <div className="flex items-center gap-2 ml-auto">
              <button
                type="button"
                onClick={onClose}
                className="min-h-[48px] min-w-[48px] px-4 py-2 text-xs font-medium text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 rounded-lg transition-colors cursor-pointer"
              >
                {t.CLOSE_DRAWER[lang]}
              </button>
              <button
                type="button"
                onClick={handleSave}
                className="min-h-[48px] px-5 py-2 text-xs font-semibold text-white bg-cyan-600 hover:bg-cyan-500 rounded-lg transition-colors cursor-pointer shadow"
              >
                {t.SAVE_CHANGES[lang]}
              </button>
            </div>
          </footer>
        </aside>
      </div>
    </div>
  );
}
