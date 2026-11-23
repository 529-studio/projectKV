# S18-T2 Implementation Plan: Web Push API Subscription & VAPID Dispatcher

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (recommended) or superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement end-to-end browser Web Push notifications (VAPID / RFC 8291 / RFC 8292) using pure Go standard library cryptography on the backend, persistent SQLite storage, and a client-side PWA service worker with dual-channel subscription UI in Next.js.

**Architecture:** A leaf package `services/go-server/internal/push` provides VAPID ES256 signing and RFC 8291 ECE encryption using Go standard library (`crypto/ecdh`, `crypto/aes`, `crypto/ecdsa`), implementing `notify.Channel` to dispatch corridor and flood alerts. Subscriptions are stored in SQLite table `web_push_subscriptions` linked to `flood_subscriptions`. The web client registers `public/sw.js` and provides an opt-in toggle in `SubscriptionModal.tsx` with $\ge 48\text{px}$ touch targets and zero emoji.

**Tech Stack:** Go 1.26+ (`crypto/ecdh`, `crypto/ecdsa`, `crypto/aes`), SQLite (`modernc.org/sqlite`), Next.js 15, TypeScript, PWA Service Worker, Vitest / Node Test Runner.

**Spec:** `docs/superpowers/specs/2026-11-23-web-push-vapid-spec.md`

---

## Global Constraints

- **AGENTS.md Rule 1 (Design & UX)**: Minimalist, monochromatic, zero-emoji in user-facing UI and backend logs. Bilingual i18n (`vi` default with full diacritics, `en` secondary).
- **AGENTS.md Rule 2 (Sound Solutions)**: Standard-compliant RFC 8291 ECE encryption and RFC 8292 VAPID, auto-pruning 404/410 dead endpoints.
- **AGENTS.md Rule 3 (Estimation Mandate)**: Active Effort: 8–10 engineering hours; Delivery Window: 2026-11-24.
- **AGENTS.md Rule 5 (Git Flow)**: Branch `feat/525-web-push-notifications` targeting `dev`.
- **AGENTS.md Rule 7 (Architecture Standards)**: Leaf modulith package `internal/push`, hand-written SQL only (no ORM), zero external third-party dependencies added to `go.mod`.
- **AGENTS.md Rule 8 (Zero Hardcoded Secrets)**: VAPID keys loaded dynamically from `.env` or generated at startup if unconfigured.
- **Kiosk Ergonomics**: Minimum 48px touch targets for all interactive controls.

---

## Proposed File Structure

### Backend (`services/go-server`)
- `internal/store/migrations/0014_web_push_subscriptions.sql`: SQLite table creation and indexes.
- `internal/push/vapid.go`: VAPID P-256 keypair generation, export, and RFC 8292 JWT signing.
- `internal/push/vapid_test.go`: Unit tests for VAPID token generation and ES256 verification.
- `internal/push/encrypt.go`: RFC 8291 AES-128-GCM ECE payload encryption with ephemeral ECDH and HKDF.
- `internal/push/encrypt_test.go`: Unit tests verifying payload encryption against RFC test vectors.
- `internal/push/store.go`: Hand-written SQLite store for `web_push_subscriptions` (insert, query by ID/endpoint, delete, prune).
- `internal/push/store_test.go`: Unit tests for subscription persistence and idempotency.
- `internal/push/client.go`: HTTP push dispatcher, header construction, and 404/410 auto-pruning.
- `internal/push/client_test.go`: Unit tests with httptest server simulating push service responses.
- `internal/push/channel.go`: `WebPushChannel` implementing `notify.Channel` (`Name() == "webpush"`, `Send`, `SendCorridor`).
- `internal/push/handler.go`: Gin HTTP handlers (`/api/push/vapid-key`, `/api/push/subscribe`, `/api/push/unsubscribe`, `/api/push/test`).
- `internal/push/handler_test.go`: HTTP endpoint tests.
- `cmd/server/routes.go`: Wire `/api/push` routes and register `WebPushChannel` into `notify.Manager`.

