#!/usr/bin/env python3
"""
generate_v2_fixture.py - Deterministic fixture generator for Feature Store v2.

Generates tools/forecast/fixtures/synthetic_congestion_v2_5m.csv with:
  * 2 cameras ('cam-hotset-01', 'cam-hotset-02')
  * 2 directional roadway series per camera ('a' and 'b')
  * 2 days of 5-minute bins in UTC (576 bins x 4 = 2304 rows)
  * Realistically modeled afternoon tropical rain showers in HCMC
  * S10-T5 Feature v2 columns:
      - lag_5m, lag_10m, lag_15m, rolling_mean_15m
      - hour_of_day, day_of_week
      - precip_mm_15m, precip_mm_60m, rain_flag, hours_since_rain, weather_code_class, radar_active
      - peer_si, si_delta_ab
"""

from __future__ import annotations

import csv
import math
from datetime import datetime, timedelta, timezone
from pathlib import Path
from zoneinfo import ZoneInfo

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
OUTPUT_PATH = REPO_ROOT / "tools" / "forecast" / "fixtures" / "synthetic_congestion_v2_5m.csv"

CAMERAS = ("cam-hotset-01", "cam-hotset-02")
DIRECTIONS = ("a", "b")
START_TIME = datetime(2026, 10, 28, 0, 0, tzinfo=timezone.utc)
TOTAL_STEPS = 576  # 2 days at 5-minute bins
STEP_MINUTES = 5
LOCAL_TZ = ZoneInfo("Asia/Ho_Chi_Minh")


