## Lesson 20: VPN SPOF, Graceful Degradation & Decoupling Cloud Daily Budgets from Local Compute

- **Incident & Tracking:** Issue #210, Sprint 5 Task S5-T4 (`infra/s5-t4-tailscale-failover-drill`).
- **Symptom:**
  Offloading CV inference to remote worker host `inference worker` via Tailscale mesh VPN introduced a network single-point-of-failure (VPN SPOF). Furthermore, during live staging audit, `/api/traffic-status` was found degraded to `source: "budget_breaker"`, causing inference requests to never even attempt network transport to `inference worker`.
- **Root Cause:**
  1. **Cloud Daily Budget Leaked into Local Compute**: `AnalysisBudget` was originally introduced in ADR-004 to guard against runaway Gemini/Cloud Vision cloud spend. When `ANALYZER=onnx` was added, the default 5,000 daily limit remained in effect. Polling 57 hotset cameras every 30s (~114 calls/min) exhausted this 5,000 quota within 44 minutes of midnight UTC, silently blocking real worker calls behind `budget_breaker`.
  2. **Unvalidated Circuit Breaker Under Network Chaos**: Graceful degradation logic (`callCtx` 10s timeout $\rightarrow$ `Verdict.SeverityUnknown` $\rightarrow$ `Source: "fallback"` $\rightarrow$ `CircuitBreaker`) had never been chaos-tested under physical network partition on live Staging.
- **Mandatory Takeaway:**
  1. **Decouple Cloud Cost Budgets from Self-Hosted Compute**: Local and edge neural compute incurs $0 API cost and should never be suffocated by cloud quota limiters. In `docker-compose.stg.yml` and staging configurations, set `ANALYZER_DAILY_BUDGET=500000` when `ANALYZER=onnx`.
  2. **Validate Resilience via Physical Chaos Drills (Rule 4)**: The live Staging disconnection drill proved that `warded-backend` maintained HTTP 200 uptime, bounded memory (37.8 MB, 14.8%), zero goroutine leaks, and instantly degraded verdicts to `SeverityUnknown` with neutral gray UI badges.
  3. **Auto-Recovery Over Human Intervention**: Verified that the Circuit Breaker automatically cooled down, probed `inference worker` upon restoration, and transitioned from `OPEN` $\rightarrow$ `HALF-OPEN` $\rightarrow$ `CLOSED`, resuming live ONNX telemetry within 1 cycle (< 30s).
  4. **Document Emergency Failover Routes**: Maintain a disaster recovery runbook ([`docs/runbook_dev_box_failover.md`](runbook_dev_box_failover.md)) documenting how to quickly switch `CV_ENGINE_URL` to standby container `warded-model:8000` (`model-local`) if the remote worker suffers prolonged hardware outage.
