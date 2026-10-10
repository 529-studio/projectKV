#!/usr/bin/env python3
"""
Verification tool for Cloudflare R2 storage integration (bucket: warded-proofs).
Follows the DevOps integration guide standard.
"""

import os
import sys
import time
from pathlib import Path

# Try importing boto3; provide clear instructions if missing
try:
    import boto3
    from botocore.config import Config
except ImportError:
    print("[WARN] boto3 is not installed in the current environment. Run: pip install boto3")
    sys.exit(0)

# Load .env if present
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

ENDPOINT = os.getenv("R2_ENDPOINT", "https://REDACTED_R2_ACCOUNT_ID.r2.cloudflarestorage.com")
BUCKET = os.getenv("R2_BUCKET_NAME", "warded-proofs")
ACCESS_KEY = os.getenv("R2_ACCESS_KEY_ID", "")
SECRET_KEY = os.getenv("R2_SECRET_ACCESS_KEY", "")
PUBLIC_URL = os.getenv("R2_PUBLIC_URL", "https://file.529studio.site")

if not ACCESS_KEY or not SECRET_KEY:
    print("[ERROR] R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY must be set in environment or .env file.")
    sys.exit(1)

def main():
    print(f"[INFO] Connecting to Cloudflare R2 bucket '{BUCKET}' at {ENDPOINT}...")
    s3_client = boto3.client(
        service_name="s3",
        endpoint_url=ENDPOINT,
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
        region_name="auto",
        config=Config(s3={"addressing_style": "path"})
    )

    test_ts = int(time.time())
    test_key = f"test-connectivity/{test_ts}_python_verify.jpg"
    test_data = b"\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x01\x00H\x00H\x00\x00\xff\xdb\x00C\x00\xff\xc0\x00\x0b\x08\x00\x01\x00\x01\x01\x01\x11\x00\xff\xc4\x00\x1f\x00\x00\x01\x05\x01\x01\x01\x01\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\n\x0b\xff\xda\x00\x08\x01\x01\x00\x00?\x00\xbf\x00\xff\xd9"

    print(f"[TEST 1/4] Uploading test object to {test_key}...")
    s3_client.put_object(
        Bucket=BUCKET,
        Key=test_key,
        Body=test_data,
        ContentType="image/jpeg"
    )
    print("[PASS] Object uploaded successfully.")

    print("[TEST 2/4] HeadObject verification...")
    head = s3_client.head_object(Bucket=BUCKET, Key=test_key)
    assert head["ContentLength"] == len(test_data), "ContentLength mismatch"
    print(f"[PASS] Object verified, length={head['ContentLength']} bytes.")

    print(f"[TEST 3/4] Public URL: {PUBLIC_URL}/{test_key}")

    print("[TEST 4/4] Deleting test object...")
    s3_client.delete_object(Bucket=BUCKET, Key=test_key)
    print("[PASS] Cleanup completed.")

    print("[SUCCESS] Cloudflare R2 Python SDK verification completed successfully!")

if __name__ == "__main__":
    main()
