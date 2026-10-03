export interface FloodHexCellLike {
  h3_id: string;
  resolution: number;
  flood_severity: string;
  camera_count: number;
  worst_camera: string;
  worst_camera_name: string;
  polygon: [number, number][];
}

export interface WeatherDataLike {
  weather_code: number;
  rain_mm: number;
}

/**
 * Checks if WMO weather code represents rain, drizzle, or thunderstorm.
 */
export function isWmoRain(code: number): boolean {
  return (code >= 51 && code <= 67) || (code >= 80 && code <= 82) || (code >= 95 && code <= 99);
}

/**
 * Calculates centroid [lat, lon] of a 2D polygon.
 */
export function getPolygonCentroid(polygon: [number, number][]): [number, number] {
  if (!polygon || polygon.length === 0) return [0, 0];
  let sumLat = 0;
  let sumLon = 0;
  for (const pt of polygon) {
    sumLat += pt[0];
    sumLon += pt[1];
  }
  return [sumLat / polygon.length, sumLon / polygon.length];
}

export interface TilePixelCoord {
  tileX: number;
  tileY: number;
  pixelX: number;
  pixelY: number;
}

/**
 * Projects (lat, lon) to Web Mercator tile index and pixel offset within tile.
 */
export function latLonToTilePixel(lat: number, lon: number, zoom = 7): TilePixelCoord {
  const n = Math.pow(2, zoom);
  const exactX = ((lon + 180.0) / 360.0) * n;
  const latRad = (lat * Math.PI) / 180.0;
  const exactY = ((1.0 - Math.asinh(Math.tan(latRad)) / Math.PI) / 2.0) * n;

  const tileX = Math.floor(exactX);
  const tileY = Math.floor(exactY);
  const pixelX = Math.max(0, Math.min(255, Math.floor((exactX - tileX) * 256)));
  const pixelY = Math.max(0, Math.min(255, Math.floor((exactY - tileY) * 256)));

  return { tileX, tileY, pixelX, pixelY };
}

/**
 * Maps RainViewer radar reflectivity pixel (RGBA) to ProjectKV Flood/Rain Severity.
 * Precedence:
 * - a < 75: Atmospheric moisture / thin cloud clutter suppressed (null)
 * - a >= 200: Heavy rain / storm risk ('FLOODED_DEEP')
 * - a >= 140: Moderate rain / water accumulation risk ('FLOODED_SHALLOW')
 * - a >= 75: Light rain / wet road ('WET')
 */
export function mapRadarPixelToSeverity(
  _r: number,
  _g: number,
  _b: number,
  a: number
): 'FLOODED_DEEP' | 'FLOODED_SHALLOW' | 'WET' | null {
  if (a < 75) return null;
  if (a >= 200) return 'FLOODED_DEEP';
  if (a >= 140) return 'FLOODED_SHALLOW';
  return 'WET';
}

/**
 * Replaces {z}, {x}, {y} placeholders in a tile template.
 */
export function buildRadarTileUrl(template: string, tileX: number, tileY: number, zoom = 7): string {
  return template
    .replace('{z}', String(zoom))
    .replace('{x}', String(tileX))
    .replace('{y}', String(tileY));
}

/**
 * Merges camera-verified flood hex cells (ground-truth, highest priority)
 * with radar-derived rain hex cells.
 */
export function mergeFloodHexCells<T extends FloodHexCellLike>(
  cameraHexCells: Map<string, T>,
  radarHexCells: Map<string, T>
): Map<string, T> {
  const merged = new Map<string, T>();

  // 1. Add all radar-derived cells as baseline spatial rain overlay
  radarHexCells.forEach((cell, id) => {
    merged.set(id, cell);
  });

  // 2. Camera ground-truth observations override radar approximations
  cameraHexCells.forEach((cell, id) => {
    merged.set(id, cell);
  });

  return merged;
}

/**
 * Fallback mapping from Open-Meteo city rainfall when radar tiles are offline or empty.
 * If Open-Meteo reports active precipitation (rain_mm >= 1.0 or active rain WMO code >= 61),
 * marks active monitored camera cells as WET / FLOODED_SHALLOW.
 */
export function generateOpenMeteoFallbackHexCells<T extends FloodHexCellLike = FloodHexCellLike>(
  weather: WeatherDataLike,
  polygons: Record<string, [number, number][]>,
  monitoredCellIds: Set<string>
): Map<string, T> {
  const result = new Map<string, T>();
  const isRaining = weather.rain_mm >= 1.0 || (weather.weather_code >= 61 && isWmoRain(weather.weather_code));

  if (!isRaining) return result;

  const severity: 'FLOODED_DEEP' | 'FLOODED_SHALLOW' | 'WET' =
    weather.rain_mm >= 15.0 || (weather.weather_code >= 95 && weather.weather_code <= 99)
      ? 'FLOODED_DEEP'
      : weather.rain_mm >= 4.0 || (weather.weather_code >= 61 && weather.weather_code <= 67)
        ? 'FLOODED_SHALLOW'
        : 'WET';

  monitoredCellIds.forEach((h3Id) => {
    const poly = polygons[h3Id];
    if (!poly || poly.length < 3) return;

    result.set(h3Id, {
      h3_id: h3Id,
      resolution: 7,
      flood_severity: severity,
      camera_count: 1,
      worst_camera: 'open-meteo',
      worst_camera_name: 'Dự báo mưa Open-Meteo',
      polygon: poly,
    } as T);
  });

  return result;
}
