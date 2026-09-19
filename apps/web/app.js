// =======================================================
// GLOBAL STATE & VARIABLES
// =======================================================
let map = null;
let currentTileLayer = null;
let allCameras = [];
let dotMarkersLayer = null;
let activeMarkerLayer = null; // Layer containing pinned/monitored cameras (larger dot, brought to front)
let mapMarkerRefs = new Map(); // Map marker references for synchronized interaction
let pinnedCameras = []; // List of cameras pinned for monitoring in the bottom dock
let selectedCameraId = null;

let autoRefreshInterval = null;
let refreshCountdown = 10;
let currentModalCamera = null;

// Lock map view strictly within Southeast mainland Vietnam (territorial safety)
const CAMERA_VIEW_MAP_BOUNDS = [
  [10.15, 106.35],
  [11.35, 107.45]
];

// Automatic API routing: route requests to backend port 3000 when frontend runs on port 3001
const API_BASE = window.location.port === '3001' ? 'http://localhost:3000' : '';



// Calculate marker radius based on map zoom level (Min 3.5px - Max 12px)
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

// Update marker scales across all markers on zoom change
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

  // Synchronize sample legend dot sizes with current zoom scale
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
// DYNAMIC CHAINED DEEP FILTER STATE
// =======================================================
// Each item: { id: number, prop: 'zone' | 'road' | 'node' | 'status', val: string }
let activeFilters = [];
let currentSearchQuery = '';
let filterIdCounter = 1;

// =======================================================
// VIETNAMESE DIACRITICS STRIPPER (ACCENT-INSENSITIVE SEARCH)
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
  // Remove camera index suffix in parentheses e.g. (20-1), (1), (2)...
  s = s.replace(/\s*\([\d\-A-Za-z\s]+\)\s*$/, '').trim();
  // Strip generic intersection prefixes (keep bridge, tunnel, port intact)
  s = s.replace(/^(giao lộ|nút giao|ngã tư|ngã ba|ngã 4)\s+/i, '').trim();
  s = s.replace(/[\(\)]+/g, '').trim();
  return s;
}

// =======================================================
// CAMERA METADATA CLASSIFIER (ZONE, CORRIDOR, INTERSECTION NODE)
// =======================================================
function getCameraZone(cam) {
  const lat = cam.latitude;
  const lon = cam.longitude;
  const name = (cam.displayName || cam.title || '').trim();
  const lower = name.toLowerCase();

  // 1. Neighboring province: Ba Ria - Vung Tau (along National Highway 51)
  if (lon && lon > 107.0) return 'Bà Rịa - Vũng Tàu';

  // 2. Binh Duong province
  if (lower.includes('mỹ phước') || lower.includes('tân vạn') || lower.includes('dĩ an') || lower.includes('thuận an') || lower.includes('dt743') || lower.includes('đại lộ bình dương') || (lat && lat > 10.92 && lon && lon > 106.65)) {
    return 'Bình Dương';
  }

  // 3. Cat Lai - My Thuy Port Cluster (strict bounding box to prevent string collision)
  const inCatLaiBox = (lat && lon) ? (lat >= 10.748 && lat <= 10.785 && lon >= 106.745 && lon <= 106.815) : false;
  const isCatLaiName = lower.includes('cát lái') || lower.includes('mỹ thủy') || lower.includes('lê phụng hiểu') ||
    (lower.includes('nguyễn thị định') && !lower.includes('thân văn nhiếp')) ||
    (lower.includes('đồng văn cống') && (lower.includes('đường c') || lower.includes('nguyễn thị định') || lower.includes('phan văn đáng') || lower.includes('phía cảng'))) ||
    /\bđường [abcdgk]\b/i.test(lower);

  if (inCatLaiBox && isCatLaiName) {
    return 'Cụm Cảng Cát Lái';
  }

  // 4. South Saigon - District 7 - Nha Be
  if ((lat && lat < 10.755 && lon && lon >= 106.68 && lon < 106.75) || lower.includes('huỳnh tấn phát') || lower.includes('nguyễn văn linh') || lower.includes('nhà bè') || lower.includes('nguyễn hữu thọ')) {
    return 'Quận 7 - Nhà Bè';
  }

  // 5. Thu Duc City (East of Saigon River)
  if (lon && lon >= 106.725 && lat >= 10.74) {
    return 'TP. Thủ Đức';
  }

  // 6. Southwest - Binh Tan - Binh Chanh (Western gateway)
  if ((lon && lon < 106.62) || lower.includes('bình chánh') || lower.includes('bình tân') || lower.includes('tân kiên') || lower.includes('an lạc') || lower.includes('võ trần chí') || lower.includes('kinh dương vương')) {
    return 'Bình Tân - Bình Chánh';
  }

  // 7. Northwest - District 12 - Hoc Mon - Cu Chi (Northwestern gateway)
  if ((lat && lat >= 10.835 && lon && lon < 106.72) || lower.includes('hóc môn') || lower.includes('củ chi') || lower.includes('quốc lộ 22') || lower.includes('ngã 4 ga') || lower.includes('an sương')) {
    return 'Q.12 - Hóc Môn - Củ Chi';
  }

  // 8. HCMC Urban Core (District 1, 3, 5, 10, Binh Thanh, Tan Binh, Phu Nhuan...)
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
  // Use word boundary & lookahead to avoid false positives (e.g. QL1 vs QL13/14)
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
// APPLICATION INITIALIZATION
// =======================================================
document.addEventListener('DOMContentLoaded', async () => {
  // Collapse sidebar on small screens (< 768px) by default to prioritize map view
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

  // Initialize standard OpenStreetMap tile layer (clean, natural colors, no watermark)
  currentTileLayer = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '&copy; <a href="https://www.openstreetmap.org/copyright" target="_blank">OpenStreetMap</a>',
    maxZoom: 19
  }).addTo(map);

  dotMarkersLayer = L.layerGroup().addTo(map);
  activeMarkerLayer = L.layerGroup().addTo(map);

  // Automatically scale marker dots based on map zoom level
  map.on('zoomend', () => {
    updateMarkerScales();
  });
}

