# Security Policy

## Supported versions

Only the `main` and `dev` branches receive security fixes. Older branches and
tags are provided as-is.

## Reporting a vulnerability

Do not open a public issue for security problems.

Use GitHub private vulnerability reporting on this repository
(Security tab, "Report a vulnerability"). If that is unavailable, contact the
maintainers at `contact@529studio.site`.

Include:

- affected component (`apps/web`, `services/go-server`, `services/cv-engine`,
  `tools/*`, deployment workflow)
- steps to reproduce or a proof-of-concept
- impact assessment (data exposure, remote code execution, denial of service)
- any suggested remediation

## What to expect

- acknowledgement within 3 business days
- a triage decision (accepted, needs information, out of scope) within 10 days
- credit in the release notes once a fix ships, unless you ask to stay anonymous

## Out of scope

- findings that require a compromised maintainer account or physical access
- rate limiting reports without a demonstrated impact
- missing hardening headers on endpoints that serve only static, public data
- dependency version bumps already tracked by Dependabot

## Handling of secrets

This repository must never contain credentials, tokens, private keys, hostnames
of internal infrastructure, or private network addresses. Every pull request runs
`scripts/check-public-safe.sh`, which fails on those patterns. If you find a
leaked credential in the tree or in history, report it privately so it can be
rotated and purged.
