export interface FloodVerifyState {
  isVerifying: boolean;
  cooldownRemaining: number;
}

export interface FloodReportState {
  isReporting: boolean;
  cooldownRemaining: number;
}

export const CAMERA_MODAL_I18N = Object.freeze({
  BUTTON_LABEL: {
    vi: 'Xác minh ngập nước',
    en: 'Verify flood status',
  },
  VERIFYING: {
    vi: 'Đang xác minh VLM...',
    en: 'Verifying with VLM...',
  },
  COOLDOWN: {
    vi: (seconds: number) => `Chờ xác minh (${seconds}s)`,
    en: (seconds: number) => `Cooldown (${seconds}s)`,
  },
});

export const CAMERA_REPORT_MODAL_I18N = Object.freeze({
  BUTTON_LABEL: {
    vi: 'Báo cáo ngập tại đây',
    en: 'Report flood here',
  },
  REPORTING: {
    vi: 'Đang gửi báo cáo...',
    en: 'Submitting report...',
  },
  COOLDOWN: {
    vi: (seconds: number) => `Đã báo cáo (${seconds}s)`,
    en: (seconds: number) => `Reported (${seconds}s)`,
  },
  SUCCESS_CHIP: {
    vi: 'Cảm ơn bạn đã hỗ trợ ghi nhận',
    en: 'Thank you for your report',
  },
  RATE_LIMITED: {
    vi: 'Bạn đã gửi báo cáo gần đây',
    en: 'You have submitted a report recently',
  },
  ERROR_GENERIC: {
    vi: 'Không thể gửi báo cáo',
    en: 'Unable to submit report',
  },
});

/**
 * Returns formatted action button text without square brackets.
 * Requirement: Reserve square brackets [...] strictly for passive status badges.
 */
export function getFloodVerifyButtonText(
  state: FloodVerifyState,
  lang: 'vi' | 'en' = 'vi'
): string {
  if (state.cooldownRemaining > 0) {
    return CAMERA_MODAL_I18N.COOLDOWN[lang](state.cooldownRemaining);
  }
  if (state.isVerifying) {
    return CAMERA_MODAL_I18N.VERIFYING[lang];
  }
  return CAMERA_MODAL_I18N.BUTTON_LABEL[lang];
}

/**
 * Determines whether the flood verification trigger button should be disabled.
 */
export function isFloodVerifyDisabled(state: FloodVerifyState): boolean {
  return state.isVerifying || state.cooldownRemaining > 0;
}

/**
 * Calculates remaining cooldown seconds given a last trigger timestamp.
 * Returns 0 if expired, negative offset, or uninitialized.
 */
export function calculateCooldownRemaining(
  lastTriggerTimestamp: number,
  cooldownDurationSeconds = 15,
  currentTimestamp = Date.now()
): number {
  if (!lastTriggerTimestamp || lastTriggerTimestamp <= 0) {
    return 0;
  }
  const elapsedMs = currentTimestamp - lastTriggerTimestamp;
  const totalMs = cooldownDurationSeconds * 1000;
  if (elapsedMs >= totalMs) {
    return 0;
  }
  return Math.max(0, Math.ceil((totalMs - elapsedMs) / 1000));
}

/**
 * Returns formatted report button text without square brackets.
 * Requirement: Reserve square brackets [...] strictly for passive status badges.
 */
export function getFloodReportButtonText(
  state: FloodReportState,
  lang: 'vi' | 'en' = 'vi'
): string {
  if (state.cooldownRemaining > 0) {
    return CAMERA_REPORT_MODAL_I18N.COOLDOWN[lang](state.cooldownRemaining);
  }
  if (state.isReporting) {
    return CAMERA_REPORT_MODAL_I18N.REPORTING[lang];
  }
  return CAMERA_REPORT_MODAL_I18N.BUTTON_LABEL[lang];
}

/**
 * Determines whether the flood report trigger button should be disabled.
 */
export function isFloodReportDisabled(state: FloodReportState): boolean {
  return state.isReporting || state.cooldownRemaining > 0;
}

