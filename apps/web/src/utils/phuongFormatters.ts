/**
 * Administrative and corridor display formatting helpers with bilingual (vi/en) support.
 */

export const OTHER_WARDS_KEY = '__OTHER_WARDS__';
export const OUTSIDE_HCMC_KEY = OTHER_WARDS_KEY;

export interface FilterableCamera {
  id: string;
  displayName?: string;
  title?: string;
  phuong_name?: string | null;
}

export interface CorridorDisplay {
  prefix: string;
  name: string;
}

export const I18N_NAMES = {
  vi: {
    ward_type_phuong: 'Phường',
    ward_type_xa: 'Xã',
    ward_type_thi_tran: 'Thị trấn',
    road_type_duong: 'Đường',
    road_type_quoc_lo: 'Quốc lộ',
    road_type_xa_lo: 'Xa lộ',
  },
  en: {
    ward_type_phuong: 'Ward',
    ward_type_xa: 'Commune',
    ward_type_thi_tran: 'Town',
    road_type_duong: 'Street',
    road_type_quoc_lo: 'National Route',
    road_type_xa_lo: 'Highway',
  },
} as const;

/**
 * Formats Vietnamese administrative unit names with proper i18n support.
 * - In vi: 'Phường X', 'Xã Y', 'Thị trấn Z'
 * - In en: 'X Ward' (or 'Ward <N>' if numeric), 'Y Commune', 'Z Town'
 * Supports prefix-less rawName when type is specified, as well as legacy prefixed rawName.
 */
export function formatPhuongName(
  rawName: string,
  lang: 'vi' | 'en',
  type?: string | null
): string {
  if (!rawName) return '';
  const trimmed = rawName.trim();

  let detectedType = type;
  let core = trimmed;

  const phuongMatch = trimmed.match(/^(?:Phường|phường)\s+(.+)$/i);
  if (phuongMatch) {
    detectedType = 'Phường';
    core = phuongMatch[1].trim();
  } else {
    const xaMatch = trimmed.match(/^(?:Xã|xã)\s+(.+)$/i);
    if (xaMatch) {
      detectedType = 'Xã';
      core = xaMatch[1].trim();
    } else {
      const ttMatch = trimmed.match(/^(?:Thị\s*trấn|thị\s*trấn)\s+(.+)$/i);
      if (ttMatch) {
        detectedType = 'Thị trấn';
        core = ttMatch[1].trim();
      }
    }
  }

  const isNumeric = /^\d+$/.test(core);

  // If numeric with no prefix/type, default to Phường/Ward
  if (isNumeric && !detectedType) {
    detectedType = 'Phường';
  }

  // If no prefix and no explicit type was provided, return rawName
  if (!detectedType) {
    return rawName;
  }

  if (lang === 'vi') {
    if (detectedType === 'Xã') {
      return `${I18N_NAMES.vi.ward_type_xa} ${core}`;
    }
    if (detectedType === 'Thị trấn') {
      return `${I18N_NAMES.vi.ward_type_thi_tran} ${core}`;
    }
    return `${I18N_NAMES.vi.ward_type_phuong} ${core}`;
  }

  // lang === 'en'
  if (detectedType === 'Xã') {
    return isNumeric
      ? `${I18N_NAMES.en.ward_type_xa} ${core}`
      : `${core} ${I18N_NAMES.en.ward_type_xa}`;
  }
  if (detectedType === 'Thị trấn') {
    return `${core} ${I18N_NAMES.en.ward_type_thi_tran}`;
  }
  return isNumeric
    ? `${I18N_NAMES.en.ward_type_phuong} ${core}`
    : `${core} ${I18N_NAMES.en.ward_type_phuong}`;
}

/**
 * Formats corridor/street names with localized type prefixes.
 * Handles Bridges, Overpasses, Junctions, Roundabouts, Expressways, National Routes, Highways, and Streets.
 */
