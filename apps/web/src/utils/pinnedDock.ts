export type DockSeverity = 'CLEAR' | 'MODERATE' | 'HEAVY' | 'CONGESTED';

export interface TrafficVerdictLike {
  camera_id?: string;
  severity?: string;
  flood_severity?: string;
  confidence?: number;
  source?: string;
  observed_at?: string;
  stale?: boolean;
  timestamp?: string;
}

export interface DockSeverityVisual {
  borderClass: string;
  textClass: string;
  dotClass: string;
  isPulsing: boolean;
}

export const DOCK_SEVERITY_I18N = Object.freeze({
  CLEAR: {
    vi: 'Thông thoáng',
    en: 'Clear',
  },
  MODERATE: {
    vi: 'Vừa phải',
    en: 'Moderate',
  },
  HEAVY: {
    vi: 'Đông đúc',
    en: 'Heavy',
  },
  CONGESTED: {
    vi: 'Tắc nghẽn',
    en: 'Congested',
  },
  NO_ANALYSIS: {
    vi: 'Chưa phân tích',
    en: 'No analysis',
  },
});

export const DEFAULT_VERDICT_TTL_MS = 180 * 1000;

function isVerdictValid(v?: TrafficVerdictLike | null): boolean {
  if (!v || !v.severity) return false;
  const timeStr = v.observed_at || v.timestamp;
  if (!timeStr) return false;
  const ts = new Date(timeStr).getTime();
  if (isNaN(ts)) return false;
  const ageMs = Date.now() - ts;
  return ageMs >= -60000 && ageMs <= 24 * 60 * 60 * 1000;
}

function isVerdictStale(v?: TrafficVerdictLike | null, ttlMs: number = DEFAULT_VERDICT_TTL_MS): boolean {
  if (!v) return false;
  if (v.stale) return true;
  const timeStr = v.observed_at || v.timestamp;
  if (!timeStr) return false;
  const ts = new Date(timeStr).getTime();
  if (isNaN(ts)) return false;
  return Date.now() - ts > ttlMs;
}

/**
 * Evaluates whether a traffic verdict is fresh and provenance-verified for display in the Pinned Dock.
 * Conforms to ADR-004 §3 A5, §9 D10, and Issue #140.
 *
 * Rules:
 * 1. Verdict must exist and be valid (valid timestamp within retention window).
 * 2. Verdict must not be stale (neither marked stale by backend nor elapsed > TTL).
 * 3. Provenance check: source != 'standby_dummy' (dummy fallback response is never displayed).
 * 4. Provenance check: source != 'mock' outside dev mode (mock is allowed only when appEnv === 'dev').
 * 5. Confidence check: confidence must be > 0 (if provided).
 * 6. Severity must be one of CLEAR, MODERATE, HEAVY, CONGESTED.
 *
 * Returns the normalized DockSeverity enum if all conditions pass, or null (triggering gray "no analysis").
 */
export function getDockSeverity(
  verdict?: TrafficVerdictLike | null,
  appEnv?: string
): DockSeverity | null {
  if (!verdict) return null;
  if (!isVerdictValid(verdict)) return null;
  if (verdict.stale || isVerdictStale(verdict)) return null;

  // Provenance check (D10, A5)
  if (verdict.source === 'standby_dummy') return null;
  if (verdict.source === 'mock' && appEnv !== 'dev') return null;

  // Minimum confidence threshold check
  if (typeof verdict.confidence === 'number' && verdict.confidence <= 0) return null;

  const rawSev = (verdict.severity || '').toUpperCase().trim();
  if (rawSev === 'CLEAR' || rawSev === 'MODERATE' || rawSev === 'HEAVY' || rawSev === 'CONGESTED') {
    return rawSev;
  }

  return null;
}

/**
 * Returns localized human-readable label for a dock severity level.
 * Falls back to "Chưa phân tích" / "No analysis" when null/undefined.
 */
export function getDockSeverityLabel(
  severity: DockSeverity | null | undefined,
  lang: 'vi' | 'en' = 'vi'
): string {
  if (!severity) {
    return DOCK_SEVERITY_I18N.NO_ANALYSIS[lang];
  }
  return DOCK_SEVERITY_I18N[severity]?.[lang] || DOCK_SEVERITY_I18N.NO_ANALYSIS[lang];
}

/**
 * Returns monochromatic + functional status accent styles for dock severity badge.
 * - CLEAR: Green accent dot and border
 * - MODERATE: Yellow accent dot and border
 * - HEAVY: Red accent dot and border
 * - CONGESTED: Red accent with pulsing dot and distinct bold/ring border
 * - Default/null: Neutral zinc border and text
 */
export function getDockSeverityVisual(severity: DockSeverity | null | undefined): DockSeverityVisual {
  switch (severity) {
    case 'CLEAR':
      return {
        borderClass: 'border-emerald-500/50',
        textClass: 'text-emerald-400',
        dotClass: 'bg-emerald-500',
        isPulsing: false,
      };
    case 'MODERATE':
      return {
        borderClass: 'border-yellow-500/50',
        textClass: 'text-yellow-400',
        dotClass: 'bg-yellow-400',
        isPulsing: false,
      };
    case 'HEAVY':
      return {
        borderClass: 'border-red-500/50',
        textClass: 'text-red-400',
        dotClass: 'bg-red-500',
        isPulsing: false,
      };
    case 'CONGESTED':
      return {
        borderClass: 'border-red-500 ring-1 ring-red-500/60 font-semibold',
        textClass: 'text-red-400',
        dotClass: 'bg-red-500',
        isPulsing: true,
      };
    default:
      return {
        borderClass: 'border-zinc-700/90',
        textClass: 'text-zinc-400',
        dotClass: 'bg-zinc-500',
        isPulsing: false,
      };
  }
}

/**
 * Reorders items in an array immutably.
 */
export function reorderPinnedCameras<T>(list: T[], fromIndex: number, toIndex: number): T[] {
  if (fromIndex < 0 || fromIndex >= list.length || toIndex < 0 || toIndex >= list.length) {
    return list;
  }
  const result = [...list];
  const [removed] = result.splice(fromIndex, 1);
  result.splice(toIndex, 0, removed);
  return result;
}
