# S14-T2 Litestream Replication & Restore Drill Verification

- **Issue:** #442 (S14-T2)
- **Branch:** `feat/s14-t2-litestream-replication-and-restore-drill`
- **Date:** 2026-11-13
- **Host:** macOS (darwin), Docker 29.2.1, Compose 5.1.1, Python 3.14.6,
  SQLite 3.43.2, Litestream v0.3.13 (official darwin-arm64 binary)
- **Artifact:** [`s14_t2_restore_drill_summary.json`](s14_t2_restore_drill_summary.json)

This report records what was executed, not what the design intends. Every
number below comes from the command shown beside it. Nothing here claims that a
production Cloudflare R2 bucket was used: the live drill ran against a real
S3-compatible endpoint on the host (section 4.3 explains why and what that
does and does not prove).

## 1. Delivered changes

| File | Change |
|---|---|
| `deploy/litestream/litestream.yml` | New. Streams `/app/data/camera-view.db` to `s3://${R2_BUCKET_NAME}/db-replicas/camera-view.db`, `sync-interval: 1s`, `snapshot-interval: 24h`, `retention: 168h`, credentials by `${...}` reference only |
| `docker-compose.stg.yml` | New `warded-init-vol` (root, one-shot, `mkdir`/`chown 10001:10001`/`chmod 775`); new `warded-litestream` sidecar (pinned `0.3.13`, UID 10001, read-only rootfs, `cap_drop: [ALL]`); `depends_on: warded-init-vol: service_completed_successfully` on backend, forecast cron and Litestream |
| `tools/ci/check_deploy_manifests.py` | New `check_litestream_config` and `check_compose_ha_stack` validators (db/replica path, 1s sync, env-only credentials, pinned image, read-only config mount, init gating, init command contents) |
| `tools/ci/check_deploy_manifests_test.py` | Fixtures extended; 14 new violation tests (20 total) |
| `tools/deploy/restore_drill.py` | New. Restore + integrity + counts + freshness + RTO/RPO drill with JSON summary |
| `tools/deploy/test_restore_drill.py` | New. 35 tests covering config parsing, timestamp parsing, simulator restore, corruption/missing-table/lost-row/FK/budget failures, live restore orchestration |
| `Makefile` | `restore-drill` and `test-deploy` targets, help text, `RESTORE_DRILL_MODE` / `RESTORE_DRILL_OUT` |
| `.env.example`, `.env.stg.example` | `LITESTREAM_*`, `INIT_VOL_CONTAINER_NAME` with `<your-...>` placeholders |
| `docs/runbooks/sqlite_litestream_disaster_recovery.md` | Operating and incident-restore runbook |
| `docs/lessons/442-litestream-r2-replication-and-restore-drill.md` | Engineering lesson |

## 2. Static validation

### 2.1 Deploy manifest invariants

```
$ make lint-deploy
python3 tools/ci/check_deploy_manifests.py
DEPLOY LINT PASSED: All deployment manifests satisfy security and scheduling invariants.
```

### 2.2 Compose resolves, including optional profiles

```
$ docker compose -f docker-compose.stg.yml config --quiet ; echo $?
0

$ docker compose -f docker-compose.stg.yml --profile harvester --profile forecast-cron config --services
warded-init-vol
warded-backend
warded-forecast-cron
warded-frontend
warded-harvester
warded-litestream
```

A missing endpoint fails closed instead of letting the S3 client default to
AWS and hand it R2 credentials:

```
$ env -u R2_ENDPOINT CV_ENGINE_URL=http://example.invalid:8000 \
  docker compose -f docker-compose.stg.yml --env-file /dev/null config --quiet
error while interpolating services.warded-litestream.environment.[]:
required variable R2_ENDPOINT is missing a value:
R2_ENDPOINT must be provided for Litestream replication
```

### 2.3 The real Litestream binary accepts the config

```
$ R2_BUCKET_NAME=warded-proofs R2_ENDPOINT=https://example.r2.cloudflarestorage.com \
  R2_REGION=auto LITESTREAM_ACCESS_KEY_ID=x LITESTREAM_SECRET_ACCESS_KEY=y \
  var/bin/litestream databases -config deploy/litestream/litestream.yml
path                      replicas
/app/data/camera-view.db  r2
```

