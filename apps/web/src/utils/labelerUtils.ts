export type FloodLevel = 'none' | 'puddle' | 'deep';
export type TrafficLevel = 'clear' | 'moderate' | 'heavy' | 'gridlock';
export type DirectionalTrafficLevel = 'clear' | 'moderate' | 'heavy' | 'congested' | 'not_applicable';

export type WaterlineDepth =
  | 'DRY'
  | 'WET_SURFACE'
  | 'TIRE_QUARTER_SUBMERGED'
  | 'TIRE_HALF_SUBMERGED'
  | 'IMPASSABLE'
  | 'UNKNOWN';

export type FloodSubmersionAnchor =
  | 'dry'
  | 'wet_asphalt_no_standing'
  | 'tire_base_under_10cm'
  | 'exhaust_hub_over_15cm'
  | 'wheel_submerged_over_30cm';

export interface SubmersionLevelConfig {
  key: '1' | '2' | '3' | '4' | '5';
  waterline: WaterlineDepth;
  anchor: FloodSubmersionAnchor;
  floodLevel: FloodLevel;
  depthCm: number;
  labelVi: string;
  labelEn: string;
  descVi: string;
  descEn: string;
  color: string;
}

export const SUBMERSION_LEVELS: readonly SubmersionLevelConfig[] = [
  {
    key: '1',
    waterline: 'DRY',
    anchor: 'dry',
    floodLevel: 'none',
    depthCm: 0,
    labelVi: '1: Khô ráo',
    labelEn: '1: Dry',
    descVi: 'Mặt đường khô, 0 cm, xe lưu thông bình thường',
    descEn: 'Dry asphalt, 0 cm, normal vehicle passage',
    color: '#10b981',
  },
  {
    key: '2',
    waterline: 'WET_SURFACE',
    anchor: 'wet_asphalt_no_standing',
    floodLevel: 'none',
    depthCm: 0,
    labelVi: '2: Ướt mặt đường',
    labelEn: '2: Wet Surface',
    descVi: 'Mặt đường ướt phản chiếu, 0 cm, không đọng vũng',
    descEn: 'Wet sheen reflection, 0 cm, no standing puddle',
    color: '#06b6d4',
  },
  {
    key: '3',
    waterline: 'TIRE_QUARTER_SUBMERGED',
    anchor: 'tire_base_under_10cm',
    floodLevel: 'puddle',
    depthCm: 8,
    labelVi: '3: Mép vành bánh',
    labelEn: '3: Tire Base (<10cm)',
    descVi: 'Ngập dưới 10 cm, ngập mép vành bánh xe, an toàn mọi phương tiện',
    descEn: 'Under 10 cm, tire contact patch, safe for all vehicles',
    color: '#eab308',
  },
  {
    key: '4',
    waterline: 'TIRE_HALF_SUBMERGED',
    anchor: 'exhaust_hub_over_15cm',
    floodLevel: 'deep',
    depthCm: 18,
    labelVi: '4: Nửa bánh / Ống xả',
    labelEn: '4: Half Wheel (15-25cm)',
    descVi: 'Ngập 15-25 cm, ngang ống xả / trục xe máy, xe ga có nguy cơ chết máy',
    descEn: '15-25 cm, exhaust pipe / wheel hub, scooter stall risk',
    color: '#f97316',
  },
  {
    key: '5',
    waterline: 'IMPASSABLE',
    anchor: 'wheel_submerged_over_30cm',
    floodLevel: 'deep',
    depthCm: 35,
    labelVi: '5: Lút bánh / Chết máy',
    labelEn: '5: Impassable (>30cm)',
    descVi: 'Ngập trên 30 cm, lút bánh xe, chết máy hàng loạt, bất khả thông hành',
    descEn: 'Over 30 cm, wheel fully submerged, widespread vehicle stall, impassable',
    color: '#ef4444',
  },
] as const;

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
  waterline_depth?: WaterlineDepth;
  submersion_depth_cm?: number;
  quality_tags?: QualityTag[];
  weather?: Weather;
  time_of_day?: TimeOfDay;
  notes?: string;
  annotations?: PolygonAnnotation[];
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
}

export type LabelFilterStatus = 'all' | 'pending_auto' | 'approved_human';

// ---------------------------------------------------------------------------
// Segmentation polygon contract (ADR-011 D1 / Issue #297)
// Mirrors services/go-server internal/archiver LabelPayload validation and
// tools/eval/export_yolo_seg.py parsing: normalized [0, 1] coordinates,
// canonical 6-class taxonomy, 3-64 vertices, max 32 annotations per label.
// ---------------------------------------------------------------------------

