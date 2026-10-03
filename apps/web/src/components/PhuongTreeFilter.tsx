'use client';

import { useState, useMemo, useEffect, useRef } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Camera } from '../hooks/useCameras';
import { usePhuongs } from '../hooks/usePhuongs';

export const OTHER_WARDS_KEY = '__OTHER_WARDS__';
export const OUTSIDE_HCMC_KEY = OTHER_WARDS_KEY;

import { FLOOD_I18N } from '../utils/floodUtils';
import { useCorridor } from '../hooks/useCorridor';
import { formatObservationAge, getCorridorWorstVerdict } from '../utils/corridorUtils';
import { CORRIDOR_I18N } from '../config/constants';

export interface PhuongTreeFilterProps {
  cameras: Camera[];
  verdicts?: Map<string, TrafficVerdictLike>;
  floodedCameraIds?: Set<string>;
  isFloodFilterActive?: boolean;
  onToggleFloodFilter?: () => void;
  loading?: boolean;
  error?: Error | null;
  selectedPhuong: string | null;
  selectedCorridor: string | null;
  onSelectPhuong: (phuong: string | null) => void;
  onSelectCorridor: (corridor: string | null) => void;
  onCameraClick: (camera: Camera) => void;
  searchTerm?: string;
  onSearchChange?: (term: string) => void;
  lang?: 'vi' | 'en';
  onLangChange?: (lang: 'vi' | 'en') => void;
  className?: string;
  autoFocusSearch?: boolean;
}

interface CorridorGroup {
  name: string;
  cameras: Camera[];
}

interface PhuongGroup {
  key: string;
  displayName: string;
  corridors: CorridorGroup[];
  totalCameras: number;
}

import {
  formatPhuongName,
  formatCorridorDisplay,
  formatRoadName,
  I18N_NAMES,
  extractCorridor,
  removeVietnameseDiacritics,
  matchesVietnameseSearch,
  computeFilteredCameraIds,
  computeActiveFilterLabel,
  calculateMarkerStyle,
  ARTERIAL_CORRIDORS,
  groupCamerasByCityCorridors,
  type CorridorDisplay,
  type TrafficVerdictLike,
  type CityCorridorGroup,
} from '../utils/phuongFormatters';

export {
  formatPhuongName,
  formatCorridorDisplay,
  formatRoadName,
  I18N_NAMES,
  extractCorridor,
  removeVietnameseDiacritics,
  matchesVietnameseSearch,
  computeFilteredCameraIds,
  computeActiveFilterLabel,
  calculateMarkerStyle,
  ARTERIAL_CORRIDORS,
  groupCamerasByCityCorridors,
  type CorridorDisplay,
  type TrafficVerdictLike,
  type CityCorridorGroup,
};

function CorridorDetailHeader({ roadKey, lang }: { roadKey?: string | null; lang: 'vi' | 'en' }) {
  const { status, loading } = useCorridor(roadKey || null);

  if (!roadKey) return null;

  return (
    <div className="flex items-center justify-between px-2.5 py-1 bg-zinc-950/90 border-b border-zinc-800/60 font-mono text-[9.5px] text-zinc-400 select-none">
      {loading ? (
        <span className="text-zinc-500 animate-pulse">{lang === 'vi' ? 'Đang tải thông tin...' : 'Loading status...'}</span>
      ) : status ? (
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-zinc-300 font-semibold">{CORRIDOR_I18N.COVERAGE[lang](status.coverage_pct)}</span>
          <span className="text-zinc-600">•</span>
          <span>
            {status.oldest_observation_age_s != null
              ? CORRIDOR_I18N.OLDEST[lang](formatObservationAge(status.oldest_observation_age_s, lang) || '')
              : CORRIDOR_I18N.UNKNOWN[lang]}
          </span>
        </div>
      ) : (
        <span>{CORRIDOR_I18N.UNKNOWN[lang]}</span>
      )}
    </div>
  );
}

