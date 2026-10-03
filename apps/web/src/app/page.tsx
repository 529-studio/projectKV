'use client';
import dynamic from 'next/dynamic';
import { useState, useMemo, useEffect, useCallback, useRef } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import type L from 'leaflet';
import { useCameras, Camera } from '../hooks/useCameras';
import { useTraffic } from '../hooks/useTraffic';
import { useFlood } from '../hooks/useFlood';
import { useWeather } from '../hooks/useWeather';
import { useRainHexRadar } from '../hooks/useRainHexRadar';
import { mergeFloodHexCells } from '../utils/rainHexUtils';
import { type HexOverlayMode } from '../components/HexOverlay';
import { FLOOD_I18N } from '../utils/floodUtils';
import { MOBILE_SEARCH_I18N, PINNED_DOCK_CAP, PINNED_DOCK_I18N } from '../config/constants';
import LandingOverlay from '../components/LandingOverlay';
import CameraModal from '../components/CameraModal';
import PinnedDock from '../components/PinnedDock';
import MapLegend from '../components/MapLegend';
import MobileCameraDrawer from '../components/MobileCameraDrawer';
import TrafficSyncBanner from '../components/TrafficSyncBanner';
import WeatherWidget from '../components/WeatherWidget';
import PhuongTreeFilter, {
  computeFilteredCameraIds,
  computeActiveFilterLabel,
} from '../components/PhuongTreeFilter';
import { getCameraSequence } from '../utils/cameraSequence';

const MapComponent = dynamic(() => import('../components/MapComponent'), { ssr: false });
const HexOverlay = dynamic(() => import('../components/HexOverlay'), { ssr: false });