export type PolygonClass =
  | 'roadway'
  | 'water_puddle'
  | 'motorcycle'
  | 'car'
  | 'bus'
  | 'truck';

export const POLYGON_CLASSES: readonly PolygonClass[] = [
  'roadway',
  'water_puddle',
  'motorcycle',
  'car',
  'bus',
  'truck',
] as const;

export const POLYGON_CLASS_ALIASES: Record<string, PolygonClass> = {
  road: 'roadway',
  asphalt: 'roadway',
  carriageway: 'roadway',
  water: 'water_puddle',
  puddle: 'water_puddle',
  flood: 'water_puddle',
  flooded: 'water_puddle',
  motorbike: 'motorcycle',
  bike: 'motorcycle',
  mc: 'motorcycle',
  automobile: 'car',
  passenger_car: 'car',
  van: 'car',
  suv: 'car',
  coach: 'bus',
  container: 'truck',
  lorry: 'truck',
};

export const MIN_POLYGON_POINTS = 3;
export const MAX_POLYGON_POINTS = 64;
export const MAX_ANNOTATIONS_PER_LABEL = 32;
export const POLYGON_PROVENANCE_SAM = 'sam_prelabel';

export type PolygonPoint = [number, number];
export type PolygonDirection = 'a' | 'b';

export interface PolygonAnnotation {
  class: PolygonClass;
  polygon: PolygonPoint[];
  direction?: PolygonDirection;
  source?: string;
}

/** Resolves an arbitrary class string to the canonical taxonomy, or null. */
export function normalizePolygonClass(raw: string): PolygonClass | null {
  const norm = raw.trim().toLowerCase();
  if ((POLYGON_CLASSES as readonly string[]).includes(norm)) {
    return norm as PolygonClass;
  }
  return POLYGON_CLASS_ALIASES[norm] ?? null;
}

/**
 * Converts pixel-space points to normalized [0, 1] coordinates against the
 * image natural size, clamping and rounding to 6 decimals. The backend rejects
 * pixel coordinates, so normalization MUST happen client-side before saving.
 */
export function normalizePixelPoints(
  points: Array<{ x: number; y: number }>,
  naturalWidth: number,
  naturalHeight: number
): PolygonPoint[] {
  const w = naturalWidth > 0 ? naturalWidth : 1;
  const h = naturalHeight > 0 ? naturalHeight : 1;
  return points.map(({ x, y }) => [
    round6(clamp01(x / w)),
    round6(clamp01(y / h)),
  ]);
}

/**
 * Client-side mirror of the backend LabelPayload annotation validation.
 * Returns an error message string, or null when the annotations are valid.
 */
export function validatePolygonAnnotations(anns: PolygonAnnotation[] | undefined | null): string | null {
  if (!anns || anns.length === 0) return null;
  if (anns.length > MAX_ANNOTATIONS_PER_LABEL) {
    return `too many polygon annotations: ${anns.length} (max ${MAX_ANNOTATIONS_PER_LABEL})`;
  }
  for (let i = 0; i < anns.length; i++) {
    const ann = anns[i];
    if (!POLYGON_CLASSES.includes(ann.class)) {
      return `annotation[${i}]: unknown class "${ann.class}"`;
    }
    if (ann.direction !== undefined && ann.direction !== null && (ann.direction as string) !== '') {
      if (ann.class !== 'roadway') {
        return `annotation[${i}] (${ann.class}): direction may only be set when class is roadway, got "${ann.direction}"`;
      }
      if (ann.direction !== 'a' && ann.direction !== 'b') {
        return `annotation[${i}] (${ann.class}): unknown direction "${ann.direction}" (must be "a" or "b")`;
      }
    }
    if (ann.polygon.length < MIN_POLYGON_POINTS || ann.polygon.length > MAX_POLYGON_POINTS) {
      return `annotation[${i}] (${ann.class}): polygon must have ${MIN_POLYGON_POINTS}-${MAX_POLYGON_POINTS} vertices`;
    }
    for (let j = 0; j < ann.polygon.length; j++) {
      const [x, y] = ann.polygon[j];
      if (x < 0 || x > 1 || y < 0 || y > 1) {
        return `annotation[${i}] (${ann.class}): vertex ${j} outside normalized range [0, 1]`;
      }
    }
    if (ann.source !== undefined && ann.source !== null && ann.source.length > 32) {
      return `annotation[${i}]: source exceeds 32 characters`;
    }
  }
  return null;
}

