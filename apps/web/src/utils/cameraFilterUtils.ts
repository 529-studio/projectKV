import type { Camera } from '../hooks/useCameras';

/**
 * Checks whether a camera is considered offline / no-signal.
 * Specifically checks for 'NOT_IMAGE' or 'NO_SIGNAL' status, which represent
 * non-functional cameras causing 503 ghost modals.
 */
export const isCameraOffline = (cam: Camera | null | undefined): boolean => {
  if (!cam) return false;
  return cam.status === 'NOT_IMAGE' || cam.status === 'NO_SIGNAL';
};

export const CAMERA_FILTER_I18N = Object.freeze({
  COUNTER: {
    vi: (visible: number, total: number) => `Đang hiển thị ${visible} / ${total} camera`,
    en: (visible: number, total: number) => `Showing ${visible} / ${total} cameras`,
  },
  HIDE_OFFLINE_BTN: {
    vi: '[Ẩn camera mất tín hiệu]',
    en: '[Hide offline cameras]',
  },
  SHOW_OFFLINE_BTN: {
    vi: '[Hiện camera mất tín hiệu]',
    en: '[Show offline cameras]',
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
