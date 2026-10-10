#!/usr/bin/env python3
"""
Lightweight Cloudflare R2 CLI explorer for Warded proofs.
Loads credentials strictly from .env per AGENTS.md Rule 8.
Zero third-party binary dependency (uses boto3).
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


load_dotenv()

try:
    import boto3
    from botocore.config import Config
except ImportError:
    print("[ERROR] boto3 is required. Install via: pip install boto3")
    sys.exit(1)

ENDPOINT = os.getenv("R2_ENDPOINT", "https://bb67574163a065e4aad475fb3aa6bb92.r2.cloudflarestorage.com")
BUCKET = os.getenv("R2_BUCKET_NAME", "warded-proofs")
ACCESS_KEY = os.getenv("R2_ACCESS_KEY_ID", "")
SECRET_KEY = os.getenv("R2_SECRET_ACCESS_KEY", "")

if not ACCESS_KEY or not SECRET_KEY:
    print("[ERROR] R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY must be configured in .env")
    sys.exit(1)


def get_client():
    return boto3.client(
        service_name="s3",
        endpoint_url=ENDPOINT,
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
        region_name="auto",
        config=Config(s3={"addressing_style": "path"})
    )


def cmd_ls(prefix=""):
    s3 = get_client()
    paginator = s3.get_paginator("list_objects_v2")
    delimiter = "/"
    count = 0
    print(f"[INFO] Listing R2 bucket '{BUCKET}' with prefix='{prefix}':\n")

    for page in paginator.paginate(Bucket=BUCKET, Prefix=prefix, Delimiter=delimiter):
        # Directories (CommonPrefixes)
        for cp in page.get("CommonPrefixes", []):
            print(f"  [DIR]  {cp['Prefix']}")
            count += 1
        # Files (Contents)
        for obj in page.get("Contents", []):
            size_kb = obj["Size"] / 1024.0
            print(f"  [FILE] {obj['Key']:<50} {size_kb:>8.1f} KB")
            count += 1

    print(f"\nTotal items listed: {count}")


def cmd_size(prefix=""):
    s3 = get_client()
    paginator = s3.get_paginator("list_objects_v2")
    total_objects = 0
    total_bytes = 0

    print(f"[INFO] Calculating total size for R2 bucket '{BUCKET}' (prefix='{prefix}')...")
    for page in paginator.paginate(Bucket=BUCKET, Prefix=prefix):
        for obj in page.get("Contents", []):
            total_objects += 1
            total_bytes += obj["Size"]

    mb = total_bytes / (1024.0 * 1024.0)
    print(f"Total objects: {total_objects:,}")
    print(f"Total size:    {mb:.2f} MiB ({total_bytes:,} bytes)")


def main():
    args = sys.argv[1:]
    if not args or args[0] in ("-h", "--help", "help"):
        print("Usage:")
        print("  python3 tools/storage/r2_cli.py ls [prefix]")
        print("  python3 tools/storage/r2_cli.py size [prefix]")
        sys.exit(0)

    cmd = args[0]
    prefix = args[1] if len(args) > 1 else ""

    if cmd == "ls":
        cmd_ls(prefix)
    elif cmd == "size":
        cmd_size(prefix)
    else:
        print(f"[ERROR] Unknown command: {cmd}")
        sys.exit(1)


if __name__ == "__main__":
    main()
