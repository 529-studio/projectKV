/**
 * Vehicle Profile Types & Clearance Thresholds (Issue #510 / Sprint 17 Task 3).
 * Minimalist, monochromatic, zero-emoji definitions tailored for HCMC commuters.
 */

import type { PassabilityVerdict } from './corridor.ts';

export type VehicleType = 'scooter' | 'motorbike' | 'sedan' | 'suv';

export interface VehicleProfile {
  id: VehicleType;
  name: {
    vi: string;
    en: string;
  };
  shortLabel: {
    vi: string;
    en: string;
  };
  subtext: {
    vi: string;
    en: string;
  };
  // Ground clearance & physical flood thresholds in centimetres
  groundClearanceCm: number;
  cautionDepthCm: number;
  impassableDepthCm: number;
  // Contextual passability labels tailored to vehicle physical mechanics
  verdictLabels: Record<
    PassabilityVerdict,
    {
      vi: string;
      en: string;
    }
  >;
}

export const VEHICLE_PROFILES: Record<VehicleType, VehicleProfile> = {
  scooter: {
    id: 'scooter',
    name: {
      vi: 'Xe tay ga gầm thấp',
      en: 'Low-clearance Scooter',
    },
    shortLabel: {
      vi: 'Xe ga',
      en: 'Scooter',
    },
    subtext: {
      vi: 'Vision, Lead, AirBlade, SH Mode, Vespa...',
      en: 'Vision, Lead, AirBlade, Vespa...',
    },
    groundClearanceCm: 13.0,
    cautionDepthCm: 10.0,
    impassableDepthCm: 20.0,
    verdictLabels: {
      PASSABLE: {
        vi: 'THÔNG THOÁNG',
        en: 'PASSABLE',
      },
      CAUTION: {
        vi: 'CẢNH BÁO XE TAY GA',
        en: 'CAUTION (SCOOTER)',
      },
      IMPASSABLE: {
        vi: 'NGUY CƠ CHẾT MÁY / THỦY KÍCH',
        en: 'IMPASSABLE (STALL RISK)',
      },
      UNKNOWN: {
        vi: 'CHƯA ĐỦ DỮ LIỆU',
        en: 'UNKNOWN',
      },
    },
  },
  motorbike: {
    id: 'motorbike',
    name: {
      vi: 'Xe số / Xe côn tay',
      en: 'Standard Motorbike',
    },
    shortLabel: {
      vi: 'Xe số / côn',
      en: 'Motorbike',
    },
    subtext: {
      vi: 'Wave, Future, Winner, Exciter, Sirius...',
      en: 'Wave, Future, Exciter, Winner...',
    },
    groundClearanceCm: 16.0,
    cautionDepthCm: 15.0,
    impassableDepthCm: 28.0,
    verdictLabels: {
      PASSABLE: {
        vi: 'THÔNG THOÁNG',
        en: 'PASSABLE',
      },
      CAUTION: {
        vi: 'NƯỚC NGẬP MÉP BÁNH XE',
        en: 'CAUTION (MODERATE WATER)',
      },
      IMPASSABLE: {
        vi: 'NGẬP ỐNG PÔ / NGUY CƠ CHẾT MÁY',
        en: 'IMPASSABLE (EXHAUST SUBMERGED)',
      },
      UNKNOWN: {
        vi: 'CHƯA ĐỦ DỮ LIỆU',
        en: 'UNKNOWN',
      },
    },
  },
  sedan: {
    id: 'sedan',
    name: {
      vi: 'Ô tô sedan gầm thấp',
      en: 'Sedan / Hatchback Car',
    },
    shortLabel: {
      vi: 'Ô tô sedan',
      en: 'Sedan',
    },
    subtext: {
      vi: 'Vios, Accent, Civic, Mazda 3, Camry...',
      en: 'Vios, Accent, Civic, Mazda 3...',
    },
    groundClearanceCm: 14.0,
    cautionDepthCm: 15.0,
    impassableDepthCm: 25.0,
    verdictLabels: {
      PASSABLE: {
        vi: 'THÔNG THOÁNG',
        en: 'PASSABLE',
      },
      CAUTION: {
        vi: 'CẢNH BÁO XE GẦM THẤP',
        en: 'CAUTION (LOW CLEARANCE)',
      },
      IMPASSABLE: {
        vi: 'NGẬP HÚT GIÓ / NGUY CƠ THỦY KÍCH',
        en: 'IMPASSABLE (HYDRO-LOCK RISK)',
      },
      UNKNOWN: {
        vi: 'CHƯA ĐỦ DỮ LIỆU',
        en: 'UNKNOWN',
      },
    },
  },
  suv: {
    id: 'suv',
    name: {
      vi: 'Ô tô SUV / Bán tải',
      en: 'SUV / Pickup Truck',
    },
    shortLabel: {
      vi: 'SUV / Bán tải',
      en: 'SUV / Truck',
    },
    subtext: {
      vi: 'Ranger, Everest, Fortuner, Hilux, CR-V...',
      en: 'Ranger, Everest, Fortuner, Hilux...',
    },
    groundClearanceCm: 22.0,
    cautionDepthCm: 25.0,
    impassableDepthCm: 40.0,
    verdictLabels: {
      PASSABLE: {
        vi: 'THÔNG THOÁNG',
        en: 'PASSABLE',
      },
      CAUTION: {
        vi: 'NƯỚC NGẬP NỬA BÁNH XE',
        en: 'CAUTION (DEEP WATER)',
      },
      IMPASSABLE: {
        vi: 'NGẬP NẮP CAPO / NGUY CƠ CHẾT MÁY',
        en: 'IMPASSABLE (EXTREME FLOOD)',
      },
      UNKNOWN: {
        vi: 'CHƯA ĐỦ DỮ LIỆU',
        en: 'UNKNOWN',
      },
    },
  },
};

export const DEFAULT_VEHICLE_TYPE: VehicleType = 'scooter';
export const VEHICLE_STORAGE_KEY = 'warded_vehicle_profile';
