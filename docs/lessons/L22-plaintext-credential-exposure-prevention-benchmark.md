## Lesson 22: Plaintext Credential Exposure Prevention & Benchmark Methodology Rigor

- **Incident & Tracking:** Sprint 5 Plan / S5-T1 Audit & S5-T4 R2 Storage, Issues #207, #214, PR #212 (`feat/r2-storage-integration`).
- **Symptom:**
  1. **Credential Exposure in Tooling**: A verification script (`tools/storage/verify_r2.py`) included Cloudflare R2 API keys as default fallback strings in `os.getenv("R2_ACCESS_KEY_ID", "...")`. Even though documentation files in `docs/` remained free of credentials, committing fallback strings into git history compromised the token pair, necessitating immediate key revocation on Cloudflare dashboard.
  2. **Smoke-Test Evaluation Flaw**: Re-running the smoke test post-T5 showed a nominal pass rate increase (20% -> 50%), but camera `66b1c426...` flipped from `SI=1.00 PASS` to `SI=0.00 FAIL`. Because scaling a non-zero Saturation Index by $1.67\times$ cannot mathematically produce zero, the discrepancy revealed that the smoke test pulled live snapshots rather than frozen reference images, conflating traffic fluctuations with parameter calibration.
- **Root Cause:**
  1. Using convenience defaults for API credentials in diagnostic scripts instead of failing fast when environment variables are omitted.
  2. Conducting before/after benchmark comparisons on live, dynamic physical streams rather than anchored, versioned ground-truth image sets.
- **Mandatory Takeaway:**
  1. **Zero Secret Fallbacks in Code**: Never provide actual credentials as fallback arguments (`os.getenv("SECRET", "hardcoded_val")`). Verification scripts must strictly enforce `if not key: sys.exit(1)` and require `.env` or environment injection.
  2. **Mandatory Key Rotation on Exposure**: Any key committed to git or shared across plain text channels must be immediately revoked on the provider dashboard and rotated.
  3. **Frozen Image Anchors for Methodology Rigor**: When benchmarking model inference or scoring calibration, before/after evaluations are only valid when executed against identical, frozen image frames. Re-fetching live snapshots invalidates causal attribution. The 764-frame controlled benchmark is the authoritative baseline.
