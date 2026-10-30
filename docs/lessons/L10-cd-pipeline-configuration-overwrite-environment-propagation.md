## Lesson 10: CD Pipeline Configuration Overwrite & Environment Propagation

- **Incident & Tracking:** Issue #168, Pull Request (`fix/staging-compose-app-env`).
- **Symptom:**
  Manual updates made directly to Portainer Stack 14 environment settings were at risk of being completely overwritten whenever the GitHub Actions CD pipeline (`docker-publish.yml`) executed a deploy step (`PUT /api/stacks/${STACK_ID}` using `docker-compose.stg.yml`). Furthermore, omitting `APP_ENV` from the compose service block caused Go backend to fall back to `production` whenever `GIN_MODE=release`, triggering safety crashes (`main.go:268-271`) during mock/dev testing.
- **Root Cause:**
  Discrepancy between live Portainer UI configuration and the git repository. When CD redeploys stacks directly from repository files, any environment variable not explicitly declared in `docker-compose.stg.yml` defaults to empty or gets dropped during the payload PUT.
- **Mandatory Takeaway:**
  1. **Git as the Single Source of Truth for CD**: Never rely solely on ad-hoc UI or host edits on Portainer. Every environment variable used by container services must be codified in `docker-compose.stg.yml` and `.env.stg.example`.
  2. **Safe Fallbacks in Compose**: Always provide defensive compose fallbacks (`APP_ENV=${APP_ENV:-staging}`) to ensure predictable container behavior even if host variables are temporarily omitted.
