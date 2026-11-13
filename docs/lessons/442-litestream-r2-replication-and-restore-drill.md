# Lesson 442: A Backup Is a Hypothesis Until a Drill Restores It

## Context

S14-T2 (`#442`) added continuous SQLite replication to the staging stack: a
Litestream sidecar streams `/app/data/camera-view.db` to
`s3://<bucket>/db-replicas/camera-view.db` every second, and
`make restore-drill` restores a replica into a sandbox and checks integrity,
record counts, freshness, RTO and RPO. The task looked like "write a config
file". The failure modes that actually mattered were elsewhere.

## What went wrong (or would have)

1. **The volume was root-owned and two consumers are non-root.** A fresh Docker
   volume belongs to `root:root`, while the forecast cron and Litestream sidecar
   run as `10001:10001`, so staging would have failed with
   `unable to open database file` before Litestream ever shipped a frame. A
   one-shot `alpine:3.20` container running as `0:0` now creates and `chown`s the
   directories, and the backend, forecast cron and Litestream gate on
   `depends_on: service_completed_successfully`. Ordering is part of the
   permission fix, not a separate concern: chowning after the sidecars start is a
   race with no retry. (The backend image itself still runs as root today; the
   gate keeps that from becoming an implicit dependency on the old permissions.)
2. **Litestream's config is not a general YAML expression language.** The
   project's config uses `${R2_BUCKET_NAME}` and friends, expanded with
   `os.ExpandEnv`. `${VAR:-default}` is not supported and silently expands to an
   empty string, so the default must live in the compose environment
   (`${LITESTREAM_ACCESS_KEY_ID:-${R2_ACCESS_KEY_ID:-}}`), not in the YAML.
3. **`litestream restore` refuses `-config` together with a replica URL, and
   with `-config` it still requires the database path argument.** The first two
   drill attempts failed on exactly this. Both messages are precise, but only
   after you run the real binary; a plausible-looking command in a runbook is
   worth nothing until executed. The runbook now shows the verified form:
   `restore -config /etc/litestream.yml -o <out> /app/data/camera-view.db`.
4. **An exact-count restore check punishes correct replication.** With a live
   writer, the replica is legitimately up to one `sync-interval` behind, so
   `restored < source` is expected and bounded by the RPO. The drill now counts
   source rows newer than the restored recovery point and only forgives a
   deficit those rows explain. Without that, the choice is a check that always
   fails under load or a check that ignores real loss.
5. **A WAL that disappears is a replication error, not a curiosity.**
   `cannot read wal header` and `database disk image is malformed` appeared when
   more than one writer closed connections on the same file. SQLite supports one
   writer; Litestream adds a read lock. One long-lived writer per database is an
   invariant, and the runbook says so where an operator will read it.
6. **The first live drill cannot be against production R2.** No replica existed
   in the bucket, so a "live R2" run would only have proven that an empty prefix
   returns `no matching backups`. The drill instead ran the real Litestream
   binary with the real config against an S3-compatible server, and the JSON
   records that endpoint and bucket. Calling that "R2 verified" would have been
   the comfortable lie; leaving it unverified and labelled is the honest state.

## Invariants worth keeping

- **A backup that has never been restored is not evidence.** Ship the restore
  path in CI (`make restore-drill`, simulator mode) so the drill cannot rot
  silently between incidents.
- **Measure RTO and RPO; do not assert them.** The restore wall clock and the
  newest recovered record are data. A configured `sync-interval` is a promise,
  not a measurement.
- **Restore-time checks must model the writer.** Counts, freshness and integrity
  mean different things on a quiesced file and on a replica of a live database.
  Encode the difference, with the tighter rule as the default.
- **Bootstrap ordering is security-sensitive.** A root container that fixes
  ownership must be one-shot, must not publish ports, and every consumer must
  wait for successful completion.
- **Credentials only by reference.** The config file and the JSON summary may
  name the environment variable; neither may contain the value.
- **Label the transport.** `mode: simulator`, `litestream-file` and
  `litestream-s3` are different claims. A summary that blurs them becomes
  evidence of nothing.

## Evidence

- `docs/reports/s14_t2_litestream_restore_drill.md` — executed drills, measured
  RTO 0.15 s and RPO 0.00 s, and the explicit statement of what was not verified.
- `docs/reports/s14_t2_restore_drill_summary.json` — machine-readable summary of
  the committed live drill.
- `docs/runbooks/sqlite_litestream_disaster_recovery.md` — incident procedure.
- `tools/ci/check_deploy_manifests.py` — the invariants above, enforced as lint
  instead of prose.
