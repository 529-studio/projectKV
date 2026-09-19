// =======================================================
// GLOBAL STATE & VARIABLES
// =======================================================
let map = null;
let currentTileLayer = null;
let allCameras = [];
let dotMarkersLayer = null;
let activeMarkerLayer = null; // Layer chứa các camera đang theo dõi (Dot to, bring to front)
let mapMarkerRefs = new Map(); // Lưu tham chiếu marker trên bản đồ để tương tác đồng bộ
let pinnedCameras = []; // Danh sách các camera đang được ghim theo dõi trong bottom dock
let selectedCameraId = null;

let autoRefreshInterval = null;
let refreshCountdown = 10;
let currentModalCamera = null;

// Khóa chặt khung nhìn trong đất liền Đông Nam Bộ (An toàn lãnh thổ, không tranh chấp biển đảo)
const CAMERA_VIEW_MAP_BOUNDS = [
  [10.15, 106.35],
  [11.35, 107.45]
];

// Tự động định tuyến API: nếu chạy frontend độc lập trên port 3001 thì gọi về backend port 3000
const API_BASE = window.location.port === '3001' ? 'http://localhost:3000' : '';



// Tính toán bán kính chấm marker theo độ zoom bản đồ có giới hạn (Min 3.5px - Max 12px)
function getMarkerRadiusForZoom(zoom, isFew = false) {
  let r = 3.5;
  if (zoom <= 10) r = 3.5;
  else if (zoom === 11) r = 4.5;
  else if (zoom === 12) r = 6;
  else if (zoom === 13) r = 7.5;
  else if (zoom === 14) r = 9;
  else if (zoom === 15) r = 10.5;
  else r = 12; // zoom >= 16

  if (isFew && zoom <= 13) r += 2;
  return Math.min(Math.max(r, 3.5), 12);
}

// Cập nhật kích thước toàn bộ marker khi zoom thay đổi
function updateMarkerScales() {
  if (!map) return;
  const zoom = map.getZoom();
  const subset = getFilteredSubset();
  const isFew = subset.length <= 40;
  const radius = getMarkerRadiusForZoom(zoom, isFew);
  const weight = zoom >= 14 ? 2 : 1.5;

  mapMarkerRefs.forEach(marker => {
    marker.setRadius(radius);
    marker.setStyle({ weight: weight });
  });

  const zoomInd = document.getElementById('legend-zoom-indicator');
  if (zoomInd) {
    zoomInd.textContent = `${zoom}x (${Math.round(radius)}px)`;
  }

  const pinSize = Math.min(Math.max(18 + (zoom - 11) * 2.5, 18), 30);
  document.querySelectorAll('.selected-dot-marker').forEach(el => {
    el.style.width = `${pinSize}px`;
    el.style.height = `${pinSize}px`;
    el.style.fontSize = `${Math.round(pinSize * 0.45)}px`;
  });

  // Đồng bộ kích thước hình học mẫu trong bảng Chú giải (Legend)
  const sampleDot = document.querySelector('.symbol-dot');
  if (sampleDot) {
    const dotD = Math.min(Math.max(Math.round(radius * 2), 8), 18);
    sampleDot.style.width = `${dotD}px`;
    sampleDot.style.height = `${dotD}px`;
  }
  const sampleOffline = document.querySelector('.symbol-dot-offline');
  if (sampleOffline) {
    const dotD = Math.min(Math.max(Math.round(radius * 2), 8), 18);
    sampleOffline.style.width = `${dotD}px`;
    sampleOffline.style.height = `${dotD}px`;
  }
  const sampleSelected = document.querySelector('.symbol-selected-dot');
  if (sampleSelected) {
    const selD = Math.min(Math.max(Math.round(pinSize * 0.85), 18), 24);
    sampleSelected.style.width = `${selD}px`;
    sampleSelected.style.height = `${selD}px`;
    sampleSelected.style.fontSize = `${Math.round(selD * 0.42)}px`;
  }
}

// =======================================================
// TRẠNG THÁI BỘ LỌC LIÊN HOÀN (DYNAMIC CHAINED DEEP FILTER)
// =======================================================
// Mỗi phần tử: { id: number, prop: 'zone' | 'road' | 'node' | 'status', val: string }
let activeFilters = [];
let currentSearchQuery = '';
let filterIdCounter = 1;

// =======================================================
// TIỆN ÍCH BỎ DẤU TIẾNG VIỆT (TÌM KIẾM KHÔNG PHÂN BIỆT DẤU)
// =======================================================
function removeVietnameseTones(str) {
  if (!str) return '';
  str = str.toLowerCase();
  str = str.normalize('NFD').replace(/[\u0300-\u036f]/g, '');
  str = str.replace(/[đĐ]/g, 'd');
  return str;
}

function cleanStreet(str) {
  if (!str) return '';
  let s = str.trim();
  // Loại bỏ số thứ tự camera trong ngoặc như (20-1), (1), (2), (phía cảng)...
  s = s.replace(/\s*\([\d\-A-Za-z\s]+\)\s*$/, '').trim();
  // Chỉ gỡ các tiền tố chung chỉ hình thái nút giao (giữ nguyên Cầu, Cổng Cảng, Hầm...)
  s = s.replace(/^(giao lộ|nút giao|ngã tư|ngã ba|ngã 4)\s+/i, '').trim();
  s = s.replace(/[\(\)]+/g, '').trim();
  return s;
}

