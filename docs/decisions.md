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
