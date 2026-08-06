# Contributing to Specter

Thanks for your interest in improving Specter. This document covers how to get
set up, the bar a change needs to clear, and how the pieces fit together.

## Getting started

```bash
git clone https://github.com/bakhod1r/spector
cd spector
go build ./...
go test ./...
```

Go is the only requirement for the core; the version floor is pinned in
[`go.mod`](go.mod). The browser suite in [`e2e/`](e2e/) additionally needs
Node.js.

## Development workflow

- Work on a branch, never directly on `main`.
- Write the test first. Specter is a generator: a change without a test that
  pins the new output is a change nobody can safely refactor later.
- Keep commits focused and their messages in the imperative mood
  (`feat(chi): resolve local prefixes`, `fix: …`, `docs: …`).
- Run the full suite before opening a pull request.

## The checks your change must pass

CI runs three workflows; you can reproduce them locally:

```bash
# Lint (see .github/workflows/lint.yml)
gofmt -l .            # must print nothing
go vet ./...
go mod tidy && git diff --exit-code go.mod go.sum

# Test (see .github/workflows/test.yml)
go test -race ./...                       # unit + race, 90% coverage floor
go test -tags integration ./tests/        # CLI integration
go test -bench . -benchtime=1x ./benchmarks/

# golangci-lint, if installed
golangci-lint run
```

The coverage floor is a guard against large regressions, not a target — raise
it deliberately, never lower it to make CI green.

## Adding a router adapter

Each adapter lives in `internal/adapter/<name>/` and implements `core.Adapter`.
Follow the existing pattern: a `Scan` that parses the directory, walks the
routing calls, resolves paths with `astutil` helpers, and returns routes,
schemas, and diagnostics. Add `testdata/` packages and wire the adapter into
`adapterFor`/`detect` in [`specter.go`](specter.go). Copy a small existing
adapter (e.g. `httprouter`) as a starting point.

## Adding an export format

Export formats live in `internal/export/`. Add the encoder, a test with a
golden `testdata` fixture, wire the flag in `cmd/specter/main.go`, and document
it in the README.

## Reporting bugs and requesting features

Open an issue with a minimal reproduction — for a scanning bug, the smallest
router snippet that produces the wrong document is ideal.

## Security

Do not open a public issue for a vulnerability. See [SECURITY.md](SECURITY.md).