export function formatCorridorDisplay(
  rawCorridor: string,
  lang: 'vi' | 'en',
  roadType?: string | null
): CorridorDisplay {
  const raw = (rawCorridor || '').trim();
  if (!raw || raw.toLowerCase() === 'khác') {
    return { prefix: '', name: lang === 'vi' ? 'Khác' : 'Other' };
  }

  // 1. Cầu vượt (Overpass / Flyover)
  let m = raw.match(/^Cầu\s+vượt\s+(.+)$/i);
  if (m || roadType === 'Cầu vượt') {
    const name = m ? m[1].trim() : raw;
    return { prefix: lang === 'vi' ? 'Cầu vượt' : 'Overpass', name };
  }

  // 2. Cầu (Bridge)
  m = raw.match(/^Cầu\s+(.+)$/i);
  if (m || roadType === 'Cầu') {
    const name = m ? m[1].trim() : raw;
    return { prefix: lang === 'vi' ? 'Cầu' : 'Bridge', name };
  }

  // 3. Nút giao (Junction)
  m = raw.match(/^Nút\s+giao\s+(.+)$/i);
  if (m || roadType === 'Nút giao') {
    const name = m ? m[1].trim() : raw;
    return { prefix: lang === 'vi' ? 'Nút giao' : 'Junction', name };
  }

  // 4. Vòng xoay (Roundabout)
  m = raw.match(/^Vòng\s+xoay\s+(.+)$/i);
  if (m || roadType === 'Vòng xoay') {
    const name = m ? m[1].trim() : raw;
    return { prefix: lang === 'vi' ? 'Vòng xoay' : 'Roundabout', name };
  }

  // 5. Cao tốc (Expressway)
  m = raw.match(/^Cao\s+tốc\s+(.+)$/i);
  if (m || roadType === 'Cao tốc') {
    const name = m ? m[1].trim() : raw;
    return { prefix: lang === 'vi' ? 'Cao tốc' : 'Expressway', name };
  }

  // 6. Xa lộ (Highway)
  m = raw.match(/^(?:Xa\s*lộ|XL)\s*(.+)$/i);
  if (m || roadType === 'Xa lộ') {
    const name = m ? m[1].trim() : raw;
    return {
      prefix: lang === 'vi' ? I18N_NAMES.vi.road_type_xa_lo : I18N_NAMES.en.road_type_xa_lo,
      name,
    };
  }

  // 7. Quốc lộ / QL (National Route)
  m = raw.match(/^(?:Quốc\s*lộ|QL)\s*(.+)$/i);
  if (m || roadType === 'Quốc lộ') {
    const name = m ? m[1].trim() : raw;
    return {
      prefix: lang === 'vi' ? I18N_NAMES.vi.road_type_quoc_lo : I18N_NAMES.en.road_type_quoc_lo,
      name,
    };
  }

  // 8. Đường hầm / Hầm (Tunnel)
  m = raw.match(/^(?:Đường\s+hầm|Hầm)(?:\s+(.+))?$/i);
  if (m || roadType === 'Hầm' || roadType === 'Đường hầm') {
    const sub = m ? (m[1] || '').trim() : raw;
    return { prefix: lang === 'vi' ? 'Đường hầm' : 'Tunnel', name: sub };
  }

  // 9. Đường số (Street No.)
  m = raw.match(/^Đường\s+số\s+(.+)$/i);
  if (m) {
    return {
      prefix: lang === 'vi' ? I18N_NAMES.vi.road_type_duong : I18N_NAMES.en.road_type_duong,
      name: lang === 'vi' ? `số ${m[1].trim()}` : `No. ${m[1].trim()}`,
    };
  }

  // 10. Đường <Name> (e.g. đường dẫn cao tốc, Đường D1)
  m = raw.match(/^Đường\s+(.+)$/i);
  if (m) {
    return {
      prefix: lang === 'vi' ? I18N_NAMES.vi.road_type_duong : I18N_NAMES.en.road_type_duong,
      name: m[1].trim(),
    };
  }

  // 11. Default street
  return {
    prefix: lang === 'vi' ? I18N_NAMES.vi.road_type_duong : I18N_NAMES.en.road_type_duong,
    name: raw,
  };
}

/**
 * Formats full localized road name: "Đường {name}" (vi) / "{name} Street" (en).
 */
export function formatRoadName(
  roadName: string,
  lang: 'vi' | 'en' = 'vi',
  roadType: string = 'Đường'
): string {
  if (!roadName) return '';
  const display = formatCorridorDisplay(roadName, lang, roadType);
  if (!display.prefix) return display.name;

  if (lang === 'en') {
    if (display.prefix === I18N_NAMES.en.road_type_duong || display.prefix === I18N_NAMES.en.road_type_xa_lo) {
      return `${display.name} ${display.prefix}`;
    }
  }

  return `${display.prefix} ${display.name}`;
}

/**
 * Extracts primary street name from camera displayName (split on ' - ', ' – ', ' — ', ' − ').
 */