The same command inside the pinned image, as UID 10001 with a read-only root
filesystem and the config mounted `:ro`:

```
$ docker run --rm -u 10001:10001 --read-only --tmpfs /tmp \
  -e R2_BUCKET_NAME=warded-proofs -e R2_ENDPOINT=https://example.r2.cloudflarestorage.com \
  -e R2_REGION=auto -e LITESTREAM_ACCESS_KEY_ID=test -e LITESTREAM_SECRET_ACCESS_KEY=test \
  -v "$PWD/deploy/litestream/litestream.yml:/etc/litestream.yml:ro" \
  litestream/litestream:0.3.13 databases -config /etc/litestream.yml
path                      replicas
/app/data/camera-view.db  r2
```

Without the bucket variable the same container fails closed with
`bucket required for s3 replica`, which is the intended behaviour: a missing
deployment variable must not silently start an unauthenticated replicator.

### 2.4 The init container actually fixes a fresh volume

Scratch volume, then the exact command from `docker-compose.stg.yml`, then a
write as the unprivileged UID:

```
$ docker volume create warded-drill-vol
$ docker run --rm -u 0:0 -v warded-drill-vol:/app/data alpine:3.20 \
    sh -c "mkdir -p /app/data/locks /app/data/forecast /app/data/proofs && \
           chown -R 10001:10001 /app/data && chmod -R 775 /app/data"
$ docker run --rm -u 10001:10001 -v warded-drill-vol:/app/data alpine:3.20 \
    sh -c "touch /app/data/probe.db && ls -lnd /app/data /app/data/locks /app/data/proofs"
drwxrwxr-x    5 10001    10001         4096 /app/data
drwxrwxr-x    2 10001    10001         4096 /app/data/locks
drwxrwxr-x    2 10001    10001         4096 /app/data/proofs
```

## 3. Drill design

`tools/deploy/restore_drill.py` restores into an isolated sandbox and then
refuses to call the result usable unless all of the following hold:

| Check | Rule |
|---|---|
| `integrity_check` | exactly `ok` |
| `quick_check` | exactly `ok` |
| `foreign_key_check` | zero rows |
| `schema` | `camera_verdicts`, `hotset_verdict_history`, `flood_subscriptions` all present |
| `record_counts` | non-empty; not below the source count at restore start, except rows that fall inside the measured recovery-point window; never above the source total |
| `record_freshness` | newest restored record is within the RPO budget of wall clock and newer than the drill's first write |
| `rto` | measured restore wall clock <= 60 s |
| `rpo` | source newest record minus restored newest record <= 10 s |

The count rule is deliberately time-aware. A replica that is one sync interval
behind a live writer is *correct*, and requiring exact equality would either
fail every streaming drill or force the drill to pause the writer and stop
measuring the thing that matters. The tool therefore counts source rows newer
than the restored recovery point with `julianday()` and only forgives a deficit
that those rows explain.

## 4. Executed drills

### 4.1 Hermetic simulator (no network, CI path)

```
$ python3 tools/deploy/restore_drill.py --mode simulator --cycles 3 --sync-interval 0.2 --out /tmp/sim_summary.json
restore drill (simulator) - PASS
  transport          : local-simulation
  RTO                : 0.00s (budget 60s)
  RPO                : 0.00s (budget 10s)
  [pass] integrity_check: ok
  [pass] quick_check: ok
  [pass] foreign_key_check: no foreign key violations
  [pass] schema: 3 business tables present (5 total)
  [pass] record_counts: verdicts=6, verdict_history=6, subscriptions=1
  [pass] record_freshness: newest record 2026-11-13T01:48:36.468404Z is 0.0s old
  [pass] rto: restore took 0.00s <= 60s budget
  [pass] rpo: recovery point lag 0.0s <= 10s budget
```