export default function Home() {
  const { cameras, loading, error } = useCameras();
  const { verdicts, hexCells, analysisEnabled, appEnv, loading: trafficLoading } = useTraffic();
  const { floodVerdicts, floodHexCells, floodedCameraIds, floodedCameraCount, loading: floodLoading } = useFlood();
  const { weather, radar, loading: weatherLoading } = useWeather();
  const { rainRadarHexCells, loading: rainRadarLoading } = useRainHexRadar(radar, weather, cameras);

  // Merge camera ground-truth flood observations with real-time RainViewer radar & Open-Meteo rain hexes
  const activeFloodHexCells = useMemo(() => {
    return mergeFloodHexCells(floodHexCells, rainRadarHexCells);
  }, [floodHexCells, rainRadarHexCells]);

  const combinedFloodLoading = floodLoading || rainRadarLoading;
  
  const [map, setMap] = useState<L.Map | null>(null);
  const [hexOverlayEnabled, setHexOverlayEnabled] = useState(true);
  const [hexOverlayMode, setHexOverlayMode] = useState<HexOverlayMode>('flood');
  const [isFloodFilterActive, setIsFloodFilterActive] = useState(false);
  const [selectedCamera, setSelectedCamera] = useState<Camera | null>(null);
  const [searchTerm, setSearchTerm] = useState('');
  const [pinnedCameraIds, setPinnedCameraIds] = useState<string[]>([]);
  const [pinNotice, setPinNotice] = useState<string | null>(null);
  const [legendOpen, setLegendOpen] = useState(false);
  const [mobileDrawerOpen, setMobileDrawerOpen] = useState(false);
  const [shouldFocusSearch, setShouldFocusSearch] = useState(false);

  // Hierarchical Filter State (D3 - Issue #41)
  const [selectedPhuong, setSelectedPhuong] = useState<string | null>(null);
  const [selectedCorridor, setSelectedCorridor] = useState<string | null>(null);
  const [lang, setLang] = useState<'vi' | 'en'>('vi');

  // Open map legend by default on desktop view (>= 768px)
  useEffect(() => {
    if (typeof window !== 'undefined' && window.matchMedia('(min-width: 768px)').matches) {
      setLegendOpen(true);
    }
  }, []);

  // Pinned camera list & order persistence (ADR-004 §A4/S5)
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const saved = localStorage.getItem('warded_pinned_camera_ids');
    if (saved) {
      try {
        const parsed = JSON.parse(saved);
        if (Array.isArray(parsed)) {
          setPinnedCameraIds(parsed.slice(0, PINNED_DOCK_CAP));
        }
      } catch {
        // ignore malformed storage
      }
    }
  }, []);

  // Zone Overview default & persistence (ADR-004 §D11)
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const saved = localStorage.getItem('warded_hex_overlay_enabled');
    if (saved === 'false') {
      setHexOverlayEnabled(false);
    }
    const savedMode = localStorage.getItem('warded_hex_mode');
    if (savedMode === 'traffic' || savedMode === 'flood') {
      setHexOverlayMode(savedMode);
    }
  }, []);

  const handleToggleHexOverlay = useCallback(() => {
    setHexOverlayEnabled(prev => {
      const next = !prev;
      if (typeof window !== 'undefined') {
        localStorage.setItem('warded_hex_overlay_enabled', String(next));
      }
      return next;
    });
  }, []);

  const handleHexModeChange = useCallback((newMode: HexOverlayMode) => {
    setHexOverlayMode(newMode);
    if (typeof window !== 'undefined') {
      localStorage.setItem('warded_hex_mode', newMode);
    }
  }, []);

  const [floodSyncNoticeDismissed, setFloodSyncNoticeDismissed] = useState(false);
  const [showFloodSyncedSuccess, setShowFloodSyncedSuccess] = useState(false);
  const prevFloodLoadingRef = useRef(combinedFloodLoading);

  useEffect(() => {
    if (prevFloodLoadingRef.current && !combinedFloodLoading) {
      if (activeFloodHexCells.size > 0 && !floodSyncNoticeDismissed) {
        setShowFloodSyncedSuccess(true);
        const timer = setTimeout(() => {
          setFloodSyncNoticeDismissed(true);
        }, 2800);
        return () => clearTimeout(timer);
      } else {
        setFloodSyncNoticeDismissed(true);
      }
    }
    prevFloodLoadingRef.current = combinedFloodLoading;
  }, [combinedFloodLoading, activeFloodHexCells.size, floodSyncNoticeDismissed]);

  // URL sync for active filter (ADR-004 stretch goal)
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const params = new URLSearchParams(window.location.search);
    const p = params.get('phuong');
    const c = params.get('corridor');
    if (p) setSelectedPhuong(p);
    if (c) setSelectedCorridor(c);
  }, []);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const url = new URL(window.location.href);
    if (selectedPhuong) {
      url.searchParams.set('phuong', selectedPhuong);
    } else {
      url.searchParams.delete('phuong');
    }
    if (selectedCorridor) {
      url.searchParams.set('corridor', selectedCorridor);
    } else {
      url.searchParams.delete('corridor');
    }
    const newSearch = url.searchParams.toString();
    const newPath = newSearch ? `${url.pathname}?${newSearch}` : url.pathname;
    window.history.replaceState({}, '', newPath);
  }, [selectedPhuong, selectedCorridor]);

  const isFiltered = Boolean(selectedPhuong || selectedCorridor || isFloodFilterActive);

  const handleFilterClear = useCallback(() => {
    setSelectedPhuong(null);
    setSelectedCorridor(null);
    setIsFloodFilterActive(false);
  }, []);

  // Escape key clears open camera modal first, then active filter (Issue #65)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (selectedCamera) {
          setSelectedCamera(null);
        } else if (isFiltered) {
          handleFilterClear();
        }
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [selectedCamera, isFiltered, handleFilterClear]);

  // Set of camera IDs matching the active filter for focus dimming (ADR-004 A3)
  const filteredCameraIds = useMemo(() => {
    const baseSet = computeFilteredCameraIds(cameras, selectedPhuong, selectedCorridor);
    if (!isFloodFilterActive) return baseSet;
    if (!baseSet) return floodedCameraIds;
    const intersected = new Set<string>();
    baseSet.forEach(id => {
      if (floodedCameraIds.has(id)) intersected.add(id);
    });
    return intersected;
  }, [cameras, selectedPhuong, selectedCorridor, isFloodFilterActive, floodedCameraIds]);

  // Active filter label for on-map chip
  const activeFilterLabel = useMemo(() => {
    const baseLabel = computeActiveFilterLabel(selectedPhuong, selectedCorridor, lang);
    const floodLabel = FLOOD_I18N.FILTER_CHIP[lang](floodedCameraCount);
    if (isFloodFilterActive && baseLabel) {
      return `${baseLabel} • ${floodLabel}`;
    }
    if (isFloodFilterActive) {
      return floodLabel;
    }
    return baseLabel;
  }, [selectedPhuong, selectedCorridor, lang, isFloodFilterActive, floodedCameraCount]);

  const pinnedCameras = useMemo(() => {
    return pinnedCameraIds.map(id => cameras.find(c => c.id === id)).filter(Boolean) as Camera[];
  }, [pinnedCameraIds, cameras]);

  const togglePin = useCallback((camId: string) => {
    setPinnedCameraIds(prev => {
      if (prev.includes(camId)) {
        const next = prev.filter(id => id !== camId);
        if (typeof window !== 'undefined') {
          localStorage.setItem('warded_pinned_camera_ids', JSON.stringify(next));
        }
        return next;
      }
      if (prev.length >= PINNED_DOCK_CAP) {
        setPinNotice(PINNED_DOCK_I18N.PIN_CAP_REACHED[lang]);
        setTimeout(() => setPinNotice(null), 3500);
        return prev;
      }
      const next = [...prev, camId];
      if (typeof window !== 'undefined') {
        localStorage.setItem('warded_pinned_camera_ids', JSON.stringify(next));
      }
      return next;
    });
  }, [lang]);

  const handleClearPins = useCallback(() => {
    setPinnedCameraIds([]);
    if (typeof window !== 'undefined') {
      localStorage.removeItem('warded_pinned_camera_ids');
    }
  }, []);

  const handleReorderPins = useCallback((newOrder: string[]) => {
    setPinnedCameraIds(newOrder);
    if (typeof window !== 'undefined') {
      localStorage.setItem('warded_pinned_camera_ids', JSON.stringify(newOrder));
    }
  }, []);

  const selectedCameraVerdict = useMemo(() => {
    if (!selectedCamera) return undefined;
    const tv = verdicts.get(selectedCamera.id);
    const fv = floodVerdicts.get(selectedCamera.id);
    if (!tv && !fv) return undefined;
    return {
      ...(tv || {}),
      ...(fv || {}),
      camera_id: selectedCamera.id,
      severity: tv?.severity || 'CLEAR',
      flood_severity: fv?.flood_severity || tv?.flood_severity,
      confidence: tv?.confidence ?? fv?.confidence ?? 1.0,
      timestamp: fv?.timestamp || tv?.timestamp || new Date().toISOString(),
    };
  }, [selectedCamera, verdicts, floodVerdicts]);

  const cameraSequence = useMemo(() => {
    return getCameraSequence({
      currentCamera: selectedCamera,
      allCameras: cameras,
      pinnedCameras,
      selectedCorridor,
      lang,
    });
  }, [selectedCamera, cameras, pinnedCameras, selectedCorridor, lang]);

  return (
    <div className="flex h-screen w-full overflow-hidden bg-zinc-950 font-sans text-zinc-300">
      <LandingOverlay />
      
      {/* SIDEBAR — Desktop Tree Filter */}
      <aside className="w-80 lg:w-96 border-r border-zinc-800 bg-zinc-950 flex flex-col z-10 shrink-0 hidden md:flex">
        <div className="p-4 border-b border-zinc-800 bg-zinc-950 shrink-0">
          <div className="flex items-center gap-2">
            <img src="/wared-icon.png" alt="Warded Logo" className="w-6 h-6 object-contain" />
            <h1 className="text-xl font-mono font-bold text-white tracking-tight">Warded</h1>
          </div>
          <p className="text-[11px] font-mono text-zinc-500 mt-1">
            {lang === 'vi'
              ? 'Giúp đoạn đường của bạn đỡ chông gai hơn mỗi ngày.'
              : 'Making your commute smoother every day.'}
          </p>
        </div>
        <div className="flex-1 min-h-0 overflow-hidden">
          <PhuongTreeFilter
            cameras={cameras}
            verdicts={verdicts}
            floodedCameraIds={floodedCameraIds}
            isFloodFilterActive={isFloodFilterActive}
            onToggleFloodFilter={() => setIsFloodFilterActive(prev => !prev)}
            loading={loading}
            error={error}
            selectedPhuong={selectedPhuong}
            selectedCorridor={selectedCorridor}
            onSelectPhuong={setSelectedPhuong}
            onSelectCorridor={setSelectedCorridor}
            onCameraClick={setSelectedCamera}
            searchTerm={searchTerm}
            onSearchChange={setSearchTerm}
            lang={lang}
            onLangChange={setLang}
          />
        </div>
      </aside>

      {/* MAP WORKSPACE */}
      <main className="flex-1 flex flex-col h-full relative overflow-hidden bg-zinc-950">
        <div className="flex-1 w-full h-full relative z-0">
          <MapComponent
            cameras={cameras}
            verdicts={verdicts}
            onCameraClick={setSelectedCamera}
            selectedCamera={selectedCamera}
            isFiltered={isFiltered}
            filteredCameraIds={filteredCameraIds}
            onFilterClear={handleFilterClear}
            activeFilterLabel={activeFilterLabel}
            lang={lang}
            appEnv={appEnv}
            onMapReady={setMap}
            hexOverlayEnabled={hexOverlayEnabled}
          >
            <HexOverlay
              map={map}
              hexCells={hexCells}
              floodHexCells={activeFloodHexCells}
              enabled={hexOverlayEnabled}
              onToggle={handleToggleHexOverlay}
              mode={hexOverlayMode}
              onModeChange={handleHexModeChange}
              lang={lang}
              floodLoading={combinedFloodLoading}
              trafficLoading={trafficLoading}
            />
          </MapComponent>
        </div>
        
        {/* BOTTOM DOCK */}
        <PinnedDock 
          pinnedCameras={pinnedCameras} 
          onUnpin={togglePin} 
          onClear={handleClearPins}
          onReorder={handleReorderPins}
          onCameraClick={setSelectedCamera}
          lang={lang}
          verdicts={verdicts}
          pinNotice={pinNotice}
        />

        {/* FLOATING STATUS PILL — Non-blocking background AI sync notification */}
        <TrafficSyncBanner
          analysisEnabled={analysisEnabled}
          verdictsCount={verdicts.size}
          lang={lang}
        />

        {/* H3 Rain Forecast Loading Floating Chip */}
        <AnimatePresence>
          {hexOverlayEnabled && hexOverlayMode === 'flood' && !floodSyncNoticeDismissed && (
            <motion.div
              initial={{ opacity: 0, y: -6, scale: 0.95 }}
              animate={{ opacity: 1, y: 0, scale: 1 }}
              exit={{ opacity: 0, y: -6, scale: 0.95 }}
              transition={{ duration: 0.25 }}
              className="fixed bottom-20 left-1/2 -translate-x-1/2 md:bottom-auto md:top-14 md:left-1/2 md:-translate-x-1/2 z-[820] pointer-events-auto flex items-center gap-2 px-3.5 py-1.5 rounded-full bg-zinc-950/95 border border-sky-500/40 text-sky-300 font-mono text-[11px] shadow-2xl backdrop-blur-md select-none"
              role="status"
              aria-live="polite"
            >
              {showFloodSyncedSuccess ? (
                <>
                  <span className="relative flex h-2 w-2">
                    <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-400" />
                  </span>
                  <span className="text-zinc-200 font-semibold">
                    {lang === 'vi'
                      ? `✓ Đã sẵn sàng dữ liệu dự báo mưa (${activeFloodHexCells.size} vùng)`
                      : `✓ H3 rain forecast ready (${activeFloodHexCells.size} zones)`}
                  </span>
                </>
              ) : (
                <>
                  <span className="relative flex h-2 w-2">
                    <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-sky-400 opacity-75" />
                    <span className="relative inline-flex rounded-full h-2 w-2 bg-sky-400" />
                  </span>
                  <span>
                    {lang === 'vi' ? 'Đang tải dự báo mưa & ngập nước H3...' : 'Loading H3 rain & flood forecast...'}
                  </span>
                </>
              )}
              <button
                type="button"
                onClick={() => setFloodSyncNoticeDismissed(true)}
                className="text-zinc-500 hover:text-zinc-200 transition-colors ml-1 p-0.5 rounded focus:outline-none cursor-pointer"
                aria-label={lang === 'vi' ? 'Đóng thông báo' : 'Dismiss notice'}
                title={lang === 'vi' ? 'Đóng thông báo' : 'Dismiss notice'}
              >
                <span className="text-xs font-bold leading-none block">✕</span>
              </button>
            </motion.div>
          )}
        </AnimatePresence>

        {/* TOP-RIGHT CONTROLS: Weather Widget & Map Legend (Desktop md+) */}
        <div className="hidden md:flex items-start gap-2.5 absolute top-3 right-3 z-[810] pointer-events-auto">
          <WeatherWidget
            weather={weather}
            loading={weatherLoading}
            lang={lang}
          />
          <MapLegend
            isOpen={legendOpen}
            onToggle={() => setLegendOpen(prev => !prev)}
            analysisEnabled={analysisEnabled}
            hexOverlayEnabled={hexOverlayEnabled}
            hexOverlayMode={hexOverlayMode}
            verdictsCount={verdicts.size}
            lang={lang}
            floodLoading={combinedFloodLoading}
            trafficLoading={trafficLoading}
          />
        </div>

        {/* MOBILE CONTROLS (<md) */}
        <div className="md:hidden absolute top-16 right-3 z-[810] flex flex-col items-end gap-2 pointer-events-none">
          <MapLegend
            isOpen={legendOpen}
            onToggle={() => setLegendOpen(prev => !prev)}
            analysisEnabled={analysisEnabled}
            hexOverlayEnabled={hexOverlayEnabled}
            hexOverlayMode={hexOverlayMode}
            verdictsCount={verdicts.size}
            lang={lang}
            floodLoading={combinedFloodLoading}
            trafficLoading={trafficLoading}
          />
          <div className="pointer-events-auto">
            <WeatherWidget
              weather={weather}
              loading={weatherLoading}
              lang={lang}
            />
          </div>
        </div>

        {/* MOBILE FLOATING QUICK SEARCH BAR (hidden on md+) */}
        <div className="md:hidden absolute top-3 left-3 right-3 z-[810] pointer-events-auto">
          <button
            type="button"
            onClick={() => {
              setShouldFocusSearch(true);
              setMobileDrawerOpen(true);
            }}
            className="w-full flex items-center justify-between gap-2.5 px-3.5 py-2.5 bg-zinc-950/95 border border-zinc-700 hover:border-zinc-500 rounded-lg text-left shadow-2xl backdrop-blur-md transition-all cursor-pointer group active:scale-[0.99]"
            aria-label={MOBILE_SEARCH_I18N.TRIGGER_LABEL[lang]}
          >
            <div className="flex items-center gap-2.5 min-w-0">
              <svg
                className="w-4 h-4 text-zinc-400 group-hover:text-white shrink-0 transition-colors"
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
              <span className="text-xs font-mono text-zinc-400 group-hover:text-zinc-200 truncate">
                {searchTerm ? searchTerm : MOBILE_SEARCH_I18N.PLACEHOLDER[lang]}
              </span>
            </div>
            <div className="flex items-center gap-1.5 shrink-0">
              <span className="text-[10px] font-mono font-medium text-zinc-400 bg-zinc-900 border border-zinc-800 px-2 py-0.5 rounded">
                {MOBILE_SEARCH_I18N.BADGE[lang]}
              </span>
            </div>
          </button>
        </div>

        {/* MOBILE CAMERA LIST & SEARCH — FAB trigger button (hidden on md+) */}
        <button
          onClick={() => {
            setShouldFocusSearch(true);
            setMobileDrawerOpen(true);
          }}
          className="md:hidden absolute bottom-20 right-3 z-[830] w-11 h-11 bg-zinc-950/95 border border-zinc-700 hover:border-zinc-400 text-white rounded-lg flex items-center justify-center shadow-2xl transition-all active:scale-95 cursor-pointer backdrop-blur-md"
          aria-label={MOBILE_SEARCH_I18N.TRIGGER_LABEL[lang]}
          title={MOBILE_SEARCH_I18N.TRIGGER_LABEL[lang]}
        >
          <svg
            className="w-4 h-4 text-zinc-200"
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
        </button>

        {/* MOBILE CAMERA DRAWER */}
        <MobileCameraDrawer
          cameras={cameras}
          verdicts={verdicts}
          floodedCameraIds={floodedCameraIds}
          isFloodFilterActive={isFloodFilterActive}
          onToggleFloodFilter={() => setIsFloodFilterActive(prev => !prev)}
          loading={loading}
          error={error}
          isOpen={mobileDrawerOpen}
          onClose={() => {
            setMobileDrawerOpen(false);
            setShouldFocusSearch(false);
          }}
          onCameraClick={setSelectedCamera}
          selectedPhuong={selectedPhuong}
          selectedCorridor={selectedCorridor}
          onSelectPhuong={setSelectedPhuong}
          onSelectCorridor={setSelectedCorridor}
          searchTerm={searchTerm}
          onSearchChange={setSearchTerm}
          lang={lang}
          onLangChange={setLang}
          autoFocusSearch={shouldFocusSearch}
        />
      </main>

      {/* MODAL — global viewport overlay */}
      <CameraModal 
        camera={selectedCamera} 
        verdict={selectedCameraVerdict} 
        onClose={() => setSelectedCamera(null)} 
        isPinned={selectedCamera ? pinnedCameraIds.includes(selectedCamera.id) : false}
        onTogglePin={() => selectedCamera && togglePin(selectedCamera.id)}
        lang={lang}
        appEnv={appEnv}
        analysisEnabled={analysisEnabled}
        pinCapReached={pinnedCameraIds.length >= PINNED_DOCK_CAP}
        onPrevCamera={() => cameraSequence.prevCamera && setSelectedCamera(cameraSequence.prevCamera as Camera)}
        onNextCamera={() => cameraSequence.nextCamera && setSelectedCamera(cameraSequence.nextCamera as Camera)}
        hasPrevCamera={Boolean(cameraSequence.prevCamera)}
        hasNextCamera={Boolean(cameraSequence.nextCamera)}
        sequenceLabel={cameraSequence.contextLabel}
      />
    </div>
  );
}
