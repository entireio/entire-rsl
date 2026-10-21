# Decisions

Non-obvious decisions, newest last. Each entry records the context, the
decision, and the alternatives rejected. Sources are cited at the version
pinned in `go.mod` (gittuf fork `github.com/pjbgf/gittuf` at `a818fabe`) or at
entireio/cli `main` as of 2026-10-19 (`21836151e`).

## 2026-10-21: Toolchain versions

- **Context:** SPEC.md section 6 names Go 1.27.3 and golangci-lint 2.15.0 but
  also says to use whatever entireio/cli `main` pins, which is Go 1.27.1 and
  golangci-lint 2.13.2.
- **Decision:** Go 1.27.3 and golangci-lint 2.15.0, the latest releases, per
  Phase 0 answer 14.
- **Rejected:** matching entireio/cli exactly. It lags the latest patch release.

## 2026-10-21: gittuf from the pjbgf fork

- **Context:** upstream gittuf v0.17.0 gates `VerifyRefFromEntry` behind
  developer mode (`experimental/gittuf/verify.go:86`, `GITTUF_DEV=1`), and the
  Entire CLI's environment filter would drop that variable anyway. Its go-git
  storage backend is also developer-mode only
  (`experimental/gittuf/storer.go:83`).
- **Decision:** `replace github.com/gittuf/gittuf => github.com/pjbgf/gittuf`
  at `a818fabe` (branch `inspect-api`), per Phase 0 answer 5. Its `pkg/policy`
  exposes `VerifyRef`, `VerifyRefFromEntry` and `InspectRoot` over any
  `gitstore.Storer`, with no developer-mode gate.
- **Rejected:** setting `GITTUF_DEV=1` in-process (it unlocks features gittuf
  documents as able to undermine security), and upstream v0.17.0 alone.
- **Follow-up:** the branch is 11 commits behind v0.17.0 and lacks c96b2a72
  ("policy: Improve validation of policies"). Move the pin once the branch is
  rebased or merged upstream.

## 2026-10-21: Process-wide debug logger is injected

- **Context:** gittuf logs through the default `slog` logger, so surfacing its
  diagnostics in debug mode means replacing the process default. That is global
  state, which parallel tests would race on.
- **Decision:** `cli.Options.SetDefaultLogger` is the only path to
  `slog.SetDefault`. `main` passes `slog.SetDefault`, tests pass nil.
- **Rejected:** calling `slog.SetDefault` inside the command tree.

## 2026-10-21: go mod tidy runs with -e

- **Context:** `go mod tidy` fails on a test of a transitive dependency:
  `tink-go-gcpkms/v2/integration/gcpkms` tests import
  `google.golang.org/genproto/googleapis/cloud/kms/v1`, which genproto dropped
  after 2026-06-22, while gittuf requires genproto 2026-09-28. The gittuf fork
  fails `go mod tidy` the same way. No package entire-rsl builds imports it.
- **Decision:** the tidy check (`mise-tasks/lint/gomod`) and the goreleaser hook
  run `go mod tidy -e`, and the check still fails if tidying changes go.mod or
  go.sum.
- **Rejected:** excluding genproto versions (gittuf's requirement sets the
  floor, so no older version can be selected), and dropping the tidy check.

## 2026-10-21: A read-only go-git store of our own

- **Context:** Phase 0 asks for a go-git backed store. gittuf's own go-git
  backend (`internal/gogitstore`) is internal, delegates several reads to the
  git binary, and is reachable only in developer mode
  (`experimental/gittuf/storer.go:83`).
- **Decision:** `internal/store` implements `gitstore.Storer` over go-git
  entirely in-process. Object reads follow `internal/gogitstore`, and the
  remaining reads (`GetCommitsBetweenRange`, `GetFilePathsChangedByCommit`,
  `KnowsCommit`, `LookupConfig`) mirror `pkg/gitinterface` (`log.go:14`,
  `changes.go:17`, `commit.go:239`, `config.go:20`). Every write method,
  including `GetMergeTree`, returns `ErrReadOnly`, which makes the read-only
  guarantee structural for anything gittuf does through the store.
  `GetMergeTree` is only used by `VerifyMergeable`
  (`internal/policy/verify.go:210`), never by `VerifyRef`.
