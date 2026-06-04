# ghost — project rules

## Build & checks

- To build, run `make build` (never bare `go build`). `make build` runs
  the full lint pass first, so compiling and linting always happen
  together.
- Before claiming work is done or committing, run `make check` and
  confirm it passes. `make check` auto-formats (`gofmt -w`) and then runs
  lint + build + test. The pre-commit hook runs the same `make check`, so
  a commit that skips it will be blocked anyway.
- Keep the tree clean: `gofmt`, `go vet`, `staticcheck`, and `modernize`
  must all be silent. Fix findings rather than suppressing them. Tool
  versions are pinned in the `Makefile` — bump them deliberately.
