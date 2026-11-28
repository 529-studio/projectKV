/**
 * Local-First Storage Management for Commuter Route Configuration.
 * 100% Client-Side Privacy: zero telemetry or coordinates leaked to third parties.
 * Enforces S17-T4 / Issue #512.
 */

import type { CommuteConfig, CommutePreset } from '../types/commute.ts';

export const COMMUTE_STORAGE_KEY = 'warded_commute_route';

export const COMMUTE_PRESETS: CommutePreset[] = [
  {
    id: 'binh_thanh_q1',
    title: {
      vi: 'Bình Thạnh ↔ Quận 1 (Landmark 81 ➔ Bến Nghé)',
      en: 'Binh Thanh ↔ District 1 (Landmark 81 ➔ Ben Nghe)',
    },
    home: {
      name: 'Landmark 81, P.22, Bình Thạnh',
      lat: 10.795,
      lng: 106.721,
      district: 'Bình Thạnh',
      corridorKey: 'nguyen_huu_canh',
    },
    work: {
      name: 'Bến Nghé, Quận 1',
      lat: 10.776,
      lng: 106.703,
      district: 'Quận 1',
      corridorKey: 'nguyen_huu_canh',
    },
    corridors: ['nguyen_huu_canh'],
  },
  {
    id: 'q7_q1',
    title: {
      vi: 'Quận 7 ↔ Quận 1 (Phú Mỹ Hưng ➔ Chợ Bến Thành)',
      en: 'District 7 ↔ District 1 (Phu My Hung ➔ Ben Thanh)',
    },
    home: {
      name: 'Phú Mỹ Hưng, Tân Phong, Quận 7',
      lat: 10.729,
      lng: 106.721,
      district: 'Quận 7',
      corridorKey: 'nguyen_van_linh',
    },
    work: {
      name: 'Chợ Bến Thành, Quận 1',
      lat: 10.772,
      lng: 106.698,
      district: 'Quận 1',
      corridorKey: 'vo_van_kiet',
    },
    corridors: ['nguyen_van_linh', 'vo_van_kiet'],
  },
  {
    id: 'tan_binh_q1',
    title: {
      vi: 'Tân Bình ↔ Quận 1 (Sân bay TSN ➔ Hồ Con Rùa)',
      en: 'Tan Binh ↔ District 1 (TSN Airport ➔ Con Rua Lake)',
    },
    home: {
      name: 'Sân bay Tân Sơn Nhất, Tân Bình',
      lat: 10.818,
      lng: 106.659,
      district: 'Tân Bình',
      corridorKey: 'nam_ky_khoi_nghia',
    },
    work: {
      name: 'Hồ Con Rùa, Phường Võ Thị Sáu, Quận 3',
      lat: 10.782,
      lng: 106.696,
      district: 'Quận 3',
      corridorKey: 'nam_ky_khoi_nghia',
    },
    corridors: ['nam_ky_khoi_nghia'],
  },
  {
    id: 'go_vap_binh_thanh',
    title: {
      vi: 'Gò Vấp ↔ Bình Thạnh (Phan Văn Trị ➔ Hàng Xanh)',
      en: 'Go Vap ↔ Binh Thanh (Phan Van Tri ➔ Hang Xanh)',
    },
    home: {
      name: 'Phan Văn Trị, Phường 11, Gò Vấp',
      lat: 10.825,
      lng: 106.693,
      district: 'Gò Vấp',
      corridorKey: 'pham_van_dong',
    },
    work: {
      name: 'Ngã tư Hàng Xanh, Bình Thạnh',
      lat: 10.801,
      lng: 106.711,
      district: 'Bình Thạnh',
      corridorKey: 'dien_bien_phu',
    },
    corridors: ['pham_van_dong', 'dien_bien_phu'],
  },
  {
    id: 'thu_duc_q1',
    title: {
      vi: 'TP. Thủ Đức ↔ Quận 1 (Thảo Điền ➔ Hầm Thủ Thiêm)',
      en: 'Thu Duc City ↔ District 1 (Thao Dien ➔ Thu Thiem Tunnel)',
    },
    home: {
      name: 'Thảo Điền, TP. Thủ Đức',
      lat: 10.804,
      lng: 106.735,
      district: 'TP. Thủ Đức',
      corridorKey: 'mai_chi_tho',
    },
    work: {
      name: 'Hầm Thủ Thiêm, Bến Chương Dương, Quận 1',
      lat: 10.768,
      lng: 106.705,
      district: 'Quận 1',
      corridorKey: 'mai_chi_tho',
    },
    corridors: ['mai_chi_tho'],
  },
];

export function getStoredCommuteConfig(): CommuteConfig | null {
  if (typeof window === 'undefined') return null;
  try {
    const raw = localStorage.getItem(COMMUTE_STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === 'object') {
      return {
        home: parsed.home ?? null,
        waypoint:
          parsed.waypoint && typeof parsed.waypoint === 'object' && parsed.waypoint.name
            ? parsed.waypoint
            : null,
        work: parsed.work ?? null,
        selectedCorridors: Array.isArray(parsed.selectedCorridors) ? parsed.selectedCorridors : [],
        direction: parsed.direction === 'to_home' ? 'to_home' : 'to_work',
        enabled: Boolean(parsed.enabled),
      };
    }
  } catch {
    // Malformed storage fallback
  }
  return null;
}

export function setStoredCommuteConfig(config: CommuteConfig): void {
  if (typeof window === 'undefined') return;
  try {
    localStorage.setItem(COMMUTE_STORAGE_KEY, JSON.stringify(config));
  } catch {
    // Storage write fallback
  }
}

export function clearStoredCommuteConfig(): void {
  if (typeof window === 'undefined') return;
  try {
    localStorage.removeItem(COMMUTE_STORAGE_KEY);
  } catch {
    // Storage remove fallback
  }
}