// =======================================================
// PHÂN LOẠI METADATA CHO CAMERA (KHU VỰC, TRỤC ĐƯỜNG, NÚT GIAO)
// =======================================================
function getCameraZone(cam) {
  const lat = cam.latitude;
  const lon = cam.longitude;
  const name = (cam.displayName || cam.title || '').trim();
  const lower = name.toLowerCase();

  // 1. Ngoại tỉnh: Bà Rịa - Vũng Tàu (dọc QL51)
  if (lon && lon > 107.0) return 'Bà Rịa - Vũng Tàu';

  // 2. Bình Dương (Dĩ An, Thuận An, Thủ Dầu Một, Mỹ Phước - Tân Vạn, ĐT743...)
  if (lower.includes('mỹ phước') || lower.includes('tân vạn') || lower.includes('dĩ an') || lower.includes('thuận an') || lower.includes('dt743') || lower.includes('đại lộ bình dương') || (lat && lat > 10.92 && lon && lon > 106.65)) {
    return 'Bình Dương';
  }

  // 3. Cụm Cảng Cát Lái - Mỹ Thủy (Khóa chặt bằng tọa độ chuẩn, loại bỏ hoàn toàn string collision)
  const inCatLaiBox = (lat && lon) ? (lat >= 10.748 && lat <= 10.785 && lon >= 106.745 && lon <= 106.815) : false;
  const isCatLaiName = lower.includes('cát lái') || lower.includes('mỹ thủy') || lower.includes('lê phụng hiểu') ||
    (lower.includes('nguyễn thị định') && !lower.includes('thân văn nhiếp')) ||
    (lower.includes('đồng văn cống') && (lower.includes('đường c') || lower.includes('nguyễn thị định') || lower.includes('phan văn đáng') || lower.includes('phía cảng'))) ||
    /\bđường [abcdgk]\b/i.test(lower);

  if (inCatLaiBox && isCatLaiName) {
    return 'Cụm Cảng Cát Lái';
  }

  // 4. Khu vực Nam Sài Gòn - Quận 7 - Nhà Bè (Nguyễn Văn Linh, Huỳnh Tấn Phát, Cầu Phú Mỹ bờ Q7, Nguyễn Hữu Thọ)
  if ((lat && lat < 10.755 && lon && lon >= 106.68 && lon < 106.75) || lower.includes('huỳnh tấn phát') || lower.includes('nguyễn văn linh') || lower.includes('nhà bè') || lower.includes('nguyễn hữu thọ')) {
    return 'Quận 7 - Nhà Bè';
  }

  // 5. TP. Thủ Đức (Toàn bộ phía Đông sông Sài Gòn: Khu CNC, Xa Lộ Hà Nội, Mai Chí Thọ, Võ Nguyên Giáp, Đỗ Xuân Hợp...)
  if (lon && lon >= 106.725 && lat >= 10.74) {
    return 'TP. Thủ Đức';
  }

  // 6. Khu vực Tây Nam - Bình Tân - Bình Chánh (Cửa ngõ miền Tây: QL1, Võ Trần Chí, Kinh Dương Vương, An Lạc)
  if ((lon && lon < 106.62) || lower.includes('bình chánh') || lower.includes('bình tân') || lower.includes('tân kiên') || lower.includes('an lạc') || lower.includes('võ trần chí') || lower.includes('kinh dương vương')) {
    return 'Bình Tân - Bình Chánh';
  }

  // 7. Khu vực Tây Bắc - Quận 12 - Hóc Môn - Củ Chi (Cửa ngõ Tây Ninh / QL22 / QL1 đoạn An Sương, Ngã Tư Ga)
  if ((lat && lat >= 10.835 && lon && lon < 106.72) || lower.includes('hóc môn') || lower.includes('củ chi') || lower.includes('quốc lộ 22') || lower.includes('ngã 4 ga') || lower.includes('an sương')) {
    return 'Q.12 - Hóc Môn - Củ Chi';
  }

  // 8. Khu vực Nội thành TP.HCM (Quận 1, 3, 5, 10, Bình Thạnh, Tân Bình, Phú Nhuận...)
  return 'Khu vực Nội thành TP.HCM';
}

function getCameraRoads(cam) {
  const raw = (cam.displayName || cam.title || '').trim();
  const clean = raw.replace(/\s*\([\d\-A-Za-z\s]+\)\s*$/, '').trim();
  const parts = clean.split(/\s*[\-–—]\s*/);
  const roads = new Set();

  if (parts.length > 0) {
    const mainRoad = cleanStreet(parts[0]);
    if (mainRoad.length >= 3 && !/^\d+$/.test(mainRoad) && !/^(tth|idico|khu\d*)$/i.test(mainRoad)) {
      roads.add(mainRoad);
    }
  }

  if (parts.length > 1) {
    const crossRoad = cleanStreet(parts[1]);
    if (crossRoad.length >= 3 && !/^\d+$/.test(crossRoad) && !/^(đoạn|khu|hướng|tth|idico)$/i.test(crossRoad)) {
      roads.add(crossRoad);
    }
  }

  const lower = raw.toLowerCase();
  // Sử dụng word boundary và lookahead để không nhầm QL1 với QL13, QL14
  if (/(quốc lộ 1\b|ql\s*1\b)(?!\d)/i.test(lower)) roads.add('Quốc Lộ 1');
  if (/(quốc lộ 13\b|ql\s*13\b)/i.test(lower)) roads.add('Quốc Lộ 13');
  if (/(quốc lộ 22\b|ql\s*22\b)/i.test(lower)) roads.add('Quốc Lộ 22');
  if (/(quốc lộ 50\b|ql\s*50\b)/i.test(lower)) roads.add('Quốc Lộ 50');
  if (/(quốc lộ 51\b|ql\s*51\b)/i.test(lower)) roads.add('Quốc Lộ 51');
  if (lower.includes('xa lộ hà nội') || lower.includes('võ nguyên giáp')) roads.add('Xa Lộ Hà Nội - Võ Nguyên Giáp');
  if (lower.includes('mỹ phước') || lower.includes('tân vạn')) roads.add('Mỹ Phước - Tân Vạn');
  if (lower.includes('mai chí thọ')) roads.add('Mai Chí Thọ');
  if (lower.includes('đồng văn cống')) roads.add('Đồng Văn Cống');
  if (lower.includes('nguyễn thị định')) roads.add('Nguyễn Thị Định');
  if (lower.includes('võ chí công')) roads.add('Võ Chí Công');
  if (lower.includes('nguyễn văn linh')) roads.add('Nguyễn Văn Linh');
  if (lower.includes('võ văn kiệt')) roads.add('Võ Văn Kiệt');
  if (lower.includes('phạm văn đồng')) roads.add('Phạm Văn Đồng');

  return [...roads];
}