- **Verification:** differential tests run every read, plus `VerifyRef` and
  `VerifyRefFromEntry`, against `gitinterface.Repository` on the fixture
  repositories and require identical results.
- **Hardening:** objects are size-checked before decoding (1 MiB commits and
  tags, 16 MiB trees and blobs). `GetReference` accepts only `HEAD` and full
  `refs/` names. gitinterface would pass revision expressions to `rev-parse`.
  `LookupConfig` reads the repository's own config only.
- **Rejected:** `gitinterface.Repository` (shells out to git for every read)
  and enabling gittuf's backend through developer mode.

## 2026-10-21: Fixtures isolate git config in TestMain

- **Context:** fixtures are written by gittuf's git binary backed store, which
  inherits the process environment, so a developer's `commit.gpgsign` or other
  global config would change fixture commits.
- **Decision:** test binaries that build fixtures call
  `testutil.IsolateGitConfig` from `TestMain`, setting `GIT_CONFIG_GLOBAL` to
  the null device and `GIT_CONFIG_NOSYSTEM=1` before any test runs. Tests stay
  parallel because nothing changes the environment afterwards.
- **Rejected:** `t.Setenv`, which forbids `t.Parallel()`.

## 2026-10-21: entire-rsl checks SSH signatures before naming a signer

- **Context:** the key embedded in an SSH signature is chosen by whoever made
  the signature, so naming it unchecked would let a forged entry display as
  signed by an Entire cluster in `log` and `entry`.
- **Decision:** `internal/rslread` names a key only after `sshsig.Verify`
  over the entry's signed payload, with namespace `git` and SHA-512, exactly as
  gittuf's SSH verifier does (`internal/signerverifier/ssh/ssh.go:38`).
  Signatures with more than one armored block are refused, as
  `pkg/gitinterface/signature.go` requires of verifiers. GPG and x509
  signatures are reported by method only. A valid signature proves who signed,
  not that they may: authorization stays with gittuf verification.
- **Rejected:** showing the embedded key unchecked, and reimplementing
  gittuf's policy evaluation to decide authorization in `log`.

## 2026-10-21: Cluster names come from a built-in table

- **Context:** Phase 0 answer 2 asks for the cluster name next to the signer,
  best effort and built into the CLI. Entire publishes no unauthenticated
  endpoint for signer keys.
- **Decision:** `internal/clusters` maps the public RSL signer and fleet root
  fingerprints in entiredb `core/cluster/fixtures/clusters.{prod,staging}.yaml`
  (e64a4f389a) to cluster names. A test derives each fingerprint from the
  fixture's public key. The table only labels keys and never affects trust.
- **Rejected:** fetching keys at run time, which needs network access and an
  authenticated admin route (`GET /api/admin/repos/{repoID}/trust`).

## 2026-10-21: RSL walks start at the tip and build entries lazily

- **Context:** an annotation is newer than the entries it refers to, and gittuf
  parses any commit that reads as an entry, reachable or not.
- **Decision:** every walk starts at the RSL tip, so an entry is only read when
  reachable and all its annotations have been seen first. `entry <id>` walks to
  the entry rather than parsing the named commit directly. Walks parse each
  entry, and only entries a command returns are completed with commit
  metadata and a signature check. Walks stop on context cancellation and after
  five million entries.
- **Rejected:** `rsl.GetEntry` on the named ID, which would accept commits that
  are not in the log.

## 2026-10-21: Checkpoints resolve through the git-refs backend only

- **Context:** Phase 0 answer 3 limits checkpoints to the git-refs backend.
  entireio/cli names checkpoint refs `refs/entire/checkpoints/<shard>/<id>`,
  the shard being the ID's last two characters
  (`cmd/entire/cli/checkpoint/refs_naming.go:36`, `id/id.go:134`), and accepts
  a shard whose case differs because case-insensitive file systems may fold a
  ULID's shard into an existing legacy one (`refs_naming.go:58`).
