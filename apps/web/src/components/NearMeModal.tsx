'use client';
/**
 * S12-T2 / Issue #400 — "Near me" bottom sheet / dialog.
 *
 * Contract: `docs/sprints/s12_t2_spec.md` §4.3, §5 E1–E22.
 * Monochromatic zinc surfaces, inline SVG icons only, every interactive target
 * at least 48 × 48 px, rows 64 px. Normative state precedence lives in
 * `resolveView()` and is mirrored by the render branches below.
 */
import { useEffect, useMemo, useRef, useState } from 'react';
import { NEAR_ME_I18N } from '../config/constants';
import type { Camera } from '../hooks/useCameras';
import type { GeolocationErrorCode, GeolocationFix, GeolocationStatus } from '../hooks/useGeolocation';
import {
  LOW_ACCURACY_BAD_METERS,
  LOW_ACCURACY_WARN_METERS,
  formatDistance,
  type NearMeResult,
} from '../utils/geoDistance';

export interface NearMeModalProps {
  isOpen: boolean;
  onClose: () => void;
  status: GeolocationStatus;
  fix: GeolocationFix | null;
  geoError: GeolocationErrorCode | null;
  results: NearMeResult[];
  offlineCountInRadius: number;
  camerasLoading: boolean;
  camerasError: Error | null;
  hideOffline: boolean;
  isOutsideServiceArea: boolean;
  lang: 'vi' | 'en';
  onLangChange?: (lang: 'vi' | 'en') => void;
  onRetry: () => void;
  onRetryCameras?: () => void;
  onRetryGeo?: () => void;
  onRecenter: () => void;
  onToggleOffline: () => void;
  onCameraClick: (cam: Camera) => void;
  onViewFullMap: () => void;
  onBackToCity: () => void;
}

const GEO_FAILURE_STATUSES: ReadonlySet<GeolocationStatus> = new Set<GeolocationStatus>([
  'denied',
  'timeout',
  'unavailable',
  'insecure',
  'unsupported',
  'invalid',
]);

const PAGE_SIZE = 10;
const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

type ErrorView = 'denied' | 'timeout' | 'unavailable' | 'insecure' | 'unsupported' | 'invalid';

function floodSeverityLabel(severity: string | null, lang: 'vi' | 'en'): string {
  switch (severity) {
    case 'FLOODED_DEEP':
      return NEAR_ME_I18N.SEVERITY.DEEP[lang];
    case 'FLOODED_SHALLOW':
      return NEAR_ME_I18N.SEVERITY.SHALLOW[lang];
    case 'WET':
      return NEAR_ME_I18N.SEVERITY.WET[lang];
    case 'DRY':
      return NEAR_ME_I18N.SEVERITY.DRY[lang];
    default:
      return NEAR_ME_I18N.SEVERITY.UNKNOWN[lang];
  }
}

function trafficSeverityLabel(severity: string | null, lang: 'vi' | 'en'): string {
  switch (severity) {
    case 'CLEAR':
      return NEAR_ME_I18N.TRAFFIC.CLEAR[lang];
    case 'MODERATE':
      return NEAR_ME_I18N.TRAFFIC.MODERATE[lang];
    case 'HEAVY':
      return NEAR_ME_I18N.TRAFFIC.HEAVY[lang];
    case 'CONGESTED':
      return NEAR_ME_I18N.TRAFFIC.CONGESTED[lang];
    default:
      return NEAR_ME_I18N.TRAFFIC.UNKNOWN[lang];
  }
}

/** Monochrome severity dot, always paired with a text label. */
function severityDotClass(severity: string | null): string {
  switch (severity) {
    case 'FLOODED_DEEP':
      return 'bg-zinc-100';
    case 'FLOODED_SHALLOW':
      return 'bg-zinc-300';
    case 'WET':
      return 'bg-zinc-500';
    case 'DRY':
      return 'bg-zinc-700';
    default:
      return 'bg-transparent border border-zinc-600';
  }
}

function cameraName(camera: Camera): string {
  return camera.displayName || camera.title || camera.id;
}

function cameraLocality(camera: Camera): string {
  return camera.road_name || camera.phuong_name || camera.zone || '';
}

