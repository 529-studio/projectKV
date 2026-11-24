# SQLite Daily Backup & Restore Runbook

- **Issue:** #442 (S14-T2) as superseded by the Phase 3 two-site migration (2026-11-20)
- **Applies to:** `docker-compose.stg.yml` (`warded-backup` sidecar), `r2:homelab-backup/warded/`
- **Last verified:** 2026-11-24 (compose renders, validator and unit tests pass; no live restore executed yet)

## 1. What this protects and what it does not

`warded-backend` keeps the only copy of the traffic database at
`/app/data/camera-view.db` in the `warded-stg-data` Docker volume. The volume is
local to one host: if the host or the volume is lost, the database is lost with
it. The `warded-backup` sidecar closes that gap once a day by writing a
consistent SQLite snapshot into Cloudflare R2.

| Property | Target | How it is produced |
|---|---|---|
| Recovery Time Objective (RTO) | minutes (manual, operator-driven) | restore is a download, gunzip and one file swap |
| Recovery Point Objective (RPO) | <= 24 h | one snapshot per day at 03:15 `Asia/Ho_Chi_Minh` |
| Integrity | `ok` | `PRAGMA integrity_check` and table counts after the restore |
| Retention | 14 days | `rclone delete --min-age 14d` after every successful upload |

Out of scope: point-in-time recovery inside the RPO window, logical corruption
introduced by the application, and R2 bucket or account deletion. This is a
single daily archive in one bucket, not a second independent copy.

Why a daily snapshot replaced streaming WAL replication: continuous
replication meant a constant Class A operation stream against R2 from a
Celeron-class host, and it bought an RPO the product does not need. `VACUUM
INTO` is WAL-safe: it reads one consistent snapshot, so committed frames still
sitting in the WAL are captured without a raw file copy that would race the
writer. A 24 h RPO is the accepted trade.

Why `VACUUM INTO` and not `sqlite3 .backup`: the online backup API restarts a
pass whenever the source is written while it copies. This database is written
continuously, so on 2026-11-24 a 558 MB `.backup` restarted for four hours on
the 5400 RPM disk, never converged and produced no archive, which is exactly
the missing `warded-20261124.sqlite3.gz` the R2 watchdog reported. `VACUUM
INTO` writes one compacted database in a single pass and cannot restart; the
sidecar bounds it with `timeout 5400` so a future stall fails the night loudly
instead of hanging until morning.

## 2. Topology

```
warded-backend (10001:10001)
        writes
          |
          v
/app/data/camera-view.db  <-- same volume --  warded-backup
  (+ -wal, -shm)                                sqlite3 VACUUM INTO -> /backup
                                                gzip -> /backup/warded-YYYYMMDD.sqlite3.gz
                                                rclone copyto
                                                      |
                                                      v
                                        r2:homelab-backup/warded/
```

| Component | Image / file | Responsibility |
|---|---|---|
| `warded-init-vol` | `alpine:3.20`, `user: "0:0"`, `restart: "no"` | Creates `/app/data/{locks,forecast,proofs}` and applies `chown 10001:10001` / `chmod 775` on a fresh volume, then exits |
| `warded-backup` | `alpine:3.20` + `apk add sqlite rclone tzdata`; root with a writable rootfs because the package install needs both; `cap_drop: [ALL]` and `no-new-privileges` still apply | Sleeps until the next 03:15 local time, snapshots the database onto the `warded-backup-staging` volume, gzips it, uploads it, prunes objects older than `KEEP_DAYS` |
| `warded-backup-staging` | named volume, `${BACKUP_VOLUME_NAME:-warded-backup-staging}` | Staging only: holds one snapshot and one archive at a time. It must not be a tmpfs, because `camera-view.db` is ~389 MB of RAM-backed memory counted against the 128M limit |
| `docker-compose.stg.yml` | service definition | Holds the whole loop; there is no separate config file to drift from it |

`warded-backup` waits on `warded-init-vol: condition:
service_completed_successfully`. Without that gate a fresh volume is root-owned
and the sidecar cannot open the database at all.