export function extractCorridor(displayName?: string, title?: string): string {
  const raw = (displayName || title || '').trim();
  if (!raw) return 'Khác';
  const parts = raw.split(/\s+[-–—−]\s+/);
  return parts[0].trim() || raw;
}

/**
 * Strips Vietnamese diacritics for insensitive search.
 */
export function removeVietnameseDiacritics(str: string): string {
  return str
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .replace(/đ/g, 'd')
    .replace(/Đ/g, 'D');
}

/**
 * Checks whether target string matches search query considering Vietnamese diacritics and multiple words.
 */
export function matchesVietnameseSearch(target: string, query: string): boolean {
  const cleanQuery = query.trim().replace(/\s+/g, ' ');
  if (!cleanQuery) return true;
  const lowerTarget = (target || '').toLowerCase();
  const lowerQuery = cleanQuery.toLowerCase();

  // 1. Direct substring match (supports exact accented search)
  if (lowerTarget.includes(lowerQuery)) return true;

  // 2. Normalized diacritics match (supports unaccented search)
  const normTarget = removeVietnameseDiacritics(lowerTarget);
  const normQuery = removeVietnameseDiacritics(lowerQuery);
  if (normTarget.includes(normQuery)) return true;

  // 3. Multi-word search (all words in query must appear in target)
  const queryWords = normQuery.split(' ').filter(Boolean);
  if (queryWords.length > 1 && queryWords.every(w => normTarget.includes(w))) {
    return true;
  }

  return false;
}

/**
 * Computes the Set of camera IDs that match the active Phường/Corridor filter.
 * Returns undefined when no filter is active.
 */
export function computeFilteredCameraIds(
  cameras: FilterableCamera[],
  selectedPhuong: string | null,
  selectedCorridor: string | null
): Set<string> | undefined {
  if (!selectedPhuong && !selectedCorridor) return undefined;

  const set = new Set<string>();
  cameras.forEach(c => {
    let matchPhuong = true;
    if (selectedPhuong) {
      if (selectedPhuong === OTHER_WARDS_KEY || selectedPhuong === OUTSIDE_HCMC_KEY) {
        matchPhuong = !c.phuong_name;
      } else {
        matchPhuong = c.phuong_name === selectedPhuong;
      }
    }
    let matchCorridor = true;
    if (selectedCorridor) {
      matchCorridor = extractCorridor(c.displayName, c.title) === selectedCorridor;
    }
    if (matchPhuong && matchCorridor) {
      set.add(c.id);
    }
  });

  return set;
}

/**
 * Computes localized active filter label for on-map chip and headers.
 */
export function computeActiveFilterLabel(
  selectedPhuong: string | null,
  selectedCorridor: string | null,
  lang: 'vi' | 'en'
): string | null {
  if (!selectedPhuong && !selectedCorridor) return null;

  const phuongName = selectedPhuong
    ? selectedPhuong === OTHER_WARDS_KEY || selectedPhuong === OUTSIDE_HCMC_KEY
      ? lang === 'vi'
        ? 'Các Phường khác'
        : 'Other Wards'
      : formatPhuongName(selectedPhuong, lang)
    : '';

  if (selectedCorridor) {
    const corrDisplay = formatCorridorDisplay(selectedCorridor, lang);
    const corrText = corrDisplay.prefix
      ? `${corrDisplay.prefix} ${corrDisplay.name}`
      : corrDisplay.name;
    return phuongName ? `${phuongName} → ${corrText}` : corrText;
  }

  return phuongName;
}

export interface MarkerStyleOptions {
  radius: number;
  weight: number;
  opacity: number;
  fillOpacity: number;
  strokeColor: string;
}

/**
 * Calculates marker style for normal vs dimmed (non-matching) cameras.
 * Per ADR-004 A3: Dimmed markers have opacity 0.2, radius reduced by 30%.
 */
export function calculateMarkerStyle(
  isDimmed: boolean,
  baseRadius: number,
  zoom: number
): MarkerStyleOptions {
  if (isDimmed) {
    return {
      radius: Math.max(2.5, Math.round(baseRadius * 0.7)),
      weight: 1,
      opacity: 0.25,
      fillOpacity: 0.2,
      strokeColor: '#27272a',
    };
  }

  return {
    radius: baseRadius,
    weight: zoom >= 14 ? 2 : 1.5,
    opacity: 1.0,
    fillOpacity: 0.95,
    strokeColor: '#000000',
  };
}
