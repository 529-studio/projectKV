export interface HourlyForecast {
  time: string;
  precipitation_probability_percent: number;
  rain_mm: number;
  weather_code: number;
  weather_desc_vi: string;
  weather_desc_en: string;
}

export interface WeatherData {
  location: string;
  latitude: number;
  longitude: number;
  temperature_c: number;
  apparent_temperature_c: number;
  humidity_percent: number;
  weather_code: number;
  weather_desc_vi: string;
  weather_desc_en: string;
  precipitation_probability_percent: number;
  rain_mm: number;
  source: string;
  hourly_forecast: HourlyForecast[];
  updated_at: string;
}

export interface RadarFrame {
  time: number;
  path: string;
}

export interface RadarMetadata {
  host: string;
  generated: number;
  past_frames: RadarFrame[];
  nowcast_frames: RadarFrame[];
  latest_tile_template: string;
  source: string;
  updated_at: string;
}

export const WEATHER_I18N = Object.freeze({
  RAIN_FORECAST: {
    vi: 'Dự báo mưa & Thời tiết',
    en: 'Rain & Weather Forecast',
  },
  RAIN_CHANCE: {
    vi: 'Xác suất mưa',
    en: 'Rain chance',
  },
  PRECIPITATION: {
    vi: 'Lượng mưa',
    en: 'Precipitation',
  },
  FEELS_LIKE: {
    vi: 'Cảm giác như',
    en: 'Feels like',
  },
  HUMIDITY: {
    vi: 'Độ ẩm',
    en: 'Humidity',
  },
  HOURLY_TITLE: {
    vi: 'Dự báo mưa 6 giờ tới',
    en: 'Next 6h Rain Forecast',
  },
  RADAR_LAYER: {
    vi: 'Radar mây mưa (Doppler)',
    en: 'Rain Radar Layer (Doppler)',
  },
  RADAR_DESC: {
    vi: 'Ảnh vệ tinh radar mây mưa thời gian thực',
    en: 'Real-time Doppler precipitation radar',
  },
  ATTRIBUTION: {
    vi: 'Dữ liệu thời tiết © Open-Meteo & RainViewer',
    en: 'Weather data © Open-Meteo & RainViewer',
  },
  LOADING: {
    vi: 'Đang tải thời tiết...',
    en: 'Loading weather...',
  },
  ERROR: {
    vi: 'Không thể cập nhật thời tiết',
    en: 'Unable to update weather',
  },
  RADAR_ACTIVE: {
    vi: 'Lớp radar đang bật',
    en: 'Radar layer active',
  },
  RADAR_TOGGLE: {
    vi: 'Bật/tắt radar',
    en: 'Toggle radar',
  },
});

export type WeatherCategory = 'sun' | 'cloud' | 'drizzle' | 'rain' | 'storm' | 'snow';

/**
 * Maps WMO code to category for icon rendering.
 */
export function getWeatherCategory(wmoCode: number): WeatherCategory {
  if (wmoCode === 0) return 'sun';
  if (wmoCode >= 1 && wmoCode <= 3) return 'cloud';
  if (wmoCode >= 45 && wmoCode <= 48) return 'cloud';
  if (wmoCode >= 51 && wmoCode <= 57) return 'drizzle';
  if (wmoCode >= 61 && wmoCode <= 67) return 'rain';
  if (wmoCode >= 71 && wmoCode <= 77) return 'snow';
  if (wmoCode >= 80 && wmoCode <= 82) return 'rain';
  if (wmoCode >= 85 && wmoCode <= 86) return 'snow';
  if (wmoCode >= 95 && wmoCode <= 99) return 'storm';
  return 'cloud';
}

/**
 * Formats Celsius temperature nicely.
 */
export function formatTemperature(temp?: number): string {
  if (temp === undefined || isNaN(temp)) return '--°C';
  return `${Math.round(temp)}°C`;
}

/**
 * Formats probability percentage.
 */
export function formatRainProbability(prob?: number): string {
  if (prob === undefined || isNaN(prob)) return '0%';
  return `${Math.round(prob)}%`;
}

/**
 * Extracts HH:mm from ISO timestamp or date-time string.
 */
export function formatHour(timeStr?: string): string {
  if (!timeStr) return '';
  const match = timeStr.match(/(\d{2}):(\d{2})/);
  if (match) {
    return `${match[1]}:${match[2]}`;
  }
  try {
    const d = new Date(timeStr);
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
  } catch {
    return timeStr;
  }
}
