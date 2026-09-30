'use client';
import { useEffect, useRef, useCallback } from 'react';
import L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import { Camera } from '../hooks/useCameras';
import { TrafficVerdict, isVerdictValid, getSeverityLabel } from '../hooks/useTraffic';
import { MAP_CONFIG, FUNCTIONAL_COLORS, TRAFFIC_SEVERITY, VERDICT_I18N } from '../config/constants';

interface CustomCircleMarker extends L.CircleMarker {
  isDimmed?: boolean;
  camId?: string;
}

interface MapComponentProps {
  onCameraClick?: (cam: Camera) => void;
  cameras: Camera[];
  verdicts: Map<string, TrafficVerdict>;
  selectedCamera?: Camera | null;
  isFiltered?: boolean;
  filteredCameraIds?: Set<string>;
  onFilterClear?: () => void;
  activeFilterLabel?: string | null;
  lang?: 'vi' | 'en';
  appEnv?: string;
}

export default function MapComponent({
  cameras,
  verdicts,
  onCameraClick,
  selectedCamera,
  isFiltered = false,
  filteredCameraIds,
  onFilterClear,
  activeFilterLabel,
  lang = 'vi',
  appEnv = 'production',
}: MapComponentProps) {
  const mapRef = useRef<L.Map | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const markersLayerRef = useRef<L.LayerGroup | null>(null);
  const prevFilteredRef = useRef(false);

  const getTrafficColor = useCallback((camId: string, isOnline: boolean) => {
    const verdict = verdicts.get(camId);
    if (isVerdictValid(verdict) && !verdict.stale) {
      if (verdict.source !== 'mock' || appEnv === 'dev') {
        const sev = verdict.severity.toUpperCase();
        if (sev === TRAFFIC_SEVERITY.CLEAR) return FUNCTIONAL_COLORS.CLEAR;
        if (sev === TRAFFIC_SEVERITY.MODERATE) return FUNCTIONAL_COLORS.MODERATE;
        if (sev === TRAFFIC_SEVERITY.HEAVY || sev === TRAFFIC_SEVERITY.CONGESTED) return FUNCTIONAL_COLORS.CONGESTED;
      }
    }
    return isOnline ? FUNCTIONAL_COLORS.ONLINE_DEFAULT : FUNCTIONAL_COLORS.OFFLINE_DEFAULT;
  }, [verdicts, appEnv]);

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

      markersLayerRef.current = L.layerGroup().addTo(map);
      mapRef.current = map;

      // Dynamically scale marker sizes on zoomend
      map.on('zoomend', () => {
        if (!markersLayerRef.current) return;
        const currentZoom = map.getZoom();
        const r = getRadius(currentZoom);
        const w = currentZoom >= 14 ? 2 : 1.5;
        markersLayerRef.current.eachLayer((layer) => {
          const marker = layer as CustomCircleMarker;
          const isDimmed = marker.isDimmed === true;
          const targetR = isDimmed ? Math.max(2.5, Math.round(r * 0.7)) : r;
          const targetW = isDimmed ? 1 : w;
          if (marker.setRadius) {
            marker.setRadius(targetR);
            marker.setStyle({ weight: targetW });
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
        mapRef.current.remove();
        mapRef.current = null;
      }
    };
  }, []);

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
    markersLayerRef.current.clearLayers();

    const zoom = mapRef.current.getZoom();
    const isFew = isFiltered && filteredCameraIds ? filteredCameraIds.size <= 40 : cameras.length <= 40;

    // Render dimmed markers first, then matching markers so matching markers appear on top
    const sortedCameras = [...cameras].sort((a, b) => {
      if (!isFiltered || !filteredCameraIds) return 0;
      const aDimmed = !filteredCameraIds.has(a.id);
      const bDimmed = !filteredCameraIds.has(b.id);
      if (aDimmed === bDimmed) return 0;
      return aDimmed ? -1 : 1; // dimmed first (-1), matching second (+1)
    });

    sortedCameras.forEach(cam => {
      if (!cam.latitude || !cam.longitude) return;

      const isDimmed = Boolean(isFiltered && filteredCameraIds && !filteredCameraIds.has(cam.id));
      const baseR = getRadius(zoom, isFew);
      const radius = isDimmed ? Math.max(2.5, Math.round(baseR * 0.7)) : baseR;
      const fillColor = getTrafficColor(cam.id, cam.isOnline);
      const strokeColor = isDimmed ? '#27272a' : FUNCTIONAL_COLORS.MAP_STROKE;
      const weight = isDimmed ? 1 : (zoom >= 14 ? 2 : 1.5);
      const opacity = isDimmed ? 0.25 : 1;
      const fillOpacity = isDimmed ? 0.2 : 0.95;

      const marker = L.circleMarker([cam.latitude, cam.longitude], {
        radius,
        fillColor,
        color: strokeColor,
        weight,
        opacity,
        fillOpacity,
      });

      const customMarker = marker as CustomCircleMarker;
      customMarker.isDimmed = isDimmed;
      customMarker.camId = cam.id;

      const verdict = verdicts.get(cam.id);
      let trafficInfo = '';
      if (isVerdictValid(verdict)) {
        const prefix = lang === 'vi' ? VERDICT_I18N.TRAFFIC_PREFIX.vi : VERDICT_I18N.TRAFFIC_PREFIX.en;
        if (verdict.stale) {
          const staleText = lang === 'vi' ? 'Dữ liệu cũ' : 'Stale data';
          trafficInfo = `<br><span style="color:#a1a1aa; font-weight:bold;">${prefix}: ${staleText}</span>`;
        } else if (verdict.source !== 'mock' || appEnv === 'dev') {
          const sevLabel = getSeverityLabel(verdict.severity, lang);
          trafficInfo = `<br><span style="color:${fillColor}; font-weight:bold;">${prefix}: ${sevLabel}</span>`;
        }
      }
      if (cam.isGhost) {
        trafficInfo += `<br><span style="color:#f59e0b; font-weight:bold;">[MẤT TÍN HIỆU THỰC ĐỊA]</span>`;
      } else if (cam.duplicateOf) {
        trafficInfo += `<br><span style="color:#a1a1aa;">[TRÙNG NGUỒN]</span>`;
      }

      marker.bindTooltip(`<b>${cam.displayName || cam.title}</b><br><span style="color:#a1a1aa;">${cam.zone}</span>${trafficInfo}`, {
        direction: 'top',
        offset: [0, -4]
      });

      marker.on('mouseover', () => {
        const curZ = mapRef.current ? mapRef.current.getZoom() : zoom;
        const curBase = getRadius(curZ, isFew);
        const hoverRadius = isDimmed ? Math.max(2.5, Math.round(curBase * 0.7)) + 2 : curBase + 3;
        marker.setStyle({ 
          radius: hoverRadius, 
          color: '#ffffff', 
          weight: isDimmed ? 2 : 2.5,
          opacity: isDimmed ? 0.8 : 1,
          fillOpacity: isDimmed ? 0.6 : 1,
        });
        marker.bringToFront();
      });

      marker.on('mouseout', () => {
        const curZ = mapRef.current ? mapRef.current.getZoom() : zoom;
        const curBase = getRadius(curZ, isFew);
        marker.setStyle({ 
          radius: isDimmed ? Math.max(2.5, Math.round(curBase * 0.7)) : curBase, 
          fillColor: getTrafficColor(cam.id, cam.isOnline), 
          color: isDimmed ? '#27272a' : FUNCTIONAL_COLORS.MAP_STROKE, 
          weight: isDimmed ? 1 : (curZ >= 14 ? 2 : 1.5),
          opacity: isDimmed ? 0.25 : 1,
          fillOpacity: isDimmed ? 0.2 : 0.95,
        });
      });

      marker.on('click', () => {
        if (isDimmed && onFilterClear) {
          onFilterClear();
        }
        if (onCameraClick) {
          onCameraClick(cam);
        }
      });

      markersLayerRef.current?.addLayer(marker);
    });
  }, [cameras, verdicts, getTrafficColor, onCameraClick, isFiltered, filteredCameraIds, onFilterClear, appEnv, lang]);

  return (
    <div className="relative w-full h-full overflow-hidden">
      <div ref={containerRef} className="w-full h-full z-0 absolute inset-0 bg-[#09090b]" />

      {/* On-map active filter chip (ADR-004 A3 - Issue #65) */}
      {isFiltered && activeFilterLabel && (
        <div
          className="absolute top-20 left-3 sm:top-3.5 sm:left-14 z-[800] max-w-[calc(100vw-32px)] sm:max-w-md bg-zinc-950/95 border border-zinc-700 text-zinc-200 px-2.5 py-1.5 rounded shadow-2xl backdrop-blur-md flex items-center gap-2 font-mono text-xs select-none transition-all"
          role="status"
          aria-live="polite"
        >
          <div className="flex items-center gap-1.5 truncate min-w-0">
            <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 shrink-0" />
            <span className="truncate font-semibold text-zinc-100">{activeFilterLabel}</span>
          </div>
          {onFilterClear && (
            <button
              type="button"
              onClick={onFilterClear}
              className="text-zinc-400 hover:text-white hover:bg-zinc-800 rounded p-0.5 ml-1 transition-colors shrink-0 focus:outline-none"
              aria-label={lang === 'vi' ? 'Xóa bộ lọc' : 'Clear filter'}
              title={lang === 'vi' ? 'Xóa bộ lọc (Esc)' : 'Clear filter (Esc)'}
            >
              <span className="text-sm font-bold leading-none block px-1">✕</span>
            </button>
          )}
        </div>
      )}
    </div>
  );
}