/** True when any polygon on the label was machine-proposed (SAM pre-label). */
export function hasAutoPolygons(anns: PolygonAnnotation[] | undefined | null): boolean {
  return !!anns && anns.some((a) => a.source === POLYGON_PROVENANCE_SAM);
}

function clamp01(v: number): number {
  return Math.max(0, Math.min(1, v));
}

function round6(v: number): number {
  return Math.round(v * 1e6) / 1e6;
}

export interface HotkeyResult {
  state: LabelState;
  action?: 'prev' | 'next' | 'save' | 'approve' | 'auto_advance';
}

export interface HotkeyMeta {
  shiftKey?: boolean;
  rapidMode?: boolean;
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

  // 1-5: Rapid Wheel Submersion Depth Classification (Issue #513)
  const submersionMatch = SUBMERSION_LEVELS.find((l) => l.key === key);
  if (submersionMatch) {
    next.flood_level = submersionMatch.floodLevel;
    next.flood_submersion_anchor = submersionMatch.anchor;
    next.waterline_depth = submersionMatch.waterline;
    next.submersion_depth_cm = submersionMatch.depthCm;
    if (meta?.rapidMode) {
      return { state: next, action: 'auto_advance' };
    }
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
    annotator.startsWith('auto_') ||
    annotator.endsWith('_auto') ||
    annotator.includes('/auto') ||
    annotator.includes('opencode') ||
    annotator === 'auto_harvester' ||
    annotator === 'auto'
  );
}

/**
 * Synthesizes a SnapshotItem from a LabelPayload if the raw snapshot is not present in the current page.
 */
export function snapshotFromLabel(
  label: LabelPayload,
  existingSnapshot?: SnapshotItem,
  cameraNameLookup?: (id: string) => string | undefined
): SnapshotItem {
  if (existingSnapshot) {
    return existingSnapshot;
  }
  const key = label.snapshot_key;
  const parts = key.split('/');
  const camID = label.camera_id || (parts.length >= 2 ? parts[1] : '');
  const camName = (cameraNameLookup ? cameraNameLookup(camID) : undefined) || camID;

  return {
    key,
    camera_id: camID,
    camera_name: camName,
    timestamp: label.timestamp || 0,
    reason: 'harvest',
    size: 0,
    raw_url: `/api/archiver/snapshots/raw?key=${encodeURIComponent(key)}`,
  };
}

/**
 * Filters snapshot queue by provenance: all, pending_auto, or approved_human.
 * When filtering for candidates (pending_auto or approved_human), it merges items
 * from both the loaded snapshots and the full labels catalog so candidates are never dropped.
 */
