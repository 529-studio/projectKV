/**
 * Administrative and corridor display formatting helpers with bilingual (vi/en) support.
 */

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
