'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Camera } from '../hooks/useCameras';
import {
  API_ENDPOINTS,
  getApiUrl,
  PINNED_DOCK_I18N,
  PINNED_DOCK_CAP,
} from '../config/constants';

export type DockSizePreset = 'S' | 'M' | 'L';
export type DockViewMode = 'strip' | 'grid';

export interface PinnedDockProps {
  pinnedCameras: Camera[];
  onUnpin: (id: string) => void;
  onClear: () => void;
  onReorder: (newOrder: string[]) => void;
  onCameraClick?: (cam: Camera) => void;
  lang?: 'vi' | 'en';
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  verdicts?: Map<string, any>;
  pinNotice?: string | null;
}

interface TileMeta {
  mountedAt: number;
  lastLoadedAt: number;
}

export default function PinnedDock({
  pinnedCameras,
  onUnpin,
  onClear,
  onReorder,
  onCameraClick,
  lang = 'vi',
  verdicts,
  pinNotice,
}: PinnedDockProps) {
  // Mobile drawer state
  const [isMobileExpanded, setIsMobileExpanded] = useState(false);
  // Desktop dock collapsed state
  const [isDesktopCollapsed, setIsDesktopCollapsed] = useState(false);

  // Size preset & view mode (persisted in localStorage)
  const [sizePreset, setSizePreset] = useState<DockSizePreset>('M');
  const [viewMode, setViewMode] = useState<DockViewMode>('strip');

  // Drag-to-reorder state via Pointer Events
  const [draggedId, setDraggedId] = useState<string | null>(null);
  const [dragOverId, setDragOverId] = useState<string | null>(null);

  // Controlled snapshot image refresh timestamp (15s cadence, matching backend proxy cache TTL)
  const [snapshotRefreshTime, setSnapshotRefreshTime] = useState<number>(Date.now());
  // Age tracker (ticks every 5 seconds for elapsed time display only)
  const [now, setNow] = useState<number>(Date.now());
  const tilesMetaRef = useRef<Map<string, TileMeta>>(new Map());

  // Drag tracking refs
  const pointerStartPos = useRef<{ x: number; y: number }>({ x: 0, y: 0 });
  const isDraggingRef = useRef(false);
  const draggedCamIdRef = useRef<string | null>(null);

  // Initialize preferences from localStorage
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const savedSize = localStorage.getItem('warded_dock_size') as DockSizePreset | null;
    if (savedSize && (savedSize === 'S' || savedSize === 'M' || savedSize === 'L')) {
      setSizePreset(savedSize);
    }
    const savedView = localStorage.getItem('warded_dock_view') as DockViewMode | null;
    if (savedView && (savedView === 'strip' || savedView === 'grid')) {
      setViewMode(savedView);
    }
  }, []);

  // Timer to refresh snapshot image URLs every 15s (prevents cache busting on re-render)
  useEffect(() => {
    const timer = setInterval(() => {
      setSnapshotRefreshTime(Date.now());
    }, 15000);
    return () => clearInterval(timer);
  }, []);

  // Timer to tick elapsed time for image age display
  useEffect(() => {
    const timer = setInterval(() => {
      setNow(Date.now());
    }, 5000);
    return () => clearInterval(timer);
  }, []);

  // Update size preset and persist
  const handleSizeChange = useCallback((newSize: DockSizePreset) => {
    setSizePreset(newSize);
    if (typeof window !== 'undefined') {
      localStorage.setItem('warded_dock_size', newSize);
    }
  }, []);

  // Update view mode and persist
  const handleViewModeChange = useCallback((newMode: DockViewMode) => {
    setViewMode(newMode);
    if (typeof window !== 'undefined') {
      localStorage.setItem('warded_dock_view', newMode);
    }
  }, []);

  // Move camera left or right (Touch & Button fallback + Keyboard support)
  const handleMove = useCallback(
    (index: number, direction: 'left' | 'right') => {
      const targetIndex = direction === 'left' ? index - 1 : index + 1;
      if (targetIndex < 0 || targetIndex >= pinnedCameras.length) return;

      const currentIds = pinnedCameras.map(c => c.id);
      const newIds = [...currentIds];
      const [moved] = newIds.splice(index, 1);
      newIds.splice(targetIndex, 0, moved);
      onReorder(newIds);
    },
    [pinnedCameras, onReorder]
  );

  // Reorder via Pointer Events (Drag & Drop)
  const handlePointerDown = (e: React.PointerEvent, camId: string) => {
    if (e.button !== 0) return; // left click / primary touch only
    pointerStartPos.current = { x: e.clientX, y: e.clientY };
    draggedCamIdRef.current = camId;
    isDraggingRef.current = false;
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    if (!draggedCamIdRef.current) return;
    const dx = Math.abs(e.clientX - pointerStartPos.current.x);
    const dy = Math.abs(e.clientY - pointerStartPos.current.y);

    if (!isDraggingRef.current && (dx > 6 || dy > 6)) {
      isDraggingRef.current = true;
      setDraggedId(draggedCamIdRef.current);
      try {
        (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
      } catch {
        // pointer capture fallback
      }
    }

    if (isDraggingRef.current) {
      const elem = document.elementFromPoint(e.clientX, e.clientY);
      const tile = elem?.closest('[data-camera-id]');
      if (tile) {
        const targetId = tile.getAttribute('data-camera-id');
        if (targetId && targetId !== draggedCamIdRef.current) {
          setDragOverId(targetId);
        }
      }
    }
  };

  const handlePointerUp = (e: React.PointerEvent, cam: Camera) => {
    if (isDraggingRef.current && draggedCamIdRef.current) {
      e.stopPropagation();
      e.preventDefault();

      if (dragOverId && dragOverId !== draggedCamIdRef.current) {
        const currentIds = pinnedCameras.map(c => c.id);
        const fromIndex = currentIds.indexOf(draggedCamIdRef.current);
        const toIndex = currentIds.indexOf(dragOverId);
        if (fromIndex !== -1 && toIndex !== -1) {
          const newIds = [...currentIds];
          const [moved] = newIds.splice(fromIndex, 1);
          newIds.splice(toIndex, 0, moved);
          onReorder(newIds);
        }
      }

      try {
        (e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
      } catch {
        // release capture fallback
      }
    } else if (!isDraggingRef.current) {
      // Normal click
      if (onCameraClick) {
        onCameraClick(cam);
        setIsMobileExpanded(false);
      }
    }

    draggedCamIdRef.current = null;
    isDraggingRef.current = false;
    setDraggedId(null);
    setDragOverId(null);
  };

  const handlePointerCancel = () => {
    draggedCamIdRef.current = null;
    isDraggingRef.current = false;
    setDraggedId(null);
    setDragOverId(null);
  };

  // Keyboard navigation for reordering and selection
  const handleKeyDown = (e: React.KeyboardEvent, index: number, cam: Camera) => {
    if (e.key === 'ArrowLeft') {
      e.preventDefault();
      handleMove(index, 'left');
    } else if (e.key === 'ArrowRight') {
      e.preventDefault();
      handleMove(index, 'right');
    } else if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      if (onCameraClick) {
        onCameraClick(cam);
        setIsMobileExpanded(false);
      }
    }
  };

  const handleUnpin = (e: React.MouseEvent, camId: string) => {
    e.stopPropagation();
    onUnpin(camId);
  };

  // Collapse dock when empty
  if (pinnedCameras.length === 0) return null;

  // Format image age
  const formatImageAge = (camId: string): string => {
    const meta = tilesMetaRef.current.get(camId);
    if (!meta) {
      tilesMetaRef.current.set(camId, { mountedAt: Date.now(), lastLoadedAt: Date.now() });
      return PINNED_DOCK_I18N.JUST_NOW[lang];
    }
    const elapsedSec = Math.floor((now - meta.lastLoadedAt) / 1000);
    if (elapsedSec < 10) {
      return PINNED_DOCK_I18N.JUST_NOW[lang];
    }
    return PINNED_DOCK_I18N.SECONDS_AGO[lang](elapsedSec);
  };

  // Dimensions based on size preset
  const getPresetStyles = (size: DockSizePreset, isGrid: boolean) => {
    switch (size) {
      case 'S':
        return {
          trackHeight: 'h-[105px]',
          tileClass: isGrid
            ? 'h-[105px] w-full'
            : 'h-full min-w-[135px] max-w-[145px] shrink-0',
          titleSize: 'text-[9px]',
          badgeSize: 'text-[8px]',
          controlBtnSize: 'w-4 h-4 text-[9px]',
        };
      case 'L':
        return {
          trackHeight: 'h-[175px]',
          tileClass: isGrid
            ? 'h-[175px] w-full'
            : 'h-full min-w-[230px] max-w-[250px] shrink-0',
          titleSize: 'text-[11px]',
          badgeSize: 'text-[9.5px]',
          controlBtnSize: 'w-5 h-5 text-[11px]',
        };
      case 'M':
      default:
        return {
          trackHeight: 'h-[135px]',
          tileClass: isGrid
            ? 'h-[135px] w-full'
            : 'h-full min-w-[180px] max-w-[195px] shrink-0',
          titleSize: 'text-[10px]',
          badgeSize: 'text-[9px]',
          controlBtnSize: 'w-4 h-4 text-[10px]',
        };
    }
  };

  const presetStyle = getPresetStyles(sizePreset, viewMode === 'grid');
  const isCapReached = pinnedCameras.length >= PINNED_DOCK_CAP;

  // Render individual camera card
  const renderTile = (cam: Camera, index: number, isMobile: boolean = false) => {
    const isDragging = draggedId === cam.id;
    const isDragOver = dragOverId === cam.id;
    const verdict = verdicts?.get(cam.id);
    const isStale = verdict?.stale || false;
    const isOnline = cam.isOnline && !cam.isGhost && cam.status !== 'DOWN';

    return (
      <div
        key={cam.id}
        data-camera-id={cam.id}
        tabIndex={0}
        onKeyDown={(e) => handleKeyDown(e, index, cam)}
        onPointerDown={(e) => handlePointerDown(e, cam.id)}
        onPointerMove={handlePointerMove}
        onPointerUp={(e) => handlePointerUp(e, cam)}
        onPointerCancel={handlePointerCancel}
        onClick={() => {
          if (!isDraggingRef.current && onCameraClick) {
            onCameraClick(cam);
            setIsMobileExpanded(false);
          }
        }}
        role="button"
        aria-label={`${cam.displayName || cam.title}. ${PINNED_DOCK_I18N.DRAG_TO_REORDER[lang]}`}
        className={`relative ${presetStyle.tileClass} bg-black border rounded-sm select-none cursor-grab active:cursor-grabbing overflow-hidden transition-all outline-none focus:ring-1 focus:ring-zinc-400 group ${
          isDragging
            ? 'opacity-40 border-white scale-[0.98] z-20 shadow-2xl'
            : isDragOver
            ? 'border-dashed border-zinc-200 bg-zinc-900/80 scale-[1.02] z-10'
            : 'border-zinc-800 hover:border-zinc-500'
        }`}
      >
        {/* Camera snapshot image */}
        <img
          src={`${getApiUrl(API_ENDPOINTS.SNAPSHOT(cam.id))}?t=${snapshotRefreshTime}`}
          onLoad={() => {
            const existing = tilesMetaRef.current.get(cam.id) || {
              mountedAt: Date.now(),
              lastLoadedAt: Date.now(),
            };
            tilesMetaRef.current.set(cam.id, { ...existing, lastLoadedAt: Date.now() });
          }}
          className="w-full h-full object-cover rounded-sm opacity-85 group-hover:opacity-100 transition-opacity pointer-events-none"
          alt={cam.title || 'Traffic camera snapshot'}
          loading="lazy"
        />

        {/* Top Badges Bar: Status dot + Age + Severity placeholder */}
        <div className="absolute top-1 left-1 right-1 flex items-center justify-between pointer-events-none gap-1">
          {/* Online / Stale / Offline dot */}
          <div className="flex items-center gap-1 bg-black/85 border border-zinc-700/80 px-1 py-0.5 rounded-sm backdrop-blur-sm">
            <span
              className={`inline-block w-1.5 h-1.5 rounded-full ${
                !isOnline
                  ? 'bg-red-500'
                  : isStale
                  ? 'bg-amber-400'
                  : 'bg-emerald-500'
              }`}
            />
            <span className={`${presetStyle.badgeSize} font-mono text-zinc-300 uppercase leading-none`}>
              {!isOnline
                ? PINNED_DOCK_I18N.OFFLINE[lang]
                : isStale
                ? PINNED_DOCK_I18N.STALE[lang]
                : formatImageAge(cam.id)}
            </span>
          </div>

          {/* Severity Badge: explicit "no analysis" placeholder state for S5 */}
          <div className="bg-black/90 border border-zinc-700/90 text-zinc-400 px-1 py-0.5 rounded-sm font-mono leading-none tracking-tight">
            <span className={`${presetStyle.badgeSize} text-zinc-400`}>
              — {PINNED_DOCK_I18N.NO_ANALYSIS[lang]}
            </span>
          </div>
        </div>

        {/* Bottom Title Bar */}
        <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/95 via-black/80 to-transparent p-1.5 pointer-events-none">
          <div
            className={`font-mono text-white truncate font-medium ${presetStyle.titleSize}`}
            title={cam.displayName || cam.title}
          >
            {cam.displayName || cam.title}
          </div>
        </div>

        {/* Action Controls Overlay (Unpin button & Reorder Left/Right buttons) */}
        <div className="absolute inset-0 pointer-events-none">
          {/* Unpin button (Top-Right) */}
          <button
            type="button"
            onClick={(e) => handleUnpin(e, cam.id)}
            onPointerDown={(e) => e.stopPropagation()}
            onPointerUp={(e) => e.stopPropagation()}
            className="pointer-events-auto absolute top-1 right-1 w-5 h-5 bg-zinc-900/90 hover:bg-red-600 border border-zinc-700 hover:border-red-500 text-zinc-300 hover:text-white rounded-sm flex items-center justify-center transition-colors cursor-pointer text-xs z-10"
            title={PINNED_DOCK_I18N.UNPIN[lang]}
            aria-label={`${PINNED_DOCK_I18N.UNPIN[lang]}: ${cam.displayName || cam.title}`}
          >
            ×
          </button>

          {/* Adjacent Camera Navigation Buttons (◀ ▶) */}
          <div
            className={`pointer-events-auto absolute bottom-1 right-1 flex items-center gap-1 z-10 transition-opacity ${
              isMobile ? 'opacity-90' : 'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100'
            }`}
          >
            {index > 0 && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  if (onCameraClick) {
                    onCameraClick(pinnedCameras[index - 1]);
                  }
                }}
                onPointerDown={(e) => e.stopPropagation()}
                onPointerUp={(e) => e.stopPropagation()}
                className={`${presetStyle.controlBtnSize} bg-zinc-950/90 hover:bg-zinc-800 border border-zinc-700 text-zinc-300 hover:text-white rounded-sm flex items-center justify-center cursor-pointer transition`}
                title={lang === 'vi' ? 'Xem camera trước' : 'View previous camera'}
                aria-label={`${lang === 'vi' ? 'Xem camera trước' : 'View previous camera'}: ${pinnedCameras[index - 1].displayName || pinnedCameras[index - 1].title}`}
              >
                ◀
              </button>
            )}
            {index < pinnedCameras.length - 1 && (
              <button
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  if (onCameraClick) {
                    onCameraClick(pinnedCameras[index + 1]);
                  }
                }}
                onPointerDown={(e) => e.stopPropagation()}
                onPointerUp={(e) => e.stopPropagation()}
                className={`${presetStyle.controlBtnSize} bg-zinc-950/90 hover:bg-zinc-800 border border-zinc-700 text-zinc-300 hover:text-white rounded-sm flex items-center justify-center cursor-pointer transition`}
                title={lang === 'vi' ? 'Xem camera sau' : 'View next camera'}
                aria-label={`${lang === 'vi' ? 'Xem camera sau' : 'View next camera'}: ${pinnedCameras[index + 1].displayName || pinnedCameras[index + 1].title}`}
              >
                ▶
              </button>
            )}
          </div>
        </div>
      </div>
    );
  };

  return (
    <>
      {/* =======================================================
          DESKTOP VIEW (md+): Horizontal strip or Full-width Grid
         ======================================================= */}
      <div className="hidden md:block absolute bottom-0 left-0 right-0 z-[800] bg-zinc-950/95 border-t border-zinc-800 shadow-2xl backdrop-blur-md transition-all">
        {/* Notice Banner (Pin Cap Feedback) */}
        {pinNotice && (
          <div className="px-3 py-1 bg-amber-950/90 border-b border-amber-800 text-amber-200 text-xs font-mono flex items-center justify-between">
            <span>⚠️ {pinNotice}</span>
          </div>
        )}

        {/* Header Toolbar */}
        <div className="flex items-center justify-between px-3 py-1.5 bg-zinc-900 border-b border-zinc-800 font-mono text-xs select-none">
          {/* Left: Title + Camera Count / Cap */}
          <div className="flex items-center gap-2">
            <span className="font-bold text-white tracking-wide flex items-center gap-1.5">
              <span>📌</span>
              <span>{PINNED_DOCK_I18N.TITLE[lang]}</span>
            </span>

            {/* Pin Count & Cap badge */}
            <span
              className={`px-1.5 py-0.5 rounded border text-[11px] font-semibold ${
                isCapReached
                  ? 'bg-amber-950/90 border-amber-600 text-amber-300'
                  : 'bg-zinc-950 border-zinc-700 text-zinc-300'
              }`}
            >
              [{pinnedCameras.length}/{PINNED_DOCK_CAP} {pinnedCameras.length === 1 ? 'CAM' : 'CAMS'}]
            </span>

            {isCapReached && (
              <span className="text-[10px] text-amber-400 font-semibold uppercase">
                [{PINNED_DOCK_I18N.PIN_LIMIT_BADGE[lang]}]
              </span>
            )}
          </div>

          {/* Right: Controls (Size Presets, Grid Toggle, Clear, Collapse) */}
          <div className="flex items-center gap-2">
            {/* Size Presets (S / M / L) */}
            <div className="flex items-center bg-zinc-950 border border-zinc-800 rounded p-0.5 text-[10.5px]">
              {(['S', 'M', 'L'] as DockSizePreset[]).map((size) => (
                <button
                  key={size}
                  type="button"
                  onClick={() => handleSizeChange(size)}
                  className={`px-1.5 py-0.5 rounded-sm transition cursor-pointer font-bold ${
                    sizePreset === size
                      ? 'bg-zinc-800 text-white shadow-sm'
                      : 'text-zinc-400 hover:text-zinc-200'
                  }`}
                  title={`${PINNED_DOCK_I18N.SIZE_PRESETS[size][lang]}`}
                  aria-label={`Size preset: ${size}`}
                >
                  {size}
                </button>
              ))}
            </div>

            {/* View Mode Toggle (Strip vs Full-width Grid) */}
            <div className="flex items-center bg-zinc-950 border border-zinc-800 rounded p-0.5 text-[10.5px]">
              <button
                type="button"
                onClick={() => handleViewModeChange('strip')}
                className={`px-2 py-0.5 rounded-sm transition cursor-pointer flex items-center gap-1 ${
                  viewMode === 'strip'
                    ? 'bg-zinc-800 text-white'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
                title={PINNED_DOCK_I18N.VIEW_MODE.STRIP[lang]}
                aria-label={PINNED_DOCK_I18N.VIEW_MODE.STRIP[lang]}
              >
                <span>▤</span>
                <span>{PINNED_DOCK_I18N.VIEW_MODE.STRIP[lang]}</span>
              </button>
              <button
                type="button"
                onClick={() => handleViewModeChange('grid')}
                className={`px-2 py-0.5 rounded-sm transition cursor-pointer flex items-center gap-1 ${
                  viewMode === 'grid'
                    ? 'bg-zinc-800 text-white'
                    : 'text-zinc-400 hover:text-zinc-200'
                }`}
                title={PINNED_DOCK_I18N.FULL_WIDTH_GRID[lang]}
                aria-label={PINNED_DOCK_I18N.FULL_WIDTH_GRID[lang]}
              >
                <span>▦</span>
                <span>{PINNED_DOCK_I18N.VIEW_MODE.GRID[lang]}</span>
              </button>
            </div>

            {/* Clear All button */}
            <button
              type="button"
              onClick={onClear}
              className="px-2 py-0.5 bg-zinc-950 border border-zinc-700 hover:border-red-500 text-zinc-400 hover:text-red-400 rounded transition cursor-pointer text-[10.5px]"
            >
              {PINNED_DOCK_I18N.CLEAR_ALL[lang]}
            </button>

            {/* Collapse / Expand Toggle */}
            <button
              type="button"
              onClick={() => setIsDesktopCollapsed((prev) => !prev)}
              className="px-1.5 py-0.5 text-zinc-400 hover:text-white transition cursor-pointer text-xs"
              title={
                isDesktopCollapsed
                  ? PINNED_DOCK_I18N.EXPAND[lang]
                  : PINNED_DOCK_I18N.COLLAPSE[lang]
              }
              aria-label={
                isDesktopCollapsed
                  ? PINNED_DOCK_I18N.EXPAND[lang]
                  : PINNED_DOCK_I18N.COLLAPSE[lang]
              }
            >
              {isDesktopCollapsed ? '▴' : '▾'}
            </button>
          </div>
        </div>

        {/* Content Area: Horizontal Scroll Track or Responsive Multi-Column Grid */}
        {!isDesktopCollapsed && (
          <div
            className={
              viewMode === 'grid'
                ? 'grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-2 p-2 max-h-[45vh] overflow-y-auto'
                : `flex gap-2 p-2 overflow-x-auto ${presetStyle.trackHeight} items-center`
            }
          >
            {pinnedCameras.map((cam, idx) => renderTile(cam, idx, false))}
          </div>
        )}
      </div>

      {/* =======================================================
          MOBILE VIEW (<md): Floating Collapsed Pill + Slide-up Drawer
         ======================================================= */}
      <div className="md:hidden">
        {/* 1. Collapsed Floating Pill (shown when not expanded) */}
        {!isMobileExpanded && (
          <motion.div
            initial={{ opacity: 0, y: 10, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: 10, scale: 0.95 }}
            className="fixed bottom-4 left-1/2 -translate-x-1/2 z-[820] pointer-events-auto select-none"
          >
            <button
              type="button"
              onClick={() => setIsMobileExpanded(true)}
              className={`px-3.5 py-1.5 rounded-full bg-zinc-950/95 border text-zinc-200 font-mono text-xs font-semibold shadow-2xl backdrop-blur-md flex items-center gap-2 cursor-pointer active:scale-95 transition-all ${
                isCapReached ? 'border-amber-600' : 'border-zinc-700 hover:border-zinc-500'
              }`}
              aria-label={PINNED_DOCK_I18N.COLLAPSED_PILL[lang](pinnedCameras.length)}
            >
              <span>{PINNED_DOCK_I18N.COLLAPSED_PILL[lang](pinnedCameras.length)}</span>
              <span className="text-[10px] text-zinc-400">
                [{pinnedCameras.length}/{PINNED_DOCK_CAP}]
              </span>
              <span className="text-zinc-400 text-xs">▴</span>
            </button>
          </motion.div>
        )}

        {/* 2. Expanded Bottom Sheet Drawer */}
        <AnimatePresence>
          {isMobileExpanded && (
            <>
              {/* Backdrop */}
              <motion.div
                key="pinned-backdrop-mobile"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                transition={{ duration: 0.2 }}
                className="fixed inset-0 z-[840] bg-black/60 md:hidden"
                onClick={() => setIsMobileExpanded(false)}
              />

              {/* Drawer Sheet */}
              <motion.div
                key="pinned-sheet-mobile"
                initial={{ y: '100%' }}
                animate={{ y: 0 }}
                exit={{ y: '100%' }}
                transition={{ duration: 0.26, ease: 'easeOut' }}
                className="fixed bottom-0 left-0 right-0 z-[850] bg-zinc-950 border-t border-zinc-700 rounded-t-xl shadow-2xl flex flex-col md:hidden pb-4 max-h-[60vh]"
              >
                {/* Drag handle */}
                <div className="pt-2 pb-1 flex justify-center shrink-0">
                  <div className="w-10 h-1 rounded-full bg-zinc-600" />
                </div>

                {/* Mobile Header Toolbar */}
                <div className="flex flex-col gap-1.5 px-3 py-1.5 border-b border-zinc-800 shrink-0 font-mono text-xs">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1.5">
                      <span className="font-bold text-white tracking-wide">
                        {PINNED_DOCK_I18N.TITLE[lang]}
                      </span>
                      <span
                        className={`px-1.5 py-0.5 rounded border text-[10px] ${
                          isCapReached
                            ? 'bg-amber-950/90 border-amber-600 text-amber-300'
                            : 'bg-zinc-900 border-zinc-700 text-zinc-300'
                        }`}
                      >
                        [{pinnedCameras.length}/{PINNED_DOCK_CAP}]
                      </span>
                    </div>

                    <div className="flex items-center gap-2">
                      <button
                        type="button"
                        onClick={onClear}
                        className="px-2 py-0.5 bg-zinc-900 border border-zinc-700 hover:border-zinc-400 text-zinc-400 hover:text-white rounded text-[10.5px] transition cursor-pointer"
                      >
                        {PINNED_DOCK_I18N.CLEAR_ALL[lang]}
                      </button>
                      <button
                        type="button"
                        onClick={() => setIsMobileExpanded(false)}
                        className="text-zinc-400 hover:text-white font-bold text-base leading-none p-1 transition-colors flex items-center gap-1 cursor-pointer"
                        aria-label={PINNED_DOCK_I18N.COLLAPSE[lang]}
                      >
                        <span className="text-[11px] font-normal text-zinc-400 mr-0.5">
                          {PINNED_DOCK_I18N.COLLAPSE[lang]}
                        </span>
                        ▾
                      </button>
                    </div>
                  </div>

                  {/* Size Preset & View Controls on Mobile */}
                  <div className="flex items-center justify-between pt-1">
                    <div className="flex items-center bg-zinc-900 border border-zinc-800 rounded p-0.5 text-[10px]">
                      {(['S', 'M', 'L'] as DockSizePreset[]).map((size) => (
                        <button
                          key={size}
                          type="button"
                          onClick={() => handleSizeChange(size)}
                          className={`px-2 py-0.5 rounded-sm transition cursor-pointer font-bold ${
                            sizePreset === size
                              ? 'bg-zinc-800 text-white'
                              : 'text-zinc-400 hover:text-zinc-200'
                          }`}
                        >
                          {size}
                        </button>
                      ))}
                    </div>

                    <div className="flex items-center bg-zinc-900 border border-zinc-800 rounded p-0.5 text-[10px]">
                      <button
                        type="button"
                        onClick={() => handleViewModeChange('strip')}
                        className={`px-2 py-0.5 rounded-sm transition cursor-pointer ${
                          viewMode === 'strip' ? 'bg-zinc-800 text-white' : 'text-zinc-400'
                        }`}
                      >
                        {PINNED_DOCK_I18N.VIEW_MODE.STRIP[lang]}
                      </button>
                      <button
                        type="button"
                        onClick={() => handleViewModeChange('grid')}
                        className={`px-2 py-0.5 rounded-sm transition cursor-pointer ${
                          viewMode === 'grid' ? 'bg-zinc-800 text-white' : 'text-zinc-400'
                        }`}
                      >
                        {PINNED_DOCK_I18N.VIEW_MODE.GRID[lang]}
                      </button>
                    </div>
                  </div>
                </div>

                {/* Mobile Content Track */}
                <div
                  className={
                    viewMode === 'grid'
                      ? 'grid grid-cols-2 gap-2 p-3 overflow-y-auto max-h-[45vh]'
                      : `flex gap-2.5 p-3 overflow-x-auto ${presetStyle.trackHeight} items-center`
                  }
                >
                  {pinnedCameras.map((cam, idx) => renderTile(cam, idx, true))}
                </div>
              </motion.div>
            </>
          )}
        </AnimatePresence>
      </div>
    </>
  );
}
