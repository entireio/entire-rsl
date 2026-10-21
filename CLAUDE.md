# entire-rsl

Read-only Go CLI that the Entire CLI dispatches as `entire rsl`. It reports,
for a branch, tag or Entire checkpoint, what the gittuf Reference State Log
(RSL) records and whether gittuf verification passes. `SPEC.md` is the build
spec. Its section 9 (Phase 0 answers) overrides the rest. Decisions with their
sources are in `docs/decisions.md`.

## Commands

| When | Run |
| --- | --- |
| Before every commit | `mise run check` (fmt, lint, `go test -race`) |
| Build | `mise run build` |
| Snapshot release | `mise run snapshot` |
| Vulnerabilities | `mise run vuln` |
| Licenses | `mise run lint:licenses` |
| Fuzz | `mise run fuzz` (`FUZZTIME=30s` per target) |
| Benchmarks | see `docs/benchmarks.md` |

## Layout

- `cmd/entire-rsl/main.go`: entry point. Signals, exit code via `cli.Report`.
- `cmd/entire-rsl/cli/`: Cobra commands, one file per verb (`rsl_<verb>.go`).
- `internal/` packages: see each package's doc comment.

## Rules that are easy to break

- **Read-only.** Nothing writes refs, objects, config or the RSL. The go-git
  store returns `ErrReadOnly` from every write method, and `internal/guard`
  fails on write APIs outside its allowlist, which holds only the gittuf ref
  sync in `internal/gitcli/fetch.go`, used by `refresh` and by fetching a
  missing RSL. It never changes a local ref except by fast-forward. Do not add
  a write path.
- **Fail closed.** `verified` needs gittuf's full verification to pass and the
  tip, root of trust and signer checks to pass. Any error is `error`. A ref
  without policy, entries or a covering rule is `unprotected`. Custom fields
  never influence a state or an exit code.
- **Signers are checked before they are named.** `internal/rslread` names a
  key only after its SSH signature verifies. Never display an embedded key
  unchecked.
- **Untrusted output.** Every repository-sourced string printed as text goes
  through `internal/sanitize`. JSON uses standard escaping.
- **Colour** only marks verification outcomes, through `styles`, never
  repository data. Every cell of a coloured table column, header included,
  must be styled so tabwriter keeps the alignment.
- **Output streams.** Results on `cmd.OutOrStdout()`, diagnostics on
  `cmd.ErrOrStderr()`. Never `os.Stdout`/`os.Stderr` outside `main.go`.
- **Environment.** Only `ENTIRE_CLI_VERSION`, `ENTIRE_REPO_ROOT`,
  `ENTIRE_PLUGIN_DATA_DIR`, `ENTIRE_LOG_LEVEL` and standard variables are read,
  through `cli.Options.Getenv`.
- **git subprocesses** go through `internal/gitcli`: absolute git path, argument
  slice, `--` before positionals, repo selectors stripped, `core.fsmonitor` and
  `core.hooksPath` neutralised.

## Tests

- `t.Parallel()` everywhere, `testify`, isolated temp repositories from
  `internal/testutil`. Never touch the developer's repo or global git config.
- Golden files live in `testdata/`. Regenerate with `go test ./... -update`
  and review the diff.
- No section comments, no comments restating the next line.

## Commits

`area: summary` messages, one logical change each, unsigned (repo-local
`commit.gpgsign=false`), ending with `Assisted-by` then `Signed-off-by`.