/**
 * Calculates remaining report cooldown seconds given a last trigger timestamp.
 * Default cooldown is 30 seconds for crowd-sourced human reporting.
 */
export function calculateReportCooldownRemaining(
  lastTriggerTimestamp: number,
  cooldownDurationSeconds = 30,
  currentTimestamp = Date.now()
): number {
  return calculateCooldownRemaining(lastTriggerTimestamp, cooldownDurationSeconds, currentTimestamp);
}

// Module-level map tracking per-camera report timestamps so 30s cooldown persists
// across modal close/open and camera switching.
const cameraReportCooldowns = new Map<string, number>();

export function getCameraReportCooldown(cameraId: string, currentTimestamp = Date.now()): number {
  const ts = cameraReportCooldowns.get(cameraId);
  if (!ts) return 0;
  return calculateReportCooldownRemaining(ts, 30, currentTimestamp);
}

export function setCameraReportCooldown(cameraId: string, timestamp = Date.now()): void {
  cameraReportCooldowns.set(cameraId, timestamp);
}

export function clearCameraReportCooldowns(): void {
  cameraReportCooldowns.clear();
}

export const MAX_SNAPSHOT_ZOOM = 3;
export const MIN_SNAPSHOT_ZOOM = 1;

export type SnapshotCompareMode = 'live' | 'baseline' | 'split';

export interface ArchivedSnapshotCandidate {
  key: string;
  camera_id?: string;
  timestamp?: number;
  reason?: string;
  raw_url?: string;
}

export const CAMERA_ZOOM_I18N = Object.freeze({
  ZOOM_IN: { vi: 'Phóng to', en: 'Zoom in' },
  ZOOM_RESET: { vi: 'Thu 1x', en: 'Reset 1x' },
  COMPARE_LIVE: { vi: 'Hiện tại', en: 'Live' },
  COMPARE_BASELINE: { vi: 'Trước ngập', en: 'Before' },
  COMPARE_SPLIT: { vi: 'So sánh', en: 'Compare' },
  BASELINE_UNAVAILABLE: { vi: 'Chưa có ảnh nền lưu trữ', en: 'No archived baseline frame' },
  SPLIT_SLIDER_LABEL: { vi: 'Thanh kéo so sánh trước và hiện tại', en: 'Before and live comparison slider' },
});

export function clampZoomScale(scale: number): number {
  if (!Number.isFinite(scale)) return MIN_SNAPSHOT_ZOOM;
  return Math.min(MAX_SNAPSHOT_ZOOM, Math.max(MIN_SNAPSHOT_ZOOM, Math.round(scale * 100) / 100));
}

export function nextTapZoom(currentScale: number): number {
  if (currentScale < 1.9) return 2;
  if (currentScale < 2.9) return 3;
  return MIN_SNAPSHOT_ZOOM;
}

export function clampPanOffset(
  x: number,
  y: number,
  scale: number,
  containerWidth: number,
  containerHeight: number
): { x: number; y: number } {
  if (scale <= 1 || containerWidth <= 0 || containerHeight <= 0) {
    return { x: 0, y: 0 };
  }
  const maxX = ((scale - 1) * containerWidth) / 2;
  const maxY = ((scale - 1) * containerHeight) / 2;
  return {
    x: Math.max(-maxX, Math.min(maxX, x)),
    y: Math.max(-maxY, Math.min(maxY, y)),
  };
}

export function selectBaselineSnapshot(
  snapshots: ArchivedSnapshotCandidate[],
  cameraId: string
): ArchivedSnapshotCandidate | null {
  if (!Array.isArray(snapshots) || snapshots.length === 0 || !cameraId) {
    return null;
  }
  const matching = snapshots.filter(
    (s) => s && s.key && (!s.camera_id || s.camera_id === cameraId)
  );
  if (matching.length === 0) return null;

  const sorted = [...matching].sort((a, b) => (a.timestamp ?? 0) - (b.timestamp ?? 0));
  return sorted[0];
}
