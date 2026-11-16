'use client';

import { useEffect, useRef } from 'react';
import L from 'leaflet';
import type { CorridorRouteResponse, CorridorCameraPointProperties } from '../types/corridor.ts';

interface CorridorRouteLayerProps {
  map: L.Map | null;
  routeData: CorridorRouteResponse | null;
  onCameraClick?: (camId: string) => void;
  lang?: 'vi' | 'en';
}

export default function CorridorRouteLayer({
  map,
  routeData,
  onCameraClick,
  lang = 'vi',
}: CorridorRouteLayerProps) {
  const layerGroupRef = useRef<L.LayerGroup | null>(null);
  const activeRoadKeyRef = useRef<string | null>(null);

  useEffect(() => {
    if (!map) return;

    // Create dedicated corridor pane with zIndex 560
    if (!map.getPane('corridorRoutePane')) {
      const pane = map.createPane('corridorRoutePane');
      pane.style.zIndex = '560';
    }

    if (!layerGroupRef.current) {
      layerGroupRef.current = L.layerGroup().addTo(map);
    }

    const layerGroup = layerGroupRef.current;
    layerGroup.clearLayers();

    if (!routeData || !routeData.geojson || !routeData.geojson.features) {
      activeRoadKeyRef.current = null;
      return;
    }

    const { summary, geojson } = routeData;
    const isNewCorridor = activeRoadKeyRef.current !== summary.road_key;
    activeRoadKeyRef.current = summary.road_key;

    let corridorPolyline: L.Polyline | null = null;

    // 1. Render LineString corridor route
    for (const feature of geojson.features) {
      if (feature.geometry.type === 'LineString') {
        const coords = (feature.geometry.coordinates as [number, number][]).map(
          ([lon, lat]) => [lat, lon] as [number, number]
        );

        if (coords.length < 2) continue;

        const color = summary.status_color || '#22c55e';

        // Outer glow polyline for high-contrast sunlight readability
        const glowLine = L.polyline(coords, {
          pane: 'corridorRoutePane',
          color,
          weight: 12,
          opacity: 0.35,
          lineCap: 'round',
          lineJoin: 'round',
        });

        // Core solid polyline
        const coreLine = L.polyline(coords, {
          pane: 'corridorRoutePane',
          color,
          weight: 6,
          opacity: 0.95,
          lineCap: 'round',
          lineJoin: 'round',
        });

        const estMinutes = Math.round(summary.estimated_seconds / 60);
        const freeFlowMinutes = Math.round(summary.free_flow_seconds / 60);
        const delayMinutes = Math.round(summary.delay_seconds / 60);

        const tooltipContent = `
          <div class="font-mono text-xs text-white bg-zinc-950 p-2 rounded border border-zinc-800 shadow-xl min-w-[200px]">
            <div class="font-bold text-sm tracking-wide mb-1 text-zinc-100">${summary.display_name}</div>
            <div class="text-[11px] text-zinc-400 mb-1.5">${summary.description}</div>
            <div class="flex items-center justify-between text-[11px] mb-1">
              <span class="text-zinc-400">${lang === 'vi' ? 'Độ dài:' : 'Length:'}</span>
              <span class="font-semibold text-zinc-200">${summary.length_km.toFixed(1)} km</span>
            </div>
            <div class="flex items-center justify-between text-[11px] mb-1">
              <span class="text-zinc-400">${lang === 'vi' ? 'Dự kiến:' : 'Est. time:'}</span>
              <span class="font-semibold text-zinc-200">${estMinutes} ${lang === 'vi' ? 'phút' : 'min'} (${freeFlowMinutes}m std)</span>
            </div>
            ${
              delayMinutes > 0
                ? `<div class="flex items-center justify-between text-[11px] mb-1 text-amber-400">
                    <span>${lang === 'vi' ? 'Chậm trễ:' : 'Delay:'}</span>
                    <span class="font-bold">+${delayMinutes} ${lang === 'vi' ? 'phút' : 'min'}</span>
                  </div>`
                : ''
            }
            ${
              summary.max_depth_cm && summary.max_depth_cm > 0
                ? `<div class="flex items-center justify-between text-[11px] mt-1 pt-1 border-t border-zinc-800 text-rose-400">
                    <span>${lang === 'vi' ? 'Ngập đỉnh:' : 'Peak flood:'}</span>
                    <span class="font-bold">${summary.max_depth_cm.toFixed(0)} cm</span>
                  </div>`
                : ''
            }
          </div>
        `;

        coreLine.bindTooltip(tooltipContent, {
          sticky: true,
          direction: 'top',
          className: 'corridor-tooltip',
        });

        layerGroup.addLayer(glowLine);
        layerGroup.addLayer(coreLine);
        corridorPolyline = coreLine;
      }
    }

    // 2. Render Checkpoint Camera Pins along Corridor
    for (const feature of geojson.features) {
      if (feature.geometry.type === 'Point') {
        const [lon, lat] = feature.geometry.coordinates as [number, number];
        const props = feature.properties as CorridorCameraPointProperties;
        const camId = props.camera_id;
        const markerColor = props['marker-color'] || '#3b82f6';
        const floodDepth = props.flood_depth_cm;

        // Interactive checkpoint circle marker
        const marker = L.circleMarker([lat, lon], {
          pane: 'corridorRoutePane',
          radius: floodDepth && floodDepth > 0 ? 9 : 7,
          fillColor: markerColor,
          color: '#ffffff',
          weight: 2,
          fillOpacity: 0.95,
        });

        if (camId && onCameraClick) {
          marker.on('click', () => {
            onCameraClick(camId);
          });
        }

        const tooltipHtml = `
          <div class="font-mono text-xs text-white bg-zinc-950 p-2 rounded border border-zinc-800">
            <div class="font-semibold text-zinc-100 mb-0.5">${props.name || camId}</div>
            <div class="text-[11px] text-zinc-400">Giao thông: ${props.severity || 'UNKNOWN'}</div>
            ${
              floodDepth && floodDepth > 0
                ? `<div class="text-[11px] font-bold text-rose-400 mt-0.5">Mực nước: ${floodDepth.toFixed(0)} cm</div>`
                : ''
            }
          </div>
        `;
        marker.bindTooltip(tooltipHtml, { direction: 'top' });

        layerGroup.addLayer(marker);
      }
    }

    // 3. Smoothly animate and fit viewport bounds on initial corridor selection
    if (corridorPolyline && isNewCorridor) {
      const bounds = corridorPolyline.getBounds();
      if (bounds.isValid()) {
        map.fitBounds(bounds, {
          padding: [60, 60],
          maxZoom: 15,
          animate: true,
        });
      }
    }

    return () => {
      layerGroup.clearLayers();
    };
  }, [map, routeData, onCameraClick, lang]);

  return null;
}
