# entire-rsl — Agent Build Spec

2026-10-21 · @Paulo Gomes

## 1. Role and operating mode

You are the lead engineer and orchestrator for `entire-rsl`, a read-only Go CLI that the Entire CLI dispatches as the external command `entire rsl`. It lets a user inspect a repository's gittuf Reference State Log (RSL) for a branch, tag or Entire checkpoint: who pushed it, when, and whether gittuf verification passes for that ref.

You work in exactly two phases:

1. **Phase 0 — questions.** Read this spec, then the referenced sources (entireio/cli and gittuf). Send the user ONE message containing every question in section 2, each with its default. Stop and wait. Do not scaffold, commit or install anything before the answers arrive.
2. **Phase 1 — autonomous build.** After the answers, build to the definition of done in section 8 without further check-ins. A reply of “defaults” accepts every default.

Rules for Phase 1:

- Ask again only for a hard blocker: a destructive action, a missing credential, or a contradiction between the answers and this spec. Everything else is a decision you make and record.
- Record each non-obvious decision in `docs/decisions.md` as a dated entry: context, decision, alternatives rejected.
- Precedence when sources disagree: the user’s Phase 0 answers, then this spec, then entireio/cli conventions (`CLAUDE.md`, `docs/development/*`, `docs/architecture/external-commands.md`), then Go community defaults.
- Never guess how `entire` or gittuf behaves. Read their source at the pinned version and cite the file in your decision log.
- You may delegate bounded tasks (tests, fixtures, docs) to sub-agents. You own integration, every commit, and every review cycle.
- Finish with a short report: what shipped, decisions taken, rejected review findings, and open follow-ups.

## 2. Phase 0 questions

Ask all of these in one message, numbered, each with its default. Add any question your source reading raises; never drop one from this list.

| # | Question | Default if unanswered |
| --- | --- | --- |
| 1 | Module path, repo location, and visibility? | `github.com/entireio/entire-rsl`, private until first release |
| 2 | What is the source of truth for “who pushed”? Which RSL custom-field keys does entire-server stamp (namespace, names, value format)? | Show the verified signer of the RSL entry as authoritative; show `custom.*` pusher fields labelled “advisory, unverified” |
| 3 | Are checkpoint refs recorded in the RSL today? For which backend: `git-refs` (`refs/entire/checkpoints/<shard>/<id>`), legacy `entire/checkpoints/v1`, or both? | Support both; `git-refs` first, then fall back to the v1 branch, matching entire’s own read routing |
| 4 | Should `checkpoint` also accept a commit SHA and resolve it through the `Entire-Checkpoint` trailer? | Yes; zero or several trailers is an error that names what was found |
| 5 | Import gittuf as a Go library, or shell out to the `gittuf` binary? | Library, pinned to `v0.17.0`, using `pkg/rsl` and `experimental/gittuf` |
| 6 | May the tool fetch the RSL from a remote? | Local only by default; `--fetch[=<remote>]` opt-in that updates remote-tracking refs only |
| 7 | Verification depth and the no-policy case? | Full `VerifyRef` by default, `--latest-only` opt-in; a repo with no gittuf policy reports `unprotected`, never `verified` |
| 8 | Is a dedicated exit code for verification failure acceptable? | Yes: 0 ok, 1 error, 2 usage, 3 verification failed or unprotected |
| 9 | Release scope: goreleaser, Homebrew tap, plugin-index entry, `officialPlugins` allowlist PR to entireio/cli? | goreleaser + `checksums.txt` + `entire-plugin.yml`; draft the index and allowlist PRs as patch files, do not open them |
| 10 | May the agent push branches or open PRs? | No. Local commits only |
| 11 | Platforms? | linux, darwin, windows on amd64 and arm64, `CGO_ENABLED=0`, matching entire |
| 12 | Codex: installed, authenticated, and which model or profile? | Use the local `codex` config as is; fail Phase 1 start if `codex --version` fails |
| 13 | Any interactive TUI in v1? | No. Text and `--json` only |

## 3. Scope and command surface

The tool reads `refs/gittuf/reference-state-log` and gittuf policy; it never writes refs, RSL entries, objects or git config. Every command works without a TTY and supports `--json`.

