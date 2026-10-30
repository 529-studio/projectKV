## Lesson 28: Zero Unauthenticated Dev Endpoints & Fail-Closed Developer Gatekeeping

- **Incident & Tracking:** Issue #214, Pull Request #218 (`feat/s5-t4-r2-shared-team-labeler`).
- **Symptom:**
  The shared team web labeler (`/labeler`) and snapshot archiver backend endpoints (`/api/archiver/*`) deployed in S5-T4 were completely exposed and publicly accessible without authentication. Any public visitor or external entity could view raw camera snapshot archives, browse captured frames, and submit forged ground-truth labels via `POST /api/archiver/labels`, creating a severe data poisoning risk for AI model evaluation and safety gates.
- **Root Cause:**
  Internal developer tooling was merged into the production web router under the assumption of development obscurity, without an explicit authorization boundary or production fail-closed security policy.
- **Mandatory Takeaway & Security Principles:**
  1. **Fail-Closed Backend Gatekeeper**: Endpoints managing training labels, raw archives, or internal developer utilities MUST enforce developer authorization via `middleware.RequireDevAuth`. In `production` and `staging` environments, if `DEV_ACCESS_TOKEN` is unset or empty, the backend must fail closed with `503 Service Unavailable`, preventing unauthenticated access under misconfiguration.
  2. **Multi-Channel Authentication**: To support both headless REST calls and browser image binary rendering, authentication must support:
     - `X-Dev-Token` header (standard custom header)
     - `Authorization: Bearer <token>` header (RFC 6750 standard)
     - `?token=` and `?dev_token=` query parameters (required for browser `<img>` elements which cannot set custom request headers)
  3. **Timing-Attack Resistance**: Token validation must use `crypto/subtle.ConstantTimeCompare` to eliminate side-channel timing attacks.
  4. **Monochromatic Client-Side Gatekeeper**: Web labeler UI (`/labeler`) displays an in-place high-contrast monochromatic lock screen until unlocked with a valid token, persisting credentials strictly in `sessionStorage` (cleared on tab close or explicit lock session), and automatically evicts cached tokens upon receiving `401 Unauthorized`.
