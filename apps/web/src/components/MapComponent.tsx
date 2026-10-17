'use client';
import { useEffect, useRef, useCallback } from 'react';
import L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import { Camera } from '../hooks/useCameras';
import { TrafficVerdict, isVerdictValid, getSeverityLabel } from '../hooks/useTraffic';
import { useHotsetStatus } from '../hooks/useHotsetStatus';
import {
  HotsetCameraStatus,
  getHotsetMarkerStyle,
  isHotsetStatusValid,
  buildHotsetTooltipSection,
} from '../utils/hotsetUtils';
import {
  MAP_CONFIG,
  FUNCTIONAL_COLORS,
  TRAFFIC_SEVERITY,
  VERDICT_I18N,
  HOTSET_LEGEND_I18N,
} from '../config/constants';
import { isHexVisibleAtZoom } from '../utils/hexUtils';

interface CustomCircleMarker extends L.CircleMarker {
  isDimmed?: boolean;
  isFilteredDimmed?: boolean;
  hasAIVerdict?: boolean;
  isHotset?: boolean;
  camId?: string;
}

interface MapComponentProps {
  onCameraClick?: (cam: Camera) => void;
  cameras: Camera[];
  verdicts: Map<string, TrafficVerdict>;
  hotsetStatus?: Map<string, HotsetCameraStatus>;
  selectedCamera?: Camera | null;
  isFiltered?: boolean;
  filteredCameraIds?: Set<string>;
  onFilterClear?: () => void;
  activeFilterLabel?: string | null;
  lang?: 'vi' | 'en';
  appEnv?: string;
  onMapReady?: (map: L.Map | null) => void;
  hexOverlayEnabled?: boolean;
  cameraFilterControl?: React.ReactNode;
  topRightControls?: React.ReactNode;
  weatherControl?: React.ReactNode;
  legendControl?: React.ReactNode;
  hasPinnedCameras?: boolean;
  children?: React.ReactNode;
}