export default function PhuongTreeFilter({
  cameras,
  verdicts,
  floodedCameraIds,
  isFloodFilterActive = false,
  onToggleFloodFilter,
  loading: camerasLoading = false,
  error = null,
  selectedPhuong,
  selectedCorridor,
  onSelectPhuong,
  onSelectCorridor,
  onCameraClick,
  searchTerm = '',
  onSearchChange,
  lang: controlledLang,
  onLangChange,
  className = '',
  autoFocusSearch = false,
}: PhuongTreeFilterProps) {
  const [internalLang, setInternalLang] = useState<'vi' | 'en'>('vi');
  const lang = controlledLang ?? internalLang;
  const setLang = onLangChange ?? setInternalLang;

  const searchInputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (autoFocusSearch) {
      const timer = setTimeout(() => {
        searchInputRef.current?.focus();
      }, 150);
      return () => clearTimeout(timer);
    }
  }, [autoFocusSearch]);

  const { phuongs: apiPhuongs, loading: phuongsLoading } = usePhuongs();

  const [explorerTab, setExplorerTab] = useState<'corridors' | 'phuongs'>('corridors');
  const [expandedCityCorridors, setExpandedCityCorridors] = useState<Set<string>>(new Set());
  const [expandedPhuongs, setExpandedPhuongs] = useState<Set<string>>(new Set());
  const [expandedCorridors, setExpandedCorridors] = useState<Set<string>>(new Set());

  // Active cameras taking flood filter into account
  const activeCameras = useMemo(() => {
    if (isFloodFilterActive && floodedCameraIds) {
      return cameras.filter(c => floodedCameraIds.has(c.id));
    }
    return cameras;
  }, [cameras, isFloodFilterActive, floodedCameraIds]);

  // City-wide corridor grouping with traffic prioritization
  const cityCorridors = useMemo(() => {
    return groupCamerasByCityCorridors(activeCameras, verdicts, searchTerm);
  }, [activeCameras, verdicts, searchTerm]);

  // Arterial corridors state & horizontal scroll controller
  const [isArterialsExpanded, setIsArterialsExpanded] = useState(false);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [canScrollLeft, setCanScrollLeft] = useState(false);
  const [canScrollRight, setCanScrollRight] = useState(true);

  const checkScrollButtons = () => {
    const el = scrollContainerRef.current;
    if (!el) return;
    setCanScrollLeft(el.scrollLeft > 4);
    setCanScrollRight(el.scrollLeft < el.scrollWidth - el.clientWidth - 4);
  };

  const scrollArterials = (direction: 'left' | 'right') => {
    const el = scrollContainerRef.current;
    if (!el) return;
    const offset = direction === 'left' ? -200 : 200;
    el.scrollBy({ left: offset, behavior: 'smooth' });
  };

  const arterialStatusMap = useMemo(() => {
    const map = new Map<string, { count: number; congested: boolean; moderate: boolean }>();
    cityCorridors.forEach(c => {
      map.set(c.name, {
        count: c.totalCameras,
        congested: c.congestedCount > 0,
        moderate: c.moderateCount > 0,
      });
    });
    return map;
  }, [cityCorridors]);

  // i18n text resources
  const t = useMemo(() => {
    return {
      title: lang === 'vi' ? 'Khám phá Camera' : 'Camera Explorer',
      subtitle: lang === 'vi' ? 'Warded • Cảnh báo ngập lụt & giao thông đô thị' : 'Warded • Urban flood alert & traffic intelligence',
      floodStatusAlert: (n: number) => (lang === 'vi' ? `CẢNH BÁO NGẬP: ${n} điểm camera xác nhận ngập` : `FLOOD ALERT: ${n} camera-verified flood spots`),
      floodStatusDry: lang === 'vi' ? 'TOÀN THÀNH PHỐ: Không ghi nhận điểm ngập nào' : 'CITYWIDE: No flooding reported by cameras',
      floodFilterActiveBtn: (n: number) => (lang === 'vi' ? `✕ Bỏ lọc ${n} camera ngập` : `✕ Clear ${n} flood cameras`),
      floodFilterInactiveBtn: (n: number) => (lang === 'vi' ? `Xem danh sách ${n} camera ngập →` : `View ${n} flood cameras →`),
      tabCorridors: lang === 'vi' ? 'Tuyến đường' : 'Corridors',
      tabPhuongs: lang === 'vi' ? 'Phường / Xã' : 'Wards',
      street: lang === 'vi' ? 'Đường' : 'Street',
      quickArterials: lang === 'vi' ? 'Trục huyết mạch' : 'Major Arterials',
      expandAll: lang === 'vi' ? 'Xem tất cả' : 'View all',
      collapse: lang === 'vi' ? 'Thu gọn' : 'Collapse',
      otherWards: lang === 'vi' ? 'Các Phường khác (cập nhật sau)' : 'Other Wards (to be updated)',
      outsideHcm: lang === 'vi' ? 'Các Phường khác (cập nhật sau)' : 'Other Wards (to be updated)',
      noCameras: lang === 'vi' ? 'Không có camera' : 'No cameras',
      noCorridors: lang === 'vi' ? 'Không tìm thấy tuyến đường' : 'No corridors found',
      allCameras: lang === 'vi' ? 'Tất cả camera' : 'All cameras',
      searchPlaceholder: lang === 'vi' ? 'Tìm camera, tuyến đường, phường...' : 'Search camera, corridor, ward...',
      activeFilter: lang === 'vi' ? 'Đang lọc:' : 'Filter:',
      clearFilter: lang === 'vi' ? 'Xóa lọc' : 'Clear',
      cameraCount: (n: number) => (lang === 'vi' ? `${n} camera` : `${n} ${n === 1 ? 'camera' : 'cameras'}`),
      congestedTag: lang === 'vi' ? 'kẹt' : 'jam',
      moderateTag: lang === 'vi' ? 'đông' : 'slow',
    };
  }, [lang]);

  // Group cameras into: Phường → Đường → Camera
  const { treeData, phuongTypeMap } = useMemo(() => {
    const term = searchTerm.trim();

    // Map of phuong name -> phuong type
    const pTypeMap = new Map<string, string>();
    apiPhuongs.forEach(p => {
      const name = p.name.trim();
      pTypeMap.set(name, p.type);
    });
    activeCameras.forEach(c => {
      if (c.phuong_name) {
        const name = c.phuong_name.trim();
        if (c.phuong_type && !pTypeMap.has(name)) {
          pTypeMap.set(name, c.phuong_type);
        }
      }
    });

    // Map: phuongKey -> Map: corridorName -> Camera[]
    const phuongMap = new Map<string, Map<string, Camera[]>>();

    activeCameras.forEach(cam => {
      const phuongKey = cam.phuong_name ? cam.phuong_name.trim() : OTHER_WARDS_KEY;
      const corridor = cam.road_name ? cam.road_name.trim() : extractCorridor(cam.displayName, cam.title);
      const roadType = cam.road_type || null;

      // Search filtering with Vietnamese diacritics and bilingual tolerance
      if (term) {
        const phuongType = cam.phuong_type || pTypeMap.get(phuongKey);
        const enPhuong = cam.phuong_name ? formatPhuongName(cam.phuong_name, 'en', phuongType) : '';
        const viPhuong = cam.phuong_name ? formatPhuongName(cam.phuong_name, 'vi', phuongType) : '';
        const enCorr = formatCorridorDisplay(corridor, 'en', roadType);
        const enCorrStr = enCorr.prefix ? `${enCorr.prefix} ${enCorr.name}` : enCorr.name;
        const viCorr = formatCorridorDisplay(corridor, 'vi', roadType);
        const viCorrStr = viCorr.prefix ? `${viCorr.prefix} ${viCorr.name}` : viCorr.name;
        const fullSearchable = `${cam.displayName || ''} ${cam.title || ''} ${corridor} ${viCorrStr} ${enCorrStr} ${cam.phuong_name || t.otherWards} ${viPhuong} ${enPhuong}`;
        if (!matchesVietnameseSearch(fullSearchable, term)) {
          return;
        }
      }

      if (!phuongMap.has(phuongKey)) {
        phuongMap.set(phuongKey, new Map());
      }
      const corridorMap = phuongMap.get(phuongKey)!;
      if (!corridorMap.has(corridor)) {
        corridorMap.set(corridor, []);
      }
      corridorMap.get(corridor)!.push(cam);
    });

    // Build ordered list of Phườngs starting with official apiPhuongs
    const allPhuongKeys = new Set<string>();
    apiPhuongs.forEach(p => allPhuongKeys.add(p.name.trim()));
    activeCameras.forEach(c => {
      if (c.phuong_name) allPhuongKeys.add(c.phuong_name.trim());
    });

    const groups: PhuongGroup[] = [];

    // 1. Known Phườngs sorted alphabetically by display name in current locale
    const sortedPhuongs = Array.from(allPhuongKeys).sort((a, b) => {
      const typeA = pTypeMap.get(a) || 'Phường';
      const typeB = pTypeMap.get(b) || 'Phường';
      const nameA = formatPhuongName(a, lang, typeA);
      const nameB = formatPhuongName(b, lang, typeB);
      return nameA.localeCompare(nameB, lang, { numeric: true });
    });

    sortedPhuongs.forEach(phuongKey => {
      const corridorMap = phuongMap.get(phuongKey);
      if (!corridorMap && term) {
        // If searching and this phuong has no matches, omit
        return;
      }

      const corridors: CorridorGroup[] = [];
      let totalCameras = 0;

      if (corridorMap) {
        const sortedCorridors = Array.from(corridorMap.keys()).sort((a, b) => a.localeCompare(b, 'vi'));
        sortedCorridors.forEach(corrName => {
          const cams = corridorMap.get(corrName)!;
          cams.sort((a, b) => (a.displayName || a.title || '').localeCompare(b.displayName || b.title || '', 'vi'));
          corridors.push({ name: corrName, cameras: cams });
          totalCameras += cams.length;
        });
      }

      const pType = pTypeMap.get(phuongKey) || 'Phường';
      groups.push({
        key: phuongKey,
        displayName: formatPhuongName(phuongKey, lang, pType),
        corridors,
        totalCameras,
      });
    });

    // 2. Fallback group for cameras without phuong_name (Các Phường khác)
    const otherMap = phuongMap.get(OTHER_WARDS_KEY);
    if (otherMap) {
      const corridors: CorridorGroup[] = [];
      let totalCameras = 0;
      const sortedCorridors = Array.from(otherMap.keys()).sort((a, b) => a.localeCompare(b, 'vi'));
      sortedCorridors.forEach(corrName => {
        const cams = otherMap.get(corrName)!;
        cams.sort((a, b) => (a.displayName || a.title || '').localeCompare(b.displayName || b.title || '', 'vi'));
        corridors.push({ name: corrName, cameras: cams });
        totalCameras += cams.length;
      });

      groups.push({
        key: OTHER_WARDS_KEY,
        displayName: t.otherWards,
        corridors,
        totalCameras,
      });
    }

    return { treeData: groups, phuongTypeMap: pTypeMap };
  }, [activeCameras, apiPhuongs, searchTerm, t.otherWards, lang]);

  // When searching, auto-expand matching phuongs and corridors for easy exploration
  useEffect(() => {
    if (searchTerm.trim()) {
      const newPhuongs = new Set<string>();
      const newCorridors = new Set<string>();
      treeData.forEach(p => {
        if (p.totalCameras > 0) {
          newPhuongs.add(p.key);
          p.corridors.forEach(c => {
            if (c.cameras.length > 0) {
              newCorridors.add(`${p.key}::${c.name}`);
            }
          });
        }
      });
      setExpandedPhuongs(newPhuongs);
      setExpandedCorridors(newCorridors);
    }
  }, [searchTerm, treeData]);

  // Auto-expand selectedPhuong when selected
  useEffect(() => {
    if (selectedPhuong) {
      setExpandedPhuongs(prev => new Set(prev).add(selectedPhuong));
    }
  }, [selectedPhuong]);

  const togglePhuongExpand = (key: string, e?: React.MouseEvent) => {
    e?.stopPropagation();
    setExpandedPhuongs(prev => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const toggleCorridorExpand = (corridorId: string, e?: React.MouseEvent) => {
    e?.stopPropagation();
    setExpandedCorridors(prev => {
      const next = new Set(prev);
      if (next.has(corridorId)) next.delete(corridorId);
      else next.add(corridorId);
      return next;
    });
  };

  // City corridor handlers
  useEffect(() => {
    if (searchTerm.trim() && explorerTab === 'corridors') {
      const newCity = new Set<string>();
      cityCorridors.forEach(c => newCity.add(c.name));
      setExpandedCityCorridors(newCity);
    }
  }, [searchTerm, explorerTab, cityCorridors]);

  useEffect(() => {
    if (selectedCorridor) {
      setExpandedCityCorridors(prev => new Set(prev).add(selectedCorridor));
    }
  }, [selectedCorridor]);

  const handleSelectCityCorridor = (corrName: string) => {
    if (selectedCorridor === corrName && !selectedPhuong) {
      onSelectCorridor(null);
    } else {
      onSelectPhuong(null);
      onSelectCorridor(corrName);
      setExpandedCityCorridors(prev => new Set(prev).add(corrName));
    }
  };

  const toggleCityCorridorExpand = (corrName: string, e?: React.MouseEvent) => {
    e?.stopPropagation();
    setExpandedCityCorridors(prev => {
      const next = new Set(prev);
      if (next.has(corrName)) next.delete(corrName);
      else next.add(corrName);
      return next;
    });
  };

  const handleSelectPhuong = (key: string) => {
    if (selectedPhuong === key) {
      // Toggle off
      onSelectPhuong(null);
      onSelectCorridor(null);
    } else {
      onSelectPhuong(key);
      onSelectCorridor(null);
      setExpandedPhuongs(prev => new Set(prev).add(key));
    }
  };

  const handleSelectCorridor = (phuongKey: string, corridorName: string, e: React.MouseEvent) => {
    e.stopPropagation();
    if (selectedPhuong === phuongKey && selectedCorridor === corridorName) {
      onSelectCorridor(null);
    } else {
      onSelectPhuong(phuongKey);
      onSelectCorridor(corridorName);
      const corridorId = `${phuongKey}::${corridorName}`;
      setExpandedCorridors(prev => new Set(prev).add(corridorId));
    }
  };

  const handleClearFilter = () => {
    onSelectPhuong(null);
    onSelectCorridor(null);
  };

  const activeFilterLabel = useMemo(() => {
    if (!selectedPhuong) return null;
    const pType = phuongTypeMap.get(selectedPhuong) || 'Phường';
    const phuongName =
      selectedPhuong === OTHER_WARDS_KEY || selectedPhuong === OUTSIDE_HCMC_KEY
        ? t.otherWards
        : formatPhuongName(selectedPhuong, lang, pType);
    if (selectedCorridor) {
      const corrDisplay = formatCorridorDisplay(selectedCorridor, lang);
      const corrText =
        lang === 'en' && corrDisplay.prefix === I18N_NAMES.en.road_type_duong
          ? `${corrDisplay.name} ${corrDisplay.prefix}`
          : corrDisplay.prefix
          ? `${corrDisplay.prefix} ${corrDisplay.name}`
          : corrDisplay.name;
      return `${phuongName} → ${corrText}`;
    }
    return phuongName;
  }, [selectedPhuong, selectedCorridor, lang, t, phuongTypeMap]);

  return (
    <div className={`flex flex-col h-full bg-zinc-950 font-mono select-none ${className}`}>
      {/* Header with Title, Subtitle, and Language Toggle */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-zinc-800 bg-zinc-900/60 shrink-0">
        <div className="flex flex-col min-w-0 pr-2">
          <span className="font-bold text-white text-xs uppercase tracking-wider truncate">
            {t.title}
          </span>
          <span className="text-[10px] text-zinc-400 truncate">
            {t.subtitle}
          </span>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          <button
            type="button"
            onClick={() => setLang(lang === 'vi' ? 'en' : 'vi')}
            className="text-[10px] uppercase font-bold text-zinc-400 hover:text-white px-1.5 py-0.5 border border-zinc-700 rounded hover:border-zinc-500 transition-colors cursor-pointer"
            title={lang === 'vi' ? 'Switch to English' : 'Chuyển sang Tiếng Việt'}
          >
            {lang === 'vi' ? 'EN' : 'VI'}
          </button>
        </div>
      </div>

      {/* City Flood Status Hero Card */}
      <div className="px-3 py-2 border-b border-zinc-800 bg-zinc-950/80 shrink-0">
        {floodedCameraIds && floodedCameraIds.size > 0 ? (
          <div className="p-2.5 rounded-lg border border-red-500/40 bg-red-950/20 text-xs">
            <div className="flex items-center gap-2">
              <span className="relative flex h-2 w-2 shrink-0">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-red-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-2 w-2 bg-red-500" />
              </span>
              <span className="font-bold text-red-300">
                {t.floodStatusAlert(floodedCameraIds.size)}
              </span>
            </div>
            {onToggleFloodFilter && (
              <button
                type="button"
                onClick={onToggleFloodFilter}
                className={`mt-2 w-full py-1 px-2 rounded text-[11px] font-semibold transition-all border flex items-center justify-center gap-1.5 cursor-pointer ${
                  isFloodFilterActive
                    ? 'bg-red-500 text-white border-red-400 shadow-sm'
                    : 'bg-red-950/60 hover:bg-red-900/60 text-red-200 border-red-800/60'
                }`}
              >
                <span>
                  {isFloodFilterActive
                    ? t.floodFilterActiveBtn(floodedCameraIds.size)
                    : t.floodFilterInactiveBtn(floodedCameraIds.size)}
                </span>
              </button>
            )}
          </div>
        ) : (
          <div className="flex items-center gap-2 px-2.5 py-1.5 rounded-lg border border-zinc-800 bg-zinc-900/40 text-[11px] text-zinc-300">
            <span className="w-2 h-2 rounded-full bg-emerald-400 shrink-0" />
            <span className="font-medium text-emerald-400 truncate">
              {t.floodStatusDry}
            </span>
          </div>
        )}
      </div>

      {/* Explorer Tab Switch: [Theo tuyến đường] vs [Theo phường] */}
      <div className="grid grid-cols-2 border-b border-zinc-800 bg-zinc-950 text-xs font-mono shrink-0">
        <button
          type="button"
          onClick={() => setExplorerTab('corridors')}
          className={`py-2 text-center font-bold transition-all border-b-2 flex items-center justify-center gap-1.5 ${
            explorerTab === 'corridors'
              ? 'text-white border-b-white bg-zinc-900/60'
              : 'text-zinc-500 border-b-transparent hover:text-zinc-300 hover:bg-zinc-900/20'
          }`}
          aria-pressed={explorerTab === 'corridors'}
        >
          <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <line x1="4" y1="21" x2="4" y2="3" />
            <line x1="20" y1="21" x2="20" y2="3" />
            <line x1="12" y1="5" x2="12" y2="7" />
            <line x1="12" y1="11" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12" y2="19" />
          </svg>
          <span>{t.tabCorridors}</span>
        </button>
        <button
          type="button"
          onClick={() => setExplorerTab('phuongs')}
          className={`py-2 text-center font-bold transition-all border-b-2 flex items-center justify-center gap-1.5 ${
            explorerTab === 'phuongs'
              ? 'text-white border-b-white bg-zinc-900/60'
              : 'text-zinc-500 border-b-transparent hover:text-zinc-300 hover:bg-zinc-900/20'
          }`}
          aria-pressed={explorerTab === 'phuongs'}
        >
          <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M20 10c0 6-8 12-8 12s-8-6-8-12a8 8 0 0 1 16 0Z" />
            <circle cx="12" cy="10" r="3" />
          </svg>
          <span>{t.tabPhuongs}</span>
        </button>
      </div>

      {/* Prominent Search Bar with SVG Search Icon & Reset Button */}
      {onSearchChange && (
        <div className="p-2.5 border-b border-zinc-800 bg-zinc-950/70 shrink-0">
          <div className="relative flex items-center bg-zinc-900 border border-zinc-700/80 rounded-md px-2.5 py-1.5 focus-within:border-zinc-400 focus-within:ring-1 focus-within:ring-zinc-400/30 transition-all shadow-inner group">
            {/* SVG Search Icon */}
            <svg
              className="w-3.5 h-3.5 text-zinc-400 group-focus-within:text-white mr-2 shrink-0 transition-colors pointer-events-none"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <circle cx="11" cy="11" r="7" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>

            {/* Search Input */}
            <input
              ref={searchInputRef}
              type="text"
              placeholder={t.searchPlaceholder}
              value={searchTerm}
              onChange={e => onSearchChange(e.target.value)}
              className="w-full bg-transparent text-xs text-white placeholder-zinc-500 focus:outline-none font-mono"
              autoComplete="off"
              spellCheck={false}
            />

            {/* SVG Reset / Clear Button */}
            {searchTerm && (
              <button
                type="button"
                onClick={() => onSearchChange('')}
                className="ml-1 text-zinc-400 hover:text-white p-0.5 rounded hover:bg-zinc-800 transition-colors shrink-0 focus:outline-none"
                aria-label={lang === 'vi' ? 'Xóa từ khóa tìm kiếm' : 'Clear search keyword'}
                title={lang === 'vi' ? 'Xóa tìm kiếm' : 'Clear search'}
              >
                <svg
                  className="w-3.5 h-3.5"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  viewBox="0 0 24 24"
                >
                  <circle cx="12" cy="12" r="9" className="opacity-25 fill-zinc-700" />
                  <path d="M15 9l-6 6M9 9l6 6" />
                </svg>
              </button>
            )}
          </div>
        </div>
      )}

      {/* Quick Flood Filter Chip */}
      {floodedCameraIds && floodedCameraIds.size > 0 && (
        <div className="px-2.5 pb-2 pt-1 bg-zinc-950/70 border-b border-zinc-800/80 flex items-center justify-between shrink-0">
          <button
            type="button"
            onClick={onToggleFloodFilter}
            className={`px-2.5 py-1 rounded text-[11px] font-semibold transition-all border flex items-center gap-1.5 cursor-pointer ${
              isFloodFilterActive
                ? 'bg-blue-600 text-white border-blue-500 font-bold shadow-sm'
                : 'bg-zinc-900 text-zinc-300 border-zinc-800 hover:text-blue-300 hover:border-blue-500/50'
            }`}
            title={lang === 'vi' ? 'Lọc danh sách chỉ hiển thị camera ngập nước' : 'Filter camera list to flooded cameras'}
          >
            <svg className="w-3.5 h-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M12 2.69l5.66 5.66a8 8 0 1 1-11.31 0z" />
            </svg>
            <span>{FLOOD_I18N.FILTER_CHIP[lang](floodedCameraIds.size)}</span>
          </button>
          {isFloodFilterActive && (
            <button
              type="button"
              onClick={onToggleFloodFilter}
              className="px-2 py-0.5 rounded text-[11px] font-bold text-red-200 bg-red-950/80 border border-red-500/60 hover:bg-red-900 hover:text-white transition-all font-mono cursor-pointer flex items-center gap-1 shadow-sm active:scale-95"
              title={lang === 'vi' ? 'Xóa lọc' : 'Clear filter'}
            >
              <span className="font-bold text-xs leading-none">✕</span>
              <span>{lang === 'vi' ? 'Xóa lọc' : 'Clear'}</span>
            </button>
          )}
        </div>
      )}

      {/* Quick Arterial Corridors Header & Expand/Collapse Toggle */}
      <div className="px-3 pt-2 pb-1 bg-zinc-950 border-b border-zinc-900 flex items-center justify-between shrink-0">
        <span className="text-[10px] text-zinc-500 font-bold uppercase tracking-wider">
          {t.quickArterials} ({ARTERIAL_CORRIDORS.length})
        </span>
        <button
          type="button"
          onClick={() => setIsArterialsExpanded(!isArterialsExpanded)}
          className="text-[10.5px] text-zinc-400 hover:text-white transition-colors flex items-center gap-1 font-medium cursor-pointer"
        >
          <span>{isArterialsExpanded ? t.collapse : t.expandAll}</span>
          <svg
            className={`w-3 h-3 transition-transform duration-200 ${isArterialsExpanded ? 'rotate-180' : ''}`}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <polyline points="6 9 12 15 18 9" />
          </svg>
        </button>
      </div>

      {/* Arterials Content: Compact Horizontal Scroll vs Expanded 2-Column Grid */}
      {!isArterialsExpanded ? (
        <div className="relative px-2.5 pb-2 pt-1 border-b border-zinc-800/80 bg-zinc-950 shrink-0 group">
          {canScrollLeft && (
            <button
              type="button"
              onClick={() => scrollArterials('left')}
              className="absolute left-1 top-1/2 -translate-y-1/2 z-10 w-5 h-6 bg-zinc-900/90 hover:bg-zinc-800 text-zinc-300 rounded-r flex items-center justify-center border-y border-r border-zinc-700 shadow-md text-xs cursor-pointer"
              aria-label="Scroll left"
            >
              ‹
            </button>
          )}

          <div
            ref={scrollContainerRef}
            onScroll={checkScrollButtons}
            className="flex items-center gap-1.5 overflow-x-auto py-0.5 scroll-smooth"
            style={{ scrollbarWidth: 'none' }}
          >
            {ARTERIAL_CORRIDORS.map(corrName => {
              const isSelected = selectedCorridor === corrName && !selectedPhuong;
              const status = arterialStatusMap.get(corrName);
              return (
                <button
                  key={corrName}
                  type="button"
                  title={corrName}
                  onClick={() => handleSelectCityCorridor(corrName)}
                  className={`px-2.5 py-1 rounded text-[11px] font-semibold whitespace-nowrap transition-colors border shrink-0 flex items-center gap-1.5 cursor-pointer ${
                    isSelected
                      ? 'bg-white text-black border-white font-bold'
                      : 'bg-zinc-900 text-zinc-300 border-zinc-800 hover:text-white hover:border-zinc-600'
                  }`}
                >
                  {status?.congested ? (
                    <span className="w-1.5 h-1.5 rounded-full bg-red-500 animate-pulse shrink-0" />
                  ) : status?.moderate ? (
                    <span className="w-1.5 h-1.5 rounded-full bg-yellow-500 shrink-0" />
                  ) : null}
                  <span>{corrName}</span>
                </button>
              );
            })}
          </div>

          {canScrollRight && (
            <>
              <div className="pointer-events-none absolute right-0 top-0 bottom-2 w-8 bg-gradient-to-l from-zinc-950 via-zinc-950/80 to-transparent" />
              <button
                type="button"
                onClick={() => scrollArterials('right')}
                className="absolute right-1 top-1/2 -translate-y-1/2 z-10 w-5 h-6 bg-zinc-900/90 hover:bg-zinc-800 text-zinc-300 rounded-l flex items-center justify-center border-y border-l border-zinc-700 shadow-md text-xs cursor-pointer"
                aria-label="Scroll right"
              >
                ›
              </button>
            </>
          )}
        </div>
      ) : (
        <motion.div
          initial={{ opacity: 0, height: 0 }}
          animate={{ opacity: 1, height: 'auto' }}
          exit={{ opacity: 0, height: 0 }}
          className="px-2.5 pb-2.5 pt-1.5 border-b border-zinc-800/80 bg-zinc-950 shrink-0"
        >
          <div className="grid grid-cols-2 gap-1.5 max-h-56 overflow-y-auto pr-1">
            {ARTERIAL_CORRIDORS.map(corrName => {
              const isSelected = selectedCorridor === corrName && !selectedPhuong;
              const status = arterialStatusMap.get(corrName);
              return (
                <button
                  key={corrName}
                  type="button"
                  title={corrName}
                  onClick={() => handleSelectCityCorridor(corrName)}
                  className={`px-2 py-1.5 rounded text-[10.5px] font-semibold transition-colors border flex items-center justify-between text-left cursor-pointer ${
                    isSelected
                      ? 'bg-white text-black border-white font-bold'
                      : 'bg-zinc-900/90 text-zinc-300 border-zinc-800 hover:text-white hover:border-zinc-600'
                  }`}
                >
                  <div className="flex items-center gap-1.5 truncate mr-1">
                    {status?.congested ? (
                      <span className="w-1.5 h-1.5 rounded-full bg-red-500 shrink-0 animate-pulse" />
                    ) : status?.moderate ? (
                      <span className="w-1.5 h-1.5 rounded-full bg-yellow-500 shrink-0" />
                    ) : null}
                    <span className="truncate">{corrName}</span>
                  </div>
                  {status?.count ? (
                    <span
                      className={`text-[9.5px] px-1 rounded shrink-0 font-normal ${
                        isSelected ? 'bg-zinc-200 text-zinc-900' : 'bg-zinc-800 text-zinc-400'
                      }`}
                    >
                      {status.count}
                    </span>
                  ) : null}
                </button>
              );
            })}
          </div>
        </motion.div>
      )}

      {/* Active Filter Bar (if a Phường or Corridor is selected) */}
      {activeFilterLabel && (
        <div className="flex items-center justify-between px-3 py-1.5 bg-zinc-900 border-b border-zinc-800 text-[11px] text-zinc-300 shrink-0 gap-2">
          <div className="flex items-center gap-1.5 min-w-0 flex-1">
            <span className="text-zinc-500 shrink-0">{t.activeFilter}</span>
            <span className="text-white font-bold whitespace-normal break-words leading-tight">{activeFilterLabel}</span>
          </div>
          <button
            type="button"
            onClick={handleClearFilter}
            className="px-2 py-0.5 rounded text-[10.5px] font-bold text-red-200 bg-red-950/80 border border-red-500/60 hover:bg-red-900 hover:text-white transition-all shrink-0 cursor-pointer flex items-center gap-1 shadow-sm active:scale-95"
            title={lang === 'vi' ? 'Xóa lọc' : 'Clear filter'}
          >
            <span className="font-bold text-xs leading-none">✕</span>
            <span>{t.clearFilter}</span>
          </button>
        </div>
      )}

      {/* Tree Content */}
      <div className="flex-1 overflow-y-auto p-1.5 space-y-1">
        {error ? (
          <p className="text-xs text-red-500 p-3 font-mono">
            {lang === 'vi' ? 'Lỗi tải camera:' : 'Error loading cameras:'} {error.message}
          </p>
        ) : (camerasLoading || phuongsLoading) && treeData.length === 0 ? (
          <p className="text-xs text-yellow-500 p-3 font-mono">
            {lang === 'vi' ? 'Đang tải dữ liệu...' : 'Loading data...'}
          </p>
        ) : explorerTab === 'corridors' ? (
          cityCorridors.length === 0 ? (
            <p className="text-xs text-zinc-500 p-3 text-center">{t.noCorridors}</p>
          ) : (
            cityCorridors.map(corr => {
              const isSelected = selectedCorridor === corr.name && !selectedPhuong;
              const isExpanded = expandedCityCorridors.has(corr.name);
              const corrDisplay = formatCorridorDisplay(corr.name, lang);
              const corrRoadKey = (corr.cameras as Camera[]).find(c => c.road_key)?.road_key || '';
              const worstSev = getCorridorWorstVerdict(corr.cameras as Camera[], verdicts);
              const corridorTooltip = worstSev
                ? `${corr.name} • ${worstSev}`
                : corr.name;

              return (
                <div
                  key={`city-corr-${corr.name}`}
                  className="border border-zinc-800/80 rounded bg-zinc-900/30 overflow-hidden transition-colors"
                >
                  <div
                    onClick={() => handleSelectCityCorridor(corr.name)}
                    className={`w-full flex items-center justify-between px-2.5 py-2 cursor-pointer transition-colors ${
                      isSelected
                        ? 'bg-zinc-800 text-white font-bold border-l-2 border-l-white'
                        : 'hover:bg-zinc-800/60 text-zinc-200'
                    }`}
                    title={corridorTooltip}
                  >
                    <div className="flex items-center gap-2 truncate flex-1 min-w-0">
                      <button
                        type="button"
                        onClick={e => toggleCityCorridorExpand(corr.name, e)}
                        className="text-zinc-400 hover:text-white text-xs w-4 h-4 flex items-center justify-center shrink-0 focus:outline-none"
                        aria-label={lang === 'vi' ? 'Đóng/mở' : 'Toggle corridor'}
                      >
                        {isExpanded ? '▾' : '▸'}
                      </button>
                      {/* Severity badge dot: only shown when corridor has real, non-stale verdict */}
                      {worstSev === 'CONGESTED' && (
                        <span
                          className="w-2 h-2 rounded-full bg-rose-500 shrink-0 shadow-sm"
                          title={lang === 'vi' ? 'Ùn tắc' : 'Congested'}
                        />
                      )}
                      {worstSev === 'MODERATE' && (
                        <span
                          className="w-2 h-2 rounded-full bg-amber-400 shrink-0 shadow-sm"
                          title={lang === 'vi' ? 'Đông xe' : 'Moderate'}
                        />
                      )}
                      {worstSev === 'CLEAR' && (
                        <span
                          className="w-2 h-2 rounded-full bg-emerald-400 shrink-0 shadow-sm"
                          title={lang === 'vi' ? 'Thông thoáng' : 'Clear'}
                        />
                      )}
                      <span className="text-xs truncate font-semibold">
                        {lang === 'en' && corrDisplay.prefix === I18N_NAMES.en.road_type_duong ? (
                          <>
                            <span>{corrDisplay.name}</span>
                            <span className="text-zinc-500 ml-1">{corrDisplay.prefix}</span>
                          </>
                        ) : (
                          <>
                            {corrDisplay.prefix && (
                              <span className="text-zinc-500 mr-1">{corrDisplay.prefix}</span>
                            )}
                            <span>{corrDisplay.name}</span>
                          </>
                        )}
                      </span>
                    </div>

                    <div className="flex items-center gap-1.5 shrink-0 ml-2">
                      {corr.congestedCount > 0 && (
                        <span className="text-[9.5px] font-bold text-red-400 bg-red-950/50 border border-red-800/60 px-1.5 py-0.5 rounded flex items-center gap-1">
                          <span className="w-1.5 h-1.5 rounded-full bg-red-400 animate-pulse" />
                          <span>{corr.congestedCount} {t.congestedTag}</span>
                        </span>
                      )}
                      {corr.congestedCount === 0 && corr.moderateCount > 0 && (
                        <span className="text-[9.5px] font-bold text-yellow-400 bg-yellow-950/50 border border-yellow-800/60 px-1.5 py-0.5 rounded">
                          {corr.moderateCount} {t.moderateTag}
                        </span>
                      )}
                      <span className="text-[10px] text-zinc-400 bg-zinc-950 px-1.5 py-0.5 rounded border border-zinc-800">
                        {corr.totalCameras}
                      </span>
                    </div>
                  </div>

                  {/* Expanded Cameras */}
                  <AnimatePresence initial={false}>
                    {isExpanded && (
                      <motion.div
                        key={`city-corr-cams-${corr.name}`}
                        initial={{ height: 0, opacity: 0 }}
                        animate={{ height: 'auto', opacity: 1 }}
                        exit={{ height: 0, opacity: 0 }}
                        transition={{ duration: 0.18, ease: 'easeInOut' }}
                        className="overflow-hidden bg-black/40 border-t border-zinc-800/60 ml-6 my-0.5 space-y-0.5"
                      >
                        <CorridorDetailHeader roadKey={corrRoadKey} lang={lang} />
                        <div className="py-1 space-y-0.5">
                          {corr.cameras.map(cam => (
                            <div
                              key={cam.id}
                              onClick={e => {
                                e.stopPropagation();
                                onCameraClick(cam as Camera);
                              }}
                              className="flex items-center justify-between px-2.5 py-1 text-[11px] text-zinc-300 hover:text-white hover:bg-zinc-800/70 active:bg-zinc-700 cursor-pointer rounded transition-colors group"
                              title={cam.displayName || cam.title}
                            >
                              <div className="flex items-center gap-2 truncate flex-1 min-w-0">
                                <span
                                  className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                                    (cam as Camera).isOnline ? 'bg-white' : 'bg-zinc-600'
                                  }`}
                                />
                                <span className="truncate">
                                  {cam.displayName || cam.title}
                                </span>
                              </div>
                              {(cam as Camera).isGhost && (
                                <span className="text-[9px] text-amber-500 shrink-0 ml-1 font-bold">
                                  [GHOST]
                                </span>
                              )}
                            </div>
                          ))}
                        </div>
                      </motion.div>
                    )}
                  </AnimatePresence>
                </div>
              );
            })
          )
        ) : treeData.length === 0 ? (
          <p className="text-xs text-zinc-500 p-3 text-center">{t.noCameras}</p>
        ) : (
          treeData.map(group => {
            const isExpanded = expandedPhuongs.has(group.key);
            const isPhuongSelected = selectedPhuong === group.key;

            return (
              <div
                key={group.key}
                className="border border-zinc-800/80 rounded bg-zinc-900/30 overflow-hidden transition-colors"
              >
                {/* TIER 1: Phường Header */}
                <div
                  onClick={() => handleSelectPhuong(group.key)}
                  className={`w-full flex items-center justify-between px-2.5 py-2 cursor-pointer transition-colors ${
                    isPhuongSelected
                      ? 'bg-zinc-800 text-white font-bold border-l-2 border-l-white'
                      : 'hover:bg-zinc-800/60 text-zinc-200'
                  }`}
                >
                  <div className="flex items-center gap-2 truncate flex-1 min-w-0">
                    <button
                      type="button"
                      onClick={e => togglePhuongExpand(group.key, e)}
                      className="text-zinc-400 hover:text-white text-xs w-4 h-4 flex items-center justify-center shrink-0 focus:outline-none"
                      aria-label={lang === 'vi' ? 'Đóng/mở' : 'Toggle ward'}
                    >
                      {isExpanded ? '▾' : '▸'}
                    </button>
                    <span className="text-xs truncate">{group.displayName}</span>
                  </div>
                  <span className="text-[10px] text-zinc-400 bg-zinc-950 px-1.5 py-0.5 rounded border border-zinc-800 shrink-0 ml-2">
                    {group.totalCameras}
                  </span>
                </div>

                {/* TIER 2: Corridors List */}
                <AnimatePresence initial={false}>
                  {isExpanded && (
                    <motion.div
                      key={`phuong-content-${group.key}`}
                      initial={{ height: 0, opacity: 0 }}
                      animate={{ height: 'auto', opacity: 1 }}
                      exit={{ height: 0, opacity: 0 }}
                      transition={{ duration: 0.18, ease: 'easeInOut' }}
                      className="overflow-hidden bg-zinc-950/60 border-t border-zinc-800/60"
                    >
                      {group.corridors.length === 0 ? (
                        <p className="text-[11px] text-zinc-500 py-1.5 px-6 italic">
                          {t.noCameras}
                        </p>
                      ) : (
                        <div className="py-1 space-y-0.5">
                          {group.corridors.map(corr => {
                            const corridorId = `${group.key}::${corr.name}`;
                            const isCorrExpanded = expandedCorridors.has(corridorId);
                            const isCorrSelected =
                              isPhuongSelected && selectedCorridor === corr.name;
                            const corrDisplay = formatCorridorDisplay(corr.name, lang);
                            const wardCorrRoadKey = corr.cameras.find(c => c.road_key)?.road_key || '';
                            const wardWorstSev = getCorridorWorstVerdict(corr.cameras, verdicts);
                            const wardCorridorTooltip = wardWorstSev
                              ? `${corr.name} • ${wardWorstSev}`
                              : corr.name;

                            return (
                              <div key={corridorId} className="flex flex-col">
                                {/* TIER 2 Header */}
                                <div
                                  onClick={e => handleSelectCorridor(group.key, corr.name, e)}
                                  className={`flex items-center justify-between pl-6 pr-2.5 py-1.5 cursor-pointer text-xs transition-colors ${
                                    isCorrSelected
                                      ? 'bg-zinc-800 text-white font-semibold'
                                      : 'hover:bg-zinc-900 text-zinc-300'
                                  }`}
                                  title={wardCorridorTooltip}
                                >
                                  <div className="flex items-center gap-1.5 truncate flex-1 min-w-0">
                                    <button
                                      type="button"
                                      onClick={e => toggleCorridorExpand(corridorId, e)}
                                      className="text-zinc-500 hover:text-white text-[11px] w-3.5 h-3.5 flex items-center justify-center shrink-0 focus:outline-none"
                                      aria-label={lang === 'vi' ? 'Đóng/mở đường' : 'Toggle corridor'}
                                    >
                                      {isCorrExpanded ? '▾' : '▸'}
                                    </button>
                                    {/* Severity badge dot: only shown when corridor has real, non-stale verdict */}
                                    {wardWorstSev === 'CONGESTED' && (
                                      <span
                                        className="w-1.5 h-1.5 rounded-full bg-rose-500 shrink-0"
                                        title={lang === 'vi' ? 'Ùn tắc' : 'Congested'}
                                      />
                                    )}
                                    {wardWorstSev === 'MODERATE' && (
                                      <span
                                        className="w-1.5 h-1.5 rounded-full bg-amber-400 shrink-0"
                                        title={lang === 'vi' ? 'Đông xe' : 'Moderate'}
                                      />
                                    )}
                                    {wardWorstSev === 'CLEAR' && (
                                      <span
                                        className="w-1.5 h-1.5 rounded-full bg-emerald-400 shrink-0"
                                        title={lang === 'vi' ? 'Thông thoáng' : 'Clear'}
                                      />
                                    )}
                                    <span className="truncate">
                                      {lang === 'en' && corrDisplay.prefix === I18N_NAMES.en.road_type_duong ? (
                                        <>
                                          <span>{corrDisplay.name}</span>
                                          <span className="text-zinc-500 ml-1">{corrDisplay.prefix}</span>
                                        </>
                                      ) : (
                                        <>
                                          {corrDisplay.prefix && (
                                            <span className="text-zinc-500 mr-1">{corrDisplay.prefix}</span>
                                          )}
                                          <span>{corrDisplay.name}</span>
                                        </>
                                      )}
                                    </span>
                                  </div>
                                  <span className="text-[10px] text-zinc-500 shrink-0 ml-2">
                                    {corr.cameras.length}
                                  </span>
                                </div>

                                {/* TIER 3: Cameras List */}
                                <AnimatePresence initial={false}>
                                  {isCorrExpanded && (
                                    <motion.div
                                      key={`corr-content-${corridorId}`}
                                      initial={{ height: 0, opacity: 0 }}
                                      animate={{ height: 'auto', opacity: 1 }}
                                      exit={{ height: 0, opacity: 0 }}
                                      transition={{ duration: 0.15, ease: 'easeInOut' }}
                                      className="overflow-hidden bg-black/40 border-l border-zinc-800/80 ml-8 my-0.5 space-y-0.5"
                                    >
                                      <CorridorDetailHeader roadKey={wardCorrRoadKey} lang={lang} />
                                      {corr.cameras.map(cam => (
                                        <div
                                          key={cam.id}
                                          onClick={() => onCameraClick(cam)}
                                          className="flex items-center justify-between px-2.5 py-1 text-[11px] text-zinc-300 hover:text-white hover:bg-zinc-800/70 active:bg-zinc-700 cursor-pointer rounded transition-colors group"
                                          title={cam.displayName || cam.title}
                                        >
                                          <div className="flex items-center gap-2 truncate flex-1 min-w-0">
                                            <span
                                              className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                                                cam.isOnline ? 'bg-white' : 'bg-zinc-600'
                                              }`}
                                            />
                                            <span className="truncate">
                                              {cam.displayName || cam.title}
                                            </span>
                                          </div>
                                          {cam.isGhost && (
                                            <span className="text-[9px] text-amber-500 shrink-0 ml-1 font-bold">
                                              [GHOST]
                                            </span>
                                          )}
                                        </div>
                                      ))}
                                    </motion.div>
                                  )}
                                </AnimatePresence>
                              </div>
                            );
                          })}
                        </div>
                      )}
                    </motion.div>
                  )}
                </AnimatePresence>
              </div>
            );
          })
        )}
      </div>

      {/* Footer / Credits */}
      <div className="px-3 py-2 border-t border-zinc-900 bg-zinc-950 text-[10.5px] text-zinc-500 text-center shrink-0 font-mono">
        Developed by{' '}
        <a
          href="https://thdatvu.vercel.app/"
          target="_blank"
          rel="noopener noreferrer"
          className="text-zinc-400 hover:text-white underline underline-offset-2 transition-colors font-medium"
        >
          ThDat Vu
        </a>
        {' and '}
        <a
          href="https://github.com/giangdq202"
          target="_blank"
          rel="noopener noreferrer"
          className="text-zinc-400 hover:text-white underline underline-offset-2 transition-colors font-medium"
        >
          Dam Quang Giang
        </a>
      </div>
    </div>
  );
}
