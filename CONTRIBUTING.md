# Contributing

## Setup

The Go toolchain is managed by mise (`mise.toml`); `go.mod` pins `toolchain go1.27.3`. Run Go through mise so a cached toolchain that differs from `go.mod` does not break `go test` with "version mismatch":

```bash
mise install
mise exec -- make tools      # golangci-lint, gofumpt, govulncheck… into ./bin
sh scripts/setup-precommit.sh
```

## Before sending a change

```bash
mise exec -- make all        # check-format (gofumpt), lint (golangci-lint --fix), test
mise exec -- go test -race ./...
mise exec -- make vulncheck
gocognit -over 15 .          # cognitive complexity, SonarQube S3776 threshold
```

- Performance-sensitive changes: `mise exec -- make bench` before and after, compared with `benchstat`.
- Every `//nolint` carries its reason (`//nolint:wrapcheck // <why>`); `nolintlint` rejects bare directives.
- Add a CHANGELOG entry in the same change. Breaking changes go under `### Incompatible` with the migration; there is no implicit backward compatibility.
- Update `README.md` or `docs/guide.md` in the same change when you alter what they describe. The README Quick Start is `Example` in `middleware/example_test.go`: change both together.

## Releasing

Give the top CHANGELOG section its version and date (`## vX.Y.Z - YYYY-MM-DD`), update the version in the README install line, then tag:

```bash
VERSION=v1.5.0
git tag "${VERSION}" && git push origin "${VERSION}"
```
