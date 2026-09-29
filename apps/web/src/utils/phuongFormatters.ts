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

/**
 * Formats Vietnamese administrative unit names to English locale.
 * - Phường <Name> -> <Name> Ward (or Ward <N> if numeric)
 * - Xã <Name> -> <Name> Commune (or Commune <N> if numeric)
 * - Thị trấn <Name> -> <Name> Town
 */
export function formatPhuongName(rawName: string, lang: 'vi' | 'en'): string {
  if (!rawName) return '';
  if (lang === 'vi') return rawName;

  // 1. Phường (Ward)
  const phuongMatch = rawName.match(/^(?:Phường|phường)\s+(.+)$/i);
  if (phuongMatch) {
    const core = phuongMatch[1].trim();
    return /^\d+$/.test(core) ? `Ward ${core}` : `${core} Ward`;
  }

  // 2. Xã (Commune)
  const xaMatch = rawName.match(/^(?:Xã|xã)\s+(.+)$/i);
  if (xaMatch) {
    const core = xaMatch[1].trim();
    return /^\d+$/.test(core) ? `Commune ${core}` : `${core} Commune`;
  }

  // 3. Thị trấn (Town)
  const ttMatch = rawName.match(/^(?:Thị\s*trấn|thị\s*trấn)\s+(.+)$/i);
  if (ttMatch) {
    const core = ttMatch[1].trim();
    return `${core} Town`;
  }

  // 4. Standalone digits (e.g. "14")
  if (/^\d+$/.test(rawName.trim())) {
    return `Ward ${rawName.trim()}`;
  }

  return rawName;
}

/**
 * Formats corridor/street names with localized type prefixes.
 * Handles Bridges, Overpasses, Junctions, Roundabouts, Expressways, Routes, and Streets.
 */
export function formatCorridorDisplay(rawCorridor: string, lang: 'vi' | 'en'): CorridorDisplay {
  const raw = (rawCorridor || '').trim();
  if (!raw || raw.toLowerCase() === 'khác') {
    return { prefix: '', name: lang === 'vi' ? 'Khác' : 'Other' };
  }

  // 1. Cầu vượt (Overpass / Flyover)
  let m = raw.match(/^Cầu\s+vượt\s+(.+)$/i);
  if (m) {
    return { prefix: lang === 'vi' ? 'Cầu vượt' : 'Overpass', name: m[1].trim() };
  }

  // 2. Cầu (Bridge)
  m = raw.match(/^Cầu\s+(.+)$/i);
  if (m) {
    return { prefix: lang === 'vi' ? 'Cầu' : 'Bridge', name: m[1].trim() };
  }

  // 3. Nút giao (Junction)
  m = raw.match(/^Nút\s+giao\s+(.+)$/i);
  if (m) {
    return { prefix: lang === 'vi' ? 'Nút giao' : 'Junction', name: m[1].trim() };
  }

  // 4. Vòng xoay (Roundabout)
  m = raw.match(/^Vòng\s+xoay\s+(.+)$/i);
  if (m) {
    return { prefix: lang === 'vi' ? 'Vòng xoay' : 'Roundabout', name: m[1].trim() };
  }

  // 5. Cao tốc (Expressway)
  m = raw.match(/^Cao\s+tốc\s+(.+)$/i);
  if (m) {
    return { prefix: lang === 'vi' ? 'Cao tốc' : 'Expressway', name: m[1].trim() };
  }

  // 6. Quốc lộ / QL (Route)
  m = raw.match(/^(?:Quốc\s*lộ|QL)\s*(.+)$/i);
  if (m) {
    return { prefix: lang === 'vi' ? 'Quốc lộ' : 'Route', name: m[1].trim() };
  }

  // 7. Đường hầm / Hầm (Tunnel)
  m = raw.match(/^(?:Đường\s+hầm|Hầm)(?:\s+(.+))?$/i);
  if (m) {
    const sub = (m[1] || '').trim();
    return { prefix: lang === 'vi' ? 'Đường hầm' : 'Tunnel', name: sub };
  }

  // 8. Đường số (Street No.)
  m = raw.match(/^Đường\s+số\s+(.+)$/i);
  if (m) {
    return {
      prefix: lang === 'vi' ? 'Đường' : 'Street',
      name: lang === 'vi' ? `số ${m[1].trim()}` : `No. ${m[1].trim()}`,
    };
  }

  // 9. Đường <Name> (e.g. đường dẫn cao tốc, Đường D1)
  m = raw.match(/^Đường\s+(.+)$/i);
  if (m) {
    return { prefix: lang === 'vi' ? 'Đường' : 'Street', name: m[1].trim() };
  }

  // 10. Default street
  return { prefix: lang === 'vi' ? 'Đường' : 'Street', name: raw };
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
