'use client';
import dynamic from 'next/dynamic';
import { useState, useMemo, useEffect, useCallback, useRef } from 'react';
import type L from 'leaflet';
import { useCameras, Camera } from '../hooks/useCameras';
import { useTraffic } from '../hooks/useTraffic';
import { useHotsetStatus } from '../hooks/useHotsetStatus';
import { useFlood } from '../hooks/useFlood';
import { useWeather } from '../hooks/useWeather';
import { useRainHexRadar } from '../hooks/useRainHexRadar';
import { useGeolocation } from '../hooks/useGeolocation';
import { FLOOD_I18N } from '../utils/floodUtils';
import { buildNearMeResults, isWithinBounds, FLY_TO_ZOOM } from '../utils/geoDistance';
import {
  MAP_CONFIG,
  MOBILE_SEARCH_I18N,
  NEAR_ME_I18N,
  PINNED_DOCK_CAP,
  PINNED_DOCK_I18N,
  SUBSCRIPTION_I18N,
} from '../config/constants';
import type { TargetSelection } from '../utils/subscriptionUtils';
import LandingOverlay from '../components/LandingOverlay';
import CameraModal from '../components/CameraModal';
import NearMeModal from '../components/NearMeModal';
import SubscriptionModal from '../components/SubscriptionModal';
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
import type { ActiveLensMode } from '../components/HexOverlay';
import CameraFilter from '../components/CameraFilter';
import { isCameraOffline, isWaterwayCamera } from '../utils/cameraFilterUtils';

const MapComponent = dynamic(() => import('../components/MapComponent'), { ssr: false });
const HexOverlay = dynamic(() => import('../components/HexOverlay'), { ssr: false });

/** A fix younger than this is reused instead of starting a new GPS request (AC2). */
const NEAR_ME_FIX_MAX_AGE_MS = 60_000;

