# SQLite Litestream Replication & Disaster Recovery Runbook

- **Issue:** #442, sprint 14 task 2 (S14-T2)
- **Applies to:** `docker-compose.stg.yml`, `deploy/litestream/litestream.yml`, `tools/deploy/restore_drill.py`
- **Last verified:** 2026-11-13 (see [the S14-T2 verification report](../reports/s14_t2_litestream_restore_drill.md))

## 1. What this protects and what it does not

`warded-backend` keeps the only copy of the traffic database at
`/app/data/camera-view.db` in the `warded-stg-data` Docker volume. The volume is
local to one host: if the host or the volume is lost, the database is lost with
it. Litestream closes that gap by streaming every committed WAL frame to
Cloudflare R2 within one second.

| Property | Target | How it is enforced |
|---|---|---|
| Recovery Time Objective (RTO) | <= 60 s | `restore-drill` measures the restore wall clock and fails above 60 s |
| Recovery Point Objective (RPO) | <= 10 s | `sync-interval: 1s` in the replica config, measured per drill |
| Integrity | `ok` | `PRAGMA integrity_check`, `quick_check`, `foreign_key_check` on the restored file |
| Completeness | no loss beyond the RPO window | per-table counts compared against the source at restore start |

Out of scope: point-in-time recovery older than the 168 h retention window,
logical corruption introduced by the application, and R2 account or bucket
deletion. This is replication, not an independent backup you can restore after
the bucket itself is lost.

## 2. Topology

```
warded-backend (10001:10001)          warded-litestream (10001:10001)
        writes                               reads WAL, ships frames
          |                                         |
          v                                         v
/app/data/camera-view.db  <-- same volume --  s3://${R2_BUCKET_NAME}
  (+ -wal, -shm)                               /db-replicas/camera-view.db/
                                                   generations/<id>/
                                                     snapshots/*.snapshot.lz4
                                                     wal/*.wal.lz4
```

| Component | Image / file | Responsibility |
|---|---|---|
| `warded-init-vol` | `alpine:3.20`, `user: "0:0"`, `restart: "no"` | Creates `/app/data/{locks,forecast,proofs}` and `chown 10001:10001` / `chmod 775` on a fresh volume, then exits |
| `warded-litestream` | `litestream/litestream:0.3.13`, `user: "10001:10001"`, `read_only: true` | `litestream replicate -config /etc/litestream.yml` |
| `deploy/litestream/litestream.yml` | mounted read-only at `/etc/litestream.yml` | Database path, R2 bucket/prefix, endpoint, credentials by reference, `sync-interval: 1s`, `snapshot-interval: 24h`, `retention: 168h` |
| `tools/deploy/restore_drill.py` | repository tool | Restores into a sandbox and proves the restore is usable |

`warded-backend` and `warded-forecast-cron` both wait on
`warded-init-vol: condition: service_completed_successfully`. Without that gate a
fresh volume is root-owned: the forecast refit (UID 10001) cannot create its
state files, Litestream cannot read the WAL, and the failure surfaces later as an
unexplained `unable to open database file` or replication error. The backend image
currently runs as root, so the chown does not change its own behaviour; it gates
on the init container so every consumer starts against the same volume state.

## 3. Configuration

Credentials are never stored in the config file. Litestream expands `${...}`
references from its environment, which compose fills from the deployment `.env`:

| Variable | Used by | Notes |
|---|---|---|
| `R2_BUCKET_NAME` | config `bucket`, sidecar env | `warded-proofs` in staging |
| `R2_ENDPOINT` | config `endpoint`, sidecar env | `https://<account-id>.r2.cloudflarestorage.com` |
| `R2_REGION` | config `region`, sidecar env | `auto` for R2 |
| `LITESTREAM_ACCESS_KEY_ID` | config `access-key-id` | Falls back to `R2_ACCESS_KEY_ID` in compose |
| `LITESTREAM_SECRET_ACCESS_KEY` | config `secret-access-key` | Falls back to `R2_SECRET_ACCESS_KEY` in compose |
| `LITESTREAM_CONTAINER_NAME` | compose | Default `warded-litestream` |
| `LITESTREAM_CPU_LIMIT` / `LITESTREAM_MEMORY_LIMIT` | compose | Default `0.20` / `64M` |
| `INIT_VOL_CONTAINER_NAME` | compose | Default `warded-init-vol` |

