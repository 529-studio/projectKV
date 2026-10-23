export type FloodLevel = 'none' | 'puddle' | 'deep';
export type TrafficLevel = 'clear' | 'moderate' | 'heavy' | 'gridlock';
export type DirectionalTrafficLevel = 'clear' | 'moderate' | 'heavy' | 'congested' | 'not_applicable';
export type FloodSubmersionAnchor =
  | 'dry'
  | 'wet_asphalt_no_standing'
  | 'tire_base_under_10cm'
  | 'exhaust_hub_over_15cm';
export type QueueReason =
  | 'free_flow'
  | 'traffic_light'
  | 'bottleneck'
  | 'flood_barrier'
  | 'accident';
export type QualityTag = 'rain_on_lens' | 'night_glare' | 'occluded' | 'signal_noise';
export type Weather = 'clear' | 'rain' | 'fog';
export type TimeOfDay = 'day' | 'dusk' | 'night';

export interface LabelState {
  flood_level?: FloodLevel;
  traffic_level?: TrafficLevel;
  traffic_dir_a?: DirectionalTrafficLevel;
  traffic_dir_b?: DirectionalTrafficLevel;
  queue_reason?: QueueReason;
  flood_submersion_anchor?: FloodSubmersionAnchor;
  quality_tags?: QualityTag[];
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

export type LabelFilterStatus = 'all' | 'pending_auto' | 'approved_human';

export interface HotkeyResult {
  state: LabelState;
  action?: 'prev' | 'next' | 'save' | 'approve';
}

export interface HotkeyMeta {
  shiftKey?: boolean;
}

/**
 * Maps single keypresses to label selections or UI navigation actions.
 */
export function handleHotkey(
  key: string,
  current: LabelState,
  meta?: HotkeyMeta
): HotkeyResult {
  const next: LabelState = { ...current };

  // Fast-Approval Hotkey: Shift + Enter
  if ((key === 'Enter' && meta?.shiftKey) || key === 'Shift+Enter') {
    return { state: next, action: 'approve' };
  }

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

  // 1-3: Flood Level & Physical Submersion Anchor
  if (key === '1') {
    next.flood_level = 'none';
    next.flood_submersion_anchor = 'dry';
    return { state: next };
  }
  if (key === '2') {
    next.flood_level = 'puddle';
    next.flood_submersion_anchor = 'tire_base_under_10cm';
    return { state: next };
  }
  if (key === '3') {
    next.flood_level = 'deep';
    next.flood_submersion_anchor = 'exhaust_hub_over_15cm';
    return { state: next };
  }

  // Shift + Q-W-E-R-T: Direction B (Far lane / Receding flow)
  if (key === 'Q') {
    next.traffic_dir_b = 'clear';
    return { state: next };
  }
  if (key === 'W') {
    next.traffic_dir_b = 'moderate';
    return { state: next };
  }
  if (key === 'E') {
    next.traffic_dir_b = 'heavy';
    return { state: next };
  }
  if (key === 'R') {
    next.traffic_dir_b = 'congested';
    return { state: next };
  }
  if (key === 'T') {
    next.traffic_dir_b = 'not_applicable';
    return { state: next };
  }

  // q-w-e-r: Direction A (Primary / Near lane) & Global Traffic Level rollup
  if (key.toLowerCase() === 'q' && key !== 'Q') {
    next.traffic_level = 'clear';
    next.traffic_dir_a = 'clear';
    return { state: next };
  }
  if (key.toLowerCase() === 'w' && key !== 'W') {
    next.traffic_level = 'moderate';
    next.traffic_dir_a = 'moderate';
    return { state: next };
  }
  if (key.toLowerCase() === 'e' && key !== 'E') {
    next.traffic_level = 'heavy';
    next.traffic_dir_a = 'heavy';
    return { state: next };
  }
  if (key.toLowerCase() === 'r' && key !== 'R') {
    next.traffic_level = 'gridlock';
    next.traffic_dir_a = 'congested';
    return { state: next };
  }

  // A-S-D: Weather
  const lower = key.toLowerCase();
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
 * Checks if a snapshot label has all required dimensions populated.
 */
export function isLabelComplete(label: Partial<LabelState>): boolean {
  return Boolean(
    label.flood_level &&
    (label.traffic_level || label.traffic_dir_a) &&
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
 * Returns true if a label was synthesized by an automated harvester agent or pipeline.
 */
export function isAutoHarvested(label?: Partial<LabelPayload> | null): boolean {
  if (!label || !label.annotator) return false;
  const annotator = label.annotator.trim().toLowerCase();
  return (
    annotator.startsWith('harvester/') ||
    annotator === 'harvester/auto' ||
    annotator === 'auto_harvester' ||
    annotator === 'auto'
  );
}

/**
 * Filters snapshot queue by provenance: all, pending_auto, or approved_human.
 */
export function filterSnapshots(
  snapshots: SnapshotItem[],
  labels: Record<string, LabelPayload>,
  filter: LabelFilterStatus
): SnapshotItem[] {
  if (filter === 'all') return snapshots;
  if (filter === 'pending_auto') {
    return snapshots.filter((s) => {
      const l = labels[s.key];
      return Boolean(l && isAutoHarvested(l));
    });
  }
  if (filter === 'approved_human') {
    return snapshots.filter((s) => {
      const l = labels[s.key];
      return Boolean(l && !isAutoHarvested(l));
    });
  }
  return snapshots;
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
    floodPuddle: '2: Vũng nước / Mấp mé (<10cm)',
    floodDeep: '3: Ngập sâu (Quá nửa bánh/ống xả >15cm)',
    floodSubmersionLabel: 'Mỏ neo ngập vật lý',
    anchorDry: 'Khô ráo / Không đọng nước',
    anchorWetNoStanding: 'Ướt phản chiếu, không ngập',
    anchorTireBase: 'Ngập mấp mé đế bánh xe (<10cm)',
    anchorExhaustHub: 'Ngập trục bánh / ống bô (>15cm)',
    trafficLabel: 'Mật độ giao thông (Phím q-r)',
    trafficClear: 'q: Thông thoáng',
    trafficModerate: 'w: Bình thường',
    trafficHeavy: 'e: Rất đông',
    trafficGridlock: 'r: Kẹt xe / Tắc đường',
    trafficDirALabel: 'Chiều A / Làn gần (Phím q-r)',
    trafficDirBLabel: 'Chiều B / Làn xa (Shift + Q-R / T)',
    trafficNotApplicable: 'T: Không áp dụng',
    queueReasonLabel: 'Nguyên nhân dừng xe',
    reasonFreeFlow: 'Lưu thông tự do',
    reasonTrafficLight: 'Chờ đèn đỏ (tạm thời)',
    reasonBottleneck: 'Ùn ứ nút giao',
    reasonFloodBarrier: 'Ngập chắn đường',
    qualityTagsLabel: 'Chất lượng ảnh & Môi trường',
    tagRainOnLens: 'Mưa bám ống kính',
    tagNightGlare: 'Lóa đèn pha đêm',
    tagOccluded: 'Bị che khuất',
    tagSignalNoise: 'Nhiễu tín hiệu',
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
    fastApproveAction: 'Duyệt Nhanh (Shift + Enter)',
    fastApproveTooltip: 'Xác nhận gán nhãn tự động và chuyển sang ảnh kế tiếp',
    filterAll: 'Tất cả',
    filterPendingAuto: 'Chờ duyệt (Auto)',
    filterApprovedHuman: 'Đã duyệt (Human)',
    badgeAutoHarvester: 'Tự động: Harvester',
    badgeHumanReviewer: 'Người duyệt',
    badgeUnlabeled: 'Chưa gán nhãn',
    prev: 'Ảnh Trước (Mũi tên Trái)',
    next: 'Ảnh Kế (Mũi tên Phải)',
    savedBadge: 'Đã lưu',
    unsavedBadge: 'Chưa lưu',
    saveError: 'Lỗi Lưu',
    exportJson: 'Xuất Dữ Liệu JSON',
    refreshData: 'Làm Mới',
    backToMap: 'Về Bản Đồ',
    devAuthRequired: 'Yêu Cầu Quyền Truy Cập Developer',
    devAuthSubtitle: 'Khu vực gán nhãn dữ liệu nội bộ. Vui lòng nhập Developer Access Token để tiếp tục.',
    devTokenPlaceholder: 'Nhập mã truy cập dev...',
    devUnlock: 'Mở Khóa (Enter)',
    devInvalidToken: 'Mã xác thực không hợp lệ hoặc chưa được cấp quyền',
    devLockSession: 'Khóa Phiên',
    badgeCfAccess: 'Cloudflare Zero Trust',
    badgeDevToken: 'Dev Token (Fallback)',
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
    floodPuddle: '2: Shallow / Ankle-deep (<10cm)',
    floodDeep: '3: Deep (Half-wheel/exhaust >15cm)',
    floodSubmersionLabel: 'Physical Flood Depth Anchor',
    anchorDry: 'Dry / No water pooling',
    anchorWetNoStanding: 'Wet sheen / reflection, zero depth',
    anchorTireBase: 'Tire contact patch submerged (<10cm)',
    anchorExhaustHub: 'Wheel hub / exhaust submerged (>15cm)',
    trafficLabel: 'Traffic Density (Keys q-r)',
    trafficClear: 'q: Clear',
    trafficModerate: 'w: Moderate',
    trafficHeavy: 'e: Heavy',
    trafficGridlock: 'r: Congested / Gridlock',
    trafficDirALabel: 'Direction A / Near Lane (Keys q-r)',
    trafficDirBLabel: 'Direction B / Far Lane (Shift + Q-R / T)',
    trafficNotApplicable: 'T: Not Applicable',
    queueReasonLabel: 'Traffic Queuing Reason',
    reasonFreeFlow: 'Free flow',
    reasonTrafficLight: 'Traffic light red phase',
    reasonBottleneck: 'Bottleneck',
    reasonFloodBarrier: 'Flooding blockage',
    qualityTagsLabel: 'Image Quality & Environmental Tags',
    tagRainOnLens: 'Rain on lens',
    tagNightGlare: 'Night headlight glare',
    tagOccluded: 'Occluded / Blocked',
    tagSignalNoise: 'Signal compression noise',
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
    fastApproveAction: 'Fast Approve (Shift + Enter)',
    fastApproveTooltip: 'Confirm auto-harvested label and advance to next frame',
    filterAll: 'All',
    filterPendingAuto: 'Pending (Auto)',
    filterApprovedHuman: 'Approved (Human)',
    badgeAutoHarvester: 'Auto: Harvester',
    badgeHumanReviewer: 'Reviewer',
    badgeUnlabeled: 'Unlabeled',
    prev: 'Previous (Left Arrow)',
    next: 'Next (Right Arrow)',
    savedBadge: 'Saved',
    unsavedBadge: 'Unsaved',
    saveError: 'Save Error',
    exportJson: 'Export JSON',
    refreshData: 'Refresh',
    backToMap: 'Back to Map',
    devAuthRequired: 'Developer Access Required',
    devAuthSubtitle: 'Internal ground truth labeling workspace. Please enter Developer Access Token to continue.',
    devTokenPlaceholder: 'Enter developer access token...',
    devUnlock: 'Unlock (Enter)',
    devInvalidToken: 'Invalid or unauthorized developer token',
    devLockSession: 'Lock Session',
    badgeCfAccess: 'Cloudflare Zero Trust',
    badgeDevToken: 'Dev Token (Fallback)',
  },
} as const;

export type AuthMethod = 'cloudflare_access' | 'dev_token' | 'none';

export interface AuthInfo {
  authenticated: boolean;
  authMethod: AuthMethod;
  userEmail?: string;
}

export const DEV_TOKEN_STORAGE_KEY = 'labeler_dev_token';

export function getStoredDevToken(): string {
  if (typeof window === 'undefined') return '';
  try {
    return sessionStorage.getItem(DEV_TOKEN_STORAGE_KEY) || '';
  } catch {
    return '';
  }
}

export function setStoredDevToken(token: string): void {
  if (typeof window === 'undefined') return;
  try {
    sessionStorage.setItem(DEV_TOKEN_STORAGE_KEY, token.trim());
  } catch {}
}

export function clearStoredDevToken(): void {
  if (typeof window === 'undefined') return;
  try {
    sessionStorage.removeItem(DEV_TOKEN_STORAGE_KEY);
  } catch {}
}

export function buildAuthHeaders(token: string, baseHeaders?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...(baseHeaders || {}) };
  const clean = token.trim();
  if (clean) {
    headers['X-Dev-Token'] = clean;
    headers['Authorization'] = `Bearer ${clean}`;
  }
  return headers;
}
