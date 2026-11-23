# L525 — Pure Go VAPID Cryptography, ECE AES-128-GCM Payload Encryption, and Dead Subscription Pruning

**Issue**: [#525](https://github.com/529-studio/projectKV/issues/525) · **Sprint**: 18 · **Date**: 2026-11-23

## 1. Zero external dependencies for Web Push cryptography

Integrating Web Push notifications in Go often leads developers to pull in third-party libraries such as `SherClockHolmes/webpush-go`. These libraries introduce heavy dependency trees, outdated crypto primitives, or unmaintained transitive dependencies. In reality, Go standard library packages (`crypto/ecdh`, `crypto/ecdsa`, `crypto/aes`, `crypto/cipher`, `crypto/sha256`, `crypto/rand`) and the standard `golang.org/x/crypto/hkdf` provide everything required to implement RFC 8291 (Encrypted Content-Encoding for Web Push) and RFC 8292 (VAPID for Web Push).

**Guardrail**: Implement RFC 8291 payload encryption and RFC 8292 VAPID token generation natively using pure Go standard library cryptography. Never add third-party push client libraries to `go.mod`. Ensure IEEE P1363 raw 64-byte `R || S` signature formatting for ES256 VAPID tokens instead of ASN.1 DER encoding.

## 2. Automatic dead subscription pruning on HTTP 404 and 410 Gone

Browser push subscriptions expire or become invalid when users revoke notification permissions, clear site data, or uninstall the browser. When a push service (Google FCM, Apple Web Push, Mozilla autopush) receives an expired or revoked subscription, it returns `410 Gone` or `404 Not Found`. Continuing to dispatch alerts to dead endpoints wastes network egress, CPU cycles, and database capacity.

**Guardrail**: When a push gateway answers with `404 Not Found` or `410 Gone`, the push client must immediately prune the subscription from `web_push_subscriptions` and remove all associated active rows in `flood_subscriptions`. Pruning prevents dead dispatch loops while keeping telemetry clean.

## 3. Storage deduplication via SHA-256 endpoint hashing

Push subscription endpoints are lengthy URLs (often 150 to 300 characters). Storing raw URLs as foreign keys in relational tables leads to index bloat and expensive lookups.

**Guardrail**: Compute a deterministic SHA-256 hex hash of the push endpoint as its primary key (`push_id`). Use this hash as the `chat_id` in `flood_subscriptions` with `channel_name = 'webpush'`, perfectly integrating with existing chokepoint and corridor subscriber finders without modifying table schemas or disrupting Telegram subscriptions.

## 4. Kiosk-grade ergonomics and zero-emoji compliance for browser push opt-in

Web Push permission prompts cannot be triggered automatically without explicit user gesture; modern browsers block unsolicited permission prompts. The opt-in UI must provide a clear dual-tab choice between the Telegram Bot and Browser Push, adhering strictly to minimalist monochromatic styling and minimum 48px touch targets.

**Guardrail**: Structure `SubscriptionModal.tsx` with dedicated channel tabs, offering one-tap browser subscription opt-in alongside Telegram deep links. All interactive buttons must enforce $\ge 48 \times 48\text{px}$ touch bounding boxes, zero emoji, and bilingual `vi`/`en` diacritic-complete translations.