Validate without deploying:

```bash
make lint-deploy                       # structural invariants for config + compose
docker compose -f docker-compose.stg.yml config --quiet
docker compose -f docker-compose.stg.yml --profile forecast-cron config --services
```

## 4. Routine verification: the restore drill

A replica is a hypothesis until it has been restored. Run the drill before every
staging release and after any credential or bucket change.

### 4.1 Hermetic simulator (no credentials, safe in CI)

```bash
make restore-drill
```

This builds a real WAL-mode SQLite database, captures a base snapshot plus WAL
segments into a local replica, restores it, and runs every integrity, count,
freshness, RTO and RPO check. It never contacts R2. The JSON summary lands in
`docs/reports/s14_t2_restore_drill_summary.json`; `EXTRA="--cycles 6"` and
`RESTORE_DRILL_OUT=<path>` override the defaults.

### 4.2 Live replica (the real path)

Requires the `litestream` binary (v0.3.13) and R2 credentials:

```bash
export LITESTREAM_BIN=/usr/local/bin/litestream       # or rely on PATH
export RESTORE_DRILL_REPLICA_URL=s3://$R2_BUCKET_NAME/db-replicas/camera-view.db
make restore-drill RESTORE_DRILL_MODE=live
```

Restore-only against a replica somebody else produced:

```bash
python3 tools/deploy/restore_drill.py --mode live \
  --replica-url s3://$R2_BUCKET_NAME/db-replicas/camera-view.db \
  --out docs/reports/s14_t2_restore_drill_summary.json
```

End-to-end drill that writes while replicating (no R2 needed, uses a file
replica), useful when validating the Litestream binary itself:

```bash
python3 tools/deploy/restore_drill.py --mode live \
  --source-db /tmp/drill/camera-view.db \
  --replica-url file:///tmp/drill/replica/camera-view.db \
  --start-replication --replicate-seconds 12 \
  --sandbox /tmp/drill/sandbox --keep-sandbox
```

`--start-replication` is only valid with a `file://` replica: the Litestream CLI
has no endpoint flag, so a URL-only `s3://` replication would authenticate
against AWS S3 instead of R2. For R2, let the `warded-litestream` sidecar do the
replicating and run the drill restore-only (section 4.2, first command).

Exit code `0` means every check passed; `1` means the drill failed and the JSON
summary names the failing checks. Treat a non-zero exit as a release blocker.

## 5. Incident procedure: restore staging

Do not restore over a live database while both the backend and Litestream are
running. Two writers and a replicating sidecar will produce a torn state.

1. **Stop the writers.** Keep the replica intact.
   ```bash
   docker compose -f docker-compose.stg.yml stop warded-backend warded-litestream
   ```
2. **Pick the target.** From the deployment host, with the sidecar's environment
   loaded (bucket, endpoint, region, keys):
   ```bash
   docker run --rm -v "$PWD/deploy/litestream/litestream.yml:/etc/litestream.yml:ro" \
     --env-file .env litestream/litestream:0.3.13 \
     restore -config /etc/litestream.yml -o /app/data/restore/camera-view.db \
     /app/data/camera-view.db
   ```
   The database path argument is required whenever `-config` is used, and
   Litestream rejects `-config` combined with a replica URL.
3. **Verify before swapping.** Never promote an unverified file:
   ```bash
   sqlite3 /app/data/restore/camera-view.db \
     "PRAGMA integrity_check; PRAGMA quick_check; PRAGMA foreign_key_check;
      SELECT COUNT(*) FROM camera_verdicts;
      SELECT COUNT(*) FROM hotset_verdict_history;
      SELECT MAX(observed_at) FROM hotset_verdict_history;"
   ```
   Or run `tools/deploy/restore_drill.py --mode live` and require exit code 0.