function setupEventListeners() {
  // Toggle sidebar
  document.getElementById('btn-toggle-sidebar').addEventListener('click', toggleSidebar);
  document.getElementById('btn-close-sidebar').addEventListener('click', toggleSidebar);

  // Close sidebar when clicking mobile backdrop
  const backdrop = document.getElementById('sidebar-backdrop');
  if (backdrop) {
    backdrop.addEventListener('click', toggleSidebar);
  }

  // Toggle map legend
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

  // Quick search input
  document.getElementById('camera-search-input').addEventListener('input', (e) => {
    currentSearchQuery = e.target.value.trim();
    applyChainedFilterEngine();
  });

  // Add filter condition button
  document.getElementById('btn-add-filter').addEventListener('click', () => {
    addNewFilterCondition();
  });

  // Clear all filter conditions button
  document.getElementById('btn-clear-all-filters').addEventListener('click', () => {
    clearAllFilterConditions();
  });

  // Clear pinned cameras button
  document.getElementById('btn-clear-pinned').addEventListener('click', clearAllPinned);

  // Toggle bottom dock
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

  // Close camera modal
  document.getElementById('btn-close-modal').addEventListener('click', closeModal);
  document.getElementById('camera-modal').addEventListener('click', (e) => {
    if (e.target.id === 'camera-modal') closeModal();
  });

  // Pin / unpin camera from modal
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
// 1. LOAD CAMERA DATA & ENRICH METADATA
// =======================================================
async function loadCameras() {
  try {
    const res = await fetch(`${API_BASE}/api/cameras`);
    const data = await res.json();
    const rawCams = data.cameras || [];

    // Enrich metadata classifications for camera catalog
    allCameras = rawCams.map(c => ({
      ...c,
      zone: getCameraZone(c),
      roads: getCameraRoads(c),
      nodes: getCameraNodes(c)
    }));

    document.getElementById('sidebar-total-cams').textContent = `[${allCameras.length}]`;

    // Initialize dynamic filter engine and render markers
    renderFilterBuilderRows();
    applyChainedFilterEngine();
    renderPinnedCameraTrack();
  } catch (err) {
    console.error('Failed to load camera data:', err);
  }
}

// =======================================================
// 2. CHAINED FILTER ENGINE
// =======================================================

// Get camera subset matching filters up to specified index (or all filters if omitted)
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

  // Combine with text search query if present
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

// Compute available facets based on camera subset from previous filter tiers
function getAvailableFacetsForFilter(filterIndex) {
  // Filtered camera subset from tier 0 to (filterIndex - 1)
  const prevSubset = filterIndex > 0 ? getFilteredSubset(filterIndex - 1) : allCameras;

  // 1. Available Zones (sorted by count descending)
  const zoneCounts = new Map();
  prevSubset.forEach(c => {
    zoneCounts.set(c.zone, (zoneCounts.get(c.zone) || 0) + 1);
  });
  const zones = [...zoneCounts.entries()]
    .map(([val, count]) => ({ val, count, label: `${val} (${count})` }))
    .sort((a, b) => b.count - a.count);

  // 2. Available Corridors/Roads (sorted by count descending)
  const roadCounts = new Map();
  prevSubset.forEach(c => {
    c.roads.forEach(r => roadCounts.set(r, (roadCounts.get(r) || 0) + 1));
  });
  const roads = [...roadCounts.entries()]
    .map(([val, count]) => ({ val, count, label: `${val} (${count})` }))
    .sort((a, b) => b.count - a.count);

  // 3. Available Intersection Nodes (sorted by count descending)
  const nodeCounts = new Map();
  prevSubset.forEach(c => {
    c.nodes.forEach(n => {
      nodeCounts.set(n, (nodeCounts.get(n) || 0) + 1);
    });
  });
  const nodes = [...nodeCounts.entries()]
    .map(([val, count]) => ({ val, count, label: `${val} (${count})` }))
    .sort((a, b) => b.count - a.count);

  // 4. Connection status
  let onlineCount = 0;
  let offlineCount = 0;
  prevSubset.forEach(c => { if (c.isOnline) onlineCount++; else offlineCount++; });
  const statuses = [
    { val: 'ONLINE', label: `[HOẠT ĐỘNG] (${onlineCount})` },
    { val: 'OFFLINE', label: `[MẤT TÍN HIỆU] (${offlineCount})` }
  ];

  return { zones, roads, nodes, statuses, totalAvailable: prevSubset.length };
}

// Add a new filter condition row
function addNewFilterCondition() {
  const newIndex = activeFilters.length;
  const facets = getAvailableFacetsForFilter(newIndex);

  // Smart property heuristic based on previous filter tier
  let smartProp = 'zone';
  if (newIndex > 0) {
    const prevProp = activeFilters[newIndex - 1].prop;
    if (prevProp === 'zone') smartProp = 'road';
    else if (prevProp === 'road') smartProp = 'node';
    else if (prevProp === 'node') smartProp = 'status';
    else smartProp = 'road';
  }

  // Select first default value for chosen property
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

// Remove a filter condition row
function removeFilterCondition(filterId) {
  activeFilters = activeFilters.filter(f => f.id !== filterId);
  renderFilterBuilderRows();
  applyChainedFilterEngine(true);
}

// Clear all filter conditions
function clearAllFilterConditions() {
  activeFilters = [];
  renderFilterBuilderRows();
  applyChainedFilterEngine(true);
}

// Render chained filter builder rows in sidebar
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

    // Filter tier index
    const idxEl = document.createElement('span');
    idxEl.className = 'filter-row-index';
    idxEl.textContent = `${index + 1}.`;
    row.appendChild(idxEl);

    // Property selector dropdown
    const propSelect = document.createElement('select');
    propSelect.className = 'filter-prop-select';
    propSelect.innerHTML = `
      <option value="zone" ${filter.prop === 'zone' ? 'selected' : ''}>Khu vực</option>
      <option value="road" ${filter.prop === 'road' ? 'selected' : ''}>Tuyến đường</option>
      <option value="node" ${filter.prop === 'node' ? 'selected' : ''}>Giao lộ cắt</option>
      <option value="status" ${filter.prop === 'status' ? 'selected' : ''}>Trạng thái</option>
    `;
    row.appendChild(propSelect);

    // Equal sign
    const eq = document.createElement('span');
    eq.className = 'filter-eq-sign';
    eq.textContent = '=';
    row.appendChild(eq);

    // Value selector dropdown (populated with valid facets for current subset)
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

      // If current value is invalid in new facet list, fall back to first item
      if (!items.some(it => it.val === filter.val)) {
        filter.val = items[0].val;
        valSelect.value = items[0].val;
      }
    }

    fillValuesForProp(filter.prop);
    valSelect.title = filter.val;
    row.appendChild(valSelect);

    // Remove condition button
    const removeBtn = document.createElement('button');
    removeBtn.className = 'filter-remove-btn';
    removeBtn.textContent = 'x';
    removeBtn.title = 'Xóa điều kiện này';
    removeBtn.addEventListener('click', () => {
      removeFilterCondition(filter.id);
    });
    row.appendChild(removeBtn);

    // On filter property change
    propSelect.addEventListener('change', (e) => {
      filter.prop = e.target.value;
      fillValuesForProp(filter.prop);
      valSelect.title = filter.val;
      // Re-render subsequent filter rows as facets have changed
      renderFilterBuilderRows();
      applyChainedFilterEngine(true);
    });

    // On filter value change
    valSelect.addEventListener('change', (e) => {
      filter.val = e.target.value;
      valSelect.title = filter.val;
      // Update subsequent filter rows
      renderFilterBuilderRows();
      applyChainedFilterEngine(true);
    });

    container.appendChild(row);
  });
}