function getCameraNodes(cam) {
  const raw = (cam.displayName || cam.title || '').trim();
  const clean = raw.replace(/\s*\([\d\-A-Za-z\s]+\)\s*$/, '').trim();
  const parts = clean.split(/\s*[\-–—]\s*/);
  const nodes = [];

  parts.forEach((p, idx) => {
    const s = cleanStreet(p);
    if (s.length >= 2 && !/^\d+$/.test(s)) {
      nodes.push(s);
    }
  });

  return nodes;
}

// =======================================================
// KHỞI TẠO ỨNG DỤNG
// =======================================================
document.addEventListener('DOMContentLoaded', async () => {
  // Mặc định thu gọn sidebar trên màn hình hẹp (< 768px) để ưu tiên bản đồ
  if (window.innerWidth <= 768) {
    const sidebar = document.getElementById('camera-sidebar');
    if (sidebar) sidebar.classList.add('collapsed');
  }
  initMap();
  setupEventListeners();
  await loadCameras();
  startRefreshCycle();
});

function initMap() {
  map = L.map('map', {
    center: [10.82, 106.75],
    zoom: 11,
    minZoom: 9,
    maxZoom: 18,
    maxBounds: CAMERA_VIEW_MAP_BOUNDS,
    maxBoundsViscosity: 1.0
  });

  // Khởi tạo lớp bản đồ chuẩn OpenStreetMap (Màu sắc tự nhiên, rõ ràng, không watermark)
  currentTileLayer = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright" target="_blank">OpenStreetMap</a>',
    maxZoom: 19
  }).addTo(map);

  dotMarkersLayer = L.layerGroup().addTo(map);
  activeMarkerLayer = L.layerGroup().addTo(map);

  // Tự động co giãn kích thước chấm marker và dot to theo độ phóng to (zoom) của bản đồ
  map.on('zoomend', () => {
    updateMarkerScales();
  });
}

function setupEventListeners() {
  // Bật / tắt Sidebar
  document.getElementById('btn-toggle-sidebar').addEventListener('click', toggleSidebar);
  document.getElementById('btn-close-sidebar').addEventListener('click', toggleSidebar);

  // Đóng Sidebar khi nhấp vào lớp phủ mờ trên mobile
  const backdrop = document.getElementById('sidebar-backdrop');
  if (backdrop) {
    backdrop.addEventListener('click', toggleSidebar);
  }

  // Thu gọn / Mở rộng Chú giải bản đồ
  const btnToggleLegend = document.getElementById('btn-toggle-legend');
  if (btnToggleLegend) {
    btnToggleLegend.addEventListener('click', () => {
      const body = document.getElementById('legend-body');
      if (body) {
        body.classList.toggle('hidden');
        btnToggleLegend.textContent = body.classList.contains('hidden') ? '[+]' : '[−]';
      }
    });
  }

  // Tìm kiếm nhanh trong Sidebar
  document.getElementById('camera-search-input').addEventListener('input', (e) => {
    currentSearchQuery = e.target.value.trim();
    applyChainedFilterEngine();
  });

  // Nút Thêm Điều Kiện Lọc
  document.getElementById('btn-add-filter').addEventListener('click', () => {
    addNewFilterCondition();
  });

  // Nút Xóa Toàn Bộ Điều Kiện Lọc
  document.getElementById('btn-clear-all-filters').addEventListener('click', () => {
    clearAllFilterConditions();
  });

  // Nút xóa toàn bộ danh sách theo dõi
  document.getElementById('btn-clear-pinned').addEventListener('click', clearAllPinned);

  // Nút thu gọn / mở rộng Dock dưới đáy
  const btnToggleDrawer = document.getElementById('btn-toggle-drawer');
  if (btnToggleDrawer) {
    btnToggleDrawer.addEventListener('click', () => {
      const drawer = document.getElementById('route-cameras-drawer');
      if (drawer) {
        drawer.classList.toggle('collapsed');
        btnToggleDrawer.textContent = drawer.classList.contains('collapsed') ? '[▲ MỞ DOCK]' : '[▼ THU GỌN]';
      }
    });
  }

  // Đóng Modal chi tiết camera
  document.getElementById('btn-close-modal').addEventListener('click', closeModal);
  document.getElementById('camera-modal').addEventListener('click', (e) => {
    if (e.target.id === 'camera-modal') closeModal();
  });

  // Ghim / Bỏ ghim camera từ trong modal
  document.getElementById('btn-pin-modal-cam').addEventListener('click', () => {
    if (currentModalCamera) {
      const isPinned = pinnedCameras.some(c => c.id === currentModalCamera.id);
      if (isPinned) {
        unpinCamera(currentModalCamera.id);
        showToast(`[OK] Đã bỏ ghim "${currentModalCamera.displayName || currentModalCamera.title}".`);
      } else {
        pinCamera(currentModalCamera);
        showToast(`[OK] Đã ghim "${currentModalCamera.displayName || currentModalCamera.title}" vào danh sách theo dõi.`);
      }
      openCameraModal(currentModalCamera);
    }
  });
}