def generate():
    # 1. First pass: compute ground truth saturation index, weather, and directional state
    # Key: (camera_id, direction, step_index) -> data
    series_data = {}

    for step in range(TOTAL_STEPS):
        utc_dt = START_TIME + timedelta(minutes=step * STEP_MINUTES)
        local_dt = utc_dt.astimezone(LOCAL_TZ)
        hour = local_dt.hour
        minute = local_dt.minute
        minute_of_day = hour * 60 + minute
        day_of_week = local_dt.weekday()

        # Afternoon rain shower: 14:00 to 16:30 local time (minute 840 to 990)
        is_raining = 840 <= minute_of_day <= 990
        if is_raining:
            # Convective peak around 15:15 (minute 915)
            dist_to_center = abs(minute_of_day - 915)
            precip_mm = max(0.0, 8.0 * (1.0 - dist_to_center / 75.0))
            weather_code_class = 3  # moderate/heavy rain
            radar_active = 1
            rain_flag = 1
        else:
            precip_mm = 0.0
            weather_code_class = 0 if (hour < 6 or hour > 18) else 1
            radar_active = 0
            rain_flag = 0

        for cam_idx, cam_id in enumerate(CAMERAS):
            for dir_idx, dir_val in enumerate(DIRECTIONS):
                # Smooth background traffic daily curve (rush hours: 7:30 and 17:30)
                morning_peak = 0.25 * math.exp(-0.5 * ((minute_of_day - 450) / 60.0) ** 2)
                evening_peak = 0.28 * math.exp(-0.5 * ((minute_of_day - 1050) / 75.0) ** 2)
                base_si = 0.20 + 0.05 * cam_idx + morning_peak + evening_peak

                # Directional asymmetry: 'a' (inbound) heavier in morning, 'b' (outbound) heavier in evening
                dir_bias = 0.0
                if dir_val == "a":
                    dir_bias = 0.08 * math.exp(-0.5 * ((minute_of_day - 450) / 60.0) ** 2)
                else:
                    dir_bias = 0.08 * math.exp(-0.5 * ((minute_of_day - 1050) / 75.0) ** 2)

                # Rain effect: rain slowdown increases saturation index
                rain_effect = 0.0
                if precip_mm > 0.0:
                    rain_effect = 0.03 * precip_mm  # up to ~0.24 boost during peak rain

                si = max(0.05, min(1.50, base_si + dir_bias + rain_effect))

                series_data[(cam_id, dir_val, step)] = {
                    "utc_dt": utc_dt,
                    "local_dt": local_dt,
                    "si": si,
                    "precip_mm": precip_mm,
                    "rain_flag": rain_flag,
                    "weather_code_class": weather_code_class,
                    "radar_active": radar_active,
                }

    # 2. Second pass: compute lags, rolling aggregates, hours_since_rain, peer_si, si_delta_ab
    rows = []
    fieldnames = [
        "camera_id",
        "direction",
        "bin_start_utc",
        "bin_minutes",
        "saturation_index",
        "lag_5m",
        "lag_10m",
        "lag_15m",
        "rolling_mean_15m",
        "hour_of_day",
        "day_of_week",
        "precip_mm_15m",
        "precip_mm_60m",
        "rain_flag",
        "hours_since_rain",
        "weather_code_class",
        "radar_active",
        "peer_si",
        "si_delta_ab",
    ]

    for cam_id in CAMERAS:
        for dir_val in DIRECTIONS:
            peer_dir = "b" if dir_val == "a" else "a"
            last_rain_step = None

            for step in range(TOTAL_STEPS):
                curr = series_data[(cam_id, dir_val, step)]
                peer = series_data[(cam_id, peer_dir, step)]

                # Lags
                lag_5 = series_data[(cam_id, dir_val, step - 1)]["si"] if step >= 1 else None
                lag_10 = series_data[(cam_id, dir_val, step - 2)]["si"] if step >= 2 else None
                lag_15 = series_data[(cam_id, dir_val, step - 3)]["si"] if step >= 3 else None

                # Rolling 15m (mean of lags 15m, 10m, 5m prior to bin start)
                if step >= 3:
                    rolling_15 = (lag_5 + lag_10 + lag_15) / 3.0
                elif step == 2:
                    rolling_15 = (lag_5 + lag_10) / 2.0
                elif step == 1:
                    rolling_15 = lag_5
                else:
                    rolling_15 = None

                # Weather aggregates
                precip_15m = 0.0
                for s in range(max(0, step - 2), step + 1):
                    precip_15m += series_data[(cam_id, dir_val, s)]["precip_mm"]

                precip_60m = 0.0
                for s in range(max(0, step - 11), step + 1):
                    precip_60m += series_data[(cam_id, dir_val, s)]["precip_mm"]

                if curr["rain_flag"] == 1:
                    last_rain_step = step
                    hours_since_rain = 0.0
                elif last_rain_step is not None:
                    hours_since_rain = min(24.0, (step - last_rain_step) * STEP_MINUTES / 60.0)
                else:
                    hours_since_rain = 24.0

                # Directional peer and delta
                peer_si = peer["si"]
                si_a = curr["si"] if dir_val == "a" else peer["si"]
                si_b = peer["si"] if dir_val == "a" else curr["si"]
                si_delta_ab = si_a - si_b

                row = {
                    "camera_id": cam_id,
                    "direction": dir_val,
                    "bin_start_utc": curr["utc_dt"].strftime("%Y-%m-%dT%H:%M:%SZ"),
                    "bin_minutes": STEP_MINUTES,
                    "saturation_index": f"{curr['si']:.6f}",
                    "lag_5m": f"{lag_5:.6f}" if lag_5 is not None else "",
                    "lag_10m": f"{lag_10:.6f}" if lag_10 is not None else "",
                    "lag_15m": f"{lag_15:.6f}" if lag_15 is not None else "",
                    "rolling_mean_15m": f"{rolling_15:.6f}" if rolling_15 is not None else "",
                    "hour_of_day": curr["local_dt"].hour,
                    "day_of_week": curr["local_dt"].weekday(),
                    "precip_mm_15m": f"{precip_15m:.2f}",
                    "precip_mm_60m": f"{precip_60m:.2f}",
                    "rain_flag": curr["rain_flag"],
                    "hours_since_rain": f"{hours_since_rain:.2f}",
                    "weather_code_class": curr["weather_code_class"],
                    "radar_active": curr["radar_active"],
                    "peer_si": f"{peer_si:.6f}",
                    "si_delta_ab": f"{si_delta_ab:.6f}",
                }
                rows.append(row)

    # Sort deterministically
    rows.sort(key=lambda r: (r["camera_id"], r["direction"], r["bin_start_utc"]))

    OUTPUT_PATH.parent.mkdir(parents=True, exist_ok=True)
    with OUTPUT_PATH.open("w", encoding="utf-8", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)

    print(f"Wrote {len(rows)} rows to {OUTPUT_PATH}")


if __name__ == "__main__":
    generate()