// Apply chained filters and update camera list & map
function applyChainedFilterEngine(shouldFitMap = false) {
  const filteredCameras = getFilteredSubset();

  // Update matched camera count badge
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

  // Render map markers ONLY for cameras in the filtered subset
  renderMapMarkers(filteredCameras);

  // Render camera items list in sidebar
  renderCameraListItems(filteredCameras);

  // Fit map viewport to matched camera coordinates
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

// Render circle markers for visible cameras
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

// Render camera list items in sidebar
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

    // Synchronize hover state with map marker
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
// 3. SELECT CAMERA: DIRECT SNAPSHOT PREVIEW (NO AUTO-PIN)
// =======================================================
function selectCamera(cam) {
  selectedCameraId = cam.id;

  // Update active markers
  renderActiveMarkers();

  // Highlight selected item in sidebar
  document.querySelectorAll('.cam-list-item').forEach(el => el.classList.remove('selected'));
  const currentCard = Array.from(document.querySelectorAll('.cam-list-item')).find(el => {
    return el.querySelector('.cam-item-name')?.getAttribute('title') === (cam.displayName || cam.title);
  });
  if (currentCard) {
    currentCard.classList.add('selected');
  }

  // Smoothly pan map viewport to selected camera
  if (cam.latitude && cam.longitude) {
    map.flyTo([cam.latitude, cam.longitude], 16, { duration: 0.8 });
  }

  // Auto-collapse sidebar on mobile when a camera is selected
  if (window.innerWidth < 768) {
    const sidebar = document.getElementById('camera-sidebar');
    const backdrop = document.getElementById('sidebar-backdrop');
    if (sidebar && !sidebar.classList.contains('collapsed')) {
      sidebar.classList.add('collapsed');
      if (backdrop) backdrop.classList.add('hidden');
    }
  }

  // Open direct snapshot modal
  openCameraModal(cam);
}

function selectAndPinCamera(cam) {
  selectCamera(cam);
}

function pinCamera(cam) {
  if (!pinnedCameras.some(c => c.id === cam.id)) {
    pinnedCameras.unshift(cam);
    // Maintain max 10 pinned cameras in dock
    if (pinnedCameras.length > 10) pinnedCameras.pop();
    renderPinnedCameraTrack();
    renderActiveMarkers();

    // Auto-expand dock if collapsed so user sees newly pinned camera
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

  // Auto-collapse dock when empty
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

  // Auto-collapse dock when empty
  const drawer = document.getElementById('route-cameras-drawer');
  const btn = document.getElementById('btn-toggle-drawer');
  if (drawer && !drawer.classList.contains('collapsed')) {
    drawer.classList.add('collapsed');
    if (btn) btn.textContent = '[▲ MỞ DOCK]';
  }
}

// Render prominent numbered dots for pinned cameras (Bring to Front)
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
        zIndexOffset: 3000 // Elevate above all other map layers
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
// 4. BOTTOM DOCK: PINNED CAMERAS TRACK
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

// Auto-refresh snapshot cycle (every 10 seconds)
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
// 5. DIRECT CAMERA SNAPSHOT MODAL
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

  // Update modal pin button state
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
// TOAST NOTIFICATIONS
// =======================================================
function showToast(msg) {
  const toast = document.getElementById('toast');
  toast.textContent = msg;
  toast.classList.remove('hidden');

  setTimeout(() => {
    toast.classList.add('hidden');
  }, 3000);
}
