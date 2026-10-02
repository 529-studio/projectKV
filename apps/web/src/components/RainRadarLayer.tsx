'use client';

import { useEffect, useRef } from 'react';
import L from 'leaflet';
import { RadarMetadata } from '../utils/weatherUtils';

interface RainRadarLayerProps {
  map: L.Map | null;
  radar: RadarMetadata | null;
  enabled: boolean;
  opacity?: number;
}

export default function RainRadarLayer({
  map,
  radar,
  enabled,
  opacity = 0.55,
}: RainRadarLayerProps) {
  const tileLayerRef = useRef<L.TileLayer | null>(null);

  useEffect(() => {
    if (!map) return;

    // Dedicated pane for RainViewer Doppler radar tiles
    // zIndex: 340 sits below hexOverlayPane (350) and cameraMarkersPane (600)
    if (!map.getPane('rainRadarPane')) {
      const radarPane = map.createPane('rainRadarPane');
      radarPane.style.zIndex = '340';
      radarPane.style.pointerEvents = 'none'; // Tile layer does not intercept mouse clicks
    }

    // Clean up existing layer if disabled or template changed
    if (!enabled || !radar?.latest_tile_template) {
      if (tileLayerRef.current) {
        map.removeLayer(tileLayerRef.current);
        tileLayerRef.current = null;
      }
      return;
    }

    const template = radar.latest_tile_template;

    if (tileLayerRef.current) {
      tileLayerRef.current.setUrl(template);
      tileLayerRef.current.setOpacity(opacity);
    } else {
      const layer = L.tileLayer(template, {
        pane: 'rainRadarPane',
        opacity,
        tileSize: 256,
        maxZoom: 18,
        updateWhenIdle: true,
        updateWhenZooming: false,
      });
      layer.addTo(map);
      tileLayerRef.current = layer;
    }

    return () => {
      if (tileLayerRef.current && map) {
        map.removeLayer(tileLayerRef.current);
        tileLayerRef.current = null;
      }
    };
  }, [map, radar?.latest_tile_template, enabled, opacity]);

  return null;
}
