# Contributing to entire-rsl

Thank you for your interest in contributing! Please read our [Code of Conduct](CODE_OF_CONDUCT.md) before participating.

## Before You Code: Discuss First

Open an issue describing the problem or feature and wait for maintainer feedback before starting implementation. For security issues, follow [SECURITY.md](SECURITY.md) instead.

## Development

Tooling is pinned in `mise.toml`.

```sh
mise install        # Go, golangci-lint, goreleaser, govulncheck, go-licenses
mise run fmt        # gofmt
mise run lint       # golangci-lint and repository checks
mise run test       # unit tests
mise run check      # fmt + lint + tests with -race; must pass before every commit
mise run build      # ./bin/entire-rsl
mise run snapshot   # local goreleaser snapshot into ./dist
```

Read [CLAUDE.md](CLAUDE.md) for the repository rules: the read-only guarantee, how verification states are decided, and test conventions. Non-obvious design decisions are recorded in [docs/decisions.md](docs/decisions.md).

## Pull Requests

- One logical change per commit, with a message in the `area: summary` style.
- Every commit carries a `Signed-off-by` trailer (DCO).
- New behaviour comes with tests. Output changes update the golden files under `testdata/` and, for `--json`, [docs/json-schema.md](docs/json-schema.md).
