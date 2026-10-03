import assert from 'node:assert/strict';
import { test, describe } from 'node:test';
import {
  getPolygonCentroid,
  latLonToTilePixel,
  mapRadarPixelToSeverity,
  buildRadarTileUrl,
  mergeFloodHexCells,
  generateOpenMeteoFallbackHexCells,
} from './rainHexUtils.ts';
import type { FloodHexCell } from './floodUtils.ts';
import type { WeatherData } from './weatherUtils.ts';

describe('Rain Hex Utils (Issue #106 / #120 Restoration)', () => {
  const samplePoly: [number, number][] = [
    [10.0, 106.0],
    [10.0, 107.0],
    [11.0, 107.0],
    [11.0, 106.0],
  ];

  test('getPolygonCentroid calculates average lat and lon correctly', () => {
    const centroid = getPolygonCentroid(samplePoly);
    assert.equal(centroid[0], 10.5);
    assert.equal(centroid[1], 106.5);

    // Empty polygon fallback
    assert.deepEqual(getPolygonCentroid([]), [0, 0]);
  });

  test('latLonToTilePixel converts HCMC coordinates to zoom 7 tile and pixel offset', () => {
    // Ho Chi Minh City center
    const coord = latLonToTilePixel(10.7769, 106.7009, 7);
    assert.equal(coord.tileX, 101);
    assert.equal(coord.tileY, 60);
    assert.ok(coord.pixelX >= 0 && coord.pixelX <= 255);
    assert.ok(coord.pixelY >= 0 && coord.pixelY <= 255);
  });

  test('mapRadarPixelToSeverity maps reflectivity alpha to flood/rain severity', () => {
    // Below threshold (clear / cloud noise suppressed)
    assert.equal(mapRadarPixelToSeverity(0, 0, 0, 0), null);
    assert.equal(mapRadarPixelToSeverity(100, 100, 100, 20), null);
    assert.equal(mapRadarPixelToSeverity(100, 100, 100, 50), null);
    assert.equal(mapRadarPixelToSeverity(120, 110, 100, 74), null);

    // Light rain (wet)
    assert.equal(mapRadarPixelToSeverity(100, 100, 100, 75), 'WET');
    assert.equal(mapRadarPixelToSeverity(120, 110, 100, 100), 'WET');
    assert.equal(mapRadarPixelToSeverity(120, 110, 100, 139), 'WET');

    // Moderate rain (shallow flood risk)
    assert.equal(mapRadarPixelToSeverity(140, 130, 110, 140), 'FLOODED_SHALLOW');
    assert.equal(mapRadarPixelToSeverity(160, 140, 110, 199), 'FLOODED_SHALLOW');

    // Heavy rain (deep flood risk)
    assert.equal(mapRadarPixelToSeverity(200, 180, 120, 200), 'FLOODED_DEEP');
    assert.equal(mapRadarPixelToSeverity(255, 200, 150, 240), 'FLOODED_DEEP');
  });

  test('buildRadarTileUrl properly substitutes {z}, {x}, {y}', () => {
    const template = 'https://tilecache.rainviewer.com/v2/radar/abc/256/{z}/{x}/{y}/2/1_1.png';
    const url = buildRadarTileUrl(template, 101, 60, 7);
    assert.equal(url, 'https://tilecache.rainviewer.com/v2/radar/abc/256/7/101/60/2/1_1.png');
  });

  test('mergeFloodHexCells prioritizes camera ground-truth observations over radar', () => {
    const cameraCells = new Map<string, FloodHexCell>();
    const radarCells = new Map<string, FloodHexCell>();

    const camCell: FloodHexCell = {
      h3_id: 'cell_overlap',
      resolution: 7,
      flood_severity: 'FLOODED_DEEP',
      camera_count: 3,
      worst_camera: 'cam_01',
      worst_camera_name: 'Cam 01',
      polygon: samplePoly,
    };

    const radarCellOverlap: FloodHexCell = {
      h3_id: 'cell_overlap',
      resolution: 7,
      flood_severity: 'WET',
      camera_count: 0,
      worst_camera: 'radar',
      worst_camera_name: 'Radar',
      polygon: samplePoly,
    };

    const radarCellOnly: FloodHexCell = {
      h3_id: 'cell_radar_only',
      resolution: 7,
      flood_severity: 'FLOODED_SHALLOW',
      camera_count: 0,
      worst_camera: 'radar',
      worst_camera_name: 'Radar',
      polygon: samplePoly,
    };

    cameraCells.set('cell_overlap', camCell);
    radarCells.set('cell_overlap', radarCellOverlap);
    radarCells.set('cell_radar_only', radarCellOnly);

    const merged = mergeFloodHexCells(cameraCells, radarCells);
    assert.equal(merged.size, 2);

    // Overlapping cell should retain camera verdict (FLOODED_DEEP instead of radar WET)
    assert.equal(merged.get('cell_overlap')?.flood_severity, 'FLOODED_DEEP');
    assert.equal(merged.get('cell_overlap')?.worst_camera, 'cam_01');

    // Radar-only cell should be preserved
    assert.equal(merged.get('cell_radar_only')?.flood_severity, 'FLOODED_SHALLOW');
  });

  test('generateOpenMeteoFallbackHexCells generates baseline wet cells when city has rain', () => {
    const weatherRain: WeatherData = {
      location: 'Ho Chi Minh City',
      latitude: 10.77,
      longitude: 106.70,
      temperature_c: 28,
      apparent_temperature_c: 32,
      humidity_percent: 85,
      weather_code: 61, // rain
      weather_desc_vi: 'Mưa vừa',
      weather_desc_en: 'Moderate rain',
      precipitation_probability_percent: 80,
      rain_mm: 5.5,
      source: 'open-meteo',
      hourly_forecast: [],
      updated_at: new Date().toISOString(),
    };

    const polygons = {
      cell_1: samplePoly,
      cell_2: samplePoly,
    };

    const monitoredCells = new Set(['cell_1', 'cell_2']);
    const result = generateOpenMeteoFallbackHexCells(weatherRain, polygons, monitoredCells);

    assert.equal(result.size, 2);
    assert.equal(result.get('cell_1')?.flood_severity, 'FLOODED_SHALLOW');

    // When dry
    const weatherDry: WeatherData = {
      ...weatherRain,
      weather_code: 0, // clear sky
      rain_mm: 0,
    };
    const dryResult = generateOpenMeteoFallbackHexCells(weatherDry, polygons, monitoredCells);
    assert.equal(dryResult.size, 0);

    // When minor trace rain / cloud moisture below threshold (rain_mm < 1.0 and cloudy weather code)
    const weatherTrace: WeatherData = {
      ...weatherRain,
      weather_code: 2, // partly cloudy
      rain_mm: 0.3,
    };
    const traceResult = generateOpenMeteoFallbackHexCells(weatherTrace, polygons, monitoredCells);
    assert.equal(traceResult.size, 0);
  });
});