### Frontend (`apps/web`)
- `public/sw.js`: Browser service worker handling `push` and `notificationclick` events.
- `src/hooks/useWebPush.ts`: React hook managing push permissions, VAPID key loading, and subscription API sync.
- `src/hooks/useWebPush.test.ts`: Unit tests for VAPID Base64URL parsing and subscription state transitions.
- `src/components/SubscriptionModal.tsx`: Dual-tab UI supporting Telegram and Web Push with $\ge 48\text{px}$ touch targets.
- `src/components/SubscriptionModal.test.ts`: Component test verifying Web Push tab, permission actions, and zero-emoji compliance.

---

## Implementation Tasks

### Task 1: Database Migration & SQLite Store
- [ ] Create `services/go-server/internal/store/migrations/0014_web_push_subscriptions.sql`.
- [ ] Write failing test in `services/go-server/internal/push/store_test.go`.
- [ ] Implement `Store` interface and `SQLiteStore` in `services/go-server/internal/push/store.go`.
- [ ] Run `cd services/go-server && go test -race ./internal/push/...` and verify tests pass.

### Task 2: Pure Go VAPID Signing & RFC 8291 ECE Encryption
- [ ] Write failing tests in `services/go-server/internal/push/vapid_test.go` and `encrypt_test.go`.
- [ ] Implement `vapid.go`: P-256 keypair loading, Base64URL export, and RFC 8292 JWT creation with ES256.
- [ ] Implement `encrypt.go`: HKDF key expansion (`crypto/sha256`) and AES-128-GCM content encryption with `aes128gcm` header format.
- [ ] Verify unit tests pass with `go test -race ./internal/push/...`.

### Task 3: Push Dispatcher Client & `notify.Channel` Adapter
- [ ] Write failing tests in `services/go-server/internal/push/client_test.go`.
- [ ] Implement `Client` in `client.go` to send encrypted payloads with `Authorization: vapid ...` headers and prune 404/410 subscriptions.
- [ ] Implement `WebPushChannel` in `channel.go` satisfying `notify.Channel` and `SendCorridor`.
- [ ] Verify client tests pass with mock HTTP push server.

### Task 4: REST API Endpoints & Server Wiring
- [ ] Implement `Handler` in `services/go-server/internal/push/handler.go` (`GET /api/push/vapid-key`, `POST /api/push/subscribe`, `POST /api/push/unsubscribe`, `POST /api/push/test`).
- [ ] Wire routes and `notify.Manager` channel in `services/go-server/cmd/server/routes.go`.
- [ ] Write handler tests in `handler_test.go`.
- [ ] Run `make test-go` to confirm no modulith violations or cyclic imports.

### Task 5: Client Service Worker & `useWebPush` Hook
- [ ] Create `apps/web/public/sw.js` with background `push` notification display and `notificationclick` window focus.
- [ ] Write failing tests in `apps/web/src/hooks/useWebPush.test.ts`.
- [ ] Implement `apps/web/src/hooks/useWebPush.ts` handling `pushManager.subscribe` and backend API sync.
- [ ] Run `cd apps/web && npm test` and verify tests pass.

### Task 6: SubscriptionModal Integration & Kiosk Touch Ergonomics
- [ ] Update `apps/web/src/components/SubscriptionModal.tsx` to add "Web Push" tab, permission request button ($\ge 48\text{px}$), and zero-emoji status badges.
- [ ] Write tests in `apps/web/src/components/SubscriptionModal.test.ts` verifying tab switching and touch target hitboxes.
- [ ] Run `cd apps/web && npm test && npm run lint`.

### Task 7: Full Verification, Documentation & Lore Memory
- [ ] Run `python3 tools/docs/docs_lint.py` (0 new violations).
- [ ] Run `make test-go` and `cd apps/web && npx tsc --noEmit && npm test`.
- [ ] Author engineering lesson `docs/lessons/525-web-push-vapid-notifications.md`.
- [ ] Record claims in Lore persistent memory (`lore write`).