function errorCopy(view: ErrorView, lang: 'vi' | 'en'): { title: string; body: string; steps?: string[]; footnote?: string } {
  const I = NEAR_ME_I18N;
  switch (view) {
    case 'denied':
      return {
        title: I.DENIED_TITLE[lang],
        body: I.DENIED_BODY[lang],
        steps: I.DENIED_STEPS[lang],
        footnote: I.DENIED_STEPS_IOS[lang],
      };
    case 'timeout':
      return { title: I.TIMEOUT_TITLE[lang], body: I.TIMEOUT_BODY[lang] };
    case 'unavailable':
      return { title: I.UNAVAILABLE_TITLE[lang], body: I.UNAVAILABLE_BODY[lang] };
    case 'insecure':
      return { title: I.INSECURE_TITLE[lang], body: I.INSECURE_BODY[lang] };
    case 'unsupported':
      return { title: I.UNSUPPORTED_TITLE[lang], body: I.UNSUPPORTED_BODY[lang] };
    case 'invalid':
    default:
      return { title: I.INVALID_TITLE[lang], body: I.INVALID_BODY[lang] };
  }
}

export default function NearMeModal({
  isOpen,
  onClose,
  status,
  fix,
  geoError,
  results,
  offlineCountInRadius,
  camerasLoading,
  camerasError,
  hideOffline,
  isOutsideServiceArea,
  lang,
  onLangChange,
  onRetry,
  onRetryCameras,
  onRetryGeo,
  onRecenter,
  onToggleOffline,
  onCameraClick,
  onViewFullMap,
  onBackToCity,
}: NearMeModalProps) {
  const dialogRef = useRef<HTMLDivElement | null>(null);
  const [visibleCount, setVisibleCount] = useState(PAGE_SIZE);

  const isLoading = status === 'locating' || status === 'idle' || (camerasLoading && status === 'success');
  const hasDataError = Boolean(camerasError) && results.length === 0;
  const isGeoFailure = GEO_FAILURE_STATUSES.has(status);

  const errorView = useMemo<ErrorView>(() => {
    if (status === 'denied') return 'denied';
    if (status === 'timeout') return 'timeout';
    if (status === 'insecure') return 'insecure';
    if (status === 'unsupported') return 'unsupported';
    if (status === 'invalid') return 'invalid';
    if (status === 'unavailable') return 'unavailable';
    switch (geoError) {
      case 'PERMISSION_DENIED':
        return 'denied';
      case 'TIMEOUT':
        return 'timeout';
      case 'INSECURE_CONTEXT':
        return 'insecure';
      case 'UNSUPPORTED':
        return 'unsupported';
      case 'INVALID_POSITION':
        return 'invalid';
      default:
        return 'unavailable';
    }
  }, [status, geoError]);

  const accuracy = fix ? Math.round(fix.accuracy) : null;
  const isLowAccuracy = accuracy !== null && accuracy > LOW_ACCURACY_WARN_METERS;
  const isBadAccuracy = accuracy !== null && accuracy > LOW_ACCURACY_BAD_METERS;

  const visibleResults = results.slice(0, visibleCount);
  const remainingResults = Math.max(0, results.length - visibleCount);

  const announcement = hasDataError
    ? NEAR_ME_I18N.DATA_ERROR_TITLE[lang]
    : isLoading
      ? NEAR_ME_I18N.LOCATING_TITLE[lang]
      : isGeoFailure
        ? errorCopy(errorView, lang).title
        : isOutsideServiceArea
          ? NEAR_ME_I18N.OUTSIDE_AREA_TITLE[lang]
          : results.length === 0
            ? NEAR_ME_I18N.EMPTY_TITLE[lang]
            : NEAR_ME_I18N.COUNT[lang](results.length);

  // Reset pagination whenever the sheet reopens.
  useEffect(() => {
    if (isOpen) setVisibleCount(PAGE_SIZE);
  }, [isOpen]);

  // Clamp pagination if the list shrinks (language switch, offline toggle).
  useEffect(() => {
    setVisibleCount((previous) => (previous > results.length ? Math.max(PAGE_SIZE, results.length) : previous));
  }, [results.length]);

  // Focus management, focus trap, Esc, and body scroll lock.
  useEffect(() => {
    if (!isOpen || typeof document === 'undefined') return undefined;

    const previouslyFocused = document.activeElement as HTMLElement | null;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    const dialog = dialogRef.current;
    const initialTarget =
      (dialog?.querySelector<HTMLElement>('[data-near-me-initial-focus]') ?? dialog) ?? null;
    initialTarget?.focus();

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

  if (!isOpen) return null;

  const titleId = 'near-me-title';

  const renderPrimaryButton = (label: string, onClick: () => void) => (
    <button
      type="button"
      onClick={onClick}
      className="flex min-h-[48px] min-w-[48px] w-full items-center justify-center rounded-lg border border-zinc-500 bg-zinc-100 px-4 text-sm font-semibold text-zinc-950 transition-colors hover:bg-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100 active:bg-zinc-300"
    >
      {label}
    </button>
  );

  const renderSecondaryButton = (label: string, onClick: () => void) => (
    <button
      type="button"
      onClick={onClick}
      className="flex min-h-[48px] min-w-[48px] w-full items-center justify-center rounded-lg border border-zinc-700 bg-zinc-900 px-4 text-sm font-medium text-zinc-200 transition-colors hover:border-zinc-500 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100 active:bg-zinc-800"
    >
      {label}
    </button>
  );

  const renderOfflineToggle = () => (
    <button
      type="button"
      onClick={onToggleOffline}
      className="flex min-h-[48px] min-w-[48px] items-center justify-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900 px-3 text-xs font-medium text-zinc-300 transition-colors hover:border-zinc-600 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
      aria-pressed={!hideOffline}
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
        {hideOffline ? (
          <>
            <path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12Z" />
            <circle cx="12" cy="12" r="2.5" />
          </>
        ) : (
          <>
            <path d="M2 12s3.5-6 10-6 10 6 10 6-3.5 6-10 6S2 12 2 12Z" />
            <circle cx="12" cy="12" r="2.5" />
            <path d="m3 3 18 18" />
          </>
        )}
      </svg>
      <span>{hideOffline ? NEAR_ME_I18N.SHOW_OFFLINE[lang] : NEAR_ME_I18N.HIDE_OFFLINE[lang]}</span>
    </button>
  );

  const renderErrorState = () => {
    const copy = errorCopy(errorView, lang);
    return (
      <div className="px-5 py-5">
        <h3 className="text-sm font-semibold text-zinc-100">{copy.title}</h3>
        <p className="mt-2 text-xs leading-relaxed text-zinc-400">{copy.body}</p>
        {copy.steps && copy.steps.length > 0 && (
          <ol className="mt-4 space-y-2">
            {copy.steps.map((step, index) => (
              <li key={step} className="flex gap-2 text-xs leading-relaxed text-zinc-300">
                <span className="shrink-0 font-mono text-zinc-500">{index + 1}.</span>
                <span>{step}</span>
              </li>
            ))}
          </ol>
        )}
        {copy.footnote && <p className="mt-3 text-[11px] leading-relaxed text-zinc-500">{copy.footnote}</p>}
        <div className="mt-5 space-y-2">
          {renderPrimaryButton(NEAR_ME_I18N.RETRY[lang], onRetryGeo || onRetry)}
          {renderSecondaryButton(NEAR_ME_I18N.VIEW_FULL_MAP[lang], onViewFullMap)}
        </div>
      </div>
    );
  };

  const renderDataErrorState = () => (
    <div className="px-5 py-5">
      <h3 className="text-sm font-semibold text-zinc-100">{NEAR_ME_I18N.DATA_ERROR_TITLE[lang]}</h3>
      <p className="mt-2 text-xs leading-relaxed text-zinc-400">{NEAR_ME_I18N.DATA_ERROR_BODY[lang]}</p>
      <div className="mt-5 space-y-2">
        {renderPrimaryButton(NEAR_ME_I18N.RETRY[lang], onRetryCameras || onRetry)}
        {renderSecondaryButton(NEAR_ME_I18N.VIEW_FULL_MAP[lang], onViewFullMap)}
      </div>
    </div>
  );

  const renderSkeletonState = () => (
    <div aria-busy="true" className="px-5 py-5">
      <p className="text-sm font-semibold text-zinc-100">{NEAR_ME_I18N.LOCATING_TITLE[lang]}</p>
      <p className="mt-2 text-xs leading-relaxed text-zinc-400">{NEAR_ME_I18N.LOCATING_BODY[lang]}</p>
      <div className="mt-4 space-y-1" aria-hidden="true">
        {[0, 1, 2, 3].map((row) => (
          <div key={row} className="flex min-h-[64px] items-center gap-3 border-b border-zinc-800/70 py-3">
            <span className="h-2.5 w-2.5 shrink-0 animate-pulse rounded-full bg-zinc-700" />
            <div className="flex-1 space-y-2">
              <span className="block h-3 w-2/3 animate-pulse rounded bg-zinc-800" />
              <span className="block h-2.5 w-1/2 animate-pulse rounded bg-zinc-800/70" />
            </div>
            <span className="h-3 w-10 shrink-0 animate-pulse rounded bg-zinc-800" />
          </div>
        ))}
      </div>
    </div>
  );

  const renderOutsideAreaState = () => (
    <div className="px-5 py-5">
      <h3 className="text-sm font-semibold text-zinc-100">{NEAR_ME_I18N.OUTSIDE_AREA_TITLE[lang]}</h3>
      <p className="mt-2 text-xs leading-relaxed text-zinc-400">{NEAR_ME_I18N.OUTSIDE_AREA_BODY[lang]}</p>
      <div className="mt-5 space-y-2">
        {renderPrimaryButton(NEAR_ME_I18N.BACK_TO_CITY[lang], onBackToCity)}
        {renderSecondaryButton(NEAR_ME_I18N.VIEW_FULL_MAP[lang], onViewFullMap)}
      </div>
    </div>
  );

  const renderEmptyState = () => (
    <div className="px-5 py-5">
      <h3 className="text-sm font-semibold text-zinc-100">{NEAR_ME_I18N.EMPTY_TITLE[lang]}</h3>
      <p className="mt-2 text-xs leading-relaxed text-zinc-400">{NEAR_ME_I18N.EMPTY_BODY[lang]}</p>
      {offlineCountInRadius > 0 && (
        <p className="mt-3 text-xs leading-relaxed text-zinc-300">
          {NEAR_ME_I18N.EMPTY_OFFLINE_HINT[lang](offlineCountInRadius)}
        </p>
      )}
      <div className="mt-5 space-y-2">
        {offlineCountInRadius > 0 && hideOffline && renderOfflineToggle()}
        {renderPrimaryButton(NEAR_ME_I18N.VIEW_FULL_MAP[lang], onViewFullMap)}
        {renderSecondaryButton(NEAR_ME_I18N.RETRY[lang], onRetry)}
      </div>
    </div>
  );

  const renderListState = () => (
    <>
      {fix && (
        <div className="border-b border-zinc-800 px-4 py-2.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <p className="font-mono text-[11px] text-zinc-400">
              {NEAR_ME_I18N.ACCURACY[lang](accuracy ?? 0)}
            </p>
            <button
              type="button"
              onClick={onRecenter}
              className="flex min-h-[48px] min-w-[48px] items-center gap-1.5 rounded-lg px-2 text-[11px] font-medium text-zinc-300 transition-colors hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
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
                <circle cx="12" cy="12" r="8" />
                <circle cx="12" cy="12" r="2.5" />
                <path d="M12 2v3M12 19v3M2 12h3M19 12h3" />
              </svg>
              <span>{NEAR_ME_I18N.RECENTER[lang]}</span>
            </button>
          </div>
          {isLowAccuracy && (
            <p className={`mt-1 text-[11px] leading-relaxed ${isBadAccuracy ? 'text-zinc-200' : 'text-zinc-400'}`}>
              {NEAR_ME_I18N.LOW_ACCURACY_WARN[lang]}
            </p>
          )}
          {isBadAccuracy && (
            <p className="mt-1 text-[11px] leading-relaxed text-zinc-300">{NEAR_ME_I18N.LOW_ACCURACY_BAD[lang]}</p>
          )}
        </div>
      )}

      <ul className="divide-y divide-zinc-800/70">
        {visibleResults.map((result) => {
          const camera = result.camera;
          const distance = formatDistance(result.distanceMeters, lang);
          const floodLabel = floodSeverityLabel(result.floodSeverity, lang);
          const trafficLabel = trafficSeverityLabel(result.trafficSeverity, lang);
          const locality = cameraLocality(camera);
          const isOffline = camera.status === 'NOT_IMAGE' || camera.status === 'NO_SIGNAL' || !camera.isOnline;
          return (
            <li key={camera.id}>
              <button
                type="button"
                onClick={() => onCameraClick(camera)}
                aria-label={NEAR_ME_I18N.ROW_ARIA[lang](cameraName(camera), distance, floodLabel)}
                className="flex min-h-[64px] min-w-[48px] w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-zinc-900 focus-visible:bg-zinc-900 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-zinc-100 active:bg-zinc-800"
              >
                <span
                  className={`h-2.5 w-2.5 shrink-0 rounded-full ${severityDotClass(result.floodSeverity)}`}
                  aria-hidden="true"
                />
                <span className="min-w-0 flex-1">
                  <span className="block line-clamp-2 break-words text-sm font-semibold text-zinc-100">{cameraName(camera)}</span>
                  <span className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-zinc-400">
                    {locality && <span className="min-w-0 max-w-full truncate">{locality}</span>}
                    <span className="text-zinc-300">{floodLabel}</span>
                    <span className="text-zinc-500">{trafficLabel}</span>
                    {result.floodStale && (
                      <span className="rounded border border-zinc-700 px-1 py-0.5 font-mono text-[10px] uppercase tracking-wide text-zinc-300">
                        {NEAR_ME_I18N.BADGE.STALE[lang]}
                      </span>
                    )}
                    {!result.floodSeverity && !result.floodStale && (
                      <span className="rounded border border-zinc-800 px-1 py-0.5 font-mono text-[10px] uppercase tracking-wide text-zinc-500">
                        {NEAR_ME_I18N.BADGE.NO_DATA[lang]}
                      </span>
                    )}
                    {isOffline && (
                      <span className="rounded border border-zinc-700 px-1 py-0.5 font-mono text-[10px] uppercase tracking-wide text-zinc-400">
                        {NEAR_ME_I18N.BADGE.OFFLINE[lang]}
                      </span>
                    )}
                  </span>
                </span>
                <span className="shrink-0 whitespace-nowrap font-mono text-xs tabular-nums text-zinc-100">
                  {distance}
                </span>
              </button>
            </li>
          );
        })}
      </ul>

      {remainingResults > 0 && (
        <div className="px-4 py-3">
          <button
            type="button"
            onClick={() => setVisibleCount((previous) => previous + PAGE_SIZE)}
            className="flex min-h-[48px] min-w-[48px] w-full items-center justify-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900 px-3 text-xs font-medium text-zinc-200 transition-colors hover:border-zinc-600 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
          >
            <span>{NEAR_ME_I18N.SHOW_MORE[lang](Math.min(remainingResults, PAGE_SIZE))}</span>
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
              <path d="m6 9 6 6 6-6" />
            </svg>
          </button>
        </div>
      )}
    </>
  );

  return (
    <>
      <div
        className="fixed inset-0 z-[940] bg-black/70"
        onClick={onClose}
        aria-hidden="true"
      />

      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        className="fixed inset-x-0 bottom-0 z-[950] flex max-h-[80vh] w-full flex-col overflow-hidden rounded-t-2xl border border-zinc-800 border-b-0 bg-zinc-950 text-zinc-300 shadow-2xl outline-none md:inset-0 md:m-auto md:h-fit md:max-h-[85vh] md:max-w-md md:rounded-xl md:border-b"
      >
        <div className="shrink-0 pt-2 md:hidden" aria-hidden="true">
          <div className="mx-auto h-1 w-10 rounded-full bg-zinc-700" />
        </div>

        <div className="flex shrink-0 items-start gap-3 border-b border-zinc-800 px-4 py-3">
          <div className="min-w-0 flex-1">
            <h2 id={titleId} className="font-mono text-sm font-bold uppercase tracking-wider text-zinc-100">
              {NEAR_ME_I18N.SHEET_TITLE[lang]}
            </h2>
            <p className="mt-1 text-[11px] text-zinc-400">{NEAR_ME_I18N.COUNT[lang](results.length)}</p>
          </div>
          {onLangChange && (
            <button
              type="button"
              onClick={() => onLangChange(lang === 'vi' ? 'en' : 'vi')}
              aria-label={lang === 'vi' ? 'Chuyển sang tiếng Anh' : 'Switch to Vietnamese'}
              className="flex min-h-[48px] min-w-[48px] shrink-0 items-center justify-center rounded-lg border border-zinc-800 bg-zinc-900 px-3 font-mono text-xs font-semibold text-zinc-300 transition-colors hover:border-zinc-600 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
            >
              {lang === 'vi' ? 'EN' : 'VI'}
            </button>
          )}
          <button
            type="button"
            onClick={onClose}
            data-near-me-initial-focus="true"
            aria-label={NEAR_ME_I18N.CLOSE[lang]}
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
            <span>{NEAR_ME_I18N.CLOSE[lang]}</span>
          </button>
        </div>

        {results.length > 0 && !hasDataError && !isLoading && !isGeoFailure && !isOutsideServiceArea && (
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-zinc-800 px-4 py-1.5">
            {renderOfflineToggle()}
          </div>
        )}

        <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          {hasDataError
            ? renderDataErrorState()
            : isLoading
              ? renderSkeletonState()
              : isGeoFailure
                ? renderErrorState()
                : isOutsideServiceArea
                  ? renderOutsideAreaState()
                  : results.length === 0
                    ? renderEmptyState()
                    : renderListState()}
        </div>

        <div className="shrink-0 border-t border-zinc-800 px-4 py-2.5">
          <p className="text-[10.5px] leading-relaxed text-zinc-500">{NEAR_ME_I18N.SORT_NOTE[lang]}</p>
          <p className="mt-1 text-[10.5px] leading-relaxed text-zinc-500">{NEAR_ME_I18N.PRIVACY_NOTE[lang]}</p>
        </div>

        <div className="sr-only" role="status" aria-live="polite">
          {announcement}
        </div>
      </div>
    </>
  );
}
