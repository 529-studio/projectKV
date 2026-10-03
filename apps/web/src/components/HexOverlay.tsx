'use client';

import { useEffect, useRef, useMemo, useState } from 'react';
import L from 'leaflet';
import {
  HexCell,
  getHexColorAndOpacity,
  isHexVisibleAtZoom,
  formatHexTooltip,
} from '../utils/hexUtils';
import {
  FloodHexCell,
  getRadarHexColorAndOpacity,
  getVerifiedFloodHexColorAndOpacity,
  isFloodVisibleAtZoom,
  getFloodInteractivityState,
  formatFloodHexTooltip,
  formatRadarHexTooltip,
  FLOOD_I18N,
} from '../utils/floodUtils';

interface CustomPolygon extends L.Polygon {
  _baseFillOpacity?: number;
}

export type ActiveLensMode = 'flood' | 'radar' | 'traffic' | 'off';

export interface HexOverlayProps {
  map: L.Map | null;
  hexCells: Map<string, HexCell>;
  floodHexCells?: Map<string, FloodHexCell>;
  radarHexCells?: Map<string, FloodHexCell>;
  mode?: ActiveLensMode;
  onModeChange?: (mode: ActiveLensMode) => void;
  lang?: 'vi' | 'en';
  floodLoading?: boolean;
  radarLoading?: boolean;
  trafficLoading?: boolean;
}