function toggleSidebar() {
  const sidebar = document.getElementById('camera-sidebar');
  const backdrop = document.getElementById('sidebar-backdrop');
  sidebar.classList.toggle('collapsed');

  if (backdrop) {
    if (sidebar.classList.contains('collapsed')) {
      backdrop.classList.add('hidden');
    } else {
      if (window.innerWidth < 768) {
        backdrop.classList.remove('hidden');
      }
    }
  }
  setTimeout(() => map && map.invalidateSize(), 300);
}

// =======================================================
// 1. TẢI DỮ LIỆU CAMERA & BỔ SUNG METADATA
// =======================================================
async function loadCameras() {
  try {
    const res = await fetch(`${API_BASE}/api/cameras`);
    const data = await res.json();
    const rawCams = data.cameras || [];

    // Bổ sung metadata phân loại cho 837 camera
    allCameras = rawCams.map(c => ({
      ...c,
      zone: getCameraZone(c),
      roads: getCameraRoads(c),
      nodes: getCameraNodes(c)
    }));

    document.getElementById('sidebar-total-cams').textContent = `[${allCameras.length}]`;

    // Khởi chạy Bộ Lọc Phân Cấp Động và vẽ marker camera theo display list
    renderFilterBuilderRows();
    applyChainedFilterEngine();
    renderPinnedCameraTrack();
  } catch (err) {
    console.error('Lỗi nạp dữ liệu camera:', err);
  }
}

// =======================================================
// 2. THUẬT TOÁN BỘ LỌC ĐIỀU KIỆN LIÊN HOÀN (CHAINED FILTER ENGINE)
// =======================================================

// Lấy tập camera vượt qua các filter từ 0 đến index (nếu index không truyền thì tất cả)
function getFilteredSubset(upToIndex = activeFilters.length - 1) {
  let subset = allCameras;

  for (let i = 0; i <= upToIndex; i++) {
    const f = activeFilters[i];
    if (!f || !f.val) continue;

    subset = subset.filter(cam => {
      if (f.prop === 'zone') {
        return cam.zone === f.val;
      }
      if (f.prop === 'road') {
        return cam.roads.includes(f.val) || (cam.displayName || '').toLowerCase().includes(f.val.toLowerCase());
      }
      if (f.prop === 'node') {
        return (cam.displayName || '').toLowerCase().includes(f.val.toLowerCase());
      }
      if (f.prop === 'status') {
        return f.val === 'ONLINE' ? cam.isOnline : !cam.isOnline;
      }
      return true;
    });
  }

  // Kết hợp cùng ô tìm kiếm nếu có
  if (currentSearchQuery) {
    const cleanQ = removeVietnameseTones(currentSearchQuery);
    subset = subset.filter(cam => {
      const cleanName = removeVietnameseTones(cam.displayName || cam.title || '');
      const cleanId = (cam.id || '').toLowerCase();
      return cleanName.includes(cleanQ) || cleanId.includes(cleanQ);
    });
  }

  return subset;
}

// Tính toán các giá trị liên kết khả dụng dựa trên tập camera của các tầng trước
function getAvailableFacetsForFilter(filterIndex) {
  // Tập camera đã lọc từ các tầng 0 đến (filterIndex - 1)
  const prevSubset = filterIndex > 0 ? getFilteredSubset(filterIndex - 1) : allCameras;

  // 1. Danh sách Khu vực khả dụng (Sắp xếp theo số lượng camera giảm dần)
  const zoneCounts = new Map();
  prevSubset.forEach(c => {
    zoneCounts.set(c.zone, (zoneCounts.get(c.zone) || 0) + 1);
  });
  const zones = [...zoneCounts.entries()]
    .map(([val, count]) => ({ val, count, label: `${val} (${count})` }))
    .sort((a, b) => b.count - a.count);

  // 2. Danh sách Tuyến đường khả dụng (Sắp xếp theo số lượng camera giảm dần)
  const roadCounts = new Map();
  prevSubset.forEach(c => {
    c.roads.forEach(r => roadCounts.set(r, (roadCounts.get(r) || 0) + 1));
  });
  const roads = [...roadCounts.entries()]
    .map(([val, count]) => ({ val, count, label: `${val} (${count})` }))
    .sort((a, b) => b.count - a.count);

  // 3. Danh sách Giao lộ / Điểm cắt ngang khả dụng (Sắp xếp theo số lượng camera giảm dần)
  const nodeCounts = new Map();
  prevSubset.forEach(c => {
    c.nodes.forEach(n => {
      nodeCounts.set(n, (nodeCounts.get(n) || 0) + 1);
    });
  });
  const nodes = [...nodeCounts.entries()]
    .map(([val, count]) => ({ val, count, label: `${val} (${count})` }))
    .sort((a, b) => b.count - a.count);

  // 4. Trạng thái kết nối
  let onlineCount = 0;
  let offlineCount = 0;
  prevSubset.forEach(c => { if (c.isOnline) onlineCount++; else offlineCount++; });
  const statuses = [
    { val: 'ONLINE', label: `[HOẠT ĐỘNG] (${onlineCount})` },
    { val: 'OFFLINE', label: `[MẤT TÍN HIỆU] (${offlineCount})` }
  ];

  return { zones, roads, nodes, statuses, totalAvailable: prevSubset.length };
}

