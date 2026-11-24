export interface HourlyHistoryItem {
  time: string; // RFC3339 string e.g. "2026-11-24T18:00:00Z"
  hour_label: string; // e.g. "17:00"
  avg_depth_cm: number;
  max_depth_cm: number;
  tide_level_m: number;
  tide_alert_level: 'NORMAL' | 'LEVEL_1' | 'LEVEL_2' | 'LEVEL_3';
  hazard_level: 'CLEAR' | 'CAUTION' | 'IMPASSABLE';
}

export interface HistorySummary {
  peak_tide_time?: string;
  peak_tide_level_m: number;
  peak_flood_time?: string;
  max_depth_cm: number;
  headline: {
    vi: string;
    en: string;
  };
}

export interface CorridorHistoryResponse {
  road_key: string;
  hours: number;
  station: string;
  station_name: {
    vi: string;
    en: string;
  };
  summary: HistorySummary;
  timeline: HourlyHistoryItem[];
}
