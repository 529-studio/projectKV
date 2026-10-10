#!/usr/bin/env python3
"""
Automated rclone configurator for Cloudflare R2 warded-proofs.
Reads credentials strictly from .env and writes ~/.config/rclone/rclone.conf.
Strictly zero hardcoded secrets per AGENTS.md Rule 8.
"""

import os
import sys
from pathlib import Path


def load_dotenv():
    for p in [".env", "../../.env", "../.env"]:
        env_file = Path(p)
        if env_file.exists():
            with open(env_file, "r") as f:
                for line in f:
                    line = line.strip()
                    if not line or line.startswith("#") or "=" not in line:
                        continue
                    k, v = line.split("=", 1)
                    k, v = k.strip(), v.strip().strip("'\"")
                    if k not in os.environ:
                        os.environ[k] = v
            break


def main():
    load_dotenv()

    endpoint = os.getenv("R2_ENDPOINT", "")
    access_key = os.getenv("R2_ACCESS_KEY_ID", "")
    secret_key = os.getenv("R2_SECRET_ACCESS_KEY", "")

    if not access_key or not secret_key:
        print("[ERROR] R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY must be set in .env")
        sys.exit(1)

    if not endpoint:
        endpoint = "https://REDACTED_R2_ACCOUNT_ID.r2.cloudflarestorage.com"

    rclone_conf_dir = Path.home() / ".config" / "rclone"
    rclone_conf_dir.mkdir(parents=True, exist_ok=True)
    rclone_conf_file = rclone_conf_dir / "rclone.conf"

    config_content = (
        "[R2]\n"
        "type = s3\n"
        "provider = Cloudflare\n"
        f"access_key_id = {access_key}\n"
        f"secret_access_key = {secret_key}\n"
        f"endpoint = {endpoint}\n"
        "acl = private\n"
    )

    rclone_conf_file.write_text(config_content)
    rclone_conf_file.chmod(0o600)
    print(f"[SUCCESS] Configured rclone remote 'R2' in {rclone_conf_file} (permissions 0600)")


if __name__ == "__main__":
    main()