// Thêm một dòng điều kiện lọc mới
function addNewFilterCondition() {
  const newIndex = activeFilters.length;
  const facets = getAvailableFacetsForFilter(newIndex);

  // Thuật toán chọn thuộc tính thông minh dựa trên tầng trước
  let smartProp = 'zone';
  if (newIndex > 0) {
    const prevProp = activeFilters[newIndex - 1].prop;
    if (prevProp === 'zone') smartProp = 'road';
    else if (prevProp === 'road') smartProp = 'node';
    else if (prevProp === 'node') smartProp = 'status';
    else smartProp = 'road';
  }

  // Chọn giá trị mặc định đầu tiên của thuộc tính đó
  let initialVal = '';
  if (smartProp === 'zone' && facets.zones.length > 0) initialVal = facets.zones[0].val;
  else if (smartProp === 'road' && facets.roads.length > 0) initialVal = facets.roads[0].val;
  else if (smartProp === 'node' && facets.nodes.length > 0) initialVal = facets.nodes[0].val;
  else if (smartProp === 'status' && facets.statuses.length > 0) initialVal = facets.statuses[0].val;

  activeFilters.push({
    id: filterIdCounter++,
    prop: smartProp,
    val: initialVal
  });

  renderFilterBuilderRows();
  applyChainedFilterEngine(true);
}

// Xóa một dòng điều kiện lọc
function removeFilterCondition(filterId) {
  activeFilters = activeFilters.filter(f => f.id !== filterId);
  renderFilterBuilderRows();
  applyChainedFilterEngine(true);
}

// Xóa tất cả các điều kiện lọc
function clearAllFilterConditions() {
  activeFilters = [];
  renderFilterBuilderRows();
  applyChainedFilterEngine(true);
}

// Render các dòng điều kiện lọc liên hoàn trong Sidebar
function renderFilterBuilderRows() {
  const container = document.getElementById('filter-rows-container');
  const clearBtn = document.getElementById('btn-clear-all-filters');
  if (!container) return;

  container.innerHTML = '';

  if (activeFilters.length > 0) {
    clearBtn.classList.remove('hidden');
  } else {
    clearBtn.classList.add('hidden');
  }

  activeFilters.forEach((filter, index) => {
    const facets = getAvailableFacetsForFilter(index);

    const row = document.createElement('div');
    row.className = 'filter-row';

    // Số thứ tự tầng
    const idxEl = document.createElement('span');
    idxEl.className = 'filter-row-index';
    idxEl.textContent = `${index + 1}.`;
    row.appendChild(idxEl);

    // Dropdown chọn Thuộc tính (Mục lọc)
    const propSelect = document.createElement('select');
    propSelect.className = 'filter-prop-select';
    propSelect.innerHTML = `
      <option value="zone" ${filter.prop === 'zone' ? 'selected' : ''}>Khu vực</option>
      <option value="road" ${filter.prop === 'road' ? 'selected' : ''}>Tuyến đường</option>
      <option value="node" ${filter.prop === 'node' ? 'selected' : ''}>Giao lộ cắt</option>
      <option value="status" ${filter.prop === 'status' ? 'selected' : ''}>Trạng thái</option>
    `;
    row.appendChild(propSelect);

    // Dấu bằng
    const eq = document.createElement('span');
    eq.className = 'filter-eq-sign';
    eq.textContent = '=';
    row.appendChild(eq);

    // Dropdown chọn Giá trị (Chỉ chứa giá trị có thật trong tập camera hiện tại)
    const valSelect = document.createElement('select');
    valSelect.className = 'filter-val-select';

    function fillValuesForProp(prop) {
      valSelect.innerHTML = '';
      let items = [];
      if (prop === 'zone') items = facets.zones;
      else if (prop === 'road') items = facets.roads;
      else if (prop === 'node') items = facets.nodes;
      else if (prop === 'status') items = facets.statuses;

      if (items.length === 0) {
        valSelect.innerHTML = '<option value="">[Không có mục khả dụng]</option>';
        filter.val = '';
        return;
      }

      items.forEach(it => {
        const opt = document.createElement('option');
        opt.value = it.val;
        opt.textContent = it.label;
        if (it.val === filter.val) opt.selected = true;
        valSelect.appendChild(opt);
      });

      // Nếu giá trị hiện tại không khớp mục nào, chọn mục đầu tiên
      if (!items.some(it => it.val === filter.val)) {
        filter.val = items[0].val;
        valSelect.value = items[0].val;
      }
    }

    fillValuesForProp(filter.prop);
    valSelect.title = filter.val;
    row.appendChild(valSelect);

    // Nút Xóa điều kiện này
    const removeBtn = document.createElement('button');
    removeBtn.className = 'filter-remove-btn';
    removeBtn.textContent = 'x';
    removeBtn.title = 'Xóa điều kiện này';
    removeBtn.addEventListener('click', () => {
      removeFilterCondition(filter.id);
    });
    row.appendChild(removeBtn);

    // Sự kiện khi thay đổi Mục lọc
    propSelect.addEventListener('change', (e) => {
      filter.prop = e.target.value;
      fillValuesForProp(filter.prop);
      valSelect.title = filter.val;
      // Cập nhật lại các tầng lọc kế tiếp vì tập dữ liệu đã đổi
      renderFilterBuilderRows();
      applyChainedFilterEngine(true);
    });

    // Sự kiện khi thay đổi Giá trị lọc
    valSelect.addEventListener('change', (e) => {
      filter.val = e.target.value;
      valSelect.title = filter.val;
      // Cập nhật lại các tầng lọc kế tiếp
      renderFilterBuilderRows();
      applyChainedFilterEngine(true);
    });

    container.appendChild(row);
  });
}