export function filterSnapshots(
  snapshots: SnapshotItem[],
  labels: Record<string, LabelPayload>,
  filter: LabelFilterStatus,
  cameraNameLookup?: (id: string) => string | undefined
): SnapshotItem[] {
  if (filter === 'all') return snapshots;

  const snapMap = new Map<string, SnapshotItem>();
  snapshots.forEach((s) => snapMap.set(s.key, s));

  if (filter === 'pending_auto') {
    const seen = new Set<string>();
    const list: SnapshotItem[] = [];
    snapshots.forEach((s) => {
      const l = labels[s.key];
      if (l && isAutoHarvested(l)) {
        list.push(s);
        seen.add(s.key);
      }
    });

    const extra: SnapshotItem[] = [];
    Object.values(labels).forEach((l) => {
      if (l.snapshot_key && isAutoHarvested(l) && !seen.has(l.snapshot_key)) {
        extra.push(snapshotFromLabel(l, snapMap.get(l.snapshot_key), cameraNameLookup));
        seen.add(l.snapshot_key);
      }
    });
    extra.sort((a, b) => b.timestamp - a.timestamp);
    return [...list, ...extra];
  }

  if (filter === 'approved_human') {
    const seen = new Set<string>();
    const list: SnapshotItem[] = [];
    snapshots.forEach((s) => {
      const l = labels[s.key];
      if (l && !isAutoHarvested(l)) {
        list.push(s);
        seen.add(s.key);
      }
    });

    const extra: SnapshotItem[] = [];
    Object.values(labels).forEach((l) => {
      if (l.snapshot_key && !isAutoHarvested(l) && !seen.has(l.snapshot_key)) {
        extra.push(snapshotFromLabel(l, snapMap.get(l.snapshot_key), cameraNameLookup));
        seen.add(l.snapshot_key);
      }
    });
    extra.sort((a, b) => b.timestamp - a.timestamp);
    return [...list, ...extra];
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
    segModeToggle: 'Chế độ phân vùng',
    segModeHint: 'Nhấp để thêm đỉnh, nhấp đỉnh đầu hoặc Enter để đóng polygon, Esc để hủy',
    segClassLabel: 'Lớp phân vùng',
    segDirectionLabel: 'Hướng đường',
    segDirectionNone: 'Không phân hướng',
    segDirectionA: 'Chiều A (Làn gần)',
    segDirectionB: 'Chiều B (Làn xa)',
    segDirectionBadgeA: 'Chiều A',
    segDirectionBadgeB: 'Chiều B',
    segPolygonsLabel: 'Polygon đã vẽ',
    segEmpty: 'Chưa có polygon nào',
    segAutoBadge: '[Tự động: SAM]',
    segDeleteBtn: 'Xóa',
    segValidationError: 'Polygon không hợp lệ',
    loading: 'Đang tải danh sách ảnh...',
    floodLabel: 'Mức độ ngập & Trục neo bánh xe (Phím 1-5)',
    rapidSubmersionTitle: 'Phân loại ngập nước siêu tốc (Phím 1-5)',
    rapidModeToggle: 'Chế độ siêu tốc (tự chuyển ảnh)',
    rapidModeOn: 'BẬT (1-5 tự lưu & chuyển ảnh)',
    rapidModeOff: 'TẮT (Chỉ chọn mức ngập)',
    rapidSessionCount: 'Đã phân loại',
    rapidThroughput: 'Tốc độ',
    rapidFramesPerMin: 'ảnh/phút',
    rapidFramesPerHour: 'ảnh/giờ',
    floodNone: '1: Khô ráo (0cm)',
    floodPuddle: '3: Vũng nước / Mép vành (<10cm)',
    floodDeep: '4: Nửa bánh / Ống xả (15-25cm)',
    floodSubmersionLabel: 'Mỏ neo ngập vật lý (Phím 1-5)',
    anchorDry: '1: Khô ráo (0cm)',
    anchorWetNoStanding: '2: Ướt phản chiếu (0cm)',
    anchorTireBase: '3: Mép vành bánh (<10cm)',
    anchorExhaustHub: '4: Nửa bánh / Ống bô (15-25cm)',
    anchorWheelSubmerged: '5: Lút bánh / Chết máy (>30cm)',
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
    prevPage: 'Trang trước',
    nextPage: 'Trang kế',
    pageDisplay: 'Trang',
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
    segModeToggle: 'Segmentation mode',
    segModeHint: 'Click to add vertices, click the first vertex or press Enter to close, Esc to cancel',
    segClassLabel: 'Segmentation class',
    segDirectionLabel: 'Roadway direction',
    segDirectionNone: 'None',
    segDirectionA: 'Direction A (near lane)',
    segDirectionB: 'Direction B (far lane)',
    segDirectionBadgeA: 'Dir A',
    segDirectionBadgeB: 'Dir B',
    segPolygonsLabel: 'Drawn polygons',
    segEmpty: 'No polygons yet',
    segAutoBadge: '[Auto: SAM]',
    segDeleteBtn: 'Delete',
    segValidationError: 'Invalid polygon',
    loading: 'Loading snapshot catalog...',
    floodLabel: 'Flood Level & Wheel Submersion (Keys 1-5)',
    rapidSubmersionTitle: 'Rapid Submersion Classification (Keys 1-5)',
    rapidModeToggle: 'Rapid Mode (auto-advance)',
    rapidModeOn: 'ON (1-5 auto-saves & advances)',
    rapidModeOff: 'OFF (Select only)',
    rapidSessionCount: 'Classified',
    rapidThroughput: 'Throughput',
    rapidFramesPerMin: 'frames/min',
    rapidFramesPerHour: 'frames/hr',
    floodNone: '1: Dry (0cm)',
    floodPuddle: '3: Tire Base (<10cm)',
    floodDeep: '4: Half Wheel / Exhaust (15-25cm)',
    floodSubmersionLabel: 'Physical Flood Depth Anchor (Keys 1-5)',
    anchorDry: '1: Dry asphalt (0cm)',
    anchorWetNoStanding: '2: Wet sheen / surface (0cm)',
    anchorTireBase: '3: Tire contact patch (<10cm)',
    anchorExhaustHub: '4: Half wheel / Exhaust (15-25cm)',
    anchorWheelSubmerged: '5: Wheel submerged / Stall (>30cm)',
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
    prevPage: 'Prev Page',
    nextPage: 'Next Page',
    pageDisplay: 'Page',
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
