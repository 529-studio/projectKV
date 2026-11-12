# Lesson 91: Staging Reverse Proxy Entrypoint and Cloudflare Tunnel Topology Alignment

## Navigation Aid & Quick Reference
- **Domain**: Infrastructure deployment, reverse proxy routing, Cloudflare Tunnel topology, Docker Compose staging runtime.
- **Relevant Issues**: Issue #432, Issue #402, Issue #428, Issue #429.
- **Key Files**:
  - `docker-compose.stg.yml` (Traefik router label configuration for `warded-backend` and `warded-frontend`)
  - `.env.stg.example` (Staging environment variable defaults)
  - `.github/workflows/docker-publish.yml` (Portainer deployment trigger and post-deploy health probe)
- **Core Invariants**:
  - **Reverse Proxy Ingress Alignment Invariant**: In deployments where TLS termination is handled upstream by Cloudflare Tunnel (`cloudflared`) and proxied internally over HTTP, local container ingress routers (Traefik) must bind to the existing HTTP entrypoint (`web` on port 80). Routers must not bind to nonexistent entrypoints (`websecure`) or declare nonexistent certificate resolvers (`tls.certresolver=cloudflare`), which causes Traefik to drop the router entirely and return HTTP 404 for all ingress traffic.
  - **Fail-Fast Post-Deploy Health Check**: CD pipelines must probe external domain endpoints (`${STG_HEALTH_URL}`) following container recreation to detect routing and proxy registration dropouts before concluding deployment success.

---

## 1. Context & Problem Statement

Following the resolution of deployment queue deadlocks on staging, the CD deployment workflow (`Docker Build, Publish and Deploy`) successfully pushed updated containers to Portainer. However, the pipeline consistently failed at the `Verify Post-Deploy Health` step:
```text
Verifying health after redeployment...
curl: (22) The requested URL returned error: 404
Staging backend health check failed!
```

Inspection of the running containers on the staging host revealed a discrepancy:
1. Container `/warded-backend` was running and reported `healthy`:
   ```text
   wget http://localhost:3000/healthz -> 200 OK
   ```
2. Reverse proxy container `/traefik` emitted recurring errors:
   ```text
   ERR EntryPoint doesn't exist entryPointName=websecure routerName=warded-stg-be@docker
   ERR No valid entryPoint for this router routerName=warded-stg-be@docker
   ERR Router uses a nonexistent certificate resolver certificateResolver=cloudflare routerName=warded-stg-be@docker
   ERR EntryPoint doesn't exist entryPointName=websecure routerName=warded-stg-fe@docker
   ERR No valid entryPoint for this router routerName=warded-stg-fe@docker
   ```

Because Traefik could not find entrypoint `websecure` or certificate resolver `cloudflare`, it dropped all routing rules for `warded-stg.529studio.site`, falling back to its default handler and serving `404 page not found` for every request.

---

## 2. Root Cause Analysis

1. **Topology Assumption Mismatch**: `docker-compose.stg.yml` was originally authored assuming Traefik directly handled public TLS via port 443 with an ACME DNS challenge (`TRAEFIK_ENTRYPOINT=websecure`, `TRAEFIK_CERTRESOLVER=cloudflare`).
2. **Upstream Ingress Reality**: The production gateway topology on the staging host runs `traefik:v3.7.11` configured with a single HTTP entrypoint on port 80 (`--entrypoints.web.address=:80`), fronted by `cloudflared` (Cloudflare Tunnel). SSL/TLS is terminated at the Cloudflare edge; Cloudflare Tunnel passes plaintext HTTP to Traefik over the internal `core-network` Docker bridge.
3. **Cascading Route Rejection**: Traefik treats references to undefined entrypoints or resolvers as fatal configuration errors for the affected routers, discarding the routes rather than falling back to default entrypoints.

---

## 3. Core Architectural Decisions

### A. Fallback Entrypoint Alignment
In `docker-compose.stg.yml`:
- Set `TRAEFIK_ENTRYPOINT` default fallback to `web`:
  ```yaml
  traefik.http.routers.${ROUTER_NAME:-warded-stg}-be.entrypoints: ${TRAEFIK_ENTRYPOINT:-web}
  traefik.http.routers.${ROUTER_NAME:-warded-stg}-fe.entrypoints: ${TRAEFIK_ENTRYPOINT:-web}
  ```
- Removed phantom `traefik.http.routers.*.tls.certresolver` label definitions, aligning `warded-stg` with peer stack conventions (`f1-backend`, `outline`, `vaultwarden`) on the same host.

### B. Environment Template Consistency
In `.env.stg.example`:
- Updated `TRAEFIK_ENTRYPOINT=web`.
- Removed `TRAEFIK_CERTRESOLVER=cloudflare`.

---

## 4. Verification & Prevention

1. **Topology Validation**: Verified peer service labels across `core-network` (`vaultwarden`, `outline`, `f1-backend`) to ensure uniform entrypoint bindings.
2. **Automated Gate Coverage**: `Verify Post-Deploy Health` in `docker-publish.yml` actively verifies external domain resolution after each Portainer stack update.