| Command | Input | Output |
| --- | --- | --- |
| `entire rsl branch <name>` | short name or `refs/heads/...` | RSL tip vs local tip, latest entry (pusher, time, signer), recent history, verification status |
| `entire rsl tag <name>` | short name or `refs/tags/...` | Same as `branch`, plus a warning when a tag has more than one non-skipped RSL entry (tag moved) |
| `entire rsl checkpoint <id\|sha>` | 12-hex or 26-char ULID checkpoint ID, or full commit SHA | Resolved ref, entries that wrote it, verification status |
| `entire rsl log` | `--ref`, `--limit` (default 50), `--before <entry>` | RSL entries newest first: number, entry ID, type, ref, target, time, signer, skipped |
| `entire rsl entry <id\|number>` | entry hash or entry number | One entry in full, including annotations and custom fields |
| `entire rsl verify <target>` | branch, tag, full ref or checkpoint | Verification result only; exit code carries it |
| `entire rsl agent-help [path...]` | optional `--json` | Agent documentation, per the entire external-command contract |
| `entire rsl version` | none | Version, commit, Go version |

Behaviour that applies to every detail command:

- Resolve short names the way git does. An ambiguous name is an error listing the candidates; never pick one.
- Run verification by default; `--no-verify` skips it. Report one of four states: `verified`, `failed`, `unprotected` (no policy covers the ref), `error`. Only `verified` maps to exit 0 in `verify`.
- Show “when” as the RSL entry’s commit time, labelled as asserted by its signer. Show local-ref drift (local tip differs from the latest RSL target) as a warning.
- Show annotations that skip an entry next to the entry they skip.
- `--json` output carries `schema_version: 1`. Document the schema in `docs/json-schema.md` and pin it with golden tests.

Out of scope for v1: recording or annotating RSL entries, policy editing, key management, network calls other than `--fetch`, and any TUI.

## 4. Integration contract with entire