// Áp dụng bộ lọc và cập nhật danh sách camera + bản đồ
function applyChainedFilterEngine(shouldFitMap = false) {
  const filteredCameras = getFilteredSubset();

  // Cập nhật badge hiển thị số lượng camera khớp
  const countBadge = document.getElementById('filter-match-count');
  if (countBadge) {
    countBadge.textContent = `[${filteredCameras.length} CAMERA]`;
    if (filteredCameras.length === 0) {
      countBadge.style.color = 'var(--status-danger)';
      countBadge.style.background = 'rgba(239, 68, 68, 0.1)';
      countBadge.style.borderColor = 'rgba(239, 68, 68, 0.3)';
    } else {
      countBadge.style.color = 'var(--status-clear)';
      countBadge.style.background = 'rgba(34, 197, 94, 0.1)';
      countBadge.style.borderColor = 'rgba(34, 197, 94, 0.3)';
    }
  }

  // Vẽ marker trên bản đồ CHỈ cho các camera nằm trong danh sách hiển thị
  renderMapMarkers(filteredCameras);

  // Render danh sách camera Cấp 2 trong sidebar
  renderCameraListItems(filteredCameras);

  // Căn chỉnh góc nhìn bản đồ theo tập camera kết quả
  if (shouldFitMap && filteredCameras.length > 0 && (activeFilters.length > 0 || currentSearchQuery)) {
    const validCoords = filteredCameras
      .map(c => [c.latitude, c.longitude])
      .filter(([lat, lon]) => lat && lon);

    if (validCoords.length > 0) {
      const bounds = L.latLngBounds(validCoords);
      map.fitBounds(bounds, { padding: [45, 45], maxZoom: 16 });
    }
  }
}

// Vẽ các chấm camera (circleMarker) chỉ cho các camera trong danh sách đang hiển thị
function renderMapMarkers(cameras) {
  if (!dotMarkersLayer) return;
  dotMarkersLayer.clearLayers();
  mapMarkerRefs.clear();

  const isFew = cameras.length <= 40;
  const zoom = map ? map.getZoom() : 11;
  const radius = getMarkerRadiusForZoom(zoom, isFew);
  const weight = zoom >= 14 ? 2 : 1.5;

  cameras.forEach(cam => {
    if (!cam.latitude || !cam.longitude) return;

    const fillColor = cam.isOnline ? '#ffffff' : '#52525b';
    const marker = L.circleMarker([cam.latitude, cam.longitude], {
      radius: radius,
      fillColor: fillColor,
      color: '#000000',
      weight: weight,
      opacity: 1,
      fillOpacity: 0.95
    });

    marker.bindTooltip(`<b>${cam.displayName || cam.title}</b><br><span style="color:#a1a1aa;">${cam.zone} | [Bấm xem ảnh & theo dõi]</span>`, {
      direction: 'top',
      offset: [0, -4]
    });

    marker.on('click', () => {
      selectCamera(cam);
    });

    marker.on('mouseover', () => {
      const curZoom = map ? map.getZoom() : 11;
      const curRadius = getMarkerRadiusForZoom(curZoom, isFew);
      marker.setStyle({ radius: curRadius + 4, fillColor: '#ffffff', color: '#ffffff', weight: 2.5 });
    });

    marker.on('mouseout', () => {
      const curZoom = map ? map.getZoom() : 11;
      const curRadius = getMarkerRadiusForZoom(curZoom, isFew);
      const curWeight = curZoom >= 14 ? 2 : 1.5;
      marker.setStyle({ radius: curRadius, fillColor: fillColor, color: '#000000', weight: curWeight });
    });

    dotMarkersLayer.addLayer(marker);
    mapMarkerRefs.set(cam.id, marker);
  });

  updateMarkerScales();
}

// Render danh sách camera trong Sidebar
function renderCameraListItems(cameras) {
  const container = document.getElementById('sidebar-cam-list');
  if (!container) return;
  container.innerHTML = '';

  if (cameras.length === 0) {
    container.innerHTML = '<div style="padding:16px; font-size:11px; color:#71717a; text-align:center;">[Không tìm thấy camera phù hợp điều kiện lọc]</div>';
    return;
  }

  const isFew = cameras.length <= 40;

  cameras.forEach(cam => {
    const item = document.createElement('div');
    const isPinned = pinnedCameras.some(c => c.id === cam.id);
    item.className = `cam-list-item ${selectedCameraId === cam.id ? 'selected' : ''}`;

    item.innerHTML = `
      <div class="cam-item-header">
        <div class="cam-item-name" title="${cam.displayName || cam.title}">${isPinned ? '[•] ' : ''}${cam.displayName || cam.title}</div>
        <span class="cam-item-tag ${cam.isOnline ? 'on' : 'off'}">${cam.isOnline ? '[HOẠT ĐỘNG]' : '[MẤT TÍN HIỆU]'}</span>
      </div>
      <div class="cam-item-meta">
        <span class="cam-zone-badge">[${cam.zone}]</span>
        <span class="cam-coords">Tọa độ: ${cam.latitude ? `${cam.latitude.toFixed(4)}, ${cam.longitude.toFixed(4)}` : 'Chưa có GPS'}</span>
      </div>
    `;

    // Hiệu ứng hover đồng bộ với marker trên bản đồ
    item.addEventListener('mouseenter', () => {
      const marker = mapMarkerRefs.get(cam.id);
      if (marker) {
        const curZoom = map ? map.getZoom() : 11;
        const curRadius = getMarkerRadiusForZoom(curZoom, isFew);
        marker.setStyle({ radius: curRadius + 4, fillColor: '#ffffff', color: '#ffffff', weight: 2.5 });
        marker.openTooltip();
      }
    });

    item.addEventListener('mouseleave', () => {
      const marker = mapMarkerRefs.get(cam.id);
      if (marker) {
        const curZoom = map ? map.getZoom() : 11;
        const curRadius = getMarkerRadiusForZoom(curZoom, isFew);
        const curWeight = curZoom >= 14 ? 2 : 1.5;
        marker.setStyle({
          radius: curRadius,
          fillColor: cam.isOnline ? '#ffffff' : '#52525b',
          color: '#000000',
          weight: curWeight
        });
        marker.closeTooltip();
      }
    });

    item.addEventListener('click', () => {
      selectCamera(cam);
    });

    container.appendChild(item);
  });
}

