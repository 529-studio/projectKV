#!/usr/bin/env python3
"""
Thermal & CPU Throttling Monitor for local-edge host (ZOTAC ZBOX-CI320NANO).

Inspects Linux kernel thermal zones and CPU frequencies to detect overheating
and thermal throttling on fanless 7.5W TDP chassis (Celeron N2930).
"""

import argparse
import glob
import json
import os
import sys
import time
from typing import Any, Dict, List, Optional


class ThermalZone:
    def __init__(self, zone_id: str, temp_c: float, zone_type: str = "acpitz"):
        self.zone_id = zone_id
        self.temp_c = temp_c
        self.zone_type = zone_type

    def to_dict(self) -> Dict[str, Any]:
        return {
            "zone_id": self.zone_id,
            "type": self.zone_type,
            "temperature_c": round(self.temp_c, 1),
        }


class CPUCoreInfo:
    def __init__(self, core_id: str, freq_mhz: float):
        self.core_id = core_id
        self.freq_mhz = freq_mhz

    def to_dict(self) -> Dict[str, Any]:
        return {
            "core_id": self.core_id,
            "frequency_mhz": round(self.freq_mhz, 1),
        }


class ThermalMonitor:
    # Thresholds for Intel Celeron Silvermont fanless chassis
    THRESHOLD_NORMAL_MAX = 60.0
    THRESHOLD_ELEVATED_MAX = 75.0
    THRESHOLD_WARNING_MAX = 82.0

    def __init__(self, sys_root: str = "/"):
        self.sys_root = sys_root

    def read_thermal_zones(self) -> List[ThermalZone]:
        zones = []
        thermal_path = os.path.join(self.sys_root, "sys/class/thermal/thermal_zone*")
        zone_dirs = sorted(glob.glob(thermal_path))

        for zd in zone_dirs:
            zid = os.path.basename(zd)
            temp_file = os.path.join(zd, "temp")
            type_file = os.path.join(zd, "type")

            if not os.path.isfile(temp_file):
                continue

            try:
                with open(temp_file, "r") as f:
                    val = float(f.read().strip())
                    # Linux thermal zone values are in millidegrees Celsius
                    temp_c = val / 1000.0 if val > 200 else val
            except Exception:
                continue

            ztype = "unknown"
            if os.path.isfile(type_file):
                try:
                    with open(type_file, "r") as f:
                        ztype = f.read().strip()
                except Exception:
                    pass

            zones.append(ThermalZone(zid, temp_c, ztype))
        return zones

    def read_cpu_frequencies(self) -> List[CPUCoreInfo]:
        cores = []
        cpu_path = os.path.join(self.sys_root, "sys/devices/system/cpu/cpu[0-9]*")
        cpu_dirs = sorted(glob.glob(cpu_path))

        for cd in cpu_dirs:
            cid = os.path.basename(cd)
            freq_file = os.path.join(cd, "cpufreq/scaling_cur_freq")
            if os.path.isfile(freq_file):
                try:
                    with open(freq_file, "r") as f:
                        freq_khz = float(f.read().strip())
                        cores.append(CPUCoreInfo(cid, freq_khz / 1000.0))
                except Exception:
                    continue
        return cores

    def evaluate_status(self, max_temp_c: float) -> str:
        if max_temp_c < self.THRESHOLD_NORMAL_MAX:
            return "NORMAL"
        elif max_temp_c <= self.THRESHOLD_ELEVATED_MAX:
            return "ELEVATED"
        elif max_temp_c <= self.THRESHOLD_WARNING_MAX:
            return "WARNING"
        else:
            return "CRITICAL_THROTTLING"

    def sample(self) -> Dict[str, Any]:
        zones = self.read_thermal_zones()
        cores = self.read_cpu_frequencies()

        max_temp = max((z.temp_c for z in zones), default=0.0)
        status = self.evaluate_status(max_temp) if zones else "NO_THERMAL_DATA"

        return {
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "status": status,
            "max_temperature_c": round(max_temp, 1) if zones else None,
            "throttling_risk": status in ("WARNING", "CRITICAL_THROTTLING"),
            "thermal_zones": [z.to_dict() for z in zones],
            "cpu_cores": [c.to_dict() for c in cores],
        }


def main():
    parser = argparse.ArgumentParser(description="Thermal & Throttling Monitor for local-edge host")
    parser.add_argument("--sys-root", default="/", help="Root directory for /sys filesystem inspection")
    parser.add_argument("--out", default="", help="Optional JSON output path")
    parser.add_argument("--interval", type=float, default=0.0, help="Monitoring interval in seconds (0 for once)")
    args = parser.parse_args()

    monitor = ThermalMonitor(sys_root=args.sys_root)

    while True:
        report = monitor.sample()
        print(f"[{report['timestamp']}] Thermal Status: {report['status']} | Max Temp: {report['max_temperature_c']}°C | Throttling Risk: {report['throttling_risk']}")
        for z in report["thermal_zones"]:
            print(f"  - Zone {z['zone_id']} ({z['type']}): {z['temperature_c']}°C")
        for c in report["cpu_cores"]:
            print(f"  - Core {c['core_id']}: {c['frequency_mhz']} MHz")

        if args.out:
            os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
            with open(args.out, "w") as f:
                json.dump(report, f, indent=2)

        if args.interval <= 0:
            break
        time.sleep(args.interval)


if __name__ == "__main__":
    main()
