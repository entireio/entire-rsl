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

- **tip**: the local ref matches the tip the RSL records.
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