// =======================================================
// 3. CHỌN CAMERA: XEM ẢNH TRỰC TIẾP (KHÔNG TỰ ĐỘNG GHIM VÀO DOCK)
// =======================================================
function selectCamera(cam) {
  selectedCameraId = cam.id;

  // Cập nhật marker đã ghim
  renderActiveMarkers();

  // Highlight dòng được chọn trong sidebar
  document.querySelectorAll('.cam-list-item').forEach(el => el.classList.remove('selected'));
  const currentCard = Array.from(document.querySelectorAll('.cam-list-item')).find(el => {
    return el.querySelector('.cam-item-name')?.getAttribute('title') === (cam.displayName || cam.title);
  });
  if (currentCard) {
    currentCard.classList.add('selected');
  }

  // Di chuyển mượt mà khung nhìn đến camera
  if (cam.latitude && cam.longitude) {
    map.flyTo([cam.latitude, cam.longitude], 16, { duration: 0.8 });
  }

  // Tự động đóng sidebar trên mobile khi người dùng bấm chọn camera
  if (window.innerWidth < 768) {
    const sidebar = document.getElementById('camera-sidebar');
    const backdrop = document.getElementById('sidebar-backdrop');
    if (sidebar && !sidebar.classList.contains('collapsed')) {
      sidebar.classList.add('collapsed');
      if (backdrop) backdrop.classList.add('hidden');
    }
  }

  // Mở modal ảnh snapshot trực tiếp
  openCameraModal(cam);
}

function selectAndPinCamera(cam) {
  selectCamera(cam);
}

function pinCamera(cam) {
  if (!pinnedCameras.some(c => c.id === cam.id)) {
    pinnedCameras.unshift(cam);
    // Giữ tối đa 10 camera trong dock theo dõi
    if (pinnedCameras.length > 10) pinnedCameras.pop();
    renderPinnedCameraTrack();
    renderActiveMarkers();

    // Tự động mở Dock nếu đang thu gọn để người dùng nhìn thấy camera mới được thêm
    const drawer = document.getElementById('route-cameras-drawer');
    const btn = document.getElementById('btn-toggle-drawer');
    if (drawer && drawer.classList.contains('collapsed')) {
      drawer.classList.remove('collapsed');
      if (btn) btn.textContent = '[▼ THU GỌN]';
    }
  }
}

function unpinCamera(camId, e) {
  if (e) e.stopPropagation();
  pinnedCameras = pinnedCameras.filter(c => c.id !== camId);
  if (selectedCameraId === camId) {
    selectedCameraId = pinnedCameras.length > 0 ? pinnedCameras[0].id : null;
  }
  renderPinnedCameraTrack();
  renderActiveMarkers();

  // Nếu không còn camera nào, tự động thu gọn dock
  if (pinnedCameras.length === 0) {
    const drawer = document.getElementById('route-cameras-drawer');
    const btn = document.getElementById('btn-toggle-drawer');
    if (drawer && !drawer.classList.contains('collapsed')) {
      drawer.classList.add('collapsed');
      if (btn) btn.textContent = '[▲ MỞ DOCK]';
    }
  }
}

function clearAllPinned() {
  pinnedCameras = [];
  selectedCameraId = null;
  renderPinnedCameraTrack();
  renderActiveMarkers();
  showToast('[OK] Đã xóa toàn bộ danh sách theo dõi.');

  // Tự động thu gọn dock khi danh sách trống
  const drawer = document.getElementById('route-cameras-drawer');
  const btn = document.getElementById('btn-toggle-drawer');
  if (drawer && !drawer.classList.contains('collapsed')) {
    drawer.classList.add('collapsed');
    if (btn) btn.textContent = '[▲ MỞ DOCK]';
  }
}

// Vẽ các Dot to hơn cho các camera đang theo dõi (Bring to Front)
function renderActiveMarkers() {
  activeMarkerLayer.clearLayers();

  const zoom = map ? map.getZoom() : 11;
  const pinSize = Math.min(Math.max(18 + (zoom - 11) * 2.5, 18), 30);

  pinnedCameras.forEach((cam, idx) => {
    if (cam.latitude && cam.longitude) {
      const isCurrent = cam.id === selectedCameraId;
      const pinHtml = `<div class="selected-dot-marker" style="${isCurrent ? 'transform:scale(1.25); background:#ffffff;' : 'background:#e4e4e7;'}; width:${pinSize}px; height:${pinSize}px; font-size:${Math.round(pinSize * 0.45)}px;" title="${cam.displayName || cam.title}">#${idx + 1}</div>`;

      const pinIcon = L.divIcon({
        className: 'custom-pin-wrap',
        html: pinHtml,
        iconSize: [pinSize, pinSize],
        iconAnchor: [pinSize / 2, pinSize / 2]
      });

      const marker = L.marker([cam.latitude, cam.longitude], {
        icon: pinIcon,
        zIndexOffset: 3000 // Đưa lên trên tất cả các layer khác
      });

      marker.on('click', () => {
        selectCamera(cam);
      });

      marker.bindTooltip(`<b>#${idx + 1}: ${cam.displayName || cam.title}</b><br>[Bấm xem ảnh trực tiếp]`, {
        direction: 'top',
        offset: [0, -10]
      });

      activeMarkerLayer.addLayer(marker);
    }
  });
}

