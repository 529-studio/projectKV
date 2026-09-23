'use client';
import { useEffect, useRef, useCallback } from 'react';
import L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import { Camera } from '../hooks/useCameras';
import { TrafficVerdict } from '../hooks/useTraffic';
import { MAP_CONFIG, FUNCTIONAL_COLORS } from '../config/constants';

interface MapComponentProps {
  onCameraClick?: (cam: Camera) => void;
  cameras: Camera[];
  verdicts: Map<string, TrafficVerdict>;
}

export default function MapComponent({ cameras, verdicts, onCameraClick }: MapComponentProps) {
  const mapRef = useRef<L.Map | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const markersLayerRef = useRef<L.LayerGroup | null>(null);

  const getTrafficColor = useCallback((camId: string, isOnline: boolean) => {
    const verdict = verdicts.get(camId);
    if (verdict?.severity) {
      const sev = verdict.severity.toUpperCase();
      if (sev === 'CLEAR') return FUNCTIONAL_COLORS.CLEAR;
      if (sev === 'MODERATE') return FUNCTIONAL_COLORS.MODERATE;
      if (sev === 'HEAVY' || sev === 'CONGESTED') return FUNCTIONAL_COLORS.CONGESTED;
    }
    return isOnline ? FUNCTIONAL_COLORS.ONLINE_DEFAULT : FUNCTIONAL_COLORS.OFFLINE_DEFAULT;
  }, [verdicts]);

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
          const marker = layer as L.CircleMarker;
          if (marker.setRadius) {
            marker.setRadius(r);
            marker.setStyle({ weight: w });
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

  useEffect(() => {
    if (!mapRef.current || !markersLayerRef.current) return;
    markersLayerRef.current.clearLayers();

    const zoom = mapRef.current.getZoom();
    const isFew = cameras.length <= 40;

    cameras.forEach(cam => {
      if (!cam.latitude || !cam.longitude) return;
      
      const fillColor = getTrafficColor(cam.id, cam.isOnline);
      const marker = L.circleMarker([cam.latitude, cam.longitude], {
        radius: getRadius(zoom, isFew),
        fillColor,
        color: FUNCTIONAL_COLORS.MAP_STROKE,
        weight: zoom >= 14 ? 2 : 1.5,
        opacity: 1,
        fillOpacity: 0.95
      });

      const verdict = verdicts.get(cam.id);
      let trafficInfo = '';
      if (verdict?.severity) {
        trafficInfo = `<br><span style="color:${fillColor}; font-weight:bold;">Traffic: ${verdict.severity}</span>`;
      }

      marker.bindTooltip(`<b>${cam.displayName || cam.title}</b><br><span style="color:#a1a1aa;">${cam.zone}</span>${trafficInfo}`, {
        direction: 'top',
        offset: [0, -4]
      });

      marker.on('mouseover', () => {
        marker.setStyle({ radius: getRadius(mapRef.current!.getZoom(), isFew) + 4, fillColor: '#ffffff', color: '#ffffff', weight: 2.5 });
      });

      marker.on('mouseout', () => {
        const curZ = mapRef.current!.getZoom();
        marker.setStyle({ 
          radius: getRadius(curZ, isFew), 
          fillColor: getTrafficColor(cam.id, cam.isOnline), 
          color: FUNCTIONAL_COLORS.MAP_STROKE, 
          weight: curZ >= 14 ? 2 : 1.5 
        });
      });

      marker.on('click', () => {
        if (onCameraClick) {
          onCameraClick(cam);
        }
      });

      markersLayerRef.current?.addLayer(marker);
    });
  }, [cameras, verdicts, getTrafficColor, onCameraClick]);

  return <div ref={containerRef} className="w-full h-full z-0 absolute inset-0 bg-[#09090b]" />;
}
