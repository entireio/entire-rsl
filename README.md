# entire-rsl

`entire rsl` shows what a repository's [gittuf](https://gittuf.dev) Reference
State Log (RSL) records for a branch, tag or Entire checkpoint: who pushed it,
when, and whether gittuf verification passes. It is a plugin for the
[Entire CLI](https://github.com/entireio/cli) and also runs standalone as
`entire-rsl`.

entire-rsl is read-only. It never writes refs, objects or git config, except
for remote-tracking RSL refs written by a fetch you confirm.

## Install

```sh
entire plugin install ./dist/entire-rsl_<version>_<os>_<arch>.tar.gz
```

## Usage

```sh
entire rsl version
```

## License

MIT. See [LICENSE](LICENSE).