// =======================================================
// 4. BOTTOM DOCK: CAMERA ĐANG THEO DÕI
// =======================================================
function renderPinnedCameraTrack() {
  const track = document.getElementById('route-camera-track');
  track.innerHTML = '';

  document.getElementById('pinned-camera-count').textContent = `[${pinnedCameras.length} CAM]`;

  const clearBtn = document.getElementById('btn-clear-pinned');
  if (clearBtn) {
    if (pinnedCameras.length === 0) {
      clearBtn.classList.add('hidden');
    } else {
      clearBtn.classList.remove('hidden');
    }
  }

  if (pinnedCameras.length === 0) {
    track.innerHTML = '<div class="flex items-center justify-center w-full h-full py-4 px-2 text-center"><div class="text-[11px] font-mono text-zinc-500 border border-dashed border-zinc-800 rounded-sm p-3 w-full max-w-lg">[Chưa có camera nào trong danh sách theo dõi. Bấm vào camera bất kỳ trên bản đồ hoặc danh sách để theo dõi hình ảnh trực tiếp liên tục tại đây]</div></div>';
    return;
  }

  pinnedCameras.forEach((cam, index) => {
    const card = document.createElement('div');
    card.className = `camera-card ${cam.id === selectedCameraId ? 'selected' : ''}`;
    card.title = cam.displayName || cam.title;

    const imgUrl = `${API_BASE}/api/cameras/snapshot/${cam.id}?t=${Date.now()}`;

    card.innerHTML = `
      <div class="card-img-wrap">
        <span class="card-order-tag" style="background:#fff; color:#000; border:1px solid #000;">#${index + 1}</span>
        <button class="card-unpin-btn" title="Bỏ theo dõi camera này" id="unpin-${cam.id}">x</button>
        <img src="${imgUrl}" alt="${cam.displayName}" loading="lazy" id="thumb-${cam.id}" />
      </div>
      <div class="card-name">${cam.displayName || cam.title}</div>
    `;

    card.querySelector(`#unpin-${cam.id}`).addEventListener('click', (e) => {
      unpinCamera(cam.id, e);
    });

    card.addEventListener('click', () => {
      selectedCameraId = cam.id;
      renderActiveMarkers();
      renderPinnedCameraTrack();
      if (cam.latitude && cam.longitude) {
        map.flyTo([cam.latitude, cam.longitude], 16, { duration: 0.8 });
      }
      openCameraModal(cam);
    });

    track.appendChild(card);
  });
}

// Chu kỳ tự động làm mới ảnh mỗi 10 giây
function startRefreshCycle() {
  refreshCountdown = 10;
  clearInterval(autoRefreshInterval);

  autoRefreshInterval = setInterval(() => {
    refreshCountdown--;
    const el = document.getElementById('countdown-timer');
    if (el) el.textContent = `${refreshCountdown}s`;

    if (refreshCountdown <= 0) {
      refreshCountdown = 10;
      refreshVisibleSnapshots();
    }
  }, 1000);
}

function refreshVisibleSnapshots() {
  const now = Date.now();
  pinnedCameras.forEach(cam => {
    const img = document.getElementById(`thumb-${cam.id}`);
    if (img) img.src = `${API_BASE}/api/cameras/snapshot/${cam.id}?t=${now}`;
  });

  if (currentModalCamera) {
    const modalImg = document.getElementById('modal-camera-img');
    if (modalImg) {
      modalImg.src = `${API_BASE}/api/cameras/snapshot/${currentModalCamera.id}?t=${now}`;
      document.getElementById('modal-img-timestamp').textContent = new Date().toLocaleTimeString('vi-VN');
    }
  }
}



// =======================================================
// 6. MODAL XEM ẢNH CAMERA TRỰC TIẾP (THUẦN TÚY, KHÔNG AI)
// =======================================================
function openCameraModal(cam) {
  currentModalCamera = cam;
  const modal = document.getElementById('camera-modal');
  modal.classList.remove('hidden');

  document.getElementById('modal-camera-name').textContent = cam.displayName || cam.title;
  
  const statusTag = document.getElementById('modal-camera-status');
  statusTag.textContent = cam.isOnline ? '[HOẠT ĐỘNG]' : '[MẤT TÍN HIỆU]';
  statusTag.className = cam.isOnline ? 'online-tag' : 'online-tag offline';

  const img = document.getElementById('modal-camera-img');
  img.src = `${API_BASE}/api/cameras/snapshot/${cam.id}?t=${Date.now()}`;
  document.getElementById('modal-img-timestamp').textContent = new Date().toLocaleTimeString('vi-VN');

  document.getElementById('modal-camera-coords').textContent = `TỌA ĐỘ: ${cam.latitude?.toFixed(5)}, ${cam.longitude?.toFixed(5)} | ID: ${cam.id}`;

  // Cập nhật trạng thái nút ghim trong modal
  const pinBtn = document.getElementById('btn-pin-modal-cam');
  if (pinBtn) {
    const isPinned = pinnedCameras.some(c => c.id === cam.id);
    if (isPinned) {
      pinBtn.textContent = '[✓ ĐÃ GHIM THEO DÕI]';
      pinBtn.title = 'Camera này đang được theo dõi trong Dock. Bấm để bỏ ghim.';
      pinBtn.classList.add('pinned-active');
    } else {
      pinBtn.textContent = '[+ GHIM THEO DÕI]';
      pinBtn.title = 'Thêm camera này vào Dock theo dõi dưới đáy bản đồ';
      pinBtn.classList.remove('pinned-active');
    }
  }
}

function closeModal() {
  currentModalCamera = null;
  document.getElementById('camera-modal').classList.add('hidden');
}

// =======================================================
// TOAST THÔNG BÁO NHANH
// =======================================================
function showToast(msg) {
  const toast = document.getElementById('toast');
  toast.textContent = msg;
  toast.classList.remove('hidden');

  setTimeout(() => {
    toast.classList.add('hidden');
  }, 3000);
}
