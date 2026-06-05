# Build/lint/test entry points. `make check` is the full gate the
# pre-commit hook runs; `make build` lints before compiling. Tool
# versions are pinned here — bump them deliberately.

STATICCHECK := honnef.co/go/tools/cmd/staticcheck@v0.7.0
MODERNIZE   := golang.org/x/tools/gopls/internal/analysis/modernize/cmd/modernize@v0.22.0

.PHONY: build test lint fmt check
.NOTPARALLEL:          # check's steps must run in order (fmt before lint)

build: lint            ## Compile (lints first) and refresh the ./ghost binary.
	go build ./...
	go build -o ghost .

test:                  ## Run the test suite.
	go test ./...

fmt:                   ## Auto-format in place.
	gofmt -w .

lint:                  ## Formatting + correctness + modern-idiom checks.
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
	  echo "✖ gofmt: run 'make fmt' to fix:" >&2; \
	  echo "$$unformatted" >&2; \
	  exit 1; \
	fi
	go vet ./...
	go run $(STATICCHECK) ./...
	go run $(MODERNIZE) ./...

check: fmt build test  ## Auto-format, then everything that must pass before a commit.
