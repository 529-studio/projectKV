'use client';
/**
 * S12-T4 / Issue #402 — citizen flood-alert subscription dialog.
 *
 * Contract: `docs/sprints/s12_t4_spec.md` §5.3, §5.4, §6.
 *
 * Monochromatic zinc surfaces, inline SVG only, zero emoji. Every interactive
 * target is at least 48 × 48 px. The dialog owns its language toggle so VI ↔ EN
 * re-renders instantly without a refetch and without losing the selection.
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { SUBSCRIPTION_I18N } from '../config/constants';
import { useSubscriptions } from '../hooks/useSubscriptions';
import {
  buildTelegramDeepLink,
  describeTarget,
  filterWards,
  isValidChatId,
  mapSubmitError,
  normalizeBotUsername,
  normalizeSearch,
  type TargetSelection,
  type TargetType,
  type WardItem,
} from '../utils/subscriptionUtils';

export interface SubscriptionModalProps {
  isOpen: boolean;
  onClose: () => void;
  lang: 'vi' | 'en';
  initialTarget?: TargetSelection;
  botUsernameFallback?: string;
}

const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

const RESULTS_LIMIT = 20;

type ModalPhase = 'idle' | 'submitting' | 'success' | 'validation-error' | 'network-error';

const SCOPES: { value: TargetType; labelKey: keyof typeof SUBSCRIPTION_I18N }[] = [
  { value: 'all', labelKey: 'SCOPE_ALL' },
  { value: 'zone', labelKey: 'SCOPE_ZONE' },
  { value: 'ward', labelKey: 'SCOPE_WARD' },
  { value: 'chokepoint', labelKey: 'SCOPE_CHOKEPOINT' },
];

function checkIcon() {
  return (
    <svg
      className="h-4 w-4 shrink-0"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      viewBox="0 0 24 24"
      aria-hidden="true"
    >
      <path d="m5 12 5 5 9-10" />
    </svg>
  );
}

export default function SubscriptionModal({
  isOpen,
  onClose,
  lang,
  initialTarget,
  botUsernameFallback,
}: SubscriptionModalProps) {
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const {
    taxonomy,
    loading,
    error,
    refetch,
    submit,
    botUsername,
  } = useSubscriptions();

  const [activeLang, setActiveLang] = useState<'vi' | 'en'>(lang);
  const [scope, setScope] = useState<TargetType>(initialTarget?.type ?? 'all');
  const [zone, setZone] = useState(initialTarget?.type === 'zone' ? initialTarget.id : '');
  const [wardQuery, setWardQuery] = useState('');
  const [selectedWard, setSelectedWard] = useState(
    initialTarget?.type === 'ward' ? initialTarget.id : '',
  );
  const [chokepointQuery, setChokepointQuery] = useState('');
  const [selectedChokepoint, setSelectedChokepoint] = useState(
    initialTarget?.type === 'chokepoint' ? initialTarget.id : '',
  );
  const [manualOpen, setManualOpen] = useState(false);
  const [chatId, setChatId] = useState('');
  const [phase, setPhase] = useState<ModalPhase>('idle');
  const [fieldError, setFieldError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);

  const t = (key: keyof typeof SUBSCRIPTION_I18N): string => SUBSCRIPTION_I18N[key][activeLang];

  // The page language is the source of truth when the dialog is reopened.
  useEffect(() => {
    setActiveLang(lang);
  }, [lang]);

  // Reset transient state every time the dialog opens, and apply the caller's
  // initial target so a camera/corridor entry point can preselect its scope.
  useEffect(() => {
    if (!isOpen) return;
    setPhase('idle');
    setFieldError(null);
    setFormError(null);
    setChatId('');
    setManualOpen(false);
    // VI is the default on every open; the page language wins until the citizen
    // flips the in-dialog toggle (AC13).
    setActiveLang(lang);
    if (!initialTarget) return;
    setScope(initialTarget.type);
    if (initialTarget.type === 'zone') setZone(initialTarget.id);
    if (initialTarget.type === 'ward') setSelectedWard(initialTarget.id);
    if (initialTarget.type === 'chokepoint') setSelectedChokepoint(initialTarget.id);
    // initialTarget is intentionally read only on open; a language switch must
    // not overwrite the citizen's current selection.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen]);

  // Lazy taxonomy: the first open triggers the request, never page mount.
  useEffect(() => {
    if (!isOpen) return;
    if (taxonomy || loading || error) return;
    void refetch();
  }, [isOpen, taxonomy, loading, error, refetch]);

  // Focus trap, Escape, scroll lock, focus restore (mirrors NearMeModal).
  useEffect(() => {
    if (!isOpen || typeof document === 'undefined') return undefined;

    const previouslyFocused = document.activeElement as HTMLElement | null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const dialog = dialogRef.current;
    const initial =
      (dialog?.querySelector<HTMLElement>('[data-subscription-initial-focus]') ?? dialog) ?? null;
    initial?.focus();

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        onClose();
        return;
      }
      if (event.key !== 'Tab') return;

      const node = dialogRef.current;
      if (!node) return;

      const focusables = Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)).filter(
        (element) => !element.hasAttribute('disabled') && element.getAttribute('aria-hidden') !== 'true',
      );

      if (focusables.length === 0) {
        event.preventDefault();
        node.focus();
        return;
      }

      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement as HTMLElement | null;

      if (event.shiftKey) {
        if (active === first || !node.contains(active)) {
          event.preventDefault();
          last.focus();
        }
      } else if (active === last || !node.contains(active)) {
        event.preventDefault();
        first.focus();
      }
    };

    document.addEventListener('keydown', handleKeyDown, true);
    return () => {
      document.removeEventListener('keydown', handleKeyDown, true);
      document.body.style.overflow = previousOverflow;
      previouslyFocused?.focus?.();
    };
  }, [isOpen, onClose]);

  const zones = useMemo(() => taxonomy?.zones ?? [], [taxonomy]);
  const wards = useMemo(() => taxonomy?.wards ?? [], [taxonomy]);
  const chokepoints = useMemo(() => taxonomy?.chokepoints ?? [], [taxonomy]);

  const wardResults = useMemo<WardItem[]>(
    () => filterWards(wards, wardQuery, RESULTS_LIMIT),
    [wards, wardQuery],
  );

  const chokepointResults = useMemo(() => {
    const needle = normalizeSearch(chokepointQuery);
    const filtered = chokepoints.filter((point) => {
      if (!needle) return true;
      return normalizeSearch(`${point.name ?? ''} ${point.id}`).includes(needle);
    });
    return filtered.slice(0, RESULTS_LIMIT);
  }, [chokepoints, chokepointQuery]);

  const selection = useMemo<TargetSelection | null>(() => {
    switch (scope) {
      case 'all':
        return { type: 'all', id: '' };
      case 'zone':
        return zone ? { type: 'zone', id: zone } : null;
      case 'ward':
        return selectedWard ? { type: 'ward', id: selectedWard } : null;
      case 'chokepoint':
        return selectedChokepoint ? { type: 'chokepoint', id: selectedChokepoint } : null;
      default:
        return null;
    }
  }, [scope, zone, selectedWard, selectedChokepoint]);

  // The API-published handle wins. The caller-supplied fallback is consulted
  // only when the taxonomy did not publish a handle at all, so an explicitly
  // unprovisioned server can never be papered over by a build-time value.
  const apiPublishedHandle = Boolean(taxonomy?.bot_username);
  const effectiveBotUsername =
    botUsername ?? (apiPublishedHandle ? null : normalizeBotUsername(botUsernameFallback));

  const deepLink = useMemo(() => {
    if (!selection || !effectiveBotUsername) return null;
    try {
      return buildTelegramDeepLink(effectiveBotUsername, selection);
    } catch {
      return null;
    }
  }, [selection, effectiveBotUsername]);

  const selectionPrompt =
    scope === 'zone'
      ? SUBSCRIPTION_I18N.ZONE_PLACEHOLDER[activeLang]
      : scope === 'ward'
        ? SUBSCRIPTION_I18N.SEARCH_WARD[activeLang]
        : SUBSCRIPTION_I18N.SEARCH_CHOKEPOINT[activeLang];

  const announcement =
    phase === 'success'
      ? t('SUCCESS_TITLE')
      : phase === 'submitting'
        ? t('SUBMITTING')
        : phase === 'validation-error' && fieldError
          ? fieldError
          : phase === 'validation-error' && formError
            ? formError
            : phase === 'network-error'
              ? t('ERROR_NETWORK')
              : t('SUBTITLE');

  // Do not render anything while closed, but keep hook state so selection
  // survives a close/reopen within the same page visit.
  if (!isOpen) return null;

  const titleId = 'subscription-title';
  const subtitleId = 'subscription-subtitle';
  const chatHintId = 'subscription-chat-hint';
  const chatErrorId = 'subscription-chat-error';

  const handleManualSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (phase === 'submitting' || !selection) return;

    const trimmed = chatId.trim();
    if (!isValidChatId(trimmed)) {
      setPhase('validation-error');
      setFieldError(t('CHAT_ID_INVALID'));
      return;
    }

    setPhase('submitting');
    setFieldError(null);
    setFormError(null);
    const result = await submit(selection, trimmed);
    if (result.ok) {
      setPhase('success');
      return;
    }
    if (result.status === 400) {
      const copy = mapSubmitError(result.status, result.body, activeLang);
      if (copy === SUBSCRIPTION_I18N.CHAT_ID_INVALID[activeLang]) {
        setFieldError(copy);
      } else {
        setFormError(copy);
      }
      setPhase('validation-error');
      return;
    }
    setPhase('network-error');
    setFieldError(mapSubmitError(result.status, result.body, activeLang));
  };

  const renderScopeSelector = () => (
    <div role="radiogroup" aria-labelledby="subscription-scope-label" className="mt-2 grid grid-cols-2 gap-2">
      <span id="subscription-scope-label" className="sr-only">
        {t('SUBTITLE')}
      </span>
      {SCOPES.map((option) => {
        const checked = scope === option.value;
        return (
          <label
            key={option.value}
            className={`flex min-h-[48px] min-w-[48px] cursor-pointer items-center justify-center rounded-lg border px-2 text-center text-xs font-semibold transition-colors focus-within:outline focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-zinc-100 ${
              checked
                ? 'border-zinc-100 bg-zinc-100 text-zinc-950'
                : 'border-zinc-700 bg-zinc-900 text-zinc-300 hover:border-zinc-500 hover:text-white'
            }`}
          >
            <input
              type="radio"
              name="subscription-scope"
              value={option.value}
              checked={checked}
              onChange={() => setScope(option.value)}
              className="sr-only"
            />
            <span>{SUBSCRIPTION_I18N[option.labelKey][activeLang]}</span>
          </label>
        );
      })}
    </div>
  );

  const renderTargetPicker = () => {
    if (scope === 'all') {
      return (
        <p className="mt-3 text-xs leading-relaxed text-zinc-400">
          {describeTarget({ type: 'all', id: '' }, activeLang)}
        </p>
      );
    }

    if (scope === 'zone') {
      return (
        <div className="mt-3">
          <label htmlFor="subscription-zone" className="text-[11px] font-medium text-zinc-400">
            {t('ZONE_LABEL')}
          </label>
          <select
            id="subscription-zone"
            value={zone}
            onChange={(event) => setZone(event.target.value)}
            className="mt-1 flex min-h-[48px] w-full rounded-lg border border-zinc-700 bg-zinc-900 px-3 text-sm text-zinc-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
          >
            <option value="">{t('ZONE_PLACEHOLDER')}</option>
            {zones.map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
        </div>
      );
    }

    if (scope === 'ward') {
      return (
        <div className="mt-3">
          <label htmlFor="subscription-ward-search" className="text-[11px] font-medium text-zinc-400">
            {t('WARD_LABEL')}
          </label>
          <input
            id="subscription-ward-search"
            type="search"
            value={wardQuery}
            onChange={(event) => setWardQuery(event.target.value)}
            placeholder={t('SEARCH_WARD')}
            autoComplete="off"
            className="mt-1 flex min-h-[48px] w-full rounded-lg border border-zinc-700 bg-zinc-900 px-3 text-sm text-zinc-100 placeholder:text-zinc-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
          />
          {wards.length === 0 ? (
            <p className="mt-2 text-[11px] text-zinc-500">{t('ERROR_UNKNOWN_TARGET')}</p>
          ) : (
            <ul className="mt-1 max-h-48 overflow-y-auto overscroll-contain rounded-lg border border-zinc-800">
              {wardResults.map((ward) => {
                const selected = selectedWard === ward.name;
                return (
                  <li key={`${ward.type}-${ward.name}`}>
                    <button
                      type="button"
                      onClick={() => setSelectedWard(ward.name)}
                      aria-pressed={selected}
                      className={`flex min-h-[48px] w-full items-center justify-between gap-2 px-3 text-left text-xs transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-zinc-100 ${
                        selected ? 'bg-zinc-800 text-white' : 'text-zinc-300 hover:bg-zinc-900 hover:text-white'
                      }`}
                    >
                      <span className="min-w-0 truncate">
                        {ward.type ? `${ward.type} ${ward.name}` : ward.name}
                      </span>
                      {selected && checkIcon()}
                    </button>
                  </li>
                );
              })}
              {wardResults.length === 0 && (
                <li className="px-3 py-3 text-[11px] text-zinc-500">{t('ERROR_UNKNOWN_TARGET')}</li>
              )}
            </ul>
          )}
        </div>
      );
    }

    return (
      <div className="mt-3">
        <label htmlFor="subscription-chokepoint-search" className="text-[11px] font-medium text-zinc-400">
          {t('CHOKEPOINT_LABEL')}
        </label>
        <input
          id="subscription-chokepoint-search"
          type="search"
          value={chokepointQuery}
          onChange={(event) => setChokepointQuery(event.target.value)}
          placeholder={t('SEARCH_CHOKEPOINT')}
          autoComplete="off"
          className="mt-1 flex min-h-[48px] w-full rounded-lg border border-zinc-700 bg-zinc-900 px-3 text-sm text-zinc-100 placeholder:text-zinc-500 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
        />
        {chokepoints.length === 0 ? (
          <p className="mt-2 text-[11px] text-zinc-500">{t('ERROR_UNKNOWN_TARGET')}</p>
        ) : (
          <ul className="mt-1 max-h-48 overflow-y-auto overscroll-contain rounded-lg border border-zinc-800">
            {chokepointResults.map((point) => {
              const selected = selectedChokepoint === point.id;
              return (
                <li key={point.id}>
                  <button
                    type="button"
                    onClick={() => setSelectedChokepoint(point.id)}
                    aria-pressed={selected}
                    className={`flex min-h-[48px] w-full items-center justify-between gap-2 px-3 text-left text-xs transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-zinc-100 ${
                      selected ? 'bg-zinc-800 text-white' : 'text-zinc-300 hover:bg-zinc-900 hover:text-white'
                    }`}
                  >
                    <span className="min-w-0">
                      <span className="block truncate">{point.name || point.id}</span>
                      {point.name && (
                        <span className="mt-0.5 block truncate font-mono text-[10px] text-zinc-500">{point.id}</span>
                      )}
                    </span>
                    {selected && checkIcon()}
                  </button>
                </li>
              );
            })}
            {chokepointResults.length === 0 && (
              <li className="px-3 py-3 text-[11px] text-zinc-500">{t('ERROR_UNKNOWN_TARGET')}</li>
            )}
          </ul>
        )}
      </div>
    );
  };

  const renderSkeleton = () => (
    <div aria-busy="true" className="space-y-3 px-5 py-5">
      <span className="block h-3 w-2/3 animate-pulse rounded bg-zinc-800" />
      <span className="block h-12 w-full animate-pulse rounded-lg bg-zinc-900" />
      <span className="block h-12 w-full animate-pulse rounded-lg bg-zinc-900" />
      <span className="block h-3 w-1/2 animate-pulse rounded bg-zinc-800" />
    </div>
  );

  const renderTaxonomyError = () => (
    <div className="px-5 py-6">
      <h3 className="text-sm font-semibold text-zinc-100">{t('ERROR_NETWORK')}</h3>
      <button
        type="button"
        onClick={() => void refetch()}
        className="mt-4 flex min-h-[48px] min-w-[48px] w-full items-center justify-center rounded-lg border border-zinc-500 bg-zinc-100 px-4 text-sm font-semibold text-zinc-950 transition-colors hover:bg-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
      >
        {t('RETRY')}
      </button>
    </div>
  );

  const renderSuccess = () => (
    <div className="px-5 py-6">
      <h3 className="text-sm font-semibold text-zinc-100">{t('SUCCESS_TITLE')}</h3>
      <p className="mt-2 text-xs leading-relaxed text-zinc-400">{t('SUCCESS_BODY')}</p>
      {selection && (
        <p className="mt-2 text-xs font-medium text-zinc-300">{describeTarget(selection, activeLang)}</p>
      )}
      <button
        type="button"
        onClick={onClose}
        className="mt-5 flex min-h-[48px] min-w-[48px] w-full items-center justify-center rounded-lg border border-zinc-500 bg-zinc-100 px-4 text-sm font-semibold text-zinc-950 transition-colors hover:bg-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
      >
        {t('CLOSE')}
      </button>
    </div>
  );

  const renderForm = () => (
    <div className="px-4 py-4">
      <fieldset disabled={phase === 'submitting'} className="m-0 min-w-0 border-0 p-0">
        {renderScopeSelector()}
        {renderTargetPicker()}
      </fieldset>

      <div className="mt-4">
        {deepLink ? (
          <a
            href={deepLink}
            target="_blank"
            rel="noopener noreferrer"
            title={t('OPEN_TELEGRAM')}
            className="flex min-h-[48px] min-w-[48px] w-full items-center justify-center gap-2 rounded-lg border border-zinc-100 bg-zinc-100 px-4 text-sm font-semibold text-zinc-950 transition-colors hover:bg-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100 active:bg-zinc-300"
          >
            <svg
              className="h-4 w-4 shrink-0"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              strokeLinejoin="round"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path d="m4 4 16 8-16 8 3-8-3-8Z" />
            </svg>
            <span>{t('TELEGRAM_CTA')}</span>
          </a>
        ) : (
          <span
            role="note"
            className="flex min-h-[48px] min-w-[48px] w-full items-center justify-center rounded-lg border border-dashed border-zinc-700 bg-zinc-900/60 px-4 text-center text-xs text-zinc-500"
          >
            {effectiveBotUsername ? selectionPrompt : t('BOT_UNAVAILABLE')}
          </span>
        )}
        <p className="mt-2 text-[11px] leading-relaxed text-zinc-500">
          {effectiveBotUsername ? t('TELEGRAM_HINT') : t('BOT_UNAVAILABLE')}
        </p>
      </div>

      <div className="mt-4 border-t border-zinc-800 pt-3">
        <button
          type="button"
          onClick={() => setManualOpen((previous) => !previous)}
          aria-expanded={manualOpen}
          aria-controls="subscription-manual"
          className="flex min-h-[48px] min-w-[48px] w-full items-center justify-between gap-2 rounded-lg px-2 text-left text-xs font-medium text-zinc-300 transition-colors hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
        >
          <span>{t('MANUAL_TOGGLE')}</span>
          <svg
            className={`h-4 w-4 shrink-0 transition-transform ${manualOpen ? 'rotate-180' : ''}`}
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <path d="m6 9 6 6 6-6" />
          </svg>
        </button>

        {manualOpen && (
          <form id="subscription-manual" className="mt-2 space-y-3" onSubmit={handleManualSubmit} noValidate>
            <div>
              <label htmlFor="subscription-chat-id" className="text-[11px] font-medium text-zinc-400">
                {t('CHAT_ID_LABEL')}
              </label>
              <input
                id="subscription-chat-id"
                name="chat_id"
                type="text"
                inputMode="numeric"
                autoComplete="off"
                value={chatId}
                onChange={(event) => {
                  setChatId(event.target.value.slice(0, 32));
                  if (fieldError) setFieldError(null);
                }}
                disabled={phase === 'submitting'}
                aria-describedby={fieldError ? `${chatHintId} ${chatErrorId}` : chatHintId}
                aria-invalid={fieldError ? true : undefined}
                className="mt-1 flex min-h-[48px] w-full rounded-lg border border-zinc-700 bg-zinc-900 px-3 font-mono text-sm tracking-wide text-zinc-100 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
              />
              <p id={chatHintId} className="mt-1 text-[11px] leading-relaxed text-zinc-500">
                {t('CHAT_ID_HINT')}
              </p>
              {fieldError && (
                <p id={chatErrorId} className="mt-1 text-[11px] leading-relaxed text-zinc-200">
                  {fieldError}
                </p>
              )}
            </div>

            <button
              type="submit"
              disabled={phase === 'submitting' || !selection}
              aria-busy={phase === 'submitting'}
              className="flex min-h-[48px] min-w-[48px] w-full items-center justify-center rounded-lg border border-zinc-500 bg-zinc-100 px-4 text-sm font-semibold text-zinc-950 transition-colors hover:bg-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100 disabled:cursor-not-allowed disabled:border-zinc-800 disabled:bg-zinc-900 disabled:text-zinc-600"
            >
              {phase === 'submitting' ? t('SUBMITTING') : t('SUBMIT')}
            </button>

            {phase === 'network-error' && (
              <div className="rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-2 text-[11px] leading-relaxed text-zinc-300">
                {t('ERROR_NETWORK')}
              </div>
            )}
            {phase === 'validation-error' && formError && (
              <div role="alert" className="rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-2 text-[11px] leading-relaxed text-zinc-300">
                {formError}
              </div>
            )}
          </form>
        )}
      </div>

      <p className="mt-3 text-[10.5px] leading-relaxed text-zinc-500">{t('PRIVACY_NOTE')}</p>
    </div>
  );

  return (
    <>
      <div className="fixed inset-0 z-[960] bg-black/70" onClick={onClose} aria-hidden="true" />

      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={subtitleId}
        tabIndex={-1}
        className="fixed inset-x-0 bottom-0 z-[970] flex max-h-[85vh] w-full flex-col overflow-hidden rounded-t-2xl border border-zinc-800 border-b-0 bg-zinc-950 text-zinc-300 shadow-2xl outline-none md:inset-0 md:m-auto md:h-fit md:max-h-[85vh] md:max-w-md md:rounded-xl md:border-b"
      >
        <div className="flex shrink-0 items-start gap-3 border-b border-zinc-800 px-4 py-3">
          <div className="min-w-0 flex-1">
            <h2 id={titleId} className="font-mono text-sm font-bold uppercase tracking-wider text-zinc-100">
              {t('TITLE')}
            </h2>
            <p id={subtitleId} className="mt-1 text-[11px] leading-relaxed text-zinc-400">
              {t('SUBTITLE')}
            </p>
          </div>
          <button
            type="button"
            onClick={() => setActiveLang(activeLang === 'vi' ? 'en' : 'vi')}
            aria-label={activeLang === 'vi' ? 'Chuyển sang tiếng Anh' : 'Switch to Vietnamese'}
            className="flex min-h-[48px] min-w-[48px] shrink-0 items-center justify-center rounded-lg border border-zinc-800 bg-zinc-900 px-3 font-mono text-xs font-semibold text-zinc-300 transition-colors hover:border-zinc-600 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
          >
            {activeLang === 'vi' ? 'EN' : 'VI'}
          </button>
          <button
            type="button"
            onClick={onClose}
            data-subscription-initial-focus="true"
            aria-label={t('CLOSE')}
            className="flex min-h-[48px] min-w-[48px] shrink-0 items-center justify-center gap-1.5 rounded-lg border border-zinc-800 bg-zinc-900 px-2 text-xs font-medium text-zinc-200 transition-colors hover:border-zinc-600 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
          >
            <svg
              className="h-4 w-4 shrink-0"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              strokeLinejoin="round"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path d="m6 6 12 12M18 6 6 18" />
            </svg>
            <span>{t('CLOSE')}</span>
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          {loading && !taxonomy
            ? renderSkeleton()
            : error && !taxonomy
              ? renderTaxonomyError()
              : phase === 'success'
                ? renderSuccess()
                : renderForm()}
        </div>

        <div className="sr-only" role="status" aria-live="polite">
          {announcement}
        </div>
      </div>
    </>
  );
}
