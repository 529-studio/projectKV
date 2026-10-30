## Lesson 21: S3-Compatible Cloudflare R2 Integration (Path-Style Routing & Timezone Partitioning)

- **Incident & Tracking:** Sprint 5 Plan / S5-T4, DevOps Guide integration (`feat/r2-storage-integration`).
- **Symptom:**
  When configuring third-party S3-compatible cloud object storage providers like Cloudflare R2, default AWS SDK settings attempt virtual-host addressing (`<bucket>.<endpoint>`), causing DNS resolution failures (`no such host`) and 404/403 routing errors. Furthermore, object keys partitioned with naive `.UTC()` dates generate day shifts across midnight in Vietnam (e.g., snapshots taken at 02:00 AM ICT on Oct 10th get filed under Oct 9th UTC).
- **Root Cause:**
  1. **Virtual-Host vs. Path-Style Addressing**: AWS S3 defaults to bucket-prefixed subdomains (`https://warded-proofs.bb67574163a065e4aad475fb3aa6bb92.r2.cloudflarestorage.com`), which Cloudflare R2 does not route without dedicated custom DNS setups. Cloudflare R2 requires S3 Path-Style addressing (`o.UsePathStyle = true` in Go AWS SDK v2, `addressing_style: path` in boto3).
  2. **Timezone Partitioning Mismatch**: Vietnam commuters and DOT incident logs operate in Indochina Time (`Asia/Ho_Chi_Minh`, UTC+7). Filing proof snapshots under UTC dates diverges snapshot directories from municipal traffic logs and ground-truth storm timelines.
- **Mandatory Takeaway:**
  1. **Mandatory Path-Style Flag for R2**: Always explicitly set `UsePathStyle = true` in AWS SDK Go v2 (`s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = true })`) and `s3={'addressing_style': 'path'}` in Python boto3.
  2. **Timezone Normalization in Key Standards**: Standardize `StoragePath` date formatting to Vietnam ICT (`time.FixedZone("ICT", 7*3600)`) so snapshot folder partitions strictly match local calendar dates.
  3. **Dual Storage Driver Abstraction**: Maintain unified `storage.Storage` interface with pluggable drivers (`STORAGE_DRIVER=local|r2`), guaranteeing that local development and offline mock pipelines run without cloud credentials while staging and production stream snapshots directly to Cloudflare R2.
