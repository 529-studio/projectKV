/**
 * Pure helpers for the physical flood hazard block (Issue #401 / S12-T3, spec §6.1).
 *
 * The backend publishes three physical bands derived from a measured centimetre
 * depth and, when nothing can be measured, an avoidance-inferred Level 3. The UI
 * must never synthesise a level for a verdict that lacks one: a pre-contract
 * verdict (or a pre-migration row) returns `null` from `resolveHazard` so the
 * legacy flood badge keeps rendering (AC14), and absence of measurement is not
 * safety, so it is never reported as Level 1.
 */

export type HazardLevel = 1 | 2 | 3;

export type HazardBasis = 'DEPTH_MEASURED' | 'AVOIDANCE_INFERRED' | 'DEPTH_AND_AVOIDANCE' | 'NONE';

export type DepthConfidence = 'MEASURED' | 'INFERRED' | 'UNMEASURED';

/**
 * Structural view of a verdict as it crosses the wire. Only snake_case keys are
 * read here; camelCase aliases from older payloads are normalised upstream in
 * `normalizeVerdict` (`trafficUtils.ts`) so there is one spelling in the UI.
 */
export interface VerdictLike {
  flood_severity?: string | null;
  flood_depth_cm?: number | null;
  flood_hazard_level?: number | null;
  flood_hazard_basis?: string | null;
  flood_depth_confidence?: string | null;
  flood_calibration_ref?: string | null;
  /** Kept only to prove it is never used as a depth. */
  water_ratio?: number | null;
  observed_at?: string;
  timestamp?: string;
  stale?: boolean;
}

export interface ResolvedHazard {
  /** Published level, or the one derived from a measured depth. `null` = unknown. */
  level: HazardLevel | null;
  basis: HazardBasis;
  /** Measured depth in cm. `null` means unmeasured — never `0`. */
  depthCm: number | null;
  depthConfidence: DepthConfidence;
  /** True whenever no centimetre figure may be rendered (AC2/AC10). */
  isUnmeasured: boolean;
}

export interface HazardBadgeStyle {
  /** Monochrome container tokens (spec §6.2). Empty for a pre-contract verdict. */
  container: string;
  /** Level 3 inverts the surface for maximum contrast (AC5). */
  inverted: boolean;
}

/** Physical band boundaries, in centimetres (spec §2.3). */
export const HAZARD_LEVEL_2_MIN_CM = 10;
export const HAZARD_LEVEL_3_MIN_CM = 20;

/** Highest depth the rider-facing meter draws; deeper water pins the bar at full. */
export const HAZARD_METER_MAX_CM = 30;

const HAZARD_LEVELS: readonly HazardLevel[] = [1, 2, 3];

const HAZARD_BASES: readonly HazardBasis[] = [
  'DEPTH_MEASURED',
  'AVOIDANCE_INFERRED',
  'DEPTH_AND_AVOIDANCE',
  'NONE',
];

const DEPTH_CONFIDENCES: readonly DepthConfidence[] = ['MEASURED', 'INFERRED', 'UNMEASURED'];

/** A depth is only a measurement when it is finite and non-negative. */
function isMeasurableDepth(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0;
}

function asHazardLevel(value: unknown): HazardLevel | null {
  if (typeof value !== 'number' || !Number.isFinite(value)) return null;
  return (HAZARD_LEVELS as readonly number[]).includes(value) ? (value as HazardLevel) : null;
}

function asHazardBasis(value: unknown): HazardBasis {
  return typeof value === 'string' && (HAZARD_BASES as readonly string[]).includes(value)
    ? (value as HazardBasis)
    : 'NONE';
}

function asDepthConfidence(value: unknown): DepthConfidence {
  return typeof value === 'string' && (DEPTH_CONFIDENCES as readonly string[]).includes(value)
    ? (value as DepthConfidence)
    : 'UNMEASURED';
}

/**
 * Map a centimetre depth to a hazard level. `< 10 -> 1`, `< 20 -> 2`, `>= 20 -> 3`.
 *
 * 20.0 is inclusive into Level 3 because 20 cm is the exhaust outlet: at exactly
 * 20 cm the pipe sits at the waterline and any ripple reaches it. Classifies the
 * unrounded value, so 19.99 is Level 2 (AC3).
 */
export function levelFromDepthCm(depthCm: number): HazardLevel {
  if (depthCm < HAZARD_LEVEL_2_MIN_CM) return 1;
  if (depthCm < HAZARD_LEVEL_3_MIN_CM) return 2;
  return 3;
}

/**
 * Resolve the hazard block for a verdict.
 *
 * Prefers the published `flood_hazard_level`; when it is absent but
 * `flood_depth_cm` is a finite number (older Go build during rollout) it derives
 * the level. Otherwise it returns `null`: a pre-contract verdict renders the
 * legacy badge, not Level 1. Never infers from `water_ratio`.
 */
export function resolveHazard(v?: VerdictLike | null): ResolvedHazard | null {
  if (!v) return null;

  const depthCm = isMeasurableDepth(v.flood_depth_cm) ? v.flood_depth_cm : null;
  const publishedLevel = asHazardLevel(v.flood_hazard_level);

  if (publishedLevel !== null) {
    return {
      level: publishedLevel,
      basis: asHazardBasis(v.flood_hazard_basis),
      depthCm,
      depthConfidence: asDepthConfidence(v.flood_depth_confidence),
      isUnmeasured: depthCm === null,
    };
  }

  // Rolling-deploy fallback (spec §6.1/E12): a measured depth with no published
  // level. `water_ratio` is deliberately never consulted.
  if (depthCm !== null) {
    return {
      level: levelFromDepthCm(depthCm),
      basis: 'DEPTH_MEASURED',
      depthCm,
      depthConfidence: 'INFERRED',
      isUnmeasured: false,
    };
  }

  return null;
}

/**
 * Format a depth for display: one decimal, VI comma or EN point.
 * `(14.5, 'vi') -> '14,5 cm'`, `(0, 'vi') -> '0 cm'`, `(null, _) -> null`.
 */
export function formatDepthCm(depthCm: number | null | undefined, lang: 'vi' | 'en'): string | null {
  if (!isMeasurableDepth(depthCm)) return null;

  // One decimal, no thousands separators (a 1000 cm reading must stay "1000 cm").
  const rounded = Math.round(depthCm * 10) / 10;
  const text = Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1);
  return `${lang === 'vi' ? text.replace('.', ',') : text} cm`;
}

/**
 * Monochrome badge tokens for a hazard level (spec §6.2): Level 1 recedes,
 * Level 2 lifts, Level 3 inverts to a light surface with dark text. Level is never
 * encoded by colour alone — the caller pairs this with a label and a 3-segment
 * meter. `null` yields an empty container so a pre-contract verdict keeps its
 * legacy rendering.
 */
export function getHazardBadgeStyle(level: HazardLevel | null): HazardBadgeStyle {
  switch (level) {
    case 1:
      return { container: 'border-zinc-700 bg-zinc-900/80 text-zinc-300', inverted: false };
    case 2:
      return { container: 'border-zinc-400 bg-zinc-800 text-zinc-100', inverted: false };
    case 3:
      return { container: 'border-zinc-100 bg-zinc-100 text-zinc-950', inverted: true };
    default:
      return { container: '', inverted: false };
  }
}
