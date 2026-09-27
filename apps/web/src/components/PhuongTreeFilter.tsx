'use client';

import { useState, useMemo, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Camera } from '../hooks/useCameras';
import { usePhuongs } from '../hooks/usePhuongs';

export const OUTSIDE_HCMC_KEY = '__OUTSIDE_HCMC__';

export interface PhuongTreeFilterProps {
  cameras: Camera[];
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

/**
 * Extracts primary street name from camera displayName (split on ' - ').
 */
export function extractCorridor(displayName?: string, title?: string): string {
  const raw = (displayName || title || '').trim();
  if (!raw) return 'Khác';
  const parts = raw.split(' - ');
  return parts[0].trim() || raw;
}

export default function PhuongTreeFilter({
  cameras,
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
}: PhuongTreeFilterProps) {
  const [internalLang, setInternalLang] = useState<'vi' | 'en'>('vi');
  const lang = controlledLang ?? internalLang;
  const setLang = onLangChange ?? setInternalLang;

  const { phuongs: apiPhuongs, loading: phuongsLoading } = usePhuongs();

  const [expandedPhuongs, setExpandedPhuongs] = useState<Set<string>>(new Set());
  const [expandedCorridors, setExpandedCorridors] = useState<Set<string>>(new Set());

  // i18n text resources
  const t = useMemo(() => {
    return {
      title: lang === 'vi' ? 'Lọc theo Phường' : 'Filter by Ward',
      street: lang === 'vi' ? 'Đường' : 'Street',
      outsideHcm: lang === 'vi' ? 'Ngoài TP.HCM' : 'Outside HCMC',
      noCameras: lang === 'vi' ? 'Không có camera' : 'No cameras',
      allCameras: lang === 'vi' ? 'Tất cả camera' : 'All cameras',
      searchPlaceholder: lang === 'vi' ? 'Tìm kiếm camera, đường, phường...' : 'Search camera, street, ward...',
      activeFilter: lang === 'vi' ? 'Đang lọc:' : 'Filter:',
      clearFilter: lang === 'vi' ? 'Xóa lọc' : 'Clear',
      cameraCount: (n: number) => (lang === 'vi' ? `${n} camera` : `${n} ${n === 1 ? 'camera' : 'cameras'}`),
    };
  }, [lang]);

  // Group cameras into: Phường → Đường → Camera
  const treeData = useMemo(() => {
    const term = searchTerm.trim().toLowerCase();

    // Map: phuongKey -> Map: corridorName -> Camera[]
    const phuongMap = new Map<string, Map<string, Camera[]>>();

    cameras.forEach(cam => {
      const phuongKey = cam.phuong_name ? cam.phuong_name.trim() : OUTSIDE_HCMC_KEY;
      const corridor = extractCorridor(cam.displayName, cam.title);

      // Search filtering
      if (term) {
        const matchName = (cam.displayName || '').toLowerCase().includes(term);
        const matchTitle = (cam.title || '').toLowerCase().includes(term);
        const matchCorridor = corridor.toLowerCase().includes(term);
        const matchPhuong = (cam.phuong_name || t.outsideHcm).toLowerCase().includes(term);
        if (!matchName && !matchTitle && !matchCorridor && !matchPhuong) {
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
    apiPhuongs.forEach(p => allPhuongKeys.add(p.trim()));
    cameras.forEach(c => {
      if (c.phuong_name) allPhuongKeys.add(c.phuong_name.trim());
    });

    const groups: PhuongGroup[] = [];

    // 1. Known Phườngs sorted alphabetically
    const sortedPhuongs = Array.from(allPhuongKeys).sort((a, b) => a.localeCompare(b, 'vi'));

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

      groups.push({
        key: phuongKey,
        displayName: phuongKey,
        corridors,
        totalCameras,
      });
    });

    // 2. Fallback group for cameras with null/empty phuong_name (Ngoài TP.HCM)
    const outsideMap = phuongMap.get(OUTSIDE_HCMC_KEY);
    if (outsideMap) {
      const corridors: CorridorGroup[] = [];
      let totalCameras = 0;
      const sortedCorridors = Array.from(outsideMap.keys()).sort((a, b) => a.localeCompare(b, 'vi'));
      sortedCorridors.forEach(corrName => {
        const cams = outsideMap.get(corrName)!;
        cams.sort((a, b) => (a.displayName || a.title || '').localeCompare(b.displayName || b.title || '', 'vi'));
        corridors.push({ name: corrName, cameras: cams });
        totalCameras += cams.length;
      });

      groups.push({
        key: OUTSIDE_HCMC_KEY,
        displayName: t.outsideHcm,
        corridors,
        totalCameras,
      });
    }

    return groups;
  }, [cameras, apiPhuongs, searchTerm, t.outsideHcm]);

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
    const phuongName = selectedPhuong === OUTSIDE_HCMC_KEY ? t.outsideHcm : selectedPhuong;
    if (selectedCorridor) {
      return `${phuongName} → ${t.street} ${selectedCorridor}`;
    }
    return phuongName;
  }, [selectedPhuong, selectedCorridor, t]);

  return (
    <div className={`flex flex-col h-full bg-zinc-950 font-mono select-none ${className}`}>
      {/* Header with Title and Language Toggle */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-zinc-800 bg-zinc-900/60 shrink-0">
        <span className="font-bold text-white text-xs uppercase tracking-wider">
          {t.title}
        </span>
        <div className="flex items-center gap-1.5">
          <button
            type="button"
            onClick={() => setLang(lang === 'vi' ? 'en' : 'vi')}
            className="text-[10px] uppercase font-bold text-zinc-400 hover:text-white px-1.5 py-0.5 border border-zinc-700 rounded hover:border-zinc-500 transition-colors"
            title={lang === 'vi' ? 'Switch to English' : 'Chuyển sang Tiếng Việt'}
          >
            {lang === 'vi' ? 'EN' : 'VI'}
          </button>
        </div>
      </div>

      {/* Optional Search Bar */}
      {onSearchChange && (
        <div className="p-2 border-b border-zinc-800 bg-zinc-900/30 shrink-0">
          <input
            type="text"
            placeholder={t.searchPlaceholder}
            value={searchTerm}
            onChange={e => onSearchChange(e.target.value)}
            className="w-full bg-zinc-950 border border-zinc-700 rounded px-2.5 py-1 text-xs text-white placeholder-zinc-500 focus:outline-none focus:border-zinc-500 font-mono"
            autoComplete="off"
          />
        </div>
      )}

      {/* Active Filter Bar (if a Phường or Corridor is selected) */}
      {activeFilterLabel && (
        <div className="flex items-center justify-between px-3 py-1.5 bg-zinc-900 border-b border-zinc-800 text-[11px] text-zinc-300 shrink-0">
          <div className="flex items-center gap-1.5 truncate mr-2">
            <span className="text-zinc-500 shrink-0">{t.activeFilter}</span>
            <span className="text-white font-bold truncate">{activeFilterLabel}</span>
          </div>
          <button
            type="button"
            onClick={handleClearFilter}
            className="text-zinc-400 hover:text-white text-[10px] underline shrink-0 cursor-pointer"
          >
            {t.clearFilter}
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
                      aria-label="Đóng/mở"
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
                                >
                                  <div className="flex items-center gap-1.5 truncate flex-1 min-w-0">
                                    <button
                                      type="button"
                                      onClick={e => toggleCorridorExpand(corridorId, e)}
                                      className="text-zinc-500 hover:text-white text-[11px] w-3.5 h-3.5 flex items-center justify-center shrink-0 focus:outline-none"
                                      aria-label="Đóng/mở đường"
                                    >
                                      {isCorrExpanded ? '▾' : '▸'}
                                    </button>
                                    <span className="truncate">
                                      <span className="text-zinc-500 mr-1">{t.street}</span>
                                      {corr.name}
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
    </div>
  );
}
