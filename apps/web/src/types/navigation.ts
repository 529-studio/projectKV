/**
 * Types and i18n constants for Turn-by-Turn Navigation Hazard Alerts.
 * Implements S18-T1 / Issue #523.
 */

import type { PassabilityVerdict } from './corridor.ts';
import type { VehicleType } from './vehicle.ts';

export interface NavigationHazardAlert {
  id: string;
  name: string;
  distanceAlongRouteMeters: number;
  distanceAheadMeters: number;
  distanceFormatted: {
    vi: string;
    en: string;
  };
  hazardType: 'flood' | 'traffic' | 'clear';
  severity: PassabilityVerdict;
  depthCm?: number | null;
  excessDepthCm?: number | null;
  vehicleType: VehicleType;
  actionableAdvice: {
    vi: string;
    en: string;
  };
  coordinates: [number, number]; // [latitude, longitude]
  isAhead: boolean;
  stale?: boolean;
}

export interface NavigationSummary {
  corridorKey: string;
  corridorName: string;
  totalLengthMeters: number;
  totalCheckpoints: number;
  hazardCheckpoints: number;
  worstVerdict: PassabilityVerdict;
  alerts: NavigationHazardAlert[];
}

export const NAVIGATION_I18N = {
  TITLE: {
    vi: 'Cảnh báo lộ trình di chuyển',
    en: 'Route Navigation Alerts',
  },
  SUBTITLE: {
    vi: 'Cảnh báo ngập & chướng ngại vật theo từng nút giao',
    en: 'Turn-by-turn intersection flood and traffic alerts',
  },
  AHEAD: {
    vi: 'Cách',
    en: 'In',
  },
  PASSED: {
    vi: 'Đã qua',
    en: 'Passed',
  },
  NO_HAZARDS: {
    vi: 'Tuyến đường thông thoáng, không có điểm ngập nguy hiểm.',
    en: 'Route is clear with no hazardous inundations ahead.',
  },
  HAZARD_AHEAD_SINGULAR: {
    vi: 'điểm cảnh báo phía trước',
    en: 'alert point ahead',
  },
  HAZARD_AHEAD_PLURAL: {
    vi: 'điểm cảnh báo phía trước',
    en: 'alert points ahead',
  },
  VIEW_ON_MAP: {
    vi: 'Xem vị trí',
    en: 'View on map',
  },
  CLEARANCE_LABEL: {
    vi: 'Độ sâu ngập vượt gầm:',
    en: 'Water exceeds ground clearance:',
  },
  COLLAPSE: {
    vi: 'Thu gọn danh sách cảnh báo',
    en: 'Collapse alerts list',
  },
  EXPAND: {
    vi: 'Mở rộng danh sách cảnh báo',
    en: 'Expand alerts list',
  },
  CLOSE: {
    vi: 'Đóng cảnh báo lộ trình',
    en: 'Close navigation alerts',
  },
  IMPASSABLE_WARNING: {
    vi: 'NGUY CƠ CHẾT MÁY',
    en: 'ENGINE STALL HAZARD',
  },
  CAUTION_WARNING: {
    vi: 'CẢNH BÁO XE GẦM THẤP',
    en: 'LOW CLEARANCE CAUTION',
  },
  PASSABLE_NOTICE: {
    vi: 'THÔNG THOÁNG',
    en: 'PASSABLE',
  },
} as const;
