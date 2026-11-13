/**
 * One hazard-depth view model for every surface that names a water depth
 * (S14-T3 / Issue #443): the map tooltip, the pinned-dock tiles and the mobile
 * camera list all call `buildHazardDepthView` and render the same level label,
 * centimetre figure, three-segment severity meter and unmeasured marker.
 *
 * Pure and framework-free so the honesty rules can be tested without a browser:
 *  - a pre-contract verdict has no hazard view at all (legacy badge stays);
 *  - an avoidance-inferred Level 3 renders a level but never a centimetre value;
 *  - a measured 0 cm renders the number 0, not "no data";
 *  - `water_ratio` is never consulted (it is an area fraction, not a depth).
 */

import { HAZARD_I18N } from '../config/constants.ts';
import {
  HAZARD_METER_MAX_CM,
  formatDepthCm,
  getHazardBadgeStyle,
  resolveHazard,
  type HazardLevel,
  type VerdictLike,
} from './hazardUtils.ts';

export type Lang = 'vi' | 'en';

export interface HazardDepthView {
  /** False for a pre-contract verdict: render the legacy flood badge instead. */
  hasHazard: boolean;
  level: HazardLevel | null;
  /** Rider-facing band label ("Nguy cơ chết máy cao"), `null` when absent. */
  label: string | null;
  /** Formatted depth ("14,5 cm"), `null` when it must not be shown. */
  depthText: string | null;
  /** True whenever no centimetre figure exists (measured 0 is *not* unmeasured). */
  isUnmeasured: boolean;
  /** Depth as a percentage of the 30 cm meter scale, clamped to 0..100. */
  meterPercent: number;
  /** Three booleans, one per severity segment: filled up to the hazard level. */
  segments: ReadonlyArray<boolean>;
  /** Filled segment count, 0..3 (screen-reader friendly). */
  filledSegments: number;
  /** Monochrome Tailwind container tokens from `getHazardBadgeStyle`. */
  containerClass: string;
  inverted: boolean;
  /** Single-line sentence for `title`/`aria-label`. */
  summary: string;
  levelAriaLabel: string | null;
  meterAriaLabel: string | null;
  /** Rendered as a "stale data" chip, never as a fresh reading. */
  isStale: boolean;
}

export interface HazardDepthOptions {
  /** Verdict age is caller-owned (`isFloodVerdictStale`), so it is passed in. */
  stale?: boolean;
}

const NO_HAZARD: HazardDepthView = Object.freeze({
  hasHazard: false,
  level: null,
  label: null,
  depthText: null,
  isUnmeasured: true,
  meterPercent: 0,
  segments: Object.freeze([false, false, false]),
  filledSegments: 0,
  containerClass: '',
  inverted: false,
  summary: '',
  levelAriaLabel: null,
  meterAriaLabel: null,
  isStale: false,
});

/**
 * Build the shared hazard-depth view model for one verdict.
 */
export function buildHazardDepthView(
  verdict: VerdictLike | null | undefined,
  lang: Lang,
  options: HazardDepthOptions = {},
): HazardDepthView {
  const hazard = resolveHazard(verdict);
  if (!hazard || hazard.level === null) return NO_HAZARD;

  const level = hazard.level;
  const label = HAZARD_I18N[`LEVEL_${level}` as 'LEVEL_1' | 'LEVEL_2' | 'LEVEL_3'][lang];
  const depthText = formatDepthCm(hazard.depthCm, lang);
  const isUnmeasured = depthText === null;
  const meterPercent =
    hazard.depthCm === null
      ? 0
      : Math.min(100, Math.max(0, (hazard.depthCm / HAZARD_METER_MAX_CM) * 100));
  const segments = [1, 2, 3].map((segment) => segment <= level);
  const filledSegments = level;
  const style = getHazardBadgeStyle(level);
  const isStale = Boolean(options.stale);

  const depthSentence = depthText
    ? HAZARD_I18N.DEPTH_VALUE[lang](depthText)
    : HAZARD_I18N.DEPTH_UNMEASURED[lang];
  const summary = isStale
    ? `${label} · ${depthSentence} · ${HAZARD_I18N.STALE[lang]}`
    : `${label} · ${depthSentence}`;

  return {
    hasHazard: true,
    level,
    label,
    depthText,
    isUnmeasured,
    meterPercent,
    segments,
    filledSegments,
    containerClass: style.container,
    inverted: style.inverted,
    summary,
    levelAriaLabel: HAZARD_I18N.LEVEL_ARIA[lang](level),
    meterAriaLabel: depthText ? HAZARD_I18N.METER_ARIA[lang](depthText) : null,
    isStale,
  };
}

const HTML_ESCAPES: Readonly<Record<string, string>> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
};

/** Escape a value before interpolating it into a Leaflet tooltip string. */
export function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (character) => HTML_ESCAPES[character] ?? character);
}

/**
 * Inline-styled hazard block for a Leaflet tooltip. Inline styles are used here
 * because the tooltip can render before Tailwind's utility sheet is applied to
 * the pane, and the block must stay legible over imagery either way.
 */
export function buildHazardDepthHtml(view: HazardDepthView, lang: Lang): string {
  if (!view.hasHazard) return '';

  const ink = view.inverted ? '#09090b' : '#e4e4e7';
  const muted = view.inverted ? '#3f3f46' : '#a1a1aa';
  const border = view.inverted ? '#a1a1aa' : '#52525b';
  const surface = view.inverted ? '#f4f4f5' : 'transparent';

  const segments = view.segments
    .map(
      (filled) =>
        `<span style="display:inline-block;width:7px;height:6px;border-radius:1px;border:1px solid ${border};background:${filled ? ink : 'transparent'};opacity:${filled ? 1 : 0.45};"></span>`,
    )
    .join('');

  const depthLine = view.depthText
    ? `<span style="font-weight:700;">${escapeHtml(HAZARD_I18N.DEPTH_VALUE[lang](view.depthText))}</span>`
    : `<span style="opacity:0.9;">${escapeHtml(HAZARD_I18N.DEPTH_UNMEASURED[lang])}</span>`;

  const staleChip = view.isStale
    ? `<span style="margin-left:4px;border:1px solid ${border};border-radius:2px;padding:0 3px;opacity:0.85;">${escapeHtml(HAZARD_I18N.STALE[lang])}</span>`
    : '';

  return (
    `<div style="margin-top:4px;padding:3px 4px;border-top:1px solid ${border};background:${surface};color:${ink};font-size:11px;line-height:1.35;">` +
    `<div style="font-weight:700;">${escapeHtml(view.label ?? '')}</div>` +
    `<div style="display:flex;align-items:center;gap:4px;margin-top:2px;color:${muted};">` +
    depthLine +
    `<span role="img" aria-label="${escapeHtml(view.meterAriaLabel ?? '')}" style="display:inline-flex;align-items:center;gap:1px;">${segments}</span>` +
    staleChip +
    `</div></div>`
  );
}
