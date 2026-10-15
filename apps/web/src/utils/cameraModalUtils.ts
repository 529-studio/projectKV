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
