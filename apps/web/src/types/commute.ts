/**
 * Types for Commuter Personal Route Glance, Bottleneck Isolation & Detour Suggestions.
 * Enforces S17-T4 / Issue #512 specifications.
 */

import type { PassabilityVerdict, CorridorRouteSummary } from './corridor.ts';
import type { VehicleType } from './vehicle.ts';

export interface CommuteLocation {
  name: string;
  lat: number;
  lng: number;
  district?: string;
  corridorKey?: string;
}

export type CommuteDirection = 'to_work' | 'to_home';

export interface CommuteConfig {
  home: CommuteLocation | null;
  waypoint?: CommuteLocation | null;
  work: CommuteLocation | null;
  selectedCorridors: string[];
  direction: CommuteDirection;
  enabled: boolean;
}

export interface CommuteBottleneck {
  camera_id: string;
  name: string;
  severity: string;
  flood_severity: string;
  flood_depth_cm?: number | null;
  flood_hazard_level?: number | null;
  corridor_key: string;
  corridor_name: string;
  description: {
    vi: string;
    en: string;
  };
}

export interface DetourSuggestion {
  original_corridor_key: string;
  original_corridor_name?: string;
  detour_corridor_key: string;
  detour_corridor_name: string;
  detour_path?: string[];
  detour_path_names?: string[];
  passability_verdict: PassabilityVerdict;
  max_depth_cm?: number | null;
  delay_seconds: number;
  estimated_delta_seconds?: number;
  is_dry_route?: boolean;
  reason: {
    vi: string;
    en: string;
  };
}

export interface CommuteEvaluation {
  passability: PassabilityVerdict;
  status_color: string;
  worst_bottleneck: CommuteBottleneck | null;
  total_distance_km: number;
  free_flow_seconds: number;
  estimated_seconds: number;
  delay_seconds: number;
  rain_probability_pct: number;
  rain_summary: {
    vi: string;
    en: string;
  };
  corridors: string[];
  corridor_summaries: CorridorRouteSummary[];
  detour: DetourSuggestion | null;
  detours?: DetourSuggestion[];
  vehicle: VehicleType;
}

export interface CommutePreset {
  id: string;
  title: {
    vi: string;
    en: string;
  };
  home: CommuteLocation;
  work: CommuteLocation;
  corridors: string[];
}

export const COMMUTE_I18N = {
  TITLE: {
    vi: 'Lộ trình của tôi',
    en: 'My Route',
  },
  SUBTITLE: {
    vi: 'Đánh giá an toàn 3 giây trên hành trình hàng ngày',
    en: '3-second safety glance on your daily commute',
  },
  HOME: {
    vi: 'Nhà',
    en: 'Home',
  },
  WAYPOINT: {
    vi: 'Điểm trung chuyển',
    en: 'Waypoint',
  },
  WORK: {
    vi: 'Cơ quan',
    en: 'Work',
  },
  DIRECTION_TO_WORK: {
    vi: 'Lượt đi (Nhà ➔ Cơ quan)',
    en: 'To Work (Home ➔ Work)',
  },
  DIRECTION_TO_HOME: {
    vi: 'Lượt về (Cơ quan ➔ Nhà)',
    en: 'To Home (Work ➔ Home)',
  },
  SWITCH_DIRECTION: {
    vi: 'Đổi chiều di chuyển',
    en: 'Switch direction',
  },
  SETUP_ROUTE: {
    vi: 'Cài đặt lộ trình',
    en: 'Setup route',
  },
  CHANGE_ROUTE: {
    vi: 'Đổi điểm đi/đến',
    en: 'Change endpoints',
  },
  BOTTLENECK_TITLE: {
    vi: 'Điểm ách tắc / ngập nguy hiểm nhất:',
    en: 'Worst hazard bottleneck:',
  },
  DETOUR_AVAILABLE: {
    vi: 'Lộ trình thay thế khả dụng:',
    en: 'Recommended detour routes:',
  },
  VIEW_DETOUR: {
    vi: 'Xem lộ trình tránh',
    en: 'View detour',
  },
  DRY_ROUTE_TAG: {
    vi: 'Khô ráo',
    en: 'Dry',
  },
  SAFER_ROUTE_TAG: {
    vi: 'Ít ngập hơn',
    en: 'Safer',
  },
  SUBSCRIBE_TELEGRAM: {
    vi: 'Nhận thông báo tuyến này',
    en: 'Subscribe alerts for route',
  },
  ZERO_TELEMETRY: {
    vi: 'Bảo mật 100%: Dữ liệu lưu cục bộ trên thiết bị, không gửi vị trí lên máy chủ.',
    en: '100% Private: Stored locally on device, zero coordinates sent to server.',
  },
  PRESETS_LABEL: {
    vi: 'Chọn nhanh tuyến thường đi:',
    en: 'Quick commute presets:',
  },
  SAVE_CONFIG: {
    vi: 'Lưu lộ trình',
    en: 'Save route',
  },
  CANCEL: {
    vi: 'Đóng',
    en: 'Close',
  },
  CLEAR: {
    vi: 'Xóa lộ trình',
    en: 'Clear route',
  },
  MINUTES_UNIT: {
    vi: 'phút',
    en: 'mins',
  },
  DELAY_PREFIX: {
    vi: 'trễ',
    en: 'delay',
  },
  ON_TIME: {
    vi: 'Đúng giờ',
    en: 'On time',
  },
  RAIN_TITLE: {
    vi: 'Dự báo mưa:',
    en: 'Rain forecast:',
  },
  NO_RAIN: {
    vi: 'Khô ráo',
    en: 'Dry',
  },
} as const;
