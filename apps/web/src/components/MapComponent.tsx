'use client';
import { useEffect, useRef, useCallback } from 'react';
import L from 'leaflet';
import 'leaflet/dist/leaflet.css';
import { Camera } from '../hooks/useCameras';
import { TrafficVerdict, isVerdictValid } from '../hooks/useTraffic';
import { MAP_CONFIG, FUNCTIONAL_COLORS, TRAFFIC_SEVERITY } from '../config/constants';

interface MapComponentProps {
  onCameraClick?: (cam: Camera) => void;
  cameras: Camera[];
  verdicts: Map<string, TrafficVerdict>;
  selectedCamera?: Camera | null;
  isFiltered?: boolean;
}

export default function MapComponent({
  cameras,
  verdicts,
  onCameraClick,
  selectedCamera,
  isFiltered = false,
}: MapComponentProps) {
  const mapRef = useRef<L.Map | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const markersLayerRef = useRef<L.LayerGroup | null>(null);
  const prevFilteredRef = useRef(false);

  const getTrafficColor = useCallback((camId: string, isOnline: boolean) => {
    const verdict = verdicts.get(camId);
    if (isVerdictValid(verdict) && verdict.source !== 'mock') {
      const sev = verdict.severity.toUpperCase();
      if (sev === TRAFFIC_SEVERITY.CLEAR) return FUNCTIONAL_COLORS.CLEAR;
      if (sev === TRAFFIC_SEVERITY.MODERATE) return FUNCTIONAL_COLORS.MODERATE;
      if (sev === TRAFFIC_SEVERITY.HEAVY || sev === TRAFFIC_SEVERITY.CONGESTED) return FUNCTIONAL_COLORS.CONGESTED;
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
    if (!mapRef.current || !isFiltered || cameras.length === 0) return;
    const validPoints = cameras
      .filter(c => c.latitude && c.longitude)
      .map(c => [c.latitude!, c.longitude!] as L.LatLngTuple);
    if (validPoints.length > 0) {
      const bounds = L.latLngBounds(validPoints);
      mapRef.current.fitBounds(bounds, { padding: [50, 50], maxZoom: 15 });
    }
  }, [cameras, isFiltered]);

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
      if (isVerdictValid(verdict) && verdict.source !== 'mock') {
        trafficInfo = `<br><span style="color:${fillColor}; font-weight:bold;">Traffic: ${verdict.severity}</span>`;
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
        marker.setStyle({ 
          radius: getRadius(mapRef.current!.getZoom(), isFew) + 3, 
          color: '#ffffff', 
          weight: 2.5 
        });
        marker.bringToFront();
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
