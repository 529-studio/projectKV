'use client';

import { HAZARD_I18N } from '../config/constants';
import { buildHazardDepthView } from '../utils/hazardDisplay';
import type { VerdictLike } from '../utils/hazardUtils';

export interface HazardDepthChipProps {
  verdict?: VerdictLike | null;
  lang: 'vi' | 'en';
  /** Age is caller-owned; a stale depth renders the stale chip, never fresh. */
  stale?: boolean;
  className?: string;
}

/**
 * The physical flood depth as a compact chip (S14-T3 / Issue #443).
 *
 * Renders "14,5 cm" plus a three-segment severity meter, or the honest
 * "depth not measurable" line when an avoidance-inferred hazard has no
 * centimetre figure. A pre-contract verdict renders nothing at all, so the
 * legacy flood badge keeps working unchanged.
 *
 * The whole sentence lives in a screen-reader span and in `title`; the visual
 * segments are aria-hidden because a three-box meter is meaningless read aloud.
 */
export default function HazardDepthChip({
  verdict,
  lang,
  stale = false,
  className = '',
}: HazardDepthChipProps) {
  const view = buildHazardDepthView(verdict, lang, { stale });
  if (!view.hasHazard) return null;

  return (
    <div
      title={view.summary}
      className={`pointer-events-auto flex max-w-full items-center gap-1 rounded-sm border px-1 py-0.5 font-mono text-[9px] leading-none tabular-nums ${view.containerClass} ${className}`}
    >
      <span className="sr-only">{view.summary}</span>
      <span aria-hidden="true" className="whitespace-nowrap font-bold">
        {view.depthText ?? HAZARD_I18N.DEPTH_UNMEASURED[lang]}
      </span>
      <span aria-hidden="true" className="flex shrink-0 items-center gap-0.5">
        {view.segments.map((filled, index) => (
          <span
            key={index}
            className={`h-1.5 w-1 rounded-[1px] border border-current ${
              filled ? 'bg-current' : 'opacity-40'
            }`}
          />
        ))}
      </span>
      {view.isStale && (
        <span
          aria-hidden="true"
          className="shrink-0 rounded-sm border border-current px-0.5 opacity-80"
        >
          {HAZARD_I18N.STALE[lang]}
        </span>
      )}
      {view.levelAriaLabel && <span className="sr-only">{view.levelAriaLabel}</span>}
    </div>
  );
}
