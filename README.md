# entire-rsl

`entire rsl` shows what a repository's [gittuf](https://gittuf.dev) Reference
State Log (RSL) records for a branch, tag or Entire checkpoint: who pushed it,
when, and whether gittuf verification passes. It is a plugin for the
[Entire CLI](https://github.com/entireio/cli) and also runs standalone as
`entire-rsl`.

entire-rsl is read-only. The one exception is when the repository has no
local RSL and you agree to fetch it: then it creates the missing
`refs/gittuf/*` refs (never changing existing ones) and updates
`refs/remotes/<remote>/gittuf/*`.

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

### Fetching a missing RSL

Clones made with plain git have no gittuf refs. entire-rsl then offers to
fetch them, asking first, and asking which remote when there are several.
Pass `--fetch` (or `--fetch=<remote>`) to fetch without a prompt, which is
required without a terminal.

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
