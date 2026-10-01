'use client';
import { useEffect, useRef } from 'react';
import L from 'leaflet';
import {
  HexCell,
  getHexColorAndOpacity,
  isHexVisibleAtZoom,
  getHexInteractivityState,
  formatHexTooltip,
} from '../utils/hexUtils';
import { HEX_I18N } from '../config/constants';

interface CustomPolygon extends L.Polygon {
  _baseFillOpacity?: number;
}

export interface HexOverlayProps {
  map: L.Map | null;
  hexCells: Map<string, HexCell>;
  enabled: boolean;
  onToggle: () => void;
  lang?: 'vi' | 'en';
}

export default function HexOverlay({
  map,
  hexCells,
  enabled,
  onToggle,
  lang = 'vi',
}: HexOverlayProps) {
  const layerGroupRef = useRef<L.LayerGroup | null>(null);

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

    if (!enabled || hexCells.size === 0) {
      const pane = map.getPane('hexOverlayPane');
      if (pane) pane.style.pointerEvents = 'none';
      return;
    }

    const currentZoom = map.getZoom();
    const { isVisible, pointerEvents, className } = getHexInteractivityState(currentZoom);

    // Sync pane pointer-events with current zoom visibility
    const hexPane = map.getPane('hexOverlayPane');
    if (hexPane) {
      hexPane.style.pointerEvents = pointerEvents;
    }

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

      // Prevent browser focus ring on SVG polygon element
      polygon.on('mousedown', (e) => {
        if (e.originalEvent && typeof e.originalEvent.preventDefault === 'function') {
          e.originalEvent.preventDefault();
        }
      });

      polygon.on('click', (e) => {
        if (!isHexVisibleAtZoom(map.getZoom())) return;
        L.DomEvent.stopPropagation(e);
        polygon.closeTooltip();

        // Remove browser focus ring on SVG polygon element immediately
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

    const handleZoomStart = () => {
      // Close open tooltips immediately when zoom starts
      if (!layerGroupRef.current) return;
      layerGroupRef.current.eachLayer((layer) => {
        (layer as L.Polygon).closeTooltip();
      });
    };

    const handleZoomEnd = () => {
      if (!layerGroupRef.current) return;
      const zoom = map.getZoom();
      const { isVisible: visible, pointerEvents } = getHexInteractivityState(zoom);

      const pane = map.getPane('hexOverlayPane');
      if (pane) {
        pane.style.pointerEvents = pointerEvents;
      }

      layerGroupRef.current.eachLayer((layer) => {
        const poly = layer as CustomPolygon;
        const baseFill = poly._baseFillOpacity ?? 0.18;
        poly.setStyle({
          fillOpacity: visible ? baseFill : 0,
          opacity: visible ? 0.8 : 0,
        });

        const el = (poly.getElement?.() || (poly as unknown as { _path?: SVGElement })._path) as HTMLElement | SVGElement | undefined;
        if (el) {
          if (visible) {
            el.classList.remove('hex-cell-hidden');
            el.style.pointerEvents = 'auto';
          } else {
            el.classList.add('hex-cell-hidden');
            el.style.pointerEvents = 'none';
          }
        }

        if (!visible) {
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
  }, [map, hexCells, enabled, lang]);

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

  return (
    <button
      type="button"
      onClick={onToggle}
      className={`pointer-events-auto flex items-center gap-1.5 px-2.5 py-1.5 rounded font-mono text-xs font-semibold transition-all shadow-2xl backdrop-blur-md focus:outline-none select-none ${
        enabled
          ? 'bg-zinc-900/95 border border-zinc-500 text-white shadow-emerald-950/20'
          : 'bg-zinc-950/90 border border-zinc-800 text-zinc-400 hover:text-zinc-200 hover:border-zinc-700'
      }`}
      aria-pressed={enabled}
      aria-label={lang === 'vi' ? 'Bật/tắt vùng tổng hợp' : 'Toggle zone overview'}
      title={lang === 'vi' ? 'Bật/tắt hiển thị vùng tổng hợp H3' : 'Toggle H3 zone overview'}
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
      <span>{HEX_I18N.TOGGLE[lang]}</span>
    </button>
  );
}
