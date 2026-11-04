# entire-rsl

`entire rsl` shows what a repository's [gittuf](https://gittuf.dev) Reference
State Log (RSL) records for a branch, tag or Entire checkpoint: who pushed it,
when, and whether gittuf verification passes. It is a plugin for the
[Entire CLI](https://github.com/entireio/cli) and also runs standalone as
`entire-rsl`.

entire-rsl is read-only, with two exceptions that only touch gittuf refs:
`entire rsl refresh`, and fetching a missing RSL when you agree to it.

## Install

From a release, once published:

```sh
entire plugin install rsl
```

From a local build:

```sh
mise run build
entire plugin install ./bin/entire-rsl
```

## Quickstart

### 1. Create a repository on Entire

Follow the [Entire quickstart](https://docs.entire.com/quickstart) to install
the Entire CLI,
[create a repository on Entire](https://docs.entire.com/quickstart#create-a-new-repository-on-entire),
clone it and
[enable Entire](https://docs.entire.com/quickstart#2-open-a-repository-and-enable-entire)
in it. Then install entire-rsl (see [Install](#install)) and run the commands
below from the repository root.

### 2. Fetch the RSL

A new repository already has a policy and a first RSL entry. `refresh` fetches
them from `origin`, or from the remote you name:

```console
$ entire rsl refresh
Fetching gittuf refs from origin...
REF                              STATUS   LOCAL  REMOTE        COMMITS
refs/gittuf/policy               created  -      6cb81fc8b792  1 commit
refs/gittuf/reference-state-log  created  -      37d1fc96740f  1 commit (entry #1)
```

### 3. Push a checkpoint

[Create a checkpoint](https://docs.entire.com/quickstart#3-create-your-first-checkpoint)
and push it. Entire pushes the checkpoint ref with the branch and records an
RSL entry for each:

```console
$ git push
[entire] Pushing 1 checkpoint ref(s) to origin... done
...
remote: RSL: entry #3 recorded
To entire://aws-eu-west-2.entire.io/et/paulo/rsl-demo
 * [new branch]      main -> main
```

Pushing does not update the local RSL, so fetch it again:

```console
$ entire rsl refresh
Fetching gittuf refs from origin...
REF                              STATUS     LOCAL         REMOTE        COMMITS
refs/gittuf/policy               unchanged  6cb81fc8b792  6cb81fc8b792  -
refs/gittuf/reference-state-log  updated    37d1fc96740f  9dec33e898e4  +2 commits (entry #1 → #3)
```

### 4. Verify the default branch

`verify` prints the state and each check, and exits 0 only when the ref is
verified:

```console
$ entire rsl verify main
Ref                  refs/heads/main
Verification         verified: gittuf verified every RSL entry for the ref against the policy, and every check passed
Verified tip         7de46d3dd2a157f83aa0163ac21eebbd6d748ee2
RSL entries checked  #3 9dec33e898e4 (1 entry)
Rules                authorize-rsl-signers
Check tip            pass: the local ref matches the tip the RSL records (7de46d3dd2a157f83aa0163ac21eebbd6d748ee2, entry #3)
Check root_of_trust  pass: anchored to Entire prod root (SHA256:Ja0D6sYQVI7aSmdIucYfwpQeV57DlvY5EE006iNQaZc), root version 1
Check signer         pass: signed by Entire aws-eu-west-2 (SHA256:aqzDRRL2FaTkQqhiQSVHbT7kBOl6xpiv6zfKzpkOPKQ)
```

`branch` adds who pushed it, when, the signer and the ref's RSL history:

```console
$ entire rsl branch main
Ref                  refs/heads/main
Local tip            7de46d3dd2a157f83aa0163ac21eebbd6d748ee2
RSL tip              7de46d3dd2a157f83aa0163ac21eebbd6d748ee2 (entry #3)
Pushed by            pjbgf, as recorded by Entire
Time                 2026-11-04T11:07:08Z (asserted by the signer)
Signer               SHA256:aqzDRRL2FaTkQqhiQSVHbT7kBOl6xpiv6zfKzpkOPKQ (Entire aws-eu-west-2), signature valid
Verification         verified: gittuf verified every RSL entry for the ref against the policy, and every check passed
...

History (1 of 1 entries, newest first)
NUMBER  ENTRY         TARGET        TIME                  BY     SIGNER                SKIPPED
3       9dec33e898e4  7de46d3dd2a1  2026-11-04T11:07:08Z  pjbgf  Entire aws-eu-west-2  -
```

Replace `main` with your default branch.

### 5. Inspect the checkpoint

Find the checkpoint ID with `entire checkpoint list`. `verify` takes it as
well as a branch:

```console
$ entire rsl verify 01M6K3TFZF4PNGVATZ6JFR6PEF
Ref                  refs/entire/checkpoints/EF/01M6K3TFZF4PNGVATZ6JFR6PEF
Verification         verified: gittuf verified every RSL entry for the ref against the policy, and every check passed
...
```

`checkpoint` shows the same detail as `branch`:

```console
$ entire rsl checkpoint 01M6K3TFZF4PNGVATZ6JFR6PEF
Checkpoint           01M6K3TFZF4PNGVATZ6JFR6PEF
Ref                  refs/entire/checkpoints/EF/01M6K3TFZF4PNGVATZ6JFR6PEF
Local tip            a6d81192ca2eb54b0b14d4745c35a1b6239de363
RSL tip              a6d81192ca2eb54b0b14d4745c35a1b6239de363 (entry #2)
Pushed by            pjbgf, as recorded by Entire
...

History (1 of 1 entries, newest first)
NUMBER  ENTRY         TARGET        TIME                  BY     SIGNER                SKIPPED
2       033a6d58d3d8  a6d81192ca2e  2026-11-04T11:07:08Z  pjbgf  Entire aws-eu-west-2  -
```

`checkpoint` also takes a full commit ID and reads the checkpoint from its
`Entire-Checkpoint` trailer: `entire rsl checkpoint "$(git rev-parse HEAD)"`.

### 6. Inspect a tag

> [!WARNING]
> Only tags signed by a key in the repository's policy verify. A tag signed by
> any other key fails verification, and so does an unsigned or lightweight
> tag. See
> [#1](https://github.com/entireio/entire-rsl/issues/1) and
> [#2](https://github.com/entireio/entire-rsl/issues/2).

Push a signed and a lightweight tag. Entire records an RSL entry for each:

```console
$ git tag -s v0.1.0 -m "v0.1.0"
$ git tag v0.1.1
$ git push origin v0.1.0 v0.1.1
...
remote: RSL: entry #4 recorded
remote: RSL: entry #5 recorded
To entire://aws-eu-west-2.entire.io/et/paulo/rsl-demo
 * [new tag]         v0.1.0 -> v0.1.0
 * [new tag]         v0.1.1 -> v0.1.1
```

Until the RSL is fetched again, the local RSL has no entries for the tags, so
they show as `unprotected`:

```console
$ entire rsl verify v0.1.0
Ref           refs/tags/v0.1.0
Verification  unprotected: the RSL has no entries for the ref, so nothing protects it. The repository's RSL coverage may exclude it
$ entire rsl refresh
Fetching gittuf refs from origin...
REF                              STATUS     LOCAL         REMOTE        COMMITS
refs/gittuf/policy               unchanged  6cb81fc8b792  6cb81fc8b792  -
refs/gittuf/reference-state-log  updated    9dec33e898e4  58a2e19f9e49  +2 commits (entry #3 → #5)
```

After the refresh, the tip, root of trust and signer checks pass, but neither
tag is signed by a key in the policy, so gittuf fails both:

```console
$ entire rsl verify v0.1.0
Ref                  refs/tags/v0.1.0
Verification         failed: gittuf verification failed
Detail               verifying tag entry failed, gittuf policy verification failed: verifying tag object's signature failed
Rules                authorize-rsl-signers
Check tip            pass: the local ref matches the tip the RSL records (9be4e7507d9880fa8ba63de2d9ff0942168148f4, entry #4)
Check root_of_trust  pass: anchored to Entire prod root (SHA256:Ja0D6sYQVI7aSmdIucYfwpQeV57DlvY5EE006iNQaZc), root version 1
Check signer         pass: signed by Entire aws-eu-west-2 (SHA256:aqzDRRL2FaTkQqhiQSVHbT7kBOl6xpiv6zfKzpkOPKQ)
$ entire rsl verify v0.1.1
Ref                  refs/tags/v0.1.1
Verification         failed: gittuf verification failed
Detail               verifying tag entry failed, gittuf policy verification failed: verifying tag object's signature failed
...
```

`entire rsl tag <name>` shows the same detail as `branch`, and warns when the
tag was moved.

### 7. Verify with gittuf

`entire rsl verify` runs gittuf's full verification of the ref, so it agrees
with [gittuf](https://github.com/gittuf/gittuf)'s own `verify-ref`:

```sh
gittuf verify-ref refs/tags/v0.1.0 --verbose
```

If gittuf fails a ref, `entire rsl verify` fails it too. entire-rsl only adds
what is Entire specific: who pushed the ref and when, the names of Entire's
roots of trust and cluster keys, and the root of trust and signer checks.

The one exception is a local ref behind the RSL tip, such as a branch you
have not pulled since someone else pushed. gittuf fails it with
`current state of Git reference does not match latest RSL entry`, because it
requires the local ref to match the newest entry. entire-rsl verifies it,
since gittuf's verification covered the state it is at, and reports the tip
check as `behind`. Pull the ref and both agree.

## Usage

```sh
entire rsl verify main                 # exit 0 only when verified
entire rsl branch main                 # RSL record, local tip, history, verification
entire rsl tag v1.2.0                  # warns when the tag was moved
entire rsl checkpoint 01JC0FFEE0BADC0DE00000007Z
entire rsl checkpoint <commit-sha>     # through its Entire-Checkpoint trailer
entire rsl log --ref refs/heads/main --limit 20
entire rsl entry 42                    # one entry by number or ID
entire rsl refresh [remote]            # fetch the latest RSL and policy
entire rsl version
entire agent-help rsl                  # documentation for AI agents
```

Every command takes `--json` and works without a terminal. The JSON schema is
in [docs/json-schema.md](docs/json-schema.md).

### Verification

`verify`, and `branch`, `tag` and `checkpoint` unless given `--no-verify`, run
gittuf's full verification of the ref from its first RSL entry, then three
checks:

- **tip**: the local ref matches the tip the RSL records, or is `behind` it
  at a state the verified history covers: an earlier recorded tip, or a commit
  inside a recorded push. A ref behind still verifies, and the check says
  which entry it is at and how many entries and commits it is behind. A ref
  ahead of the RSL (unrecorded commits), diverged from it, or at a commit a
  skipped (revoked) entry brought in fails.
- **root_of_trust**: the policy descends from a known root of trust:
  Entire's production or staging root, or a key given with
  `--root-key "ssh-ed25519 ..."` (or `--root-key @file`) for self-managed
  gittuf repositories.
- **signer**: in a repository rooted by Entire, the latest entry is signed by
  a known Entire cluster key, shown with the cluster's name.

The result is one of four states:

| State | Meaning | `verify` exit |
| --- | --- | --- |
| `verified` | gittuf verified every RSL entry for the ref and every check passed | 0 |
| `failed` | gittuf found a violation, or a check failed | 3 |
| `unprotected` | no RSL, no policy, no RSL entries for the ref, or no rule covering it | 3 |
| `error` | verification could not complete | 1 |

Exit code 2 means a usage error.

"Pushed by", the actor, force-push and the committer come from the entry and
are shown as recorded. They never affect the state: only the signature and
gittuf policy do.

### Refreshing from a remote

`entire rsl refresh [remote]` fetches the remote's gittuf refs into
`refs/remotes/<remote>/gittuf/*` and updates the local `refs/gittuf/*` refs:
missing ones are created and existing ones are fast-forwarded. Each ref shows
how many commits it gained, and the RSL also its entry numbers before and
after, such as `+2 commits (entry #14 → #16)`. A local ref the remote's does
not descend from is left alone and reported as `diverged`, with exit code 1.
When several remotes are configured and none is named, `origin` is offered
first. Branches, tags and checkpoint refs are not fetched, so fetch
them with git before comparing them with the RSL.

### Fetching a missing RSL

Clones made with plain git have no gittuf refs. entire-rsl then offers to
fetch them, asking first, and asking which remote when there are several.
Pass `--fetch` (or `--fetch=<remote>`) to fetch without a prompt, which is
required without a terminal.

### Colour

On a terminal, verification states, check results and signatures are
coloured: green for verified, passing or valid, red for failed, in error or
invalid, yellow for unprotected. `NO_COLOR`, `TERM=dumb`, `ACCESSIBLE` and
`--json` turn colour off, and output to a pipe or file is never coloured.

### Debugging

`--debug` or `ENTIRE_LOG_LEVEL=debug` prints diagnostics to stderr, including
the gittuf version entire-rsl was built with.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md). The build spec is [SPEC.md](SPEC.md),
design decisions are in [docs/decisions.md](docs/decisions.md), review
records in [docs/reviews/](docs/reviews/), and benchmarks in
[docs/benchmarks.md](docs/benchmarks.md).

## License

MIT. See [LICENSE](LICENSE).