`entire` resolves `entire rsl ...` lazily with `exec.LookPath("entire-rsl")` when `rsl` is not a built-in, then execs the binary with the remaining arguments verbatim. Build to that contract exactly, as specified in [external-commands.md](https://github.com/entireio/cli/blob/main/docs/architecture/external-commands.md).

- **Binary name** is `entire-rsl` (`entire-rsl.exe` on Windows). The release archive contains exactly one binary with that name.
- **Arguments** arrive unparsed by entire, including `--help`. Cobra in `entire-rsl` owns all parsing.
- **Stdio and exit codes** pass through. stdout carries results only; diagnostics and progress go to stderr.
- **Signals:** entire sends SIGINT with a 5-second grace before SIGKILL. Cancel the root context on SIGINT and exit cleanly.
- **Environment** is filtered by an allowlist. Rely only on `ENTIRE_CLI_VERSION`, `ENTIRE_REPO_ROOT` (absent outside a repo), `ENTIRE_PLUGIN_DATA_DIR` (not pre-created), plus POSIX, locale and terminal variables. Do not require any credential variable.
- **Working directory** is the user’s CWD, unchanged. Prefer `ENTIRE_REPO_ROOT`; fall back to git discovery from CWD so the binary also runs standalone.
- **agent-help** is a verb with the shape `agent-help [path...] [--json]`. entire delegates `entire agent-help rsl` to it.
- **Settings:** do not add keys to `.entire/settings.json`; it decodes with `DisallowUnknownFields`. v1 needs no config file.
- **Release assets** follow goreleaser naming (`entire-rsl_<version>_<os>_<arch>.tar.gz`, `.zip` on Windows) with `checksums.txt`. entire refuses unverified installs without `--allow-unverified`.
- **entire-plugin.yml** at the repo root declares `name: rsl`. Do not add `requires` in v1.
- **Telemetry:** none in the plugin. entire emits `cli_plugin_executed` only for names in `officialPlugins`.

Acceptance test: `entire plugin install ./dist/<archive>` followed by `entire rsl branch main` in a fixture repo produces the same output as running `entire-rsl branch main` directly.

## 5. Security requirements

Treat every byte read from the repository as attacker-controlled. The tool’s output is a security statement, so it must fail closed and never overstate what was proven.

**Trust model**

- Verification is gittuf’s `VerifyRef` / `VerifyRefFromEntry`, nothing else. Never reimplement policy evaluation.
- Any error during verification yields `error`, never `verified`. Partial results are not reported as success.
- RSL custom fields are advisory. gittuf’s own [pkg/customfields](https://github.com/gittuf/gittuf/blob/main/pkg/customfields/customfields.go) states they must not drive security decisions. Render them under an “unverified” label and keep them out of every exit-code decision.
- The verified identity of an entry is its signing key, mapped to a principal through gittuf policy. Committer name and email are display-only and labelled unverified.

**Read-only enforcement**

- No code path writes refs, objects, config or the RSL. Add a source-guard test that fails if write APIs (`RecordRSLEntry*`, `Commit*`, `SetReference`, `update-ref`, `push`) appear outside an allowlist, which is empty except for `--fetch`.
- `--fetch` writes only remote-tracking gittuf refs and only when the flag is passed.

**Input and output handling**

- Validate ref names against `git check-ref-format` rules and reject any leading `-`. Validate checkpoint IDs as exactly 12 hex or 26 Crockford base32 characters.
- Strip C0/C1 control characters, ANSI escapes and Unicode bidi overrides from every repo-sourced string in text output. JSON output uses standard escaping.
- Bound all walks: `--limit` caps entries, a context deadline caps verification, and fixed caps limit field sizes read into memory.
- Fuzz the ref-name, checkpoint-ID and entry-ID parsers with native Go fuzzing; keep seed corpora in `testdata/`.

**Subprocess and filesystem**

- Resolve `git` to an absolute path once. Pass arguments as a slice, put `--` before positionals, and never invoke a shell.
- Strip inherited `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE` and similar selectors when targeting `ENTIRE_REPO_ROOT`. Neutralise repo-config execution vectors (`core.fsmonitor`, `core.hooksPath`) with `-c` overrides.
- Pass `--no-optional-locks` to every `git status`. Write nothing outside `ENTIRE_PLUGIN_DATA_DIR`, and refuse symlinks there.
- Redact URL userinfo in every error, log line and output.

**Supply chain**

- Pin every dependency in `go.sum`, every GitHub Action by commit SHA, and workflow `permissions: contents: read`.
- Run `govulncheck` in CI and daily, CodeQL, `gosec` through golangci-lint, and the license check against `.allowed-licenses`.
- Build with `-trimpath`, `CGO_ENABLED=0`, and `-s -w`. Release `checksums.txt`; match entire’s signing and notarisation setup for darwin.
- Ship `SECURITY.md` adapted from entireio/cli, with the same reporting channel.

## 6. Tooling, license and repo conventions

Copy entireio/cli’s choices unless a row below says otherwise. Values were read from entireio/cli `main` on 2026-10-21; re-read them at Phase 1 start and use whatever `main` pins then.

| Area | entire-rsl uses | Source in entireio/cli |
| --- | --- | --- |
| Go | `go 1.27.3` in `go.mod`, same in `mise.toml` (latest stable today) | `go.mod`, `mise.toml` |
| License | MIT, “Copyright (c) 2026 Entire Inc.” | `LICENSE` |
| Dependency licenses | Same `.allowed-licenses` list; gittuf is Apache-2.0, which is allowed | `.allowed-licenses`, `license-check.yml` |
| CLI framework | `github.com/spf13/cobra` v1.10.2; `charm.land/lipgloss/v2` only if styling is needed | `go.mod` |
| Tests | `testify`, `gotestsum`, `t.Parallel()` everywhere, isolated temp repos | `CLAUDE.md` Test safety |
| Lint | golangci-lint 2.15.0 with `.golangci.yaml` copied, entire-specific `forbidigo` rules adapted | `.golangci.yaml` |
| Task runner | mise tasks `fmt`, `lint`, `test`, `test:ci`, `check` | `mise.toml`, `mise-tasks/` |
| Vulnerabilities | `govulncheck` v1.9.0, PR + daily cron | `govulncheck.yml` |
| Release | goreleaser, `checksums.txt`, ldflags `-s -w` + version/commit stamping | `.goreleaser.yaml` |
| CI | `ci.yml`, `lint.yml`, `codeql.yml`, `govulncheck.yml`, `license-check.yml`; Actions pinned by SHA | `.github/workflows/` |
| Agent docs | `CLAUDE.md` as the entry point, `AGENTS.md` symlinked to it | repo root |
| Community files | `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `CHANGELOG.md` | repo root |

Repository layout:

- `cmd/entire-rsl/main.go` — entry point; prints returned errors, maps exit codes.
- `cmd/entire-rsl/cli/` — Cobra commands, one file per verb (`rsl_branch.go`, `rsl_verify.go`), matching entire’s `<noun>_<verb>.go` naming.
- `internal/rslread/` — RSL walking, entry rendering model, annotations.
- `internal/refresolve/` — branch, tag and checkpoint resolution.
- `internal/verify/` — thin wrapper over gittuf verification returning the four states.
- `internal/sanitize/` — terminal-safe rendering of untrusted strings.
- `internal/testutil/` — fixture repos with a gittuf root, policy and signed RSL entries.

Go practices: accept `context.Context` first on every I/O function; wrap errors with `%w` and test with `errors.Is`; no `init()`; no global mutable state; operational logs through `log/slog` to stderr, never logging file contents or commit messages; user output only through `cmd.OutOrStdout()` and `cmd.ErrOrStderr()`.

## 7. Commit and codex review workflow

Every commit is unsigned, and every 3–4 feature commits get a codex review followed by one fix commit.

**Commits**

- Set repo-local config once: `git config commit.gpgsign false` and `git config tag.gpgsign false`. Also commit with `git -c commit.gpgsign=false commit ...`. Never touch global config.
- `mise run check` passes before every commit. A failing check is fixed, not skipped with `--no-verify`.
- One logical change per commit. Match the message style in entireio/cli’s `git log`.

**Review cycle**

1. Count feature commits since the last review base. At 3, review if the next change is unrelated; review at 4 regardless.
2. Check the codex CLI’s current help for a non-interactive review command and use it on the range `<review-base>..HEAD`. If none exists, use `codex exec` in a read-only sandbox with the diff and the prompt below.
3. Triage every finding as fix or reject. Fix all accepted findings in one commit titled `Address codex review of <base>..<head>`. Run `mise run check` before it.
4. Record each rejected finding with a one-line reason in `docs/reviews/NNNN.md`, committed with the fix commit.
5. Move the review base to HEAD. The fix commit does not count toward the next cycle. If codex finds nothing, record that and add no fix commit.
6. Run one final review over all commits since the last base before declaring done, whatever the count.

Review prompt to give codex:

> Review this diff of entire-rsl, a read-only Go CLI that inspects gittuf RSL data and is dispatched by the Entire CLI. Prioritise: (1) security — untrusted repo data, fail-open verification, terminal injection, subprocess argument injection, unintended writes; (2) correctness against gittuf’s RSL and verification semantics; (3) Go idioms, error handling, context use, test isolation; (4) drift from the spec in SPEC.md. Report each finding with file, line, severity, and a concrete fix. Do not report style issues that golangci-lint already enforces.

Commit this spec into the repository as `SPEC.md` in the first commit so codex can read it.

## 8. Milestones, definition of done, references

Build in this order. Each milestone ends green on `mise run check`; review cycles run on commit count, not milestone boundaries.

1. **Scaffold** — `SPEC.md`, license, community files, `CLAUDE.md`/`AGENTS.md`, `go.mod`, `mise.toml`, `.golangci.yaml`, CI workflows, goreleaser, Cobra root, `version`, exit-code mapping, signal handling.
2. **Fixtures** — `internal/testutil` builds repos with a gittuf root of trust, policy, signed RSL entries, annotations and a moved tag.
3. **RSL reading** — entry walking, annotations, sanitisation; `log` and `entry` commands with golden text and JSON output.
4. **Branches and tags** — ref resolution, local-drift warning, moved-tag warning; `branch` and `tag` commands.
5. **Checkpoints** — ID validation, `git-refs` then v1 fallback, `Entire-Checkpoint` trailer lookup; `checkpoint` command.
6. **Verification** — four-state wrapper over gittuf, `--latest-only`, `--no-verify`, `verify` command with exit codes.
7. **Integration** — `agent-help`, `entire-plugin.yml`, `goreleaser release --snapshot --clean`, install through `entire plugin install` and run through `entire rsl`.
8. **Hardening** — fuzz targets, benchmarks on a 100k-entry RSL, read-only source guard, final codex review.

Definition of done:

- [ ] Every command in section 3 works in text and `--json`, with golden tests
- [ ] Verification never reports `verified` on error, missing policy or partial evaluation; tests prove each case
- [ ] Untrusted strings containing ANSI, control and bidi characters render inert; tests prove it
- [ ] Read-only source guard passes; no write path exists outside `--fetch`
- [ ] `mise run check`, govulncheck, CodeQL and license check are green
- [ ] Snapshot release installs via `entire plugin install` and runs as `entire rsl`
- [ ] Every commit unsigned; a codex review plus fix commit after every 3–4 feature commits; final review done
- [ ] `docs/decisions.md`, `docs/reviews/`, `docs/json-schema.md` and README usage are complete
- [ ] Final report delivered: shipped scope, decisions, rejected findings, follow-ups

References (read in Phase 0):

- [entireio/cli CLAUDE.md](https://github.com/entireio/cli/blob/main/CLAUDE.md) — repo rules, test safety, git and subprocess safety
- [External commands](https://github.com/entireio/cli/blob/main/docs/architecture/external-commands.md) — dispatch, environment, install and release contract
- [CLI conventions](https://github.com/entireio/cli/blob/main/docs/development/cli-conventions.md) — command layout and output rules
- [Ref checkpoint backend](https://github.com/entireio/cli/blob/main/docs/architecture/ref-checkpoint-backend.md) — `refs/entire/checkpoints/<shard>/<id>` layout and ID formats
- [gittuf ](https://github.com/gittuf/gittuf/tree/main/pkg/rsl)[pkg/rsl](https://github.com/gittuf/gittuf/tree/main/pkg/rsl) — entry types and walking functions
- [gittuf ](https://github.com/gittuf/gittuf/blob/main/experimental/gittuf/verify.go)[experimental/gittuf/verify.go](https://github.com/gittuf/gittuf/blob/main/experimental/gittuf/verify.go) — `VerifyRef`, `VerifyRefFromEntry`
- [gittuf ](https://github.com/gittuf/gittuf/blob/main/pkg/customfields/customfields.go)[pkg/customfields](https://github.com/gittuf/gittuf/blob/main/pkg/customfields/customfields.go) — custom-field trust contract

## 9. Phase 0 answers (2026-10-21)

These answers take precedence over every section above.

| # | Answer |
| --- | --- |
| 1 | `github.com/entireio/entire-rsl`, private until first release. |
| 2 | Verifying the RSL signature is the main thing that can produce a non-zero exit. After it, show the metadata available: the signer key with the cluster name in parentheses (cluster name is best effort, baked into the CLI); actors translated to their meaning; the pusher as the user Entire observed pushing; `force-push` and `operation` when present. |
| 3 | Only the `git-refs` backend (`refs/entire/checkpoints/<shard>/<id>`). A checkpoint that is not there is reported as not found. No v1 branch fallback. |
| 4 | Yes. `checkpoint` accepts a commit SHA and resolves it through the `Entire-Checkpoint` trailer. Zero or several trailers is an error naming what was found. |
| 5 | Pin the gittuf fork (`github.com/pjbgf/gittuf`, branch `inspect-api`) through a `replace` until it is merged upstream. |
| 6 | When the RSL is not found locally, offer to fetch it from a remote, asking first. With several remotes, ask the user to pick one. |
| 7 | Full verification only. No `--latest-only` flag in v1. A repo with no gittuf policy reports `unprotected`, never `verified`. |
| 8 | Exit codes: 0 ok, 1 error, 2 usage, 3 verification failed or unprotected. |
| 9 | goreleaser with `checksums.txt` and `entire-plugin.yml`. No darwin signing or notarisation for now. No Homebrew. Plugin-index and `officialPlugins` changes drafted as patch files only. |
| 10 | Everything stays local. No pushes, no PRs. |
| 11 | linux, darwin and windows on amd64 and arm64, `CGO_ENABLED=0`, with tests to match. |
| 12 | Use the local codex config as is. |
| 13 | No TUI. Interaction stays in line with the Entire CLI (prompts only on a TTY, with a non-interactive fallback). |
| 14 | Use the latest Go and golangci-lint releases. |
| 15 | Commits are unsigned (no GPG) and made autonomously, carrying `Assisted-by` and `Signed-off-by` trailers with author `paulo@entire.io`. |
| 16 | Commit on `main`. |
| 17 | Accept the gittuf dependency tree and its binary size. |
| 18 | `ENTIRE_REPO_ROOT` wins over CWD discovery. Log the difference at debug level. |
| 19 | A ref with no RSL entries reports `no RSL entries` with a hint that coverage may exclude it. `verify` exits 3. |
| 20 | Reviews cover this repository only, not the gittuf fork. |

Additional requirements from the answers:

- Read the repository through a go-git backed store.
- Debug mode prints additional diagnostics, including the gittuf version in use.
