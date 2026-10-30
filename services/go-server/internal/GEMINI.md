# Go Modulith — `services/go-server/internal/`

This directory is governed by `docs/GO_BACKEND_ARCHITECTURE.md` §1-§3. That document is
the single source of truth for the allowed import graph, the banned dependencies, the
package responsibilities and the directory layout.

This file deliberately holds **no copy** of those rules. The previous copy had drifted:
it was labelled "exhaustive" while covering only 15 of the 20 packages that exist here
(`feedback`, `flood`, `middleware`, `notify`, `subscription` were absent), so agents
editing those packages were held to a rule set that did not describe them.

Before editing any file under `internal/`:

1. Read §1-§3 of `docs/GO_BACKEND_ARCHITECTURE.md`.
2. If a change needs an import that §2 does not list, define an interface in the
   consuming package instead of importing the producing package directly.
3. Never reach into another package's unexported types.

After a change: `gofmt -w` and `go vet ./...`.