export default function MapComponent({
  cameras,
  verdicts,
  hotsetStatus: propHotsetStatus,
  onCameraClick,
  selectedCamera,
  isFiltered = false,
  filteredCameraIds,
  onFilterClear,
  activeFilterLabel,
  lang = 'vi',
  appEnv = 'production',
  onMapReady,
  hexOverlayEnabled = false,
  cameraFilterControl,
  topRightControls,
  weatherControl,
  legendControl,
  hasPinnedCameras = false,
  children,
}: MapComponentProps) {
  const mapRef = useRef<L.Map | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const markersLayerRef = useRef<L.LayerGroup | null>(null);
  const prevFilteredRef = useRef(false);
  const hexOverlayEnabledRef = useRef(hexOverlayEnabled);
  const activeTooltipMarkerRef = useRef<L.CircleMarker | null>(null);
  const markersMapRef = useRef<Map<string, CustomCircleMarker>>(new Map());
  const verdictsRef = useRef(verdicts);

  const internalHotset = useHotsetStatus({ enabled: !propHotsetStatus });
  const hotsetStatus = propHotsetStatus ?? internalHotset.hotsetStatus;
  const hotsetStatusRef = useRef(hotsetStatus);

  useEffect(() => {
    hexOverlayEnabledRef.current = hexOverlayEnabled;
  }, [hexOverlayEnabled]);

  useEffect(() => {
    verdictsRef.current = verdicts;
  }, [verdicts]);

  useEffect(() => {
    hotsetStatusRef.current = hotsetStatus;
  }, [hotsetStatus]);

  const getTrafficColorForVerdict = useCallback((camId: string, isOnline: boolean, currentVerdicts: Map<string, TrafficVerdict>) => {
    const verdict = currentVerdicts.get(camId);
    if (isVerdictValid(verdict) && !verdict.stale) {
      if (verdict.source !== 'mock' || appEnv === 'dev') {
        const sev = verdict.severity.toUpperCase();
        if (sev === TRAFFIC_SEVERITY.CLEAR) return FUNCTIONAL_COLORS.CLEAR;
        if (sev === TRAFFIC_SEVERITY.MODERATE) return FUNCTIONAL_COLORS.MODERATE;
        if (sev === TRAFFIC_SEVERITY.HEAVY || sev === TRAFFIC_SEVERITY.CONGESTED) return FUNCTIONAL_COLORS.CONGESTED;
      }
    }
    return isOnline ? FUNCTIONAL_COLORS.ONLINE_DEFAULT : FUNCTIONAL_COLORS.OFFLINE_DEFAULT;
  }, [appEnv]);

  const getCameraMarkerVisual = useCallback((
    camId: string,
    isOnline: boolean,
    currentVerdicts: Map<string, TrafficVerdict>,
    currentHotset: Map<string, HotsetCameraStatus>
  ) => {
    const hotset = currentHotset.get(camId);
    if (hotset) {
      const style = getHotsetMarkerStyle(hotset, isOnline);
      return {
        fillColor: style.fillColor,
        isCongested: style.isCongested,
        isStale: style.isStale,
        isHotset: true,
      };
    }
    const fillColor = getTrafficColorForVerdict(camId, isOnline, currentVerdicts);
    const verdict = currentVerdicts.get(camId);
    return {
      fillColor,
      isCongested: false,
      isStale: verdict?.stale ?? false,
      isHotset: false,
    };
  }, [getTrafficColorForVerdict]);

  const buildTooltipHtml = useCallback((
    cam: Camera,
    verdict: TrafficVerdict | undefined,
    hotset: HotsetCameraStatus | undefined,
    fillColor: string
  ) => {
    let trafficInfo = '';
    if (hotset) {
      const prefix = lang === 'vi' ? VERDICT_I18N.TRAFFIC_PREFIX.vi : VERDICT_I18N.TRAFFIC_PREFIX.en;
      const isStale = Boolean(hotset.stale);
      const isUnknown = !hotset.severity || hotset.severity.toUpperCase() === 'UNKNOWN';

      if (isStale) {
        const staleText = lang === 'vi' ? 'Dữ liệu cũ' : 'Stale data';
        trafficInfo = `<br><span style="color:#a1a1aa; font-weight:bold;">${prefix}: ${staleText}</span>`;
      } else if (isUnknown) {
        const unknownText = lang === 'vi' ? 'Chưa xác định' : 'Unknown';
        trafficInfo = `<br><span style="color:#a1a1aa; font-weight:bold;">${prefix}: ${unknownText}</span>`;
      } else {
        const sevLabel = getSeverityLabel(hotset.severity, lang);
        trafficInfo = `<br><span style="color:${fillColor}; font-weight:bold;">${prefix}: ${sevLabel}</span>`;
      }
      trafficInfo += buildHotsetTooltipSection(hotset, lang);
    } else if (isVerdictValid(verdict)) {
      const prefix = lang === 'vi' ? VERDICT_I18N.TRAFFIC_PREFIX.vi : VERDICT_I18N.TRAFFIC_PREFIX.en;
      if (verdict.stale) {
        const staleText = lang === 'vi' ? 'Dữ liệu cũ' : 'Stale data';
        trafficInfo = `<br><span style="color:#a1a1aa; font-weight:bold;">${prefix}: ${staleText}</span>`;
      } else if (verdict.source !== 'mock' || appEnv === 'dev') {
        const isUnknown = !verdict.severity || verdict.severity.toUpperCase() === 'UNKNOWN';
        const textColor = isUnknown ? '#a1a1aa' : fillColor;
        const sevLabel = getSeverityLabel(verdict.severity, lang);
        trafficInfo = `<br><span style="color:${textColor}; font-weight:bold;">${prefix}: ${sevLabel}</span>`;
      }
    }
    if (cam.isGhost) {
      trafficInfo += `<br><span style="color:#f59e0b; font-weight:bold;">[MẤT TÍN HIỆU THỰC ĐỊA]</span>`;
    } else if (cam.duplicateOf) {
      trafficInfo += `<br><span style="color:#a1a1aa;">[TRÙNG NGUỒN]</span>`;
    }

    return `<b>${cam.displayName || cam.title}</b><br><span style="color:#a1a1aa;">${cam.zone}</span>${trafficInfo}`;
  }, [lang, appEnv]);

  const getRadius = (z: number, isFew: boolean = false) => {
    let r = 4;
    if (z >= 12) r = 5.5;
    if (z >= 14) r = 8;
    if (z >= 16) r = 12;
    return isFew ? r + 2 : r;
  };

  useEffect(() => {
    if (typeof window === 'undefined' || !containerRef.current) return;
    
    if (!mapRef.current) {
      const map = L.map(containerRef.current, {
        center: MAP_CONFIG.DEFAULT_CENTER as L.LatLngTuple,
        zoom: MAP_CONFIG.DEFAULT_ZOOM,
        minZoom: MAP_CONFIG.MIN_ZOOM,
        maxZoom: MAP_CONFIG.MAX_ZOOM,
        maxBounds: MAP_CONFIG.MAX_BOUNDS as L.LatLngBoundsLiteral,
        maxBoundsViscosity: MAP_CONFIG.MAX_BOUNDS_VISCOSITY,
        zoomAnimation: true,
        fadeAnimation: true,
      });

      L.tileLayer(MAP_CONFIG.TILE_URL, {
        attribution: MAP_CONFIG.TILE_ATTRIBUTION,
        maxZoom: MAP_CONFIG.MAX_ZOOM,
        keepBuffer: 6,
        updateWhenIdle: true,
        updateWhenZooming: false,
      }).addTo(map);

      // Dedicated pane for camera markers to ensure they always stay on top of overlay polygons
      if (!map.getPane('cameraMarkersPane')) {
        const cameraPane = map.createPane('cameraMarkersPane');
        cameraPane.style.zIndex = '600';
      }

      markersLayerRef.current = L.layerGroup().addTo(map);
      mapRef.current = map;
      onMapReady?.(map);

      // Close open camera tooltips when map begins zooming or panning
      map.on('zoomstart', () => {
        if (activeTooltipMarkerRef.current) {
          activeTooltipMarkerRef.current.closeTooltip();
          activeTooltipMarkerRef.current = null;
        }
      });
      map.on('movestart', () => {
        if (activeTooltipMarkerRef.current) {
          activeTooltipMarkerRef.current.closeTooltip();
          activeTooltipMarkerRef.current = null;
        }
      });

      // Dynamically scale marker sizes and macro dimming on zoomend
      map.on('zoomend', () => {
        if (!markersLayerRef.current) return;
        const currentZoom = map.getZoom();
        const r = getRadius(currentZoom);
        const w = currentZoom >= 14 ? 2 : 1.5;
        const isMacro = Boolean(hexOverlayEnabledRef.current && isHexVisibleAtZoom(currentZoom));

        markersLayerRef.current.eachLayer((layer) => {
          const marker = layer as CustomCircleMarker;
          const isFilteredDimmed = marker.isFilteredDimmed === true;
          const isMacroHexDimmed = Boolean(isMacro && !isFilteredDimmed && !marker.hasAIVerdict);
          const isDimmed = isFilteredDimmed || isMacroHexDimmed;
          marker.isDimmed = isDimmed;

          const targetR = isDimmed ? Math.max(4.0, Math.round(r * 0.7)) : r;
          const targetW = isDimmed ? 1 : w;
          const strokeColor = isFilteredDimmed ? '#27272a' : (isMacroHexDimmed ? '#3f3f46' : FUNCTIONAL_COLORS.MAP_STROKE);
          const opacity = isFilteredDimmed ? 0.25 : (isMacroHexDimmed ? 0.35 : 1);
          const fillOpacity = isFilteredDimmed ? 0.2 : (isMacroHexDimmed ? 0.25 : 0.95);

          if (marker.setRadius) {
            marker.setRadius(targetR);
            marker.setStyle({
              weight: targetW,
              color: strokeColor,
              opacity,
              fillOpacity,
            });
          }
        });
      });
    }

    // Auto-adjust map on container size changes (e.g. mobile URL bar collapse/expand)
    const resizeObserver = new ResizeObserver(() => {
      mapRef.current?.invalidateSize();
    });
    if (containerRef.current) {
      resizeObserver.observe(containerRef.current);
    }

    return () => {
      resizeObserver.disconnect();
      if (mapRef.current) {
        onMapReady?.(null);
        mapRef.current.remove();
        mapRef.current = null;
      }
    };
  }, [onMapReady]);

  // Smoothly fly to selected camera
  useEffect(() => {
    if (!mapRef.current || !selectedCamera?.latitude || !selectedCamera?.longitude) return;
    mapRef.current.flyTo(
      [selectedCamera.latitude, selectedCamera.longitude],
      Math.max(mapRef.current.getZoom(), 15),
      { duration: 1 }
    );
  }, [selectedCamera]);

  // Fit bounds when a filtered subset of cameras (Phường or Corridor) is selected
  useEffect(() => {
    if (!mapRef.current || !isFiltered || !filteredCameraIds || filteredCameraIds.size === 0) return;
    const validPoints = cameras
      .filter(c => filteredCameraIds.has(c.id) && c.latitude && c.longitude)
      .map(c => [c.latitude!, c.longitude!] as L.LatLngTuple);
    if (validPoints.length > 0) {
      const bounds = L.latLngBounds(validPoints);
      mapRef.current.fitBounds(bounds, { padding: [50, 50], maxZoom: 15 });
    }
  }, [cameras, isFiltered, filteredCameraIds]);

  // When filter is cleared, smoothly return to default network view
  useEffect(() => {
    if (prevFilteredRef.current && !isFiltered && mapRef.current) {
      mapRef.current.flyTo(
        MAP_CONFIG.DEFAULT_CENTER as L.LatLngTuple,
        MAP_CONFIG.DEFAULT_ZOOM,
        { duration: 0.8 }
      );
    }
    prevFilteredRef.current = isFiltered;
  }, [isFiltered]);

  useEffect(() => {
    if (!mapRef.current || !markersLayerRef.current) return;
    if (activeTooltipMarkerRef.current) {
      activeTooltipMarkerRef.current.closeTooltip();
      activeTooltipMarkerRef.current = null;
    }
    markersLayerRef.current.clearLayers();
    markersMapRef.current.clear();

    const zoom = mapRef.current.getZoom();
    const isFew = isFiltered && filteredCameraIds ? filteredCameraIds.size <= 40 : cameras.length <= 40;
    const isMacro = Boolean(hexOverlayEnabled && isHexVisibleAtZoom(zoom));
    const currentVerdicts = verdictsRef.current;
    const currentHotset = hotsetStatusRef.current;

    // Render dimmed markers first, then matching/active markers so matching markers appear on top
    const sortedCameras = [...cameras].sort((a, b) => {
      const aVerdict = currentVerdicts.get(a.id);
      const bVerdict = currentVerdicts.get(b.id);
      const aHotset = currentHotset.get(a.id);
      const bHotset = currentHotset.get(b.id);
      const aHasAI = isVerdictValid(aVerdict) || isHotsetStatusValid(aHotset);
      const bHasAI = isVerdictValid(bVerdict) || isHotsetStatusValid(bHotset);
      const aFilterDimmed = Boolean(isFiltered && filteredCameraIds && !filteredCameraIds.has(a.id));
      const bFilterDimmed = Boolean(isFiltered && filteredCameraIds && !filteredCameraIds.has(b.id));
      const aDimmed = aFilterDimmed || (isMacro && !aFilterDimmed && !aHasAI);
      const bDimmed = bFilterDimmed || (isMacro && !bFilterDimmed && !bHasAI);
      if (aDimmed === bDimmed) return 0;
      return aDimmed ? -1 : 1; // dimmed first (-1), matching/active second (+1)
    });

    sortedCameras.forEach(cam => {
      if (!cam.latitude || !cam.longitude) return;

      const verdict = currentVerdicts.get(cam.id);
      const hotset = currentHotset.get(cam.id);
      const hasAIVerdict = isVerdictValid(verdict) || isHotsetStatusValid(hotset);
      const isFilteredDimmed = Boolean(isFiltered && filteredCameraIds && !filteredCameraIds.has(cam.id));
      const isMacroHexDimmed = Boolean(isMacro && !isFilteredDimmed && !hasAIVerdict);
      const isDimmed = isFilteredDimmed || isMacroHexDimmed;

      const baseR = getRadius(zoom, isFew);
      const radius = isDimmed ? Math.max(4.0, Math.round(baseR * 0.7)) : baseR;
      const visual = getCameraMarkerVisual(cam.id, cam.isOnline, currentVerdicts, currentHotset);
      const fillColor = visual.fillColor;
      const strokeColor = isFilteredDimmed ? '#27272a' : (isMacroHexDimmed ? '#3f3f46' : FUNCTIONAL_COLORS.MAP_STROKE);
      const weight = isDimmed ? 1 : (zoom >= 14 ? 2 : 1.5);
      const opacity = isFilteredDimmed ? 0.25 : (isMacroHexDimmed ? 0.35 : 1);
      const fillOpacity = isFilteredDimmed ? 0.2 : (isMacroHexDimmed ? 0.25 : 0.95);
      const className = visual.isCongested ? 'marker-congested-pulse' : '';

      const marker = L.circleMarker([cam.latitude, cam.longitude], {
        radius,
        fillColor,
        color: strokeColor,
        weight,
        opacity,
        fillOpacity,
        className,
        pane: 'cameraMarkersPane',
      });

      const customMarker = marker as CustomCircleMarker;
      customMarker.isDimmed = isDimmed;
      customMarker.isFilteredDimmed = isFilteredDimmed;
      customMarker.hasAIVerdict = hasAIVerdict;
      customMarker.isHotset = visual.isHotset;
      customMarker.camId = cam.id;
      markersMapRef.current.set(cam.id, customMarker);

      marker.bindTooltip(buildTooltipHtml(cam, verdict, hotset, fillColor), {
        direction: 'top',
        offset: [0, -4]
      });

      marker.on('mouseover', () => {
        if (activeTooltipMarkerRef.current && activeTooltipMarkerRef.current !== marker) {
          activeTooltipMarkerRef.current.closeTooltip();
        }
        activeTooltipMarkerRef.current = marker;

        const curZ = mapRef.current ? mapRef.current.getZoom() : zoom;
        const curBase = getRadius(curZ, isFew);
        const hoverRadius = customMarker.isDimmed ? Math.max(5.5, Math.round(curBase * 0.7) + 2) : curBase + 3;
        marker.setRadius(hoverRadius);
        marker.setStyle({ 
          color: '#ffffff', 
          weight: customMarker.isDimmed ? 2 : 2.5,
          opacity: 1,
          fillOpacity: 1,
        });
      });

      marker.on('mouseout', () => {
        marker.closeTooltip();
        if (activeTooltipMarkerRef.current === marker) {
          activeTooltipMarkerRef.current = null;
        }

        const curZ = mapRef.current ? mapRef.current.getZoom() : zoom;
        const curBase = getRadius(curZ, isFew);
        const isFilteredDim = customMarker.isFilteredDimmed === true;
        const isMacroNow = Boolean(hexOverlayEnabledRef.current && isHexVisibleAtZoom(curZ));
        const isMacroDim = Boolean(isMacroNow && !isFilteredDim && !customMarker.hasAIVerdict);
        const isDim = isFilteredDim || isMacroDim;
        customMarker.isDimmed = isDim;

        const targetRadius = isDim ? Math.max(4.0, Math.round(curBase * 0.7)) : curBase;
        marker.setRadius(targetRadius);

        const strokeColor = isFilteredDim ? '#27272a' : (isMacroDim ? '#3f3f46' : FUNCTIONAL_COLORS.MAP_STROKE);
        const opacity = isFilteredDim ? 0.25 : (isMacroDim ? 0.35 : 1);
        const fillOpacity = isFilteredDim ? 0.2 : (isMacroDim ? 0.25 : 0.95);
        const visual = getCameraMarkerVisual(cam.id, cam.isOnline, verdictsRef.current, hotsetStatusRef.current);

        marker.setStyle({ 
          fillColor: visual.fillColor, 
          color: strokeColor, 
          weight: isDim ? 1 : (curZ >= 14 ? 2 : 1.5),
          opacity,
          fillOpacity,
          className: visual.isCongested ? 'marker-congested-pulse' : '',
        });

        const el = marker.getElement();
        if (el) {
          if (visual.isCongested) el.classList.add('marker-congested-pulse');
          else el.classList.remove('marker-congested-pulse');
        }
      });

      marker.on('click', () => {
        if (customMarker.isFilteredDimmed && onFilterClear) {
          onFilterClear();
        }
        if (onCameraClick) {
          onCameraClick(cam);
        }
      });

      markersLayerRef.current?.addLayer(marker);
    });
  }, [cameras, getCameraMarkerVisual, onCameraClick, isFiltered, filteredCameraIds, onFilterClear, appEnv, lang, hexOverlayEnabled, buildTooltipHtml]);

  // In-place marker style and tooltip updates on SSE live verdict streams or Hot Set status updates without tearing down DOM layers
  useEffect(() => {
    if (!markersLayerRef.current || markersMapRef.current.size === 0) return;

    const curZoom = mapRef.current ? mapRef.current.getZoom() : MAP_CONFIG.DEFAULT_ZOOM;
    const isMacro = Boolean(hexOverlayEnabledRef.current && isHexVisibleAtZoom(curZoom));

    cameras.forEach((cam) => {
      const marker = markersMapRef.current.get(cam.id);
      if (!marker) return;

      const verdict = verdicts.get(cam.id);
      const hotset = hotsetStatus.get(cam.id);
      const hasAIVerdict = isVerdictValid(verdict) || isHotsetStatusValid(hotset);
      marker.hasAIVerdict = hasAIVerdict;

      const isFilteredDimmed = marker.isFilteredDimmed === true;
      const isMacroHexDimmed = Boolean(isMacro && !isFilteredDimmed && !hasAIVerdict);
      const isDimmed = isFilteredDimmed || isMacroHexDimmed;
      marker.isDimmed = isDimmed;

      const visual = getCameraMarkerVisual(cam.id, cam.isOnline, verdicts, hotsetStatus);
      const strokeColor = isFilteredDimmed ? '#27272a' : (isMacroHexDimmed ? '#3f3f46' : FUNCTIONAL_COLORS.MAP_STROKE);
      const opacity = isFilteredDimmed ? 0.25 : (isMacroHexDimmed ? 0.35 : 1);
      const fillOpacity = isFilteredDimmed ? 0.2 : (isMacroHexDimmed ? 0.25 : 0.95);

      marker.setStyle({
        fillColor: visual.fillColor,
        color: strokeColor,
        opacity,
        fillOpacity,
        className: visual.isCongested ? 'marker-congested-pulse' : '',
      });

      const el = marker.getElement();
      if (el) {
        if (visual.isCongested) {
          el.classList.add('marker-congested-pulse');
        } else {
          el.classList.remove('marker-congested-pulse');
        }
      }

      marker.setTooltipContent(buildTooltipHtml(cam, verdict, hotset, visual.fillColor));
    });
  }, [verdicts, hotsetStatus, cameras, getCameraMarkerVisual, buildTooltipHtml]);

  return (
    <div className="relative w-full h-full overflow-hidden">
      <div ref={containerRef} className="w-full h-full z-0 absolute inset-0 bg-[#09090b]" />

      {/* Left controls: Lens switchers & camera filter toggle (Row 1), and contextual active filter chip (Row 2) */}
      {/* Desktop (md+): top-3.5 left-14; Mobile (<md): top-16 left-3 */}
      <div className="absolute top-16 left-3 md:top-3.5 md:left-14 z-[800] flex flex-col items-start gap-2 pointer-events-none">
        {/* Row 1: Lens switchers & camera filter toggle */}
        <div className="flex flex-col md:flex-row items-start md:items-center gap-2">
          {children}
          {cameraFilterControl}
        </div>

        {/* Row 2: On-map active filter chip (ADR-004 A3 - Issue #65 / Layout Deconfliction) */}
        {isFiltered && activeFilterLabel && (
          <div
            className="pointer-events-auto bg-zinc-950/95 border border-zinc-700 text-zinc-200 px-2.5 py-1.5 rounded-lg shadow-2xl backdrop-blur-md flex items-center gap-2 font-mono text-xs select-none max-w-[calc(100vw-24px)] md:max-w-xl transition-all"
            role="status"
            aria-live="polite"
          >
            <div className="flex items-start gap-1.5 min-w-0 flex-1">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 shrink-0 mt-1" />
              <span className="font-semibold text-zinc-100 whitespace-normal break-words leading-tight text-[11px] md:text-xs">
                {activeFilterLabel}
              </span>
            </div>
            {onFilterClear && (
              <button
                type="button"
                onClick={onFilterClear}
                className="px-2 py-1 rounded text-[10.5px] font-bold text-red-200 bg-red-950/80 border border-red-500/60 hover:bg-red-900 hover:text-white transition-all shrink-0 cursor-pointer flex items-center gap-1 shadow-sm active:scale-95 ml-1"
                aria-label={lang === 'vi' ? 'Xóa bộ lọc' : 'Clear filter'}
                title={lang === 'vi' ? 'Xóa bộ lọc (Esc)' : 'Clear filter (Esc)'}
              >
                <span className="font-bold text-xs leading-none">✕</span>
                <span className="leading-none">{lang === 'vi' ? 'Xóa lọc' : 'Clear'}</span>
              </button>
            )}
          </div>
        )}
      </div>

      {/* Right controls: Weather Widget & Map Legend */}
      {/* If split controls provided: Mobile renders Legend at bottom-20 left-3, Desktop renders Weather & Legend at top-3.5 right-3. Mobile weather is integrated in the top header bar. */}
      {weatherControl || legendControl ? (
        <>
          {/* Desktop view (md+): Both side-by-side in top-right */}
          <div className="hidden md:flex absolute top-3.5 right-3 z-[800] items-start gap-2 pointer-events-auto">
            {weatherControl}
            {legendControl}
          </div>

          {/* Mobile view (<md): Legend trigger at bottom-20 left-3 (bottom left corner, opposite to search FAB) */}
          {legendControl && (
            <div className="md:hidden absolute bottom-20 left-3 z-[830] pointer-events-auto">
              {legendControl}
            </div>
          )}
        </>
      ) : topRightControls ? (
        <div className="absolute top-28 right-3 md:top-3.5 md:right-3 z-[800] flex items-start gap-2 pointer-events-auto">
          {topRightControls}
        </div>
      ) : null}

      {/* Bottom map footer legend: Minimalist monochromatic arterial status (Issue #251 / S7-T6 / Layout Deconfliction) */}
      <div
        className={`absolute left-1/2 -translate-x-1/2 z-[700] pointer-events-auto select-none flex items-center gap-2 md:gap-3 px-3 py-1.5 rounded-md bg-zinc-950/95 border border-zinc-800 text-zinc-300 font-mono text-[10px] md:text-[11px] shadow-2xl backdrop-blur-md transition-all ${
          hasPinnedCameras ? 'bottom-14 md:bottom-2.5' : 'bottom-2.5'
        }`}
        role="region"
        aria-label={lang === 'vi' ? 'Chú giải mật độ điểm nghẽn trục chính' : 'Arterial saturation legend'}
      >
        <div className="flex items-center gap-1.5">
          <span className="w-2.5 h-2.5 rounded-full border border-black shrink-0" style={{ backgroundColor: '#10b981' }} />
          <span>{HOTSET_LEGEND_I18N.CLEAR[lang]}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="w-2.5 h-2.5 rounded-full border border-black shrink-0" style={{ backgroundColor: '#f59e0b' }} />
          <span>{HOTSET_LEGEND_I18N.MODERATE[lang]}</span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="relative flex h-2.5 w-2.5 shrink-0 items-center justify-center">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-red-400 opacity-60" />
            <span className="relative inline-flex h-2.5 w-2.5 rounded-full border border-black" style={{ backgroundColor: '#ef4444' }} />
          </span>
          <span>{HOTSET_LEGEND_I18N.CONGESTED[lang]}</span>
        </div>
      </div>
    </div>
  );
}
