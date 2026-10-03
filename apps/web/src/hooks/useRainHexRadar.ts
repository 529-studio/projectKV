'use client';

import { useState, useEffect, useRef } from 'react';
import type { FloodHexCell } from '../utils/floodUtils';
import type { RadarMetadata, WeatherData } from '../utils/weatherUtils';
import type { Camera } from './useCameras';
import {
  getPolygonCentroid,
  latLonToTilePixel,
  mapRadarPixelToSeverity,
  buildRadarTileUrl,
  generateOpenMeteoFallbackHexCells,
} from '../utils/rainHexUtils';

// Module-level cache for static H3 resolution 7 polygon definitions (121KB)
let cachedPolygons: Record<string, [number, number][]> | null = null;
let polygonFetchPromise: Promise<Record<string, [number, number][]> | null> | null = null;

async function loadH3Polygons(): Promise<Record<string, [number, number][]> | null> {
  if (cachedPolygons) return cachedPolygons;
  if (polygonFetchPromise) return polygonFetchPromise;

  polygonFetchPromise = fetch('/h3_hex_polygons.json')
    .then((res) => (res.ok ? res.json() : null))
    .then((data) => {
      if (data && typeof data === 'object') {
        cachedPolygons = data;
        return data;
      }
      return null;
    })
    .catch((err) => {
      console.warn('Failed to load /h3_hex_polygons.json:', err);
      return null;
    });

  return polygonFetchPromise;
}

function loadImage(src: string): Promise<HTMLImageElement | null> {
  return new Promise((resolve) => {
    const img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = () => resolve(img);
    img.onerror = (e) => {
      console.warn('Radar tile load failed:', src, e);
      resolve(null);
    };
    img.src = src;
  });
}

export interface UseRainHexRadarReturn {
  rainRadarHexCells: Map<string, FloodHexCell>;
  loading: boolean;
  error: string | null;
}

export function useRainHexRadar(
  radar: RadarMetadata | null,
  weather: WeatherData | null,
  cameras: Camera[]
): UseRainHexRadarReturn {
  const [rainRadarHexCells, setRainRadarHexCells] = useState<Map<string, FloodHexCell>>(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Map camera count by H3 cell ID for richer tooltips
  const camerasByCellRef = useRef<Map<string, number>>(new Map());
  useEffect(() => {
    const counts = new Map<string, number>();
    cameras.forEach((cam) => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const h3Id = (cam as any).h3_id || (cam as any).h3_res7 || (cam as any).h3Res7;
      if (h3Id) {
        counts.set(h3Id, (counts.get(h3Id) || 0) + 1);
      }
    });
    camerasByCellRef.current = counts;
  }, [cameras]);

  useEffect(() => {
    let isCancelled = false;

    async function processRadar() {
      try {
        setLoading(true);
        setError(null);

        const polygons = await loadH3Polygons();
        if (isCancelled) return;

        if (!polygons) {
          setRainRadarHexCells(new Map());
          return;
        }

        const template = radar?.latest_tile_template;
        if (!template) {
          // If no radar template, check Open-Meteo fallback
          if (weather) {
            const monitoredSet = new Set(Object.keys(polygons));
            const fallback = generateOpenMeteoFallbackHexCells(weather, polygons, monitoredSet);
            if (!isCancelled) setRainRadarHexCells(fallback);
          } else {
            if (!isCancelled) setRainRadarHexCells(new Map());
          }
          return;
        }

        // HCMC & Southeast Vietnam fit inside Zoom 7 tiles: (x=101, y=60) and (x=102, y=60)
        const tile101Url = buildRadarTileUrl(template, 101, 60, 7);
        const tile102Url = buildRadarTileUrl(template, 102, 60, 7);

        const [img101, img102] = await Promise.all([
          loadImage(tile101Url),
          loadImage(tile102Url),
        ]);

        if (isCancelled) return;

        // If both tile images failed (e.g. offline / CORS error), apply Open-Meteo fallback
        if (!img101 && !img102) {
          if (weather) {
            const monitoredSet = new Set(Object.keys(polygons));
            const fallback = generateOpenMeteoFallbackHexCells(weather, polygons, monitoredSet);
            if (!isCancelled) setRainRadarHexCells(fallback);
          }
          return;
        }

        // Offscreen canvas for pixel extraction
        const canvas = document.createElement('canvas');
        canvas.width = 256;
        canvas.height = 256;
        const ctx = canvas.getContext('2d', { willReadFrequently: true });

        let data101: ImageData | null = null;
        let data102: ImageData | null = null;

        if (img101 && ctx) {
          ctx.clearRect(0, 0, 256, 256);
          ctx.drawImage(img101, 0, 0);
          data101 = ctx.getImageData(0, 0, 256, 256);
        }

        if (img102 && ctx) {
          ctx.clearRect(0, 0, 256, 256);
          ctx.drawImage(img102, 0, 0);
          data102 = ctx.getImageData(0, 0, 256, 256);
        }

        const mappedCells = new Map<string, FloodHexCell>();

        // Sample each H3 cell centroid against the radar tiles
        for (const [h3Id, pts] of Object.entries(polygons)) {
          if (!pts || pts.length < 3) continue;
          const [lat, lon] = getPolygonCentroid(pts);
          const { tileX, tileY, pixelX, pixelY } = latLonToTilePixel(lat, lon, 7);

          if (tileY !== 60) continue; // Outside HCMC latitude range

          let targetData: ImageData | null = null;
          if (tileX === 101) targetData = data101;
          else if (tileX === 102) targetData = data102;

          if (!targetData) continue;

          const idx = (pixelY * 256 + pixelX) * 4;
          const r = targetData.data[idx];
          const g = targetData.data[idx + 1];
          const b = targetData.data[idx + 2];
          const a = targetData.data[idx + 3];

          const severity = mapRadarPixelToSeverity(r, g, b, a);
          if (severity) {
            const camCount = camerasByCellRef.current.get(h3Id) || 0;
            mappedCells.set(h3Id, {
              h3_id: h3Id,
              resolution: 7,
              flood_severity: severity,
              camera_count: camCount,
              worst_camera: 'radar_rainviewer',
              worst_camera_name: 'Dự báo radar thời tiết',
              polygon: pts,
            });
          }
        }

        if (!isCancelled) {
          setRainRadarHexCells(mappedCells);
        }
      } catch (err) {
        console.warn('Error sampling RainViewer radar onto H3 cells:', err);
        if (!isCancelled) {
          setError(err instanceof Error ? err.message : String(err));
          // Graceful fallback to Open-Meteo
          if (weather && cachedPolygons) {
            const fallback = generateOpenMeteoFallbackHexCells(
              weather,
              cachedPolygons,
              new Set(Object.keys(cachedPolygons))
            );
            setRainRadarHexCells(fallback);
          }
        }
      } finally {
        if (!isCancelled) {
          setLoading(false);
        }
      }
    }

    processRadar();

    return () => {
      isCancelled = true;
    };
  }, [radar?.latest_tile_template, weather]);

  return {
    rainRadarHexCells,
    loading,
    error,
  };
}
