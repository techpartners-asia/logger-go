# Development

## Development

```
make test        # all modules, race detector
make lint        # go vet + gofmt (+ golangci-lint if installed)
make bench
make examples
make templates   # vet and test the starter projects
```

The repo is a Go workspace (`go.work`) so adapters build against the local
core. Adapter `go.mod` files carry a `replace ../..` directive for the same
reason; consumers ignore it and resolve the tagged core version.

Requirements: Go 1.24+ for the core module; Go 1.25+ for the Fiber adapter
(Fiber v3's own requirement).

## Releasing

Releases are automatic. Every push to `main` that passes CI gets the next
version: the `release` job in `.github/workflows/ci.yml` tags the core
(`vX.Y.Z`) and each adapter changed since the last release
(`adapters/<name>/vX.Y.Z`), then publishes a GitHub release with generated
notes.

The bump is patch by default. Control it from commit messages:

| In a commit message | Effect |
|---|---|
| `#minor` (any commit since the last release) | `v0.4.2` → `v0.5.0` |
| `#major` or `BREAKING CHANGE` (any commit since the last release) | `v0.4.2` → `v1.0.0` |
| `[skip release]` (the pushed head commit) | no release for this push |

Adapters pin the core version in their `go.mod`. When an adapter starts
depending on new core API, bump that requirement in the same change.
