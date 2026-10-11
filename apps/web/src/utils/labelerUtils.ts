export type FloodLevel = 'none' | 'puddle' | 'deep';
export type TrafficLevel = 'clear' | 'moderate' | 'heavy' | 'gridlock';
export type Weather = 'clear' | 'rain' | 'fog';
export type TimeOfDay = 'day' | 'dusk' | 'night';

export interface LabelState {
  flood_level?: FloodLevel;
  traffic_level?: TrafficLevel;
  weather?: Weather;
  time_of_day?: TimeOfDay;
  notes?: string;
}

export interface LabelPayload extends LabelState {
  date: string;
  camera_id: string;
  snapshot_key: string;
  annotator: string;
  timestamp: number;
}

export interface SnapshotItem {
  key: string;
  camera_id: string;
  camera_name?: string;
  timestamp: number;
  reason: string;
  size: number;
  raw_url: string;
  public_url?: string;
}

export interface HotkeyResult {
  state: LabelState;
  action?: 'prev' | 'next' | 'save';
}

/**
 * Maps single keypresses to label selections or UI navigation actions.
 */
export function handleHotkey(key: string, current: LabelState): HotkeyResult {
  const next: LabelState = { ...current };
  const lower = key.toLowerCase();

  // Navigation & Submission
  if (key === 'ArrowLeft') {
    return { state: next, action: 'prev' };
  }
  if (key === 'ArrowRight') {
    return { state: next, action: 'next' };
  }
  if (key === 'Enter') {
    return { state: next, action: 'save' };
  }

  // 1-3: Flood Level
  if (key === '1') {
    next.flood_level = 'none';
    return { state: next };
  }
  if (key === '2') {
    next.flood_level = 'puddle';
    return { state: next };
  }
  if (key === '3') {
    next.flood_level = 'deep';
    return { state: next };
  }

  // Q-W-E-R: Traffic Level
  if (lower === 'q') {
    next.traffic_level = 'clear';
    return { state: next };
  }
  if (lower === 'w') {
    next.traffic_level = 'moderate';
    return { state: next };
  }
  if (lower === 'e') {
    next.traffic_level = 'heavy';
    return { state: next };
  }
  if (lower === 'r') {
    next.traffic_level = 'gridlock';
    return { state: next };
  }

  // A-S-D: Weather
  if (lower === 'a') {
    next.weather = 'clear';
    return { state: next };
  }
  if (lower === 's') {
    next.weather = 'rain';
    return { state: next };
  }
  if (lower === 'd') {
    next.weather = 'fog';
    return { state: next };
  }

  // Z-X-C: Time of Day
  if (lower === 'z') {
    next.time_of_day = 'day';
    return { state: next };
  }
  if (lower === 'x') {
    next.time_of_day = 'dusk';
    return { state: next };
  }
  if (lower === 'c') {
    next.time_of_day = 'night';
    return { state: next };
  }

  return { state: next };
}

/**
 * Checks if a snapshot label has all 4 required dimensions populated.
 */
export function isLabelComplete(label: Partial<LabelState>): boolean {
  return Boolean(
    label.flood_level &&
    label.traffic_level &&
    label.weather &&
    label.time_of_day
  );
}

/**
 * Formats unix timestamp in Vietnam Standard Time (ICT).
 */
export function formatSnapshotTimestamp(timestamp: number): string {
  if (!timestamp) return '--:--';
  const d = new Date(timestamp * 1000);
  return d.toLocaleString('vi-VN', {
    timeZone: 'Asia/Ho_Chi_Minh',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
}

/**
 * Zero-emoji bilingual dictionary adhering to AGENTS.md Rule 1.
 */
export const LABELER_I18N = {
  vi: {
    title: 'Gán Nhãn Ảnh Giám Sát',
    subtitle: 'Nền tảng gắn nhãn cộng tác cho dữ liệu huấn luyện và kiểm thử giao thông & ngập lụt',
    dateSelect: 'Ngày phân vùng',
    annotatorLabel: 'Người gán nhãn',
    annotatorPlaceholder: 'Nhập tên của bạn...',
    progress: 'Tiến độ',
    noSnapshots: 'Không tìm thấy ảnh chụp nào cho ngày đã chọn',
    loading: 'Đang tải danh sách ảnh...',
    floodLabel: 'Mức độ ngập (Phím 1-3)',
    floodNone: '1: Khô ráo',
    floodPuddle: '2: Vũng nước / Mấp mé',
    floodDeep: '3: Ngập sâu (Nửa bánh+)',
    trafficLabel: 'Mật độ giao thông (Phím Q-R)',
    trafficClear: 'Q: Thông thoáng',
    trafficModerate: 'W: Bình thường',
    trafficHeavy: 'E: Đông đúc',
    trafficGridlock: 'R: Kẹt xe / Đứng yên',
    weatherLabel: 'Thời tiết (Phím A-D)',
    weatherClear: 'A: Nắng ráo',
    weatherRain: 'S: Mưa',
    weatherFog: 'D: Sương mù / Bụi',
    timeOfDayLabel: 'Khung giờ (Phím Z-C)',
    timeOfDayDay: 'Z: Ban ngày',
    timeOfDayDusk: 'X: Hoàng hôn / Bình minh',
    timeOfDayNight: 'C: Ban đêm',
    notesLabel: 'Ghi chú thêm',
    notesPlaceholder: 'Nhập ghi chú quan sát...',
    saveAndNext: 'Lưu & Kế Tiếp (Enter)',
    prev: 'Ảnh Trước (Mũi tên Trái)',
    next: 'Ảnh Kế (Mũi tên Phải)',
    savedBadge: 'Đã lưu',
    unsavedBadge: 'Chưa lưu',
    exportJson: 'Xuất Dữ Liệu JSON',
    backToMap: 'Về Bản Đồ',
  },
  en: {
    title: 'Snapshot Dataset Labeler',
    subtitle: 'Collaborative labeling platform for traffic and flood evaluation ground truth',
    dateSelect: 'Partition Date',
    annotatorLabel: 'Annotator',
    annotatorPlaceholder: 'Enter your name...',
    progress: 'Progress',
    noSnapshots: 'No snapshots found for selected date',
    loading: 'Loading snapshot catalog...',
    floodLabel: 'Flood Level (Keys 1-3)',
    floodNone: '1: None',
    floodPuddle: '2: Puddle / Ankle-deep',
    floodDeep: '3: Deep (Half-wheel+)',
    trafficLabel: 'Traffic Density (Keys Q-R)',
    trafficClear: 'Q: Clear',
    trafficModerate: 'W: Moderate',
    trafficHeavy: 'E: Heavy',
    trafficGridlock: 'R: Gridlock / Standstill',
    weatherLabel: 'Weather (Keys A-D)',
    weatherClear: 'A: Clear',
    weatherRain: 'S: Rain',
    weatherFog: 'D: Fog / Haze',
    timeOfDayLabel: 'Time of Day (Keys Z-C)',
    timeOfDayDay: 'Z: Day',
    timeOfDayDusk: 'X: Dusk / Dawn',
    timeOfDayNight: 'C: Night',
    notesLabel: 'Notes',
    notesPlaceholder: 'Optional observations...',
    saveAndNext: 'Save & Next (Enter)',
    prev: 'Previous (Left Arrow)',
    next: 'Next (Right Arrow)',
    savedBadge: 'Saved',
    unsavedBadge: 'Unsaved',
    exportJson: 'Export JSON',
    backToMap: 'Back to Map',
  },
} as const;
