# Contributing

## Before you start

- Read [`AGENTS.md`](AGENTS.md) for repository conventions. They apply to humans
  and automated agents alike.
- For non-trivial changes, open an issue first so the approach can be agreed on.
- Security problems must follow [`SECURITY.md`](SECURITY.md), never a public issue.

## Branch and pull request flow

1. Branch from `dev` using `feat/*`, `fix/*`, `chore/*` or `docs/*`.
2. Never push directly to `dev` or `main`.
3. Open a pull request targeting `dev`. Describe the problem, the change and how
   it was verified.
4. Keep pull requests focused. Unrelated refactors belong in separate pull requests.

## Local checks

Run the same checks CI runs before opening a pull request.

```bash
# Public-safety guard (mandatory, runs on every PR)
bash scripts/check-public-safe.sh

# Go backend
cd services/go-server && go vet ./... && go test -race ./... && go build ./...

# Web frontend
cd apps/web && npm ci && npm run lint && npm test && npm run build

# CV engine
cd services/cv-engine && python3 -m unittest discover -s . -p "test_*.py"
```

## Commit messages

Conventional commits, scoped and imperative:

```
fix(go-server): guard against empty camera catalog
feat(web): add corridor dock severity badge
chore(ci): pin action versions by commit sha
```

## Code and content rules

- No emoji anywhere: not in code, logs, documentation, commit messages or pull
  request text. Use text tags such as `[OK]`, `[ERR]`, `[WARN]`.
- No secrets, tokens, internal host names or private network addresses. Load
  configuration from environment variables; the guard rejects the rest.
- User-facing strings are bilingual (`vi` / `en`) where the UI supports it.
- Documentation lives next to the code it describes. Update the affected pages in
  the same pull request.

## License

By contributing you agree that your contribution is licensed under the GNU Affero
General Public License v3.0, as described in [`LICENSE`](LICENSE).