- **Decision:** IDs are 12 lowercase hex characters or a canonical ULID,
  matching `ulid.ParseStrict` plus the canonical round trip in `id/id.go:84`
  without a ULID dependency. The RSL is read under the canonical ref, which is
  what Entire's server records. The local ref falls back to a
  case-insensitive shard match. A checkpoint with neither a local ref nor RSL
  entries is an error.
- **Trailer:** a commit ID resolves through `Entire-Checkpoint` trailer lines.
  The match is anchored to whole lines, stricter than entireio/cli's
  `CheckpointTrailerRegex` (`trailers/trailers.go:54`), so an ID quoted in the
  body does not count. Repeated identical IDs count once, distinct IDs are an
  error listing them.
- **Rejected:** the legacy `entire/checkpoints/v1` branch, per the answer.

## 2026-10-21: Four verification states and how they are decided

- **Context:** gittuf's verifier passes a ref no rule covers without checking
  anything ("This target is not protected by gittuf policy",
  `internal/policy/verify.go:1110` at a818fabe), and its `ErrPolicyNotFound`
  is internal. Full verification is `VerifyRelativeForRef` from the ref's
  first entry, which `pkg/policy.VerifyRefFromEntry` exposes over a store.
- **Decision:** `internal/verify` decides in this order. No RSL, or no applied
  policy (gittuf's own `HasPolicy` query), is `unprotected`. An incomplete
  local RSL (unreadable entry or shallow boundary) is `error`, checked first
  because every gittuf walk reads a gap as the start of history and could
  otherwise hide the policy or the ref's early entries. No RSL entries for the
  ref is `unprotected` (Phase 0 answer 19). No covering rule is `unprotected`.
  Coverage is gittuf's own: top-level rules from `ListRules` (allow rule
  excluded) and global rules from `ListGlobalRules`, matched with each rule's
  `Matches`, mirroring `findVerifiersForPathIfProtected`. Then
  `VerifyRefFromEntry` from the first entry: no error is `verified`. Context
  errors, store read errors, `ErrReadOnly`, a missing local ref and panics are
  `error`, any other gittuf error is `failed`. Every outcome other than
  `verified` exits non-zero in `verify`.
- **Coverage uses the git binary:** `ListRules` exists only on
  `experimental/gittuf.Repository`, which is tied to gittuf's git binary
  backend. Reading the rules is the only place entire-rsl relies on it, so
  `main` hardens the process environment first (`internal/gitcli`): repository
  selectors removed, `core.fsmonitor` and `core.hooksPath` neutralised through
  `GIT_CONFIG_COUNT`. Follow-up: expose coverage over a `gitstore.Storer` in
  the fork's `pkg/policy`.
- **Local drift:** superseded by the checks entry below, which fails
  verification on drift.
- **Known gittuf limitation:** when a ref's very first entry is a violation,
  gittuf cannot recover even if it is skipped and fixed, because it has no last
  good state (`TODO: what if the very first entry for a ref is a violation?`).
  Such refs report `failed`.
- **Rejected:** treating all gittuf errors as `failed` or all as `error`, and
  `--latest-only` (Phase 0 answer 7).

## 2026-10-21: Local installs use the binary, not the archive

- **Context:** SPEC.md section 4's acceptance test installs
  `./dist/<archive>`. `entire plugin install <path>` refuses a non-executable
  source ("is not executable (chmod +x)"). Archives are only for remote
  installs, which download them and check `checksums.txt`
  (external-commands.md, "Remote install").
- **Decision:** the acceptance test (`cmd/entire-rsl/acceptance_test.go`)
  builds the binary, installs it with `entire plugin install` into a temporary
  `ENTIRE_PLUGIN_DIR`, and compares `entire rsl ...` with the binary run
  directly, output and exit code. It skips when `entire` is not on PATH. The
  snapshot's `dist/entire-rsl_linux_amd64_v1/entire-rsl` was installed the
  same way by hand. The archive layout (one `entire-rsl` binary per archive,
  goreleaser naming, `checksums.txt`) is what remote installs select.

## 2026-10-21: agent-help mirrors the Entire CLI's shape

