# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- `entire rsl` plugin for the Entire CLI: inspect the gittuf Reference State
  Log for branches, tags and Entire checkpoints, and verify them against gittuf
  policy.
- Commands: `verify`, `branch`, `tag`, `checkpoint`, `log`, `entry`,
  `agent-help` and `version`, each with `--json`.
- Verification in four states with tip, root of trust and signer checks, and
  `--root-key` for self-managed roots.
- Fetching a missing RSL from a remote with consent.
- `--fetch` to fetch the latest gittuf refs before any command, as `refresh`
  does.
- `refresh` to fetch the latest gittuf refs from a remote, fast-forward only.
- Green and red colouring of verification outcomes on a terminal.