Simulator metrics: RTO 0.003 s, RPO 0.000 s. This path builds a real WAL-mode
database, takes a base snapshot with SQLite's online backup API, captures one
full WAL per sync interval, then restores snapshot + newest WAL and lets SQLite
run WAL recovery. It is not Litestream's LTX format, and the summary says
`mode: simulator` so it can never be mistaken for live replication evidence.

### 4.2 Streaming file replica (real Litestream, live writer)

The same tool drove the real binary over a `file://` replica while heartbeat
transactions were committed:

```
$ python3 tools/deploy/restore_drill.py --mode live \
  --source-db /tmp/live-drill/camera-view.db \
  --replica-url file:///tmp/live-drill/replica/camera-view.db \
  --start-replication --replicate-seconds 12 --sync-interval 1 \
  --litestream var/bin/litestream --out -
restore drill (live) - PASS
  transport          : litestream-file
  RTO                : 0.06s (budget 60s)
  RPO                : 0.00s (budget 10s)
  [pass] integrity_check: ok
  [pass] quick_check: ok
  [pass] foreign_key_check: no foreign key violations
  [pass] schema: 3 business tables present (12 total)
  [pass] record_counts: verdicts=196, verdict_history=53223, subscriptions=0
  [pass] record_freshness: newest record 2026-11-13T01:34:33.655878Z is 1.1s old
```

Source: a copy of the local `services/go-server/data/camera-view.db`
(20,742,144 bytes; 53,211 history rows, 193 verdicts, 0 subscriptions).
Twelve heartbeat transactions were committed during replication and all twelve
were present in the restored copy (`53,211 + 12 = 53,223`).

A later run against the same S3-compatible endpoint while the writer was still
committing measured `rpo_seconds: 1.01` and reported
`verdict_history: 1 row(s) pending inside the recovery-point window` — the
replica was exactly one sync interval behind, which is the expected RPO for
`sync-interval: 1s` and well inside the 10 s budget.

### 4.3 S3-compatible replica using the deployment config (committed artifact)

```
$ R2_BUCKET_NAME=warded-drill \
  R2_ENDPOINT=http://127.0.0.1:19000 \
  R2_REGION=us-east-1 \
  LITESTREAM_ACCESS_KEY_ID=... LITESTREAM_SECRET_ACCESS_KEY=... \
  python3 tools/deploy/restore_drill.py --mode live \
    --source-db var/restore-drill/s3source/camera-view.db \
    --replica-url s3://warded-drill/db-replicas/camera-view.db \
    --sandbox var/restore-drill/s3-restore --keep-sandbox \
    --out docs/reports/s14_t2_restore_drill_summary.json

restore drill (live) - PASS
  transport          : litestream-s3
  replica            : s3://warded-drill/db-replicas/camera-view.db
  RTO                : 0.15s (budget 60s)
  RPO                : 0.00s (budget 10s)
  [pass] integrity_check: ok
  [pass] quick_check: ok
  [pass] foreign_key_check: no foreign key violations
  [pass] schema: 3 business tables present (12 total)
  [pass] record_counts: verdicts=195, verdict_history=53227, subscriptions=0
  [pass] record_freshness: newest record 2026-11-13T01:48:23.030275Z is 1.0s old
  [pass] rto: restore took 0.15s <= 60s budget
  [pass] rpo: recovery point lag 0.0s <= 10s budget
```

The replication side was not simulated: the pinned `litestream/litestream:0.3.13`
image ran `replicate -config /etc/litestream.yml` with
`deploy/litestream/litestream.yml` mounted read-only, the database bind-mounted at
`/app/data`, and bucket, endpoint, region and credentials supplied only through
environment variables. Litestream wrote the expected object layout:

```
$ find var/restore-drill/s3data -type f | head -3
.../db-replicas/camera-view.db/generations/fe5d904499f543f7/snapshots/00000000.snapshot.lz4
.../db-replicas/camera-view.db/generations/fe5d904499f543f7/wal/00000000_00000000.wal.lz4
```

The restore then used the production config and the production command shape:

```
var/bin/litestream restore -o .../camera-view.db \
  -config deploy/litestream/litestream.yml /app/data/camera-view.db
```