The sidecar never exits on a failed night. Every step logs with an explicit
`[INFO]` / `[ERR]` prefix; a failure is reported on stderr and the loop waits for
the next slot. Detection of a stalled backup is the R2 quota watchdog's job (it
requires today's `warded/` object to exist), not the container's.

## 3. Configuration

No credential enters the compose file. The sidecar builds an rclone S3 remote
named `r2` from environment variables that compose fills from the Portainer
stack environment:

| Variable | Compose default | Notes |
|---|---|---|
| `R2_ACCESS_KEY_ID` | empty | Object read/write on `homelab-backup` |
| `R2_SECRET_ACCESS_KEY` | empty | Never logged, never written to disk |
| `R2_ACCOUNT_ID` | `bb67574163a065e4aad475fb3aa6bb92` | Used to build the bucket-scoped endpoint, so a wrong account cannot reach AWS S3 |
| `R2_REGION` | `auto` | R2 requires `auto` |
| `BACKUP_BUCKET_NAME` | `homelab-backup` | Target bucket, prefix `warded/` |
| `BACKUP_KEEP_DAYS` | `14` | Age passed to `rclone delete --min-age` |
| `BACKUP_VOLUME_NAME` | `warded-backup-staging` | Named volume holding the snapshot and archive during a cycle |
| `BACKUP_CONTAINER_NAME` | `warded-backup` | Container name |
| `BACKUP_CPU_LIMIT` | `0.10` | cgroup CPU limit |
| `BACKUP_MEMORY_LIMIT` | `128M` | cgroup memory limit |
| `TZ` | `Asia/Ho_Chi_Minh` | Decides what 03:15 means |

Validate without deploying:

```bash
make lint-deploy
docker compose -f docker-compose.stg.yml config --quiet
```

## 4. Routine verification

Check the sidecar is alive and its last cycle is intact:

```bash
docker logs --tail 40 warded-backup
```

A healthy day ends with these lines:

```text
[INFO] sleeping 43200s until the next 03:15 Asia/Ho_Chi_Minh backup
[INFO] 2026-11-21 03:15:00 +07 starting backup of camera-view.db
[INFO] uploaded /backup/warded-20261121.sqlite3.gz to r2:homelab-backup/warded/
[INFO] retention applied: objects older than 14d deleted
[INFO] backup cycle finished
```

Confirm the object exists in the bucket:

```bash
rclone lsl r2:homelab-backup/warded/
```

Expect one object per day named `warded-YYYYMMDD.sqlite3.gz`. A missing object for
today is a backup failure, not a slow day.

## 5. Verifying a backup

`tools/deploy/restore_drill.py` proves the archive is restorable before you need
it. It writes a JSON summary (`docs/reports/sqlite_backup_drill_summary.json`)
and exits non-zero when a budget is missed.

```bash
make restore-drill EXTRA="--cycles 3"
cat docs/reports/sqlite_backup_drill_summary.json | python3 -m json.tool
```

Three modes, standard library only:

| Mode | What it does | Network |
|---|---|---|
| `hermetic` | Creates a WAL-mode SQLite database, snapshots it with `VACUUM INTO` exactly like the sidecar, gzips it, restores it in a sandbox, then checks `PRAGMA integrity_check`, row count, a table checksum and that every row committed before the snapshot is present. Run it with `make restore-drill RESTORE_DRILL_MODE=hermetic` | none |
| `live` | Lists `r2:<bucket>/warded/` with `rclone`, downloads the newest `warded-*.sqlite3.gz`, restores it and reports its age in hours | bucket read only |
| `auto` (default) | Runs `live` when `rclone` is on `PATH` and `RCLONE_CONFIG_R2_*` is configured; otherwise prints `[INFO] live drill skipped: <reason>` and runs the hermetic drill, so the snapshot/restore path is still exercised | may read the bucket |

Read the summary by field:

| Field | Healthy value |
|---|---|
| `verdict` | `pass`; a `skipped` summary only happens when no drill could run at all |
| `integrity` | `ok`, meaning `PRAGMA integrity_check` returned `ok` after the restore |
| `rows` | Row count of the sample table in the restored database; for a live drill, compare it with the source table |
| `checksum` | SHA-256 over the sample table rows; the hermetic drill compares it with the pre-snapshot value |
| `rto_seconds` | Restore wall time (gunzip, open, integrity check, table read); must stay inside `rto_budget_seconds` (default 60 s) |
| `rpo_seconds` | Archive age; must stay inside `min_age_hours` (default 24 h) |
| `archive` | Local archive name or `r2:<bucket>/warded/<object>` actually verified |
| `notes` | Why the live phase was skipped when `auto` fell back to the hermetic drill |

The RTO budget is a local-restore budget, not a service-level promise: it
measures the time to turn the archive into a verified file. Cutting the file
over and restarting the writers are operator steps and are not timed.

Run the live drill on the staging host, where the bucket credentials already
exist:

```bash
make restore-drill RESTORE_DRILL_MODE=live RESTORE_DRILL_BUCKET=homelab-backup
```

A `verdict: fail` with an `integrity` or `row count` error means the archive
itself is damaged; restore from the previous day and investigate the sidecar
logs for the night that produced it. A `verdict: fail` with an RPO error means
no fresh archive reached the bucket; check `docker logs warded-backup`.

## 6. Restore procedure

The restore is destructive only to the file you replace, and it needs the
writers stopped: SQLite allows one writer, and a running backend will keep
writing into a file you are about to swap.

1. **Stop the writers.** Keep the volume in place.
   ```bash
   docker compose -f docker-compose.stg.yml stop warded-backend warded-forecast-cron
   ```
2. **Pick the archive.** List the bucket and choose the newest object that
   predates the damage:
   ```bash
   rclone lsl r2:homelab-backup/warded/
   ```
3. **Download and unpack** into the data volume, using the same UID the services
   run as (`10001:10001`):
   ```bash
   export ARCHIVE=warded-20261121.sqlite3.gz
   docker run --rm -it \
     -v warded-stg_warded-stg-data:/app/data \
     -e RCLONE_CONFIG_R2_TYPE=s3 \
     -e RCLONE_CONFIG_R2_PROVIDER=Cloudflare \
     -e RCLONE_CONFIG_R2_ENDPOINT="https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com" \
     -e RCLONE_CONFIG_R2_ACCESS_KEY_ID -e RCLONE_CONFIG_R2_SECRET_ACCESS_KEY \
     alpine:3.20 sh -c '
       apk add --no-cache rclone >/dev/null &&
       rclone copyto "r2:homelab-backup/warded/$ARCHIVE" /tmp/restore.sqlite3.gz &&
       gunzip -c /tmp/restore.sqlite3.gz > /app/data/camera-view.db.restored &&
       chown 10001:10001 /app/data/camera-view.db.restored
     '
   ```
   The credentials come from your shell environment, so export
   `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID` and `R2_SECRET_ACCESS_KEY` first. Nothing
   is copied into the image and the archive is deleted when the container exits.
4. **Verify before swapping.** Never promote an unverified file:
   ```bash
   docker run --rm -v warded-stg_warded-stg-data:/app/data alpine:3.20 sh -c '
     apk add --no-cache sqlite >/dev/null &&
     sqlite3 /app/data/camera-view.db.restored \
       "PRAGMA integrity_check; PRAGMA quick_check; PRAGMA foreign_key_check;
        SELECT COUNT(*) FROM camera_verdicts;
        SELECT MAX(observed_at) FROM hotset_verdict_history;"
   '
   ```
   `integrity_check` must print `ok`, and the newest `observed_at` must sit
   within the expected 24 h window of the archive date.
5. **Swap atomically.** Keep the old file for forensics, and delete the stale
   `-wal` / `-shm` sidecars: leaving them behind makes SQLite replay an old WAL
   over the restored database and produces a torn state.
   ```bash
   docker run --rm -v warded-stg_warded-stg-data:/app/data alpine:3.20 sh -c '
     mv /app/data/camera-view.db /app/data/camera-view.db.corrupt &&
     mv /app/data/camera-view.db.restored /app/data/camera-view.db &&
     rm -f /app/data/camera-view.db-wal /app/data/camera-view.db-shm &&
     chown -R 10001:10001 /app/data
   '
   ```
