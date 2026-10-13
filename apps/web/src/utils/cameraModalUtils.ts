export interface FloodVerifyState {
  isVerifying: boolean;
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