export default function Home() {
  const { cameras, loading, error, markCameraGhost, refetch: refetchCameras } = useCameras();
  const { verdicts, hexCells, analysisEnabled, appEnv, loading: trafficLoading } = useTraffic();
  const { hotsetStatus } = useHotsetStatus();
  const {
    floodVerdicts,
    floodHexCells,
    userFloodReports,
    floodedCameraIds,
    floodedCameraCount,
    wetCameraIds,
    wetCameraCount,
    loading: floodLoading,
  } = useFlood();
  const { weather, radar, loading: weatherLoading } = useWeather();
  const { rainRadarHexCells, loading: rainRadarLoading } = useRainHexRadar(radar, weather, cameras, floodVerdicts);

  // S12-T2 / Issue #400: one geolocation instance for the whole page.
  const geo = useGeolocation();
  const [nearMeOpen, setNearMeOpen] = useState(false);
  // S12-T4 / Issue #402: citizen alert subscription dialog.
  const [subscriptionOpen, setSubscriptionOpen] = useState(false);
  const [subscriptionTarget, setSubscriptionTarget] = useState<TargetSelection>({ type: 'all', id: '' });
  const hasFlownToFixRef = useRef(false);

  const [map, setMap] = useState<L.Map | null>(null);
  // S9 (Issue #144): Independent 1-lens switcher ('flood' | 'radar' | 'traffic' | 'off')
  // Default to 'traffic' for commuter arterial saturation flow
  const [activeLens, setActiveLens] = useState<ActiveLensMode>('traffic');
  const [isFloodFilterActive, setIsFloodFilterActive] = useState(false);
  // Store only the selected camera id and derive the live object from the
  // cameras array so runtime updates (e.g. markCameraGhost) propagate instantly.
  const [selectedCameraId, setSelectedCameraId] = useState<string | null>(null);
  const selectedCamera = useMemo(
    () => (selectedCameraId ? cameras.find(c => c.id === selectedCameraId) ?? null : null),
    [selectedCameraId, cameras]
  );
  const handleSelectCamera = useCallback((cam: Camera | null) => {
    setSelectedCameraId(cam ? cam.id : null);
  }, []);
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

  // Offline cameras filter default & persistence (S7-T2 / Issue #247)
  const [hideOfflineCameras, setHideOfflineCameras] = useState(true);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const saved = localStorage.getItem('warded_hide_offline_cameras');
    if (saved !== null) {
      setHideOfflineCameras(saved === 'true');
    }
  }, []);

  const handleToggleHideOffline = useCallback(() => {
    setHideOfflineCameras(prev => {
      const next = !prev;
      if (typeof window !== 'undefined') {
        localStorage.setItem('warded_hide_offline_cameras', String(next));
      }
      return next;
    });
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

  // Active Lens default & persistence (ADR-004 §D11 / S9 Issue #144)
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const savedLens = localStorage.getItem('warded_active_lens');
    if (savedLens === 'flood' || savedLens === 'radar' || savedLens === 'traffic' || savedLens === 'off') {
      setActiveLens(savedLens as ActiveLensMode);
      return;
    }
    // Backward compatibility for legacy 'warded_hex_mode'
    const savedMode = localStorage.getItem('warded_hex_mode');
    if (savedMode === 'traffic') {
      setActiveLens('traffic');
    } else if (savedMode === 'flood') {
      setActiveLens('flood');
    }
  }, []);

  const handleModeChange = useCallback((newMode: ActiveLensMode) => {
    setActiveLens(newMode);
    if (typeof window !== 'undefined') {
      localStorage.setItem('warded_active_lens', newMode);
    }
  }, []);

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

  // Escape key clears open camera modal first, then active filter (Issue #65).
  // The Near-Me and subscription sheets own Esc while they are open.
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        if (nearMeOpen || subscriptionOpen) return;
        if (selectedCameraId) {
          handleSelectCamera(null);
        } else if (isFiltered) {
          handleFilterClear();
        }
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [nearMeOpen, subscriptionOpen, selectedCameraId, isFiltered, handleFilterClear, handleSelectCamera]);

  // Set of camera IDs matching the active filter for focus dimming (ADR-004 A3)
  const filteredCameraIds = useMemo(() => {
    const baseSet = computeFilteredCameraIds(cameras, selectedPhuong, selectedCorridor);
    if (!isFloodFilterActive) return baseSet;
    const floodOrWetIds = new Set<string>();
    if (floodedCameraIds) floodedCameraIds.forEach(id => floodOrWetIds.add(id));
    if (wetCameraIds) wetCameraIds.forEach(id => floodOrWetIds.add(id));

    if (!baseSet) return floodOrWetIds;
    const intersected = new Set<string>();
    baseSet.forEach(id => {
      if (floodOrWetIds.has(id)) intersected.add(id);
    });
    return intersected;
  }, [cameras, selectedPhuong, selectedCorridor, isFloodFilterActive, floodedCameraIds, wetCameraIds]);

  // Active filter label for on-map chip
  const activeFilterLabel = useMemo(() => {
    const baseLabel = computeActiveFilterLabel(selectedPhuong, selectedCorridor, lang);
    const floodLabel = floodedCameraCount > 0
      ? FLOOD_I18N.FILTER_CHIP[lang](floodedCameraCount)
      : (lang === 'vi' ? `Camera đường ướt (${wetCameraCount})` : `Wet cameras (${wetCameraCount})`);
    if (isFloodFilterActive && baseLabel) {
      return `${baseLabel} • ${floodLabel}`;
    }
    if (isFloodFilterActive) {
      return floodLabel;
    }
    return baseLabel;
  }, [selectedPhuong, selectedCorridor, lang, isFloodFilterActive, floodedCameraCount, wetCameraCount]);

  // Filter out offline / no-signal cameras by default for map hygiene (S7-T2 / Issue #247).
  // Non-road waterway monitoring cameras beneath bridge footings are permanently excluded.
  const displayCameras = useMemo(() => {
    const roadCameras = cameras.filter(c => !isWaterwayCamera(c));
    if (!hideOfflineCameras) return roadCameras;
    return roadCameras.filter(c => !isCameraOffline(c));
  }, [cameras, hideOfflineCameras]);

  const visibleCameraCount = useMemo(() => {
    if (isFiltered && filteredCameraIds) {
      return displayCameras.filter(c => filteredCameraIds.has(c.id)).length;
    }
    return displayCameras.length;
  }, [displayCameras, isFiltered, filteredCameraIds]);

  // S12-T2 / Issue #400 — proximity list derived from the live fix. Coordinates
  // never leave the browser: no fetch, no storage, no logging.
  const nearMeOrigin = useMemo(
    () => (geo.fix ? { latitude: geo.fix.latitude, longitude: geo.fix.longitude } : null),
    [geo.fix]
  );

  const nearMeResults = useMemo(() => {
    if (!nearMeOrigin) return [];
    return buildNearMeResults({
      origin: nearMeOrigin,
      cameras,
      floodVerdicts,
      trafficVerdicts: verdicts,
      hideOffline: hideOfflineCameras,
    });
  }, [nearMeOrigin, cameras, floodVerdicts, verdicts, hideOfflineCameras]);

  const nearMeOfflineCount = useMemo(() => {
    if (!nearMeOrigin) return 0;
    const withOffline = buildNearMeResults({
      origin: nearMeOrigin,
      cameras,
      floodVerdicts,
      trafficVerdicts: verdicts,
      hideOffline: false,
    });
    return Math.max(0, withOffline.length - nearMeResults.length);
  }, [nearMeOrigin, cameras, floodVerdicts, verdicts, nearMeResults.length]);

  const isNearMeOutsideServiceArea = useMemo(
    () =>
      geo.fix
        ? !isWithinBounds(geo.fix, MAP_CONFIG.MAX_BOUNDS as [[number, number], [number, number]])
        : false,
    [geo.fix]
  );

  const applyNearMeView = useCallback((targetMap: L.Map, latitude: number, longitude: number) => {
    const reduceMotion =
      typeof window !== 'undefined' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    if (reduceMotion) {
      targetMap.setView([latitude, longitude], FLY_TO_ZOOM);
    } else {
      targetMap.flyTo([latitude, longitude], FLY_TO_ZOOM, { duration: 0.8 });
    }
  }, []);

  // Pan to the first fix of each "Near me" activation; watch updates while the
  // sheet stays open never re-center the map. If the map is not ready yet the
  // effect re-runs when `map` arrives.
  useEffect(() => {
    if (!nearMeOpen || !map || !geo.fix || hasFlownToFixRef.current) return;
    hasFlownToFixRef.current = true;
    applyNearMeView(map, geo.fix.latitude, geo.fix.longitude);
  }, [nearMeOpen, map, geo.fix, applyNearMeView]);

  // Watch only while the sheet is open and permission has been granted.
  const { startWatch: startGeoWatch, stopWatch: stopGeoWatch } = geo;
  useEffect(() => {
    if (nearMeOpen && geo.status === 'success') {
      startGeoWatch();
      return () => stopGeoWatch();
    }
    return undefined;
  }, [nearMeOpen, geo.status, startGeoWatch, stopGeoWatch]);

  const handleNearMeTrigger = useCallback(() => {
    // Each activation may center the map once on its first fix; watch fixes
    // that follow never move the map (E11).
    hasFlownToFixRef.current = false;
    setSubscriptionOpen(false);
    setNearMeOpen(true);
    const fix = geo.fix;
    const isFresh = Boolean(fix) && Date.now() - (fix ? fix.timestamp : 0) < NEAR_ME_FIX_MAX_AGE_MS;
    if (!isFresh) geo.request();
  }, [geo]);

  const handleNearMeClose = useCallback(() => {
    setNearMeOpen(false);
  }, []);

  const handleSubscriptionOpen = useCallback(() => {
    setNearMeOpen(false);
    setSubscriptionTarget({ type: 'all', id: '' });
    setSubscriptionOpen(true);
  }, []);

  const handleSubscriptionClose = useCallback(() => {
    setSubscriptionOpen(false);
  }, []);

  const handleNearMeRecenter = useCallback(() => {
    const fix = geo.fix;
    if (!fix || !map) {
      geo.request();
      return;
    }
    applyNearMeView(map, fix.latitude, fix.longitude);
  }, [geo, map, applyNearMeView]);

  const handleNearMeCameraClick = useCallback((cam: Camera) => {
    setNearMeOpen(false);
    handleSelectCamera(cam);
  }, [handleSelectCamera]);

  // Retry preserves the GPS state: a camera catalog failure refetches only the
  // catalog, a geolocation failure re-requests only the position.
  const handleNearMeRetryCameras = useCallback(() => {
    refetchCameras();
  }, [refetchCameras]);

  const handleNearMeRetryGeo = useCallback(() => {
    geo.refresh();
  }, [geo]);

  const handleNearMeRetry = useCallback(() => {
    if (error && nearMeResults.length === 0) {
      refetchCameras();
      return;
    }
    geo.refresh();
  }, [error, nearMeResults.length, refetchCameras, geo]);

  const handleNearMeBackToCity = useCallback(() => {
    setNearMeOpen(false);
    if (!map) return;
    map.flyTo(
      MAP_CONFIG.DEFAULT_CENTER as [number, number],
      MAP_CONFIG.DEFAULT_ZOOM,
      { duration: 0.8 }
    );
  }, [map]);

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
            floodVerdicts={floodVerdicts}
            floodedCameraIds={floodedCameraIds}
            wetCameraIds={wetCameraIds}
            wetCameraCount={wetCameraCount}
            isFloodFilterActive={isFloodFilterActive}
            onToggleFloodFilter={() => setIsFloodFilterActive(prev => !prev)}
            loading={loading}
            error={error}
            selectedPhuong={selectedPhuong}
            selectedCorridor={selectedCorridor}
            onSelectPhuong={setSelectedPhuong}
            onSelectCorridor={setSelectedCorridor}
            onCameraClick={handleSelectCamera}
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
            cameras={displayCameras}
            verdicts={verdicts}
            floodVerdicts={floodVerdicts}
            userFloodReports={userFloodReports}
            activeLens={activeLens}
            hotsetStatus={hotsetStatus}
            onCameraClick={handleSelectCamera}
            selectedCamera={selectedCamera}
            isFiltered={isFiltered}
            filteredCameraIds={filteredCameraIds}
            onFilterClear={handleFilterClear}
            activeFilterLabel={activeFilterLabel}
            lang={lang}
            appEnv={appEnv}
            onMapReady={setMap}
            nearMeLocation={geo.fix}
            hexOverlayEnabled={activeLens !== 'off'}
            hasPinnedCameras={pinnedCameras.length > 0}
            cameraFilterControl={
              <CameraFilter
                hideOfflineCameras={hideOfflineCameras}
                onToggleHideOffline={handleToggleHideOffline}
                visibleCount={visibleCameraCount}
                totalCount={cameras.length}
                lang={lang}
              />
            }
            weatherControl={
              <div className="flex items-center gap-2">
                {/* S12-T4 / Issue #402: citizen alert subscription entry point (desktop) */}
                <button
                  type="button"
                  onClick={handleSubscriptionOpen}
                  aria-label={SUBSCRIPTION_I18N.TITLE[lang]}
                  title={SUBSCRIPTION_I18N.TITLE[lang]}
                  aria-haspopup="dialog"
                  aria-expanded={subscriptionOpen}
                  className="flex h-12 min-h-[48px] w-12 min-w-[48px] items-center justify-center rounded-lg border border-zinc-700 bg-zinc-950/95 text-zinc-200 shadow-2xl backdrop-blur-md transition-colors hover:border-zinc-500 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100 active:bg-zinc-900 cursor-pointer"
                >
                  <svg
                    className="h-4 w-4 shrink-0"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.8"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    viewBox="0 0 24 24"
                    aria-hidden="true"
                  >
                    <path d="M18 8a6 6 0 1 0-12 0c0 6-2 7-2 7h16s-2-1-2-7" />
                    <path d="M10.5 20a2 2 0 0 0 3 0" />
                  </svg>
                </button>
                {/* S12-T2 / Issue #400: one-click proximity entry point (desktop stack) */}
                <button
                  type="button"
                  onClick={handleNearMeTrigger}
                  aria-label={NEAR_ME_I18N.BUTTON_ARIA[lang]}
                  title={NEAR_ME_I18N.BUTTON_LABEL[lang]}
                  aria-haspopup="dialog"
                  aria-expanded={nearMeOpen}
                  className="flex h-12 min-h-[48px] min-w-[48px] items-center gap-2 rounded-lg border border-zinc-700 bg-zinc-950/95 px-3 font-mono text-xs font-semibold text-zinc-200 shadow-2xl backdrop-blur-md transition-colors hover:border-zinc-500 hover:text-white focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100 active:bg-zinc-900 cursor-pointer"
                >
                  <svg
                    className="h-4 w-4 shrink-0"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.8"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    viewBox="0 0 24 24"
                    aria-hidden="true"
                  >
                    <circle cx="12" cy="12" r="7" />
                    <circle cx="12" cy="12" r="2" />
                    <path d="M12 2v3M12 19v3M2 12h3M19 12h3" />
                  </svg>
                  <span>{NEAR_ME_I18N.BUTTON_LABEL[lang]}</span>
                </button>
                <WeatherWidget
                  weather={weather}
                  loading={weatherLoading}
                  lang={lang}
                />
              </div>
            }
            legendControl={
              <MapLegend
                isOpen={legendOpen}
                onToggle={() => setLegendOpen(prev => !prev)}
                analysisEnabled={analysisEnabled}
                lensMode={activeLens}
                verdictsCount={verdicts.size}
                lang={lang}
                floodLoading={floodLoading}
                radarLoading={rainRadarLoading}
                trafficLoading={trafficLoading}
              />
            }
          >
            <HexOverlay
              map={map}
              hexCells={hexCells}
              floodHexCells={floodHexCells}
              radarHexCells={rainRadarHexCells}
              mode={activeLens}
              onModeChange={handleModeChange}
              lang={lang}
              floodLoading={floodLoading}
              radarLoading={rainRadarLoading}
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
          onCameraClick={handleSelectCamera}
          lang={lang}
          verdicts={verdicts}
          floodVerdicts={floodVerdicts}
          pinNotice={pinNotice}
          appEnv={appEnv}
        />

        {/* FLOATING STATUS PILL — Non-blocking background AI sync notification */}
        <TrafficSyncBanner
          analysisEnabled={analysisEnabled}
          verdictsCount={verdicts.size}
          lang={lang}
        />

        {/* MOBILE FLOATING QUICK SEARCH & WEATHER HEADER (hidden on md+) */}
        <div className="md:hidden absolute top-3 left-3 right-3 z-[810] pointer-events-auto flex items-center gap-2">
          <button
            type="button"
            onClick={() => {
              setShouldFocusSearch(true);
              setMobileDrawerOpen(true);
            }}
            className="flex-1 min-w-0 min-h-[48px] flex items-center justify-between gap-2 px-3 py-2 bg-zinc-950/95 border border-zinc-700 hover:border-zinc-500 rounded-lg text-left shadow-2xl backdrop-blur-md transition-all cursor-pointer group active:scale-[0.99]"
            aria-label={MOBILE_SEARCH_I18N.TRIGGER_LABEL[lang]}
          >
            <div className="flex items-center gap-2 min-w-0">
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
            <div className="flex items-center gap-1 shrink-0">
              <span className="text-[10px] font-mono font-medium text-zinc-400 bg-zinc-900 border border-zinc-800 px-1.5 py-0.5 rounded">
                {MOBILE_SEARCH_I18N.BADGE[lang]}
              </span>
            </div>
          </button>

          {/* S12-T4 / Issue #402: subscription bell between search and weather */}
          <button
            type="button"
            onClick={handleSubscriptionOpen}
            aria-label={SUBSCRIPTION_I18N.TITLE[lang]}
            title={SUBSCRIPTION_I18N.TITLE[lang]}
            aria-haspopup="dialog"
            aria-expanded={subscriptionOpen}
            className="shrink-0 w-12 h-12 min-w-[48px] min-h-[48px] flex items-center justify-center rounded-lg bg-zinc-950/95 border border-zinc-700 hover:border-zinc-400 text-zinc-100 shadow-2xl backdrop-blur-md transition-all active:scale-95 cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100"
          >
            <svg
              className="w-5 h-5"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
              strokeLinejoin="round"
              viewBox="0 0 24 24"
              aria-hidden="true"
            >
              <path d="M18 8a6 6 0 1 0-12 0c0 6-2 7-2 7h16s-2-1-2-7" />
              <path d="M10.5 20a2 2 0 0 0 3 0" />
            </svg>
          </button>

          {/* Mobile Weather Widget Trigger */}
          <div className="shrink-0">
            <WeatherWidget
              weather={weather}
              loading={weatherLoading}
              lang={lang}
            />
          </div>
        </div>

        {/* MOBILE CAMERA LIST & SEARCH — FAB trigger button (hidden on md+) */}
        <button
          onClick={() => {
            setShouldFocusSearch(true);
            setMobileDrawerOpen(true);
          }}
          className={`md:hidden absolute right-3 z-[830] w-12 h-12 min-w-[48px] min-h-[48px] bg-zinc-950/95 border border-zinc-700 hover:border-zinc-400 text-white rounded-lg flex items-center justify-center shadow-2xl transition-all active:scale-95 cursor-pointer backdrop-blur-md ${
            pinnedCameras.length > 0 ? 'bottom-[4.5rem]' : 'bottom-3.5'
          }`}
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

        {/* S12-T2 / Issue #400 — one-click near-me trigger (mobile, bottom-left).
            bottom-[4.5rem] when the pin dock is visible so nothing stacks. */}
        <button
          type="button"
          onClick={handleNearMeTrigger}
          aria-label={NEAR_ME_I18N.BUTTON_ARIA[lang]}
          title={NEAR_ME_I18N.BUTTON_LABEL[lang]}
          aria-haspopup="dialog"
          aria-expanded={nearMeOpen}
          className={`md:hidden absolute left-3 z-[840] w-12 h-12 min-w-[48px] min-h-[48px] bg-zinc-950/95 border border-zinc-700 hover:border-zinc-400 text-zinc-100 rounded-lg flex items-center justify-center shadow-2xl backdrop-blur-md transition-all active:scale-95 cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-zinc-100 ${
            pinnedCameras.length > 0 ? 'bottom-[4.5rem]' : 'bottom-3.5'
          }`}
        >
          <svg
            className="w-5 h-5"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinecap="round"
            strokeLinejoin="round"
            viewBox="0 0 24 24"
            aria-hidden="true"
          >
            <circle cx="12" cy="12" r="7" />
            <circle cx="12" cy="12" r="2" />
            <path d="M12 2v3M12 19v3M2 12h3M19 12h3" />
          </svg>
        </button>

        {/* MOBILE CAMERA DRAWER */}
        <MobileCameraDrawer
          cameras={cameras}
          verdicts={verdicts}
          floodVerdicts={floodVerdicts}
          floodedCameraIds={floodedCameraIds}
          wetCameraIds={wetCameraIds}
          wetCameraCount={wetCameraCount}
          isFloodFilterActive={isFloodFilterActive}
          onToggleFloodFilter={() => setIsFloodFilterActive(prev => !prev)}
          loading={loading}
          error={error}
          isOpen={mobileDrawerOpen}
          onClose={() => {
            setMobileDrawerOpen(false);
            setShouldFocusSearch(false);
          }}
          onCameraClick={handleSelectCamera}
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
      {/* S12-T4 / Issue #402: citizen subscription dialog. Opening it closes the
          Near-Me sheet (and vice versa) so two focus traps never coexist. */}
      <SubscriptionModal
        isOpen={subscriptionOpen}
        onClose={handleSubscriptionClose}
        lang={lang}
        initialTarget={subscriptionTarget}
        botUsernameFallback={process.env.NEXT_PUBLIC_TELEGRAM_BOT_USERNAME}
      />

      {/* S12-T2 / Issue #400: sheet renders under CameraModal (z-[9999]) and is
          closed before a row opens the camera detail modal. */}
      <NearMeModal
        isOpen={nearMeOpen}
        onClose={handleNearMeClose}
        status={geo.status}
        fix={geo.fix}
        geoError={geo.error}
        results={nearMeResults}
        offlineCountInRadius={nearMeOfflineCount}
        camerasLoading={loading}
        camerasError={error}
        hideOffline={hideOfflineCameras}
        isOutsideServiceArea={isNearMeOutsideServiceArea}
        lang={lang}
        onLangChange={setLang}
        onRetry={handleNearMeRetry}
        onRetryCameras={handleNearMeRetryCameras}
        onRetryGeo={handleNearMeRetryGeo}
        onRecenter={handleNearMeRecenter}
        onToggleOffline={handleToggleHideOffline}
        onCameraClick={handleNearMeCameraClick}
        onViewFullMap={handleNearMeClose}
        onBackToCity={handleNearMeBackToCity}
      />

      <CameraModal 
        camera={selectedCamera} 
        verdict={selectedCameraVerdict} 
        userFloodReport={selectedCameraId ? userFloodReports.get(selectedCameraId) : undefined}
        onClose={() => handleSelectCamera(null)} 
        isPinned={selectedCamera ? pinnedCameraIds.includes(selectedCamera.id) : false}
        onTogglePin={() => selectedCamera && togglePin(selectedCamera.id)}
        lang={lang}
        onLangChange={setLang}
        appEnv={appEnv}
        analysisEnabled={analysisEnabled}
        pinCapReached={pinnedCameraIds.length >= PINNED_DOCK_CAP}
        onPrevCamera={() => cameraSequence.prevCamera && handleSelectCamera(cameraSequence.prevCamera as Camera)}
        onNextCamera={() => cameraSequence.nextCamera && handleSelectCamera(cameraSequence.nextCamera as Camera)}
        hasPrevCamera={Boolean(cameraSequence.prevCamera)}
        hasNextCamera={Boolean(cameraSequence.nextCamera)}
        sequenceLabel={cameraSequence.contextLabel}
        onGhostDetected={markCameraGhost}
      />
    </div>
  );
}