6. **Start in order.** Backend first, then the cron sidecars:
   ```bash
   docker compose -f docker-compose.stg.yml up -d warded-backend warded-frontend warded-forecast-cron
   docker compose -f docker-compose.stg.yml up -d warded-backup
   ```
7. **Confirm.**
   ```bash
   curl -fsS https://warded.529studio.site/health
   docker logs --tail 20 warded-backup
   ```
   `/health` must answer 200. The backup sidecar only logs after 03:15, so an
   empty log beyond the install line is expected immediately after the swap.
8. **Record the outcome.** Write the incident, the archive date restored, and the
   measured data loss (restore time minus archive time) into the sprint report
   and delete `camera-view.db.corrupt` once the restore is confirmed good.

## 7. Failure modes

| Symptom | Cause | Action |
|---|---|---|
| `[ERR] sqlite3 VACUUM INTO failed or exceeded 5400s` | Volume owned by root, the destination file already exists, the database is locked by an unresponsive writer, or the 5400 s bound was hit | `docker compose run --rm warded-init-vol`; verify `ls -ldn /app/data` shows `10001 10001`; confirm `/backup` holds no stale `camera-view.db`; restart the backend |
| `[ERR] rclone upload failed` | Missing or wrong `R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY`, endpoint typo, or an R2 outage | Check the keys in the stack environment; a correct endpoint always resolves under `r2.cloudflarestorage.com` |
| `[ERR] apk add sqlite rclone tzdata failed` | No egress from the container, or the Alpine mirror is unreachable | Check the host DNS path; the sidecar retries every 300 s and never crash-loops |
| Backup lands at the wrong hour | `TZ` not applied (missing tzdata in a custom image) | Keep tzdata in the install line; confirm the log line prints `Asia/Ho_Chi_Minh` |
| `[ERR] rclone retention delete failed` | Bucket-level lifecycle conflict, or insufficient delete permission | Objects keep accumulating; fix permissions, then run the delete by hand once |
| No object for today, container healthy | The backup slot has not been reached yet | Compare the log timestamp to the 03:15 slot before treating it as a failure |
| `database disk image is malformed` after a restore | Old `-wal` / `-shm` left in place over the restored file | Repeat the swap and remove both sidecars in the same step |

## 8. Operating limits

- **One writer.** Never run two backends against the same volume, and stop the
  writers before replacing the file.
- **Retention is 14 days.** Restores older than that are impossible from this
  archive set; raise `BACKUP_KEEP_DAYS` before claiming a longer window, and
  check the bucket size it implies.
- **Bucket lifecycle rules must not expire `warded/`.** A lifecycle rule that
  deletes the prefix is data loss, not cleanup. The plan adds a `warded-14d`
  lifecycle rule that matches `BACKUP_KEEP_DAYS`.
- **The sidecar is bounded, not unprivileged**: it runs as root on a writable
  rootfs because `apk add sqlite rclone tzdata` needs both, and it is contained
  by `cap_drop: [ALL]`, `no-new-privileges`, no published port, and writable
  mounts limited to `warded-backup-staging` plus a 32 MB `/tmp` tmpfs. It has
  read-write access to the data volume because SQLite requires it, but it never
  writes there.
- **Staging volume size.** One snapshot (~389 MB) plus one gz archive at a time;
  the loop deletes both after every cycle. If the volume fills, the sidecar logs
  `[ERR] sqlite3 .backup failed` and keeps retrying the next night.
- **One archive is one copy.** A lost or corrupted bucket is a lost backup set;
  treat bucket deletion as a data-loss event.

## 9. References

- Compose stack: [`docker-compose.stg.yml`](../../docker-compose.stg.yml)
- Validator: [`tools/ci/check_deploy_manifests.py`](../../tools/ci/check_deploy_manifests.py)
- Restore drill: [`tools/deploy/restore_drill.py`](../../tools/deploy/restore_drill.py)
- Plan: `self-infras/plans/2026-11-20-phase3-two-site-tunnels.md`
- Superseded runbook (removed with the streaming sidecar):
  `docs/runbooks/sqlite_litestream_disaster_recovery.md` in git history
