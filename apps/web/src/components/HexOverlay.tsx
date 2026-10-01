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
  getFloodHexColorAndOpacity,
  isFloodVisibleAtZoom,
  getFloodInteractivityState,
  formatFloodHexTooltip,
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
  enabled: boolean;
  onToggle: () => void;
  mode?: HexOverlayMode;
  onModeChange?: (mode: HexOverlayMode) => void;
  lang?: 'vi' | 'en';
  floodLoading?: boolean;
  trafficLoading?: boolean;
}

export default function HexOverlay({
  map,
  hexCells,
  floodHexCells = new Map(),
  enabled,
  onToggle,
  mode,
  onModeChange,
  lang = 'vi',
  floodLoading = false,
  trafficLoading = false,
}: HexOverlayProps) {
  const layerGroupRef = useRef<L.LayerGroup | null>(null);
  const [internalMode, setInternalMode] = useState<HexOverlayMode>('flood');

  const currentMode = mode ?? internalMode;
  const handleModeChange = (newMode: HexOverlayMode) => {
    if (onModeChange) {
      onModeChange(newMode);
    } else {
      setInternalMode(newMode);
    }
  };

  const activeCellsCount = currentMode === 'traffic' ? hexCells.size : floodHexCells.size;

  useEffect(() => {
    if (!map) return;

    if (!map.getPane('hexOverlayPane')) {
      const hexPane = map.createPane('hexOverlayPane');
      hexPane.style.zIndex = '350';
    }

    if (!layerGroupRef.current) {
      layerGroupRef.current = L.layerGroup().addTo(map);
    }
    const layerGroup = layerGroupRef.current;
    layerGroup.clearLayers();

    if (!enabled || activeCellsCount === 0) {
      const pane = map.getPane('hexOverlayPane');
      if (pane) pane.style.pointerEvents = 'none';
      return;
    }

    const currentZoom = map.getZoom();
    const interactivity = currentMode === 'traffic'
      ? getHexInteractivityState(currentZoom)
      : getFloodInteractivityState(currentZoom);

    const { isVisible, pointerEvents, className } = interactivity;

    // Sync pane pointer-events with current zoom visibility
    const hexPane = map.getPane('hexOverlayPane');
    if (hexPane) {
      hexPane.style.pointerEvents = pointerEvents;
    }

    if (currentMode === 'traffic') {
      hexCells.forEach((cell) => {
        if (!cell.polygon || cell.polygon.length < 3) return;

        const { fillColor, fillOpacity } = getHexColorAndOpacity(cell.severity);

        const polygon = L.polygon(cell.polygon as L.LatLngTuple[], {
          fillColor,
          fillOpacity: isVisible ? fillOpacity : 0,
          color: '#52525b',
          weight: 1,
          opacity: isVisible ? 0.8 : 0,
          className,
          interactive: isVisible,
          pane: 'hexOverlayPane',
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
          polygon.bringToFront();
        });

        polygon.on('mouseout', () => {
          if (!isHexVisibleAtZoom(map.getZoom())) return;
          polygon.setStyle({
            weight: 1,
            color: '#52525b',
            fillOpacity,
          });
        });

        polygon.on('mousedown', (e) => {
          if (e.originalEvent && typeof e.originalEvent.preventDefault === 'function') {
            e.originalEvent.preventDefault();
          }
        });

        polygon.on('click', (e) => {
          if (!isHexVisibleAtZoom(map.getZoom())) return;
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

        layerGroup.addLayer(polygon);
      });
    } else {
      // Flood mode: render flood hex cells
      floodHexCells.forEach((cell) => {
        if (!cell.polygon || cell.polygon.length < 3) return;

        const { fillColor, fillOpacity, strokeColor, strokeWeight } = getFloodHexColorAndOpacity(cell.flood_severity);

        const polygon = L.polygon(cell.polygon as L.LatLngTuple[], {
          fillColor,
          fillOpacity: isVisible ? fillOpacity : 0,
          color: strokeColor,
          weight: strokeWeight,
          opacity: isVisible ? 0.85 : 0,
          className,
          interactive: isVisible,
          pane: 'hexOverlayPane',
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
            fillOpacity: Math.min(0.45, fillOpacity + 0.12),
          });
          polygon.bringToFront();
        });

        polygon.on('mouseout', () => {
          if (!isFloodVisibleAtZoom(map.getZoom())) return;
          polygon.setStyle({
            weight: strokeWeight,
            color: strokeColor,
            fillOpacity,
          });
        });

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
      const isVisibleNow = currentMode === 'traffic'
        ? isHexVisibleAtZoom(zoom)
        : isFloodVisibleAtZoom(zoom);

      const pane = map.getPane('hexOverlayPane');
      if (pane) {
        pane.style.pointerEvents = isVisibleNow ? 'auto' : 'none';
      }

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
  }, [map, hexCells, floodHexCells, enabled, currentMode, lang, activeCellsCount]);

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

  const floodedHexCount = useMemo(() => {
    let count = 0;
    floodHexCells.forEach((cell) => {
      const s = cell.flood_severity.toUpperCase().trim();
      if (s === 'FLOODED_DEEP' || s === 'FLOODED_SHALLOW' || s === 'WET') {
        count++;
      }
    });
    return count;
  }, [floodHexCells]);

  const buttonTitle = useMemo(() => {
    if (currentMode === 'flood') {
      if (floodedHexCount > 0) {
        return lang === 'vi'
          ? `Vùng ngập nước (${floodedHexCount} vùng ngập/mưa)`
          : `Flooded zones (${floodedHexCount} flood/rain zones)`;
      }
      return lang === 'vi' ? 'Bật/tắt vùng ngập nước H3' : 'Toggle flood zones';
    }

    if (congestedCount > 0) {
      return lang === 'vi'
        ? `Vùng tổng hợp (${congestedCount} vùng đang ùn tắc)`
        : `Zone overview (${congestedCount} congested zones)`;
    }
    return lang === 'vi' ? 'Bật/tắt hiển thị vùng tổng hợp H3' : 'Toggle H3 zone overview';
  }, [currentMode, floodedHexCount, congestedCount, lang]);

  return (
    <div className="pointer-events-auto flex items-center rounded border border-zinc-800 bg-zinc-950/90 shadow-2xl backdrop-blur-md p-1 gap-1 font-mono text-xs select-none">
      {/* Main Toggle Button */}
      <button
        type="button"
        onClick={onToggle}
        className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded font-semibold transition-all focus:outline-none cursor-pointer ${
          enabled
            ? 'bg-zinc-800 text-white border border-zinc-600'
            : 'text-zinc-400 hover:text-zinc-200'
        }`}
        aria-pressed={enabled}
        aria-label={lang === 'vi' ? 'Bật/tắt vùng tổng hợp' : 'Toggle zone overview'}
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

      {/* Mode Switcher Segmented Control (Visible when enabled) */}
      {enabled && (
        <>
          <span className="w-px h-4 bg-zinc-800 shrink-0 mx-0.5" />

          {/* Rain / Flood Mode Button */}
          <button
            type="button"
            onClick={() => handleModeChange('flood')}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded text-[11px] font-semibold transition-all focus:outline-none cursor-pointer ${
              currentMode === 'flood'
                ? 'bg-sky-500 text-zinc-950 font-bold shadow-sm'
                : 'text-zinc-400 hover:text-sky-300 hover:bg-zinc-900'
            }`}
            title={lang === 'vi' ? 'Hiển thị khả năng có mưa & ngập nước H3' : 'Show rain & flood forecast H3'}
          >
            <svg
              className={`w-3.5 h-3.5 shrink-0 transition-colors ${
                currentMode === 'flood' ? 'text-zinc-950' : 'text-sky-400'
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
            <span className="hidden sm:inline">{FLOOD_I18N.LAYER_FLOOD[lang]}</span>
            {floodLoading ? (
              <span
                className={`ml-0.5 px-1.5 py-0.5 rounded-full text-[9.5px] font-bold flex items-center gap-1 ${
                  currentMode === 'flood'
                    ? 'bg-zinc-950 text-sky-400'
                    : 'bg-sky-500/20 text-sky-300 border border-sky-500/40'
                }`}
                title={lang === 'vi' ? 'Đang tải dự báo mưa H3...' : 'Loading H3 rain forecast...'}
              >
                <span className="w-1.5 h-1.5 rounded-full bg-sky-400 animate-pulse" />
                <span className="hidden sm:inline text-[9px]">{lang === 'vi' ? 'đang tải...' : 'loading...'}</span>
              </span>
            ) : floodedHexCount > 0 ? (
              <span
                className={`ml-0.5 px-1.5 py-0.5 rounded-full text-[9.5px] font-bold ${
                  currentMode === 'flood'
                    ? 'bg-zinc-950 text-sky-400'
                    : 'bg-sky-500/20 text-sky-300 border border-sky-500/40'
                }`}
              >
                {floodedHexCount}
              </span>
            ) : null}
          </button>

          {/* Traffic Mode Button */}
          <button
            type="button"
            onClick={() => handleModeChange('traffic')}
            className={`flex items-center gap-1.5 px-2.5 py-1 rounded text-[11px] font-semibold transition-all focus:outline-none cursor-pointer ${
              currentMode === 'traffic'
                ? 'bg-white text-black font-bold shadow-sm'
                : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-900'
            }`}
            title={lang === 'vi' ? 'Hiển thị mật độ giao thông H3' : 'Show traffic density H3'}
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
            {trafficLoading ? (
              <span
                className={`ml-0.5 px-1.5 py-0.5 rounded-full text-[9.5px] font-bold flex items-center gap-1 ${
                  currentMode === 'traffic'
                    ? 'bg-red-600 text-white'
                    : 'bg-zinc-800 text-zinc-400 border border-zinc-700'
                }`}
                title={lang === 'vi' ? 'Đang tải dữ liệu giao thông H3...' : 'Loading H3 traffic data...'}
              >
                <span className="w-1.5 h-1.5 rounded-full bg-zinc-400 animate-pulse" />
              </span>
            ) : congestedCount > 0 ? (
              <span
                className={`ml-0.5 px-1.5 py-0.5 rounded-full text-[9.5px] font-bold ${
                  currentMode === 'traffic'
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
