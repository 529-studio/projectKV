import type { Camera } from '../hooks/useCameras';

/**
 * Checks whether a camera is considered offline / no-signal.
 * Specifically checks for 'NOT_IMAGE', 'NO_SIGNAL', or non-road waterway monitors.
 */
export const isCameraOffline = (cam: Camera | null | undefined): boolean => {
  if (!cam) return false;
  return cam.status === 'NOT_IMAGE' || cam.status === 'NO_SIGNAL' || cam.status === 'WATERWAY_MONITOR' || Boolean(cam.isWaterway);
};

/**
 * Checks whether a camera is a non-road waterway / navigation clearance monitoring sensor.
 * These cameras point directly at river water beneath bridge footings and must be permanently
 * excluded from road traffic maps and commuter navigation layers.
 */
export const isWaterwayCamera = (cam: Camera | null | undefined): boolean => {
  if (!cam) return false;
  return Boolean(cam.isWaterway) || cam.status === 'WATERWAY_MONITOR';
};

export const CAMERA_FILTER_I18N = Object.freeze({
  COUNTER: {
    vi: (visible: number, total: number) => `Đang hiển thị ${visible} / ${total} camera`,
    en: (visible: number, total: number) => `Showing ${visible} / ${total} cameras`,
  },
  COUNTER_COMPACT: {
    vi: (visible: number, total: number) => `${visible} / ${total} cam`,
    en: (visible: number, total: number) => `${visible} / ${total} cams`,
  },
  HIDE_OFFLINE_BTN: {
    vi: '[Ẩn camera mất tín hiệu]',
    en: '[Hide offline cameras]',
  },
  HIDE_OFFLINE_BTN_COMPACT: {
    vi: '[Ẩn offline]',
    en: '[Hide offline]',
  },
  SHOW_OFFLINE_BTN: {
    vi: '[Hiện camera mất tín hiệu]',
    en: '[Show offline cameras]',
  },
  SHOW_OFFLINE_BTN_COMPACT: {
    vi: '[Hiện offline]',
    en: '[Show offline]',
  },
  TOOLTIP: {
    vi: (hidden: boolean) =>
      hidden
        ? 'Bấm để hiển thị cả 135 camera mất tín hiệu'
        : 'Bấm để ẩn 135 camera mất tín hiệu khỏi bản đồ',
    en: (hidden: boolean) =>
      hidden
        ? 'Click to show all 135 offline cameras'
        : 'Click to hide 135 offline cameras from map',
  },
});