**Why not the real R2 bucket?** No Litestream replica exists in the staging R2
bucket yet (this PR is the first to introduce replication), so a live R2 restore
would prove only that an empty prefix returns `no matching backups`. Instead the
transport ran against a real S3 server (`rclone serve s3`) on the host, using the
same config file, the same bucket layout and the same Litestream release. The
committed JSON records the actual endpoint (`http://127.0.0.1:19000`) and bucket
(`warded-drill`) rather than implying R2 was exercised. First staging deployment
must rerun the drill with `RESTORE_DRILL_REPLICA_URL=s3://$R2_BUCKET_NAME/...`
before the feature is called verified in production.

## 5. Artifact contents

`docs/reports/s14_t2_restore_drill_summary.json` (abridged):

```json
{
  "task": "S14-T2",
  "issue": 442,
  "mode": "live",
  "verdict": "pass",
  "metrics": {
    "rto_seconds": 0.151,
    "rto_budget_seconds": 60.0,
    "rpo_seconds": 0.0,
    "rpo_budget_seconds": 10.0,
    "rpo_method": "reference_newest_minus_restored_newest",
    "restored_newest_record_at": "2026-11-13T01:48:23.030275Z",
    "reference_newest_record_at": "2026-11-13T01:48:23.030275Z"
  },
  "restore": {
    "transport": "litestream-s3",
    "replica_url": "s3://warded-drill/db-replicas/camera-view.db"
  },
  "counts": {
    "verdicts":         { "count": 195,   "committed_before_restore": 195 },
    "verdict_history":  { "count": 53227, "committed_before_restore": 53227 },
    "subscriptions":    { "count": 0,     "committed_before_restore": 0 }
  }
}
```

`flood_subscriptions` is empty on this dataset; the drill treats an empty
subscription table as valid (a fresh deployment has none) while an empty verdict
or history table fails.

## 6. Test and gate results

```
$ make test-deploy
Ran 35 tests in 0.511s
OK

$ make test-ci
Ran 72 tests in 0.971s
OK            # 20 deploy-manifest invariant tests + evidence-integrity suite

$ make test-docs
Ran 27 tests in 1.282s
OK

$ make docs-lint
20 violation(s), 20 in the baseline, 0 new, 0 stale
Pass: nothing new, nothing stale.

$ python3 tools/ci/check_evidence_integrity.py --file docs/reports/s14_t2_restore_drill_summary.json
Evidence Integrity CI Gate: evaluated 1 artifacts.
PASS: All evidence artifacts satisfy integrity, temporal, model, and anti-spoofing gates.
```

Tests exercise the failure paths, not only the happy path: truncated restore,
dropped table, deleted rows, rows present in the restore but absent from the
source, foreign-key violation, RTO breach, RPO breach, stale records, missing
binary, missing source database, `-config`/URL mismatch, and the case where a
deficit is legitimately inside the recovery-point window.

CI now runs `make lint-deploy`, `make test-deploy` and a hermetic
`make restore-drill` on every change that touches `tools/**` or the manifests.

## 7. Limitations and follow-ups

1. **Real R2 round trip is unproven until first staging deploy.** The config,
   binary, object layout and restore command were verified against an
   S3-compatible server; the R2 endpoint itself was not exercised. Run
   `RESTORE_DRILL_REPLICA_URL=s3://$R2_BUCKET_NAME/db-replicas/camera-view.db make
   restore-drill RESTORE_DRILL_MODE=live` on staging after the first deploy and
   attach the JSON.
2. **No long-running soak.** Replication was observed for seconds to minutes, not
   days. Generation rollover, retention cleanup and R2 lifecycle interaction
   remain unmeasured.
3. **RTO was measured on a 20 MB database.** A larger database or a cold R2
   connection will be slower; the 60 s budget has not been tested near its limit.
4. **The sidecar is not a backup.** It replicates to one bucket; bucket loss is
   still data loss. An independent archive remains future work.

See the runbook for the operating procedure:
[`docs/runbooks/sqlite_litestream_disaster_recovery.md`](../runbooks/sqlite_litestream_disaster_recovery.md).