- **Context:** entire delegates `entire agent-help rsl [path...] [--json]` to
  `entire-rsl agent-help [path...] [--json]` (external-commands.md, "Agent
  help").
- **Decision:** the JSON uses entireio/cli's `agentHelpJSON` field names
  (`cmd/entire/cli/agent_help_cmd.go:652`): `command`, `short`, `long`,
  `guidance`, `flags`, `subcommands` with `audience`. Every command is
  `read-only`. Guidance for agents lives in its own table, not in cobra's help,
  as entireio/cli requires.

## 2026-10-21: Fetching a missing RSL creates local gittuf refs

- **Context:** Phase 0 answer 6 asks entire-rsl to offer a fetch when the RSL
  is missing, asking first and asking for a remote when there are several.
  SPEC.md section 5 allows writes only to remote-tracking refs. Coverage is
  read through `experimental/gittuf.Repository`, which reads `refs/gittuf/*`
  with the git binary and cannot be pointed at tracking refs, so a
  tracking-only fetch would leave `verify` reporting `error`. Asked on
  2026-10-21: the user chose to create local gittuf refs.
- **Decision:** only when the repository has no local RSL, and only with
  consent (`--fetch[=<remote>]`, or a yes or a remote picked at a prompt on a
  terminal), `internal/gitcli` fetches `+refs/gittuf/*` into gittuf's own
  remote-tracking layout `refs/remotes/<remote>/gittuf/*` (`pkg/rsl`
  `remoteTrackerRef`), then creates each missing local `refs/gittuf/*` ref
  from its tracking ref in one `git update-ref --stdin` transaction of
  `create` commands, which fails if a ref appeared meanwhile. Existing local
  refs are never changed (review 0003). The remote must be a configured remote
  name, so no URL reaches git from the command line.
- **Hardening:** git is resolved to an absolute path once; arguments are a
  slice with `--` before the remote; `core.fsmonitor`, `core.hooksPath` and
  the `ext` transport are disabled; `--refmap=`, `--no-prune` and
  `--no-prune-tags` stop configured mappings and pruning from widening the
  write; `--no-write-fetch-head`, `--no-auto-maintenance`,
  `--no-recurse-submodules` and `fetch.writeCommitGraph=false` keep the fetch
  from writing anything else; the fetch is refused when the repository's own
  config names a program git would run (`core.sshCommand`,
  `remote.<name>.uploadpack`, credential helpers, includes); output is
  captured and URL credentials redacted; cancellation interrupts git with a
  five second grace.
- **Non-interactive:** without a terminal the command fails with how to
  fetch (`--fetch`, or `--fetch=<remote>` when there are several). Prompts are
  plain lines on stderr, so they work with screen readers like the Entire
  CLI's accessible mode.
- **Rejected:** tracking refs only (breaks verification of a fetched RSL),
  and `huh` forms (a new dependency for two prompts).

## 2026-10-21: Verification runs tip, root of trust and signer checks

- **Context:** the user asked, during Phase 1, for verification to check that
  the server tip matches the local ref, the root of trust matches a known
  value, and an Entire cluster signer matches its known key and name.
- **Decision:** after gittuf's verification, `internal/verify` runs three
  checks and turns `verified` into `failed` when one fails:
  - `tip`: the target of the ref's latest RSL entry equals the local ref, as
    gittuf's own `VerifyRef` requires (`experimental/gittuf/verify.go:210`).
    Skipped when the ref does not exist locally, since there is nothing to
    compare.
  - `root_of_trust`: `pkg/policy.InspectRoot` with `WithInitialRootKeys`
    proves the policy history starts from a known root and every transition
    verifies. gittuf requires every initial key it is given to have signed
    the first root (`internal/policy/policy.go:301`, threshold = number of
    keys), so each known root is tried alone. The current root keys must all
    be known too. Known roots are Entire's prod and staging fleet roots, plus
    `--root-key` ed25519 keys for self-managed gittuf repositories.
  - `signer`: for an Entire root, the latest entry's checked SSH signer must
    be a known Entire cluster key of the same environment, named in the
    result. Skipped for user-supplied roots.
- **Rejected:** reporting drift only as a warning (the user asked for it as a
  check), and accepting unknown roots silently, which would let any
  self-rooted RSL verify as if Entire had signed it.