export default function HexOverlay({
  map,
  hexCells,
  floodHexCells = new Map(),
  radarHexCells = new Map(),
  mode,
  onModeChange,
  lang = 'vi',
  floodLoading = false,
  radarLoading = false,
  trafficLoading = false,
}: HexOverlayProps) {
  const layerGroupRef = useRef<L.LayerGroup | null>(null);
  const [internalMode, setInternalMode] = useState<ActiveLensMode>('flood');

  const currentMode = mode ?? internalMode;

  const handleSelectMode = (newMode: ActiveLensMode) => {
    // If clicking already active mode, toggle it off for clean map exploration
    const nextMode = currentMode === newMode ? 'off' : newMode;
    if (onModeChange) {
      onModeChange(nextMode);
    } else {
      setInternalMode(nextMode);
    }
  };

  useEffect(() => {
    if (!map) return;

    if (!map.getPane('radarHexPane')) {
      const radarPane = map.createPane('radarHexPane');
      radarPane.style.zIndex = '340';
    }
    if (!map.getPane('trafficHexPane')) {
      const trafficPane = map.createPane('trafficHexPane');
      trafficPane.style.zIndex = '350';
    }
    if (!map.getPane('floodHexPane')) {
      const floodPane = map.createPane('floodHexPane');
      floodPane.style.zIndex = '360';
    }

    if (!layerGroupRef.current) {
      layerGroupRef.current = L.layerGroup().addTo(map);
    }
    const layerGroup = layerGroupRef.current;
    layerGroup.clearLayers();

    if (currentMode === 'off') {
      const pR = map.getPane('radarHexPane');
      if (pR) pR.style.pointerEvents = 'none';
      const pT = map.getPane('trafficHexPane');
      if (pT) pT.style.pointerEvents = 'none';
      const pF = map.getPane('floodHexPane');
      if (pF) pF.style.pointerEvents = 'none';
      return;
    }

    const currentZoom = map.getZoom();
    const interactivity = getFloodInteractivityState(currentZoom);
    const { isVisible, pointerEvents, className } = interactivity;

    const pRadar = map.getPane('radarHexPane');
    if (pRadar) pRadar.style.pointerEvents = pointerEvents;
    const pTraffic = map.getPane('trafficHexPane');
    if (pTraffic) pTraffic.style.pointerEvents = pointerEvents;
    const pFlood = map.getPane('floodHexPane');
    if (pFlood) pFlood.style.pointerEvents = pointerEvents;

    const attachCommonEvents = (polygon: CustomPolygon, baseFill: number) => {
      polygon._baseFillOpacity = baseFill;
      polygon.on('mousedown', (e) => {
        if (e.originalEvent && typeof e.originalEvent.preventDefault === 'function') {
          e.originalEvent.preventDefault();
        }
      });

      polygon.on('click', (e) => {
        if (!isFloodVisibleAtZoom(map.getZoom())) return;
        L.DomEvent.stopPropagation(e);
        polygon.closeTooltip();

        const target = e.originalEvent?.target as HTMLElement | SVGElement | undefined;
        if (target && typeof target.blur === 'function') {
          target.blur();
        }
        if (document.activeElement && typeof (document.activeElement as HTMLElement).blur === 'function') {
          (document.activeElement as HTMLElement).blur();
        }

        const bounds = polygon.getBounds();
        map.fitBounds(bounds, {
          padding: [30, 30],
          maxZoom: 14,
        });
      });
    };

    // 1. LENS 1: CAMERA-VERIFIED FLOOD (Observed Ground Truth - Default #1)
    if (currentMode === 'flood') {
      floodHexCells.forEach((cell) => {
        if (!cell.polygon || cell.polygon.length < 3) return;
        if (cell.h3_id && cell.h3_id.startsWith('89')) return;

        const { fillColor, fillOpacity, strokeColor, strokeWeight } =
          getVerifiedFloodHexColorAndOpacity(cell.flood_severity);

        const polygon = L.polygon(cell.polygon as L.LatLngTuple[], {
          fillColor,
          fillOpacity: isVisible ? fillOpacity : 0,
          color: strokeColor,
          weight: strokeWeight,
          opacity: isVisible ? 0.85 : 0,
          className: `${className} hex-cell-verified-flood`,
          interactive: isVisible,
          pane: 'floodHexPane',
        }) as CustomPolygon;

        polygon._baseFillOpacity = fillOpacity;

        const tooltipContent = formatFloodHexTooltip(cell, lang);
        polygon.bindTooltip(tooltipContent, {
          sticky: true,
          direction: 'top',
          offset: [0, -10],
          opacity: 0.95,
        });

        polygon.on('mouseover', () => {
          if (!isFloodVisibleAtZoom(map.getZoom())) return;
          polygon.setStyle({
            weight: strokeWeight + 1,
            color: '#38bdf8',
            fillOpacity: Math.min(0.5, fillOpacity + 0.12),
          });
        });

        polygon.on('mouseout', () => {
          polygon.closeTooltip();
          if (!isFloodVisibleAtZoom(map.getZoom())) return;
          polygon.setStyle({
            weight: strokeWeight,
            color: strokeColor,
            fillOpacity,
          });
        });

        attachCommonEvents(polygon, fillOpacity);
        layerGroup.addLayer(polygon);
      });
    }

    // 2. LENS 2: WEATHER RADAR FORECAST (Atmospheric Prediction)
    if (currentMode === 'radar') {
      radarHexCells.forEach((cell) => {
        if (!cell.polygon || cell.polygon.length < 3) return;
        if (cell.h3_id && cell.h3_id.startsWith('89')) return;

        const { fillColor, fillOpacity, strokeColor, strokeWeight, dashArray } =
          getRadarHexColorAndOpacity(cell.flood_severity);

        const polygon = L.polygon(cell.polygon as L.LatLngTuple[], {
          fillColor,
          fillOpacity: isVisible ? fillOpacity : 0,
          color: strokeColor,
          weight: strokeWeight,
          dashArray,
          opacity: isVisible ? 0.6 : 0,
          className: `${className} hex-cell-radar`,
          interactive: isVisible,
          pane: 'radarHexPane',
        }) as CustomPolygon;

        polygon._baseFillOpacity = fillOpacity;

        const tooltipContent = formatRadarHexTooltip(cell, lang);
        polygon.bindTooltip(tooltipContent, {
          sticky: true,
          direction: 'top',
          offset: [0, -10],
          opacity: 0.95,
        });

        polygon.on('mouseover', () => {
          if (!isFloodVisibleAtZoom(map.getZoom())) return;
          polygon.setStyle({
            weight: strokeWeight + 1,
            color: '#38bdf8',
            fillOpacity: Math.min(0.35, fillOpacity + 0.1),
          });
        });

        polygon.on('mouseout', () => {
          polygon.closeTooltip();
          if (!isFloodVisibleAtZoom(map.getZoom())) return;
          polygon.setStyle({
            weight: strokeWeight,
            color: strokeColor,
            fillOpacity,
          });
        });

        attachCommonEvents(polygon, fillOpacity);
        layerGroup.addLayer(polygon);
      });
    }

    // 3. LENS 3: TRAFFIC DENSITY (H3 congestion)
    if (currentMode === 'traffic') {
      hexCells.forEach((cell) => {
        if (!cell.polygon || cell.polygon.length < 3) return;
        if (cell.h3_id && cell.h3_id.startsWith('89')) return;

        const { fillColor, fillOpacity } = getHexColorAndOpacity(cell.severity);

        const polygon = L.polygon(cell.polygon as L.LatLngTuple[], {
          fillColor,
          fillOpacity: isVisible ? fillOpacity : 0,
          color: '#52525b',
          weight: 1,
          opacity: isVisible ? 0.8 : 0,
          className: `${className} hex-cell-traffic`,
          interactive: isVisible,
          pane: 'trafficHexPane',
        }) as CustomPolygon;

        polygon._baseFillOpacity = fillOpacity;

        const tooltipContent = formatHexTooltip(cell, lang);
        polygon.bindTooltip(tooltipContent, {
          sticky: true,
          direction: 'top',
          offset: [0, -10],
          opacity: 0.95,
        });

        polygon.on('mouseover', () => {
          if (!isHexVisibleAtZoom(map.getZoom())) return;
          polygon.setStyle({
            weight: 2,
            color: '#a1a1aa',
            fillOpacity: Math.min(0.4, fillOpacity + 0.1),
          });
        });

        polygon.on('mouseout', () => {
          polygon.closeTooltip();
          if (!isHexVisibleAtZoom(map.getZoom())) return;
          polygon.setStyle({
            weight: 1,
            color: '#52525b',
            fillOpacity,
          });
        });

        attachCommonEvents(polygon, fillOpacity);
        layerGroup.addLayer(polygon);
      });
    }

    const handleZoomStart = () => {
      if (!layerGroupRef.current) return;
      layerGroupRef.current.eachLayer((layer) => {
        (layer as L.Polygon).closeTooltip();
      });
    };

    const handleZoomEnd = () => {
      if (!layerGroupRef.current) return;
      const zoom = map.getZoom();
      const isVisibleNow = isFloodVisibleAtZoom(zoom);

      const pR = map.getPane('radarHexPane');
      if (pR) pR.style.pointerEvents = isVisibleNow ? 'auto' : 'none';
      const pT = map.getPane('trafficHexPane');
      if (pT) pT.style.pointerEvents = isVisibleNow ? 'auto' : 'none';
      const pF = map.getPane('floodHexPane');
      if (pF) pF.style.pointerEvents = isVisibleNow ? 'auto' : 'none';

      layerGroupRef.current.eachLayer((layer) => {
        const poly = layer as CustomPolygon;
        const baseFill = poly._baseFillOpacity ?? 0.18;
        poly.setStyle({
          fillOpacity: isVisibleNow ? baseFill : 0,
          opacity: isVisibleNow ? 0.8 : 0,
        });

        const el = (poly.getElement?.() || (poly as unknown as { _path?: SVGElement })._path) as HTMLElement | SVGElement | undefined;
        if (el) {
          if (isVisibleNow) {
            el.classList.remove('hex-cell-hidden');
            el.style.pointerEvents = 'auto';
          } else {
            el.classList.add('hex-cell-hidden');
            el.style.pointerEvents = 'none';
          }
        }

        if (!isVisibleNow) {
          poly.closeTooltip();
        }
      });
    };

    map.on('zoomstart', handleZoomStart);
    map.on('zoomend', handleZoomEnd);

    return () => {
      map.off('zoomstart', handleZoomStart);
      map.off('zoomend', handleZoomEnd);
      layerGroup.clearLayers();
    };
  }, [
    map,
    hexCells,
    floodHexCells,
    radarHexCells,
    currentMode,
    lang,
  ]);

  useEffect(() => {
    return () => {
      if (layerGroupRef.current) {
        layerGroupRef.current.clearLayers();
        layerGroupRef.current.remove();
        layerGroupRef.current = null;
      }
    };
  }, []);

  const congestedCount = useMemo(() => {
    let count = 0;
    hexCells.forEach((cell) => {
      const s = cell.severity.toUpperCase().trim();
      if (s === 'CONGESTED' || s === 'HEAVY') {
        count++;
      }
    });
    return count;
  }, [hexCells]);

  const floodedCameraHexCount = useMemo(() => {
    let count = 0;
    floodHexCells.forEach((cell) => {
      const s = cell.flood_severity.toUpperCase().trim();
      if (s === 'FLOODED_DEEP' || s === 'FLOODED_SHALLOW' || s === 'WET') {
        count++;
      }
    });
    return count;
  }, [floodHexCells]);

  const radarRainActive = useMemo(() => {
    return radarHexCells.size > 0;
  }, [radarHexCells]);

  return (
    <div className="pointer-events-auto flex items-center rounded-lg border border-zinc-800 bg-zinc-950/95 shadow-2xl backdrop-blur-md p-1 gap-1 font-mono text-xs select-none">
      {/* 1. LENS 1: Camera Verified Flood (Primary Default) */}
      <button
        type="button"
        onClick={() => handleSelectMode('flood')}
        aria-pressed={currentMode === 'flood'}
        className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-[11px] font-semibold transition-all focus:outline-none cursor-pointer border ${
          currentMode === 'flood'
            ? 'bg-blue-600 text-white border-blue-500 shadow-sm font-bold'
            : 'text-zinc-400 hover:text-blue-300 hover:bg-zinc-900 border-transparent'
        }`}
        title={`${FLOOD_I18N.LAYER_FLOOD_VERIFIED[lang]} — ${FLOOD_I18N.LAYER_FLOOD_VERIFIED_SUBTITLE[lang]}`}
      >
        <svg
          className={`w-3.5 h-3.5 shrink-0 transition-colors ${
            currentMode === 'flood' ? 'text-white' : 'text-blue-400'
          }`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M12 2.69l5.66 5.66a8 8 0 1 1-11.31 0z" />
        </svg>
        <span className="hidden sm:inline">
          {lang === 'vi' ? 'Camera ngập' : 'Camera flood'}
        </span>
        <span className="sm:hidden">
          {lang === 'vi' ? 'Ngập' : 'Flood'}
        </span>
        {floodLoading ? (
          <span className="w-1.5 h-1.5 rounded-full bg-blue-300 animate-pulse ml-0.5" />
        ) : floodedCameraHexCount > 0 ? (
          <span
            className={`ml-0.5 px-1.5 py-0.2 rounded-full text-[9.5px] font-bold ${
              currentMode === 'flood'
                ? 'bg-white text-blue-900'
                : 'bg-blue-500/20 text-blue-300 border border-blue-500/40'
            }`}
          >
            {floodedCameraHexCount}
          </span>
        ) : null}
      </button>

      {/* 2. LENS 2: Weather Radar Forecast */}
      <button
        type="button"
        onClick={() => handleSelectMode('radar')}
        aria-pressed={currentMode === 'radar'}
        className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-[11px] font-semibold transition-all focus:outline-none cursor-pointer border ${
          currentMode === 'radar'
            ? 'bg-sky-500 text-zinc-950 border-sky-400 shadow-sm font-bold'
            : 'text-zinc-400 hover:text-sky-300 hover:bg-zinc-900 border-transparent'
        }`}
        title={`${FLOOD_I18N.LAYER_RADAR[lang]} — ${FLOOD_I18N.LAYER_RADAR_SUBTITLE[lang]}`}
      >
        <svg
          className={`w-3.5 h-3.5 shrink-0 transition-colors ${
            currentMode === 'radar' ? 'text-zinc-950' : 'text-sky-400'
          }`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M4 14.899A7 7 0 1 1 15.71 8h1.79a4.5 4.5 0 0 1 2.5 8.242" />
          <path d="M16 14v6" />
          <path d="M8 14v6" />
          <path d="M12 16v6" />
        </svg>
        <span className="hidden sm:inline">{FLOOD_I18N.LAYER_RADAR[lang]}</span>
        <span className="sm:hidden">{FLOOD_I18N.LAYER_RADAR_SHORT[lang]}</span>
        {radarLoading ? (
          <span className="w-1.5 h-1.5 rounded-full bg-sky-300 animate-pulse ml-0.5" />
        ) : radarRainActive ? (
          <span
            className={`ml-0.5 px-1.5 py-0.2 rounded-full text-[9.5px] font-bold ${
              currentMode === 'radar'
                ? 'bg-zinc-950 text-sky-400'
                : 'bg-sky-500/20 text-sky-300 border border-sky-500/40'
            }`}
          >
            {lang === 'vi' ? 'Mưa' : 'Rain'}
          </span>
        ) : null}
      </button>

      {/* 3. LENS 3: Traffic Density */}
      <button
        type="button"
        onClick={() => handleSelectMode('traffic')}
        aria-pressed={currentMode === 'traffic'}
        className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-[11px] font-semibold transition-all focus:outline-none cursor-pointer border ${
          currentMode === 'traffic'
            ? 'bg-white text-black border-zinc-200 shadow-sm font-bold'
            : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900 border-transparent'
        }`}
        title={lang === 'vi' ? 'Vùng giao thông (mật độ xe)' : 'Traffic density zones'}
      >
        <svg
          className={`w-3.5 h-3.5 shrink-0 transition-colors ${
            currentMode === 'traffic' ? 'text-black' : 'text-zinc-400'
          }`}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M19 17h2c.6 0 1-.4 1-1v-3c0-.9-.7-1.7-1.5-1.9C18.7 10.6 16 10 16 10s-1.3-1.4-2.2-2.3c-.5-.4-1.1-.7-1.8-.7H5c-.6 0-1.1.4-1.4.9l-1.4 2.9A3 3 0 0 0 2 12v4c0 .6.4 1 1 1h2" />
          <circle cx="7" cy="17" r="2" />
          <path d="M9 17h6" />
          <circle cx="17" cy="17" r="2" />
        </svg>
        <span className="hidden sm:inline">{FLOOD_I18N.LAYER_TRAFFIC[lang]}</span>
        <span className="sm:hidden">{FLOOD_I18N.LAYER_TRAFFIC_SHORT[lang]}</span>
        {trafficLoading ? (
          <span className="w-1.5 h-1.5 rounded-full bg-zinc-400 animate-pulse ml-0.5" />
        ) : congestedCount > 0 ? (
          <span
            className={`ml-0.5 px-1.5 py-0.2 rounded-full text-[9.5px] font-bold ${
              currentMode === 'traffic'
                ? 'bg-red-600 text-white'
                : 'bg-red-500/20 text-red-400 border border-red-500/40'
            }`}
          >
            {congestedCount}
          </span>
        ) : null}
      </button>
    </div>
  );
}
