'use client';
/**
 * NotificationPromptBanner.tsx — Subtle, monochromatic opt-in prompt inviting commuters
 * to configure real-time flood alerts along their commute route.
 * Issue #550 / Sprint 19 Task 2.
 * Strictly zero emoji per AGENTS.md Rule 1. Touch targets >= 48px.
 */

import { useState } from 'react';
import { NOTIFICATION_DRAWER_I18N } from '../config/constants';
import { useWebPush } from '../hooks/useWebPush';

export interface NotificationPromptBannerProps {
  onOpenDrawer: () => void;
  lang?: 'vi' | 'en';
}

export default function NotificationPromptBanner({
  onOpenDrawer,
  lang = 'vi',
}: NotificationPromptBannerProps) {
  const t = NOTIFICATION_DRAWER_I18N;
  const webPush = useWebPush();
  const [dismissed, setDismissed] = useState(false);

  // Only render if Web Push is supported, not yet subscribed, and permission is still default
  if (!webPush.isSupported || webPush.isSubscribed || webPush.permission !== 'default' || dismissed) {
    return null;
  }

  return (
    <div
      role="region"
      aria-label={t.PROMPT_TITLE[lang]}
      className="p-3 bg-zinc-900/95 border-b border-zinc-800 text-zinc-100 backdrop-blur-md transition-all shadow-md"
    >
      <div className="max-w-7xl mx-auto flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3 px-2">
        <div className="flex items-center gap-3">
          <div className="min-w-[36px] min-h-[36px] flex items-center justify-center rounded-lg bg-cyan-950/60 border border-cyan-800/80 text-cyan-400 shrink-0">
            <svg className="w-5 h-5 fill-none stroke-current" strokeWidth="2" viewBox="0 0 24 24" aria-hidden="true">
              <path strokeLinecap="round" strokeLinejoin="round" d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
            </svg>
          </div>
          <div>
            <h3 className="text-xs font-semibold text-zinc-100">
              {t.PROMPT_TITLE[lang]}
            </h3>
            <p className="text-[11px] text-zinc-400 leading-tight mt-0.5">
              {t.PROMPT_DESC[lang]}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-2 self-end sm:self-auto shrink-0">
          <button
            type="button"
            onClick={() => setDismissed(true)}
            className="min-h-[48px] min-w-[48px] px-3 py-2 text-xs text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800 rounded-lg transition-colors cursor-pointer"
          >
            {t.PROMPT_DISMISS[lang]}
          </button>
          <button
            type="button"
            onClick={onOpenDrawer}
            className="min-h-[48px] px-4 py-2 text-xs font-semibold text-white bg-cyan-600 hover:bg-cyan-500 rounded-lg transition-colors cursor-pointer shadow flex items-center justify-center"
          >
            {t.PROMPT_CTA[lang]}
          </button>
        </div>
      </div>
    </div>
  );
}