4. **Swap atomically.** Keep the old file for forensics; remove stale sidecars or
   SQLite will replay the old WAL over the new database.
   ```bash
   mv /app/data/camera-view.db /app/data/camera-view.db.corrupt
   mv /app/data/restore/camera-view.db /app/data/camera-view.db
   rm -f /app/data/camera-view.db-wal /app/data/camera-view.db-shm
   chown 10001:10001 /app/data/camera-view.db
   ```
5. **Restart in order.** Init container (already complete), backend, then
   Litestream:
   ```bash
   docker compose -f docker-compose.stg.yml up -d warded-backend warded-forecast-cron
   docker compose -f docker-compose.stg.yml up -d warded-litestream
   ```
6. **Confirm.**
   ```bash
   docker compose -f docker-compose.stg.yml logs --tail 30 warded-litestream
   curl -fsS https://<staging-host>/health
   ```
   Litestream starts a new generation (new snapshot) when it does not recognise
   the restored file. Keep the previous generation until the new snapshot is
   visible in the bucket; `retention: 168h` removes old generations later.
7. **Record the outcome.** Write the incident, the recovered timestamp and the
   measured RPO into the sprint report, and rerun `make restore-drill`.

## 6. Failure modes

| Symptom | Cause | Action |
|---|---|---|
| `unable to open database file` on first boot | Volume owned by root; `warded-init-vol` did not run | `docker compose run --rm warded-init-vol`, verify `ls -ldn /app/data` shows `10001 10001` |
| `bucket required for s3 replica` | Missing `R2_BUCKET_NAME` in the sidecar environment | Fill `.env`, recreate the container |
| `required variable R2_ENDPOINT is missing a value` | Endpoint not set; compose refuses to start the sidecar | Set `R2_ENDPOINT`; never default it to empty, or the S3 client would target AWS with R2 keys |
| `InvalidAccessKeyId` / `AccessDenied` | Wrong or expired keys | Rotate; either `LITESTREAM_*` or the `R2_*` fallback must be set |
| `no matching backups found` | Wrong bucket/prefix, or the first snapshot has not landed yet | Check the object path `db-replicas/camera-view.db/generations/`, wait for the first snapshot |
| `cannot specify a replica URL and the -config flag` | Restore command combined both | Drop the URL; pass the config and the database path |
| `cannot read wal header` / `database disk image is malformed` | WAL disappeared or was checkpointed while Litestream was reading it; a second writer closed the only connection | Keep one long-lived writer; restart the sidecar; verify `PRAGMA integrity_check` on the source |
| Replica lag keeps growing | Egress throttling, R2 outage, or the sidecar has no CPU | Check `docker stats`, sidecar logs, R2 dashboard; raise `LITESTREAM_CPU_LIMIT` only after confirming the network path |
| Restore check fails on counts | Real data loss beyond the RPO window | Do not promote the restore; investigate Litestream logs and the R2 object timeline first |

## 7. Operating limits

- **One writer.** SQLite allows a single writer at a time; Litestream adds a read
  lock. Never run two backends on the same volume, and never run
  `litestream replicate` twice for one database.
- **Retention is 168 h.** Restores older than that are impossible from this
  replica; extend `retention` and `snapshot-interval` before claiming a longer
  window, and measure the larger RTO it implies.
- **R2 lifecycle rules must not expire `db-replicas/`.** A bucket-wide expiry
  rule that deletes the replica is data loss, not cleanup.
- **The config mount is read-only** and the sidecar runs with `read_only: true`,
  `cap_drop: [ALL]` and `no-new-privileges`; it needs write access only to the
  data volume.
- **Backups are not replicas.** The drill proves recoverability of the replica;
  it does not prove that an independent archive exists. Do not describe the
  sidecar as a backup in reports.

## 8. References

- Replication config: [`deploy/litestream/litestream.yml`](../../deploy/litestream/litestream.yml)
- Compose stack: [`docker-compose.stg.yml`](../../docker-compose.stg.yml)
- Drill tool: [`tools/deploy/restore_drill.py`](../../tools/deploy/restore_drill.py)
- Verification report: [S14-T2 restore drill](../reports/s14_t2_litestream_restore_drill.md)
- Lesson: [Litestream R2 replication and restore drills](../lessons/442-litestream-r2-replication-and-restore-drill.md)
