'use client';

import { useEffect, useRef, useMemo, useState } from 'react';
import L from 'leaflet';
import {
  HexCell,
  getHexColorAndOpacity,
  isHexVisibleAtZoom,
  getHexInteractivityState,
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
import { HEX_I18N } from '../config/constants';

interface CustomPolygon extends L.Polygon {
  _baseFillOpacity?: number;
}

export type HexOverlayMode = 'traffic' | 'flood';

export interface HexOverlayProps {
  map: L.Map | null;
  hexCells: Map<string, HexCell>;
  floodHexCells?: Map<string, FloodHexCell>;
  radarHexCells?: Map<string, FloodHexCell>;
  enabled: boolean;
  onToggle: () => void;
  // Independent layer controls (S9 - Issue #144)
  showRadar?: boolean;
  onToggleRadar?: () => void;
  showFlood?: boolean;
  onToggleFlood?: () => void;
  showTraffic?: boolean;
  onToggleTraffic?: () => void;
  // Backwards compatibility for existing mode/onModeChange
  mode?: HexOverlayMode;
  onModeChange?: (mode: HexOverlayMode) => void;
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
  enabled,
  onToggle,
  showRadar,
  onToggleRadar,
  showFlood,
  onToggleFlood,
  showTraffic,
  onToggleTraffic,
  mode,
  onModeChange,
  lang = 'vi',
  floodLoading = false,
  radarLoading = false,
  trafficLoading = false,
}: HexOverlayProps) {
  const layerGroupRef = useRef<L.LayerGroup | null>(null);

  // Fallback internal state if independent toggles not supplied
  const [internalShowRadar, setInternalShowRadar] = useState(true);
  const [internalShowFlood, setInternalShowFlood] = useState(true);
  const [internalShowTraffic, setInternalShowTraffic] = useState(false);

  // Determine active state for each layer
  const isRadarActive = showRadar !== undefined
    ? showRadar
    : (mode !== undefined ? mode === 'flood' : internalShowRadar);

  const isFloodActive = showFlood !== undefined
    ? showFlood
    : (mode !== undefined ? mode === 'flood' : internalShowFlood);

  const isTrafficActive = showTraffic !== undefined
    ? showTraffic
    : (mode !== undefined ? mode === 'traffic' : internalShowTraffic);

  const handleToggleRadar = () => {
    if (onToggleRadar) {
      onToggleRadar();
    } else {
      setInternalShowRadar((prev) => !prev);
      if (mode !== undefined && onModeChange && !isRadarActive) {
        onModeChange('flood');
      }
    }
  };

  const handleToggleFlood = () => {
    if (onToggleFlood) {
      onToggleFlood();
    } else {
      setInternalShowFlood((prev) => !prev);
      if (mode !== undefined && onModeChange && !isFloodActive) {
        onModeChange('flood');
      }
    }
  };

  const handleToggleTraffic = () => {
    if (onToggleTraffic) {
      onToggleTraffic();
    } else {
      setInternalShowTraffic((prev) => !prev);
      if (mode !== undefined && onModeChange) {
        onModeChange(isTrafficActive ? 'flood' : 'traffic');
      }
    }
  };

  const activeCellsCount =
    (isTrafficActive ? hexCells.size : 0) +
    (isFloodActive ? floodHexCells.size : 0) +
    (isRadarActive ? radarHexCells.size : 0);

  useEffect(() => {
    if (!map) return;

    // Create separate z-indexed panes to guarantee camera-verified flood hexes
    // are physically drawn ON TOP of radar forecast hexes and never obscured.
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

    if (!enabled || activeCellsCount === 0) {
      const pRadar = map.getPane('radarHexPane');
      if (pRadar) pRadar.style.pointerEvents = 'none';
      const pTraffic = map.getPane('trafficHexPane');
      if (pTraffic) pTraffic.style.pointerEvents = 'none';
      const pFlood = map.getPane('floodHexPane');
      if (pFlood) pFlood.style.pointerEvents = 'none';
      return;
    }

    const currentZoom = map.getZoom();
    const interactivity = getFloodInteractivityState(currentZoom);
    const { isVisible, pointerEvents, className } = interactivity;

    // Sync pane pointer-events with current zoom visibility
    const pRadar = map.getPane('radarHexPane');
    if (pRadar) pRadar.style.pointerEvents = pointerEvents;
    const pTraffic = map.getPane('trafficHexPane');
    if (pTraffic) pTraffic.style.pointerEvents = pointerEvents;
    const pFlood = map.getPane('floodHexPane');
    if (pFlood) pFlood.style.pointerEvents = pointerEvents;

    // Helper for polygon zoom-to-bounds click
    const attachCommonEvents = (polygon: CustomPolygon, baseFill: number) => {
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

    // 1. RENDER RADAR FORECAST LAYER (Pane: radarHexPane, zIndex: 340)
    // Uses dashed border and subtle opacity to visually represent forecast prediction
    if (isRadarActive) {
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

    // 2. RENDER TRAFFIC DENSITY LAYER (Pane: trafficHexPane, zIndex: 350)
    if (isTrafficActive) {
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

    // 3. RENDER CAMERA-VERIFIED FLOOD LAYER (Pane: floodHexPane, zIndex: 360)
    // Solid bold border, high visual precedence — never hidden behind radar
    if (isFloodActive) {
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
    enabled,
    isRadarActive,
    isFloodActive,
    isTrafficActive,
    lang,
    activeCellsCount,
  ]);

  // Clean up layer group when component unmounts
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

  const radarHexCount = useMemo(() => {
    let count = 0;
    radarHexCells.forEach((cell) => {
      const s = cell.flood_severity.toUpperCase().trim();
      if (s === 'FLOODED_DEEP' || s === 'FLOODED_SHALLOW' || s === 'WET') {
        count++;
      }
    });
    return count;
  }, [radarHexCells]);

  const buttonTitle = useMemo(() => {
    const parts: string[] = [];
    if (isFloodActive && floodedCameraHexCount > 0) {
      parts.push(lang === 'vi' ? `${floodedCameraHexCount} điểm ngập camera` : `${floodedCameraHexCount} camera flood spots`);
    }
    if (isRadarActive && radarHexCount > 0) {
      parts.push(lang === 'vi' ? 'Dự báo radar' : 'Radar forecast');
    }
    if (isTrafficActive && congestedCount > 0) {
      parts.push(lang === 'vi' ? `${congestedCount} vùng ùn tắc` : `${congestedCount} congested zones`);
    }
    if (parts.length > 0) return parts.join(' • ');
    return lang === 'vi' ? 'Bật/tắt hiển thị vùng' : 'Toggle zone layers';
  }, [isFloodActive, isRadarActive, isTrafficActive, floodedCameraHexCount, radarHexCount, congestedCount, lang]);

  return (
    <div className="pointer-events-auto flex items-center rounded border border-zinc-800 bg-zinc-950/90 shadow-2xl backdrop-blur-md p-1 gap-1 font-mono text-xs select-none">
      {/* Master Toggle Button */}
      <button
        type="button"
        onClick={onToggle}
        className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded font-semibold transition-all focus:outline-none cursor-pointer ${
          enabled
            ? 'bg-zinc-800 text-white border border-zinc-600'
            : 'text-zinc-400 hover:text-zinc-200'
        }`}
        aria-pressed={enabled}
        aria-label={lang === 'vi' ? 'Bật/tắt lớp bản đồ' : 'Toggle map layers'}
        title={buttonTitle}
      >
        <svg
          className={`w-3.5 h-3.5 shrink-0 transition-colors ${
            enabled ? 'text-zinc-100 fill-zinc-100/20' : 'text-zinc-400 fill-none'
          }`}
          viewBox="0 0 24 24"
          stroke="currentColor"
          strokeWidth="1.75"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <polygon points="12 2.5 21 7.5 21 16.5 12 21.5 3 16.5 3 7.5" />
        </svg>
        <span className="hidden sm:inline">{HEX_I18N.TOGGLE[lang]}</span>
        <span className="sm:hidden">{lang === 'vi' ? 'Vùng' : 'Zone'}</span>
      </button>

      {/* Independent Layer Toggle Chips (Visible when master overlay enabled) */}
      {enabled && (
        <>
          <span className="w-px h-4 bg-zinc-800 shrink-0 mx-0.5" />

          {/* 1. Camera-verified Flood Layer Toggle Chip (Observed Ground Truth) */}
          <button
            type="button"
            onClick={handleToggleFlood}
            aria-pressed={isFloodActive}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded text-[11px] font-semibold transition-all focus:outline-none cursor-pointer border ${
              isFloodActive
                ? 'bg-blue-600 text-white border-blue-500 shadow-sm font-bold'
                : 'text-zinc-400 hover:text-blue-300 hover:bg-zinc-900 border-transparent'
            }`}
            title={`${FLOOD_I18N.LAYER_FLOOD_VERIFIED[lang]} — ${FLOOD_I18N.LAYER_FLOOD_VERIFIED_SUBTITLE[lang]}`}
          >
            <svg
              className={`w-3.5 h-3.5 shrink-0 transition-colors ${
                isFloodActive ? 'text-white' : 'text-blue-400'
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
            <span className="hidden sm:inline">{FLOOD_I18N.LAYER_FLOOD_VERIFIED[lang]}</span>
            <span className="sm:hidden">{FLOOD_I18N.LAYER_FLOOD_VERIFIED_SHORT[lang]}</span>
            {floodLoading ? (
              <span className="w-1.5 h-1.5 rounded-full bg-blue-300 animate-pulse ml-0.5" />
            ) : floodedCameraHexCount > 0 ? (
              <span
                className={`ml-0.5 px-1.5 py-0.2 rounded-full text-[9.5px] font-bold ${
                  isFloodActive
                    ? 'bg-white text-blue-900'
                    : 'bg-blue-500/20 text-blue-300 border border-blue-500/40'
                }`}
              >
                {floodedCameraHexCount}
              </span>
            ) : null}
          </button>

          {/* 2. Radar Forecast Layer Toggle Chip (Atmospheric Prediction) */}
          <button
            type="button"
            onClick={handleToggleRadar}
            aria-pressed={isRadarActive}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded text-[11px] font-semibold transition-all focus:outline-none cursor-pointer border ${
              isRadarActive
                ? 'bg-sky-500 text-zinc-950 border-sky-400 shadow-sm font-bold'
                : 'text-zinc-400 hover:text-sky-300 hover:bg-zinc-900 border-transparent'
            }`}
            title={`${FLOOD_I18N.LAYER_RADAR[lang]} — ${FLOOD_I18N.LAYER_RADAR_SUBTITLE[lang]}`}
          >
            <svg
              className={`w-3.5 h-3.5 shrink-0 transition-colors ${
                isRadarActive ? 'text-zinc-950' : 'text-sky-400'
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
            ) : radarHexCount > 0 ? (
              <span
                className={`ml-0.5 px-1.5 py-0.2 rounded-full text-[9.5px] font-bold ${
                  isRadarActive
                    ? 'bg-zinc-950 text-sky-400'
                    : 'bg-sky-500/20 text-sky-300 border border-sky-500/40'
                }`}
              >
                {lang === 'vi' ? 'Mưa' : 'Rain'}
              </span>
            ) : null}
          </button>

          {/* 3. Traffic Density Layer Toggle Chip */}
          <button
            type="button"
            onClick={handleToggleTraffic}
            aria-pressed={isTrafficActive}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded text-[11px] font-semibold transition-all focus:outline-none cursor-pointer border ${
              isTrafficActive
                ? 'bg-white text-black border-zinc-200 shadow-sm font-bold'
                : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900 border-transparent'
            }`}
            title={lang === 'vi' ? 'Vùng giao thông (mật độ xe)' : 'Traffic density zones'}
          >
            <svg
              className={`w-3.5 h-3.5 shrink-0 transition-colors ${
                isTrafficActive ? 'text-black' : 'text-zinc-400'
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
                  isTrafficActive
                    ? 'bg-red-600 text-white'
                    : 'bg-red-500/20 text-red-400 border border-red-500/40'
                }`}
              >
                {congestedCount}
              </span>
            ) : null}
          </button>
        </>
      )}
    </div>
  );
}
