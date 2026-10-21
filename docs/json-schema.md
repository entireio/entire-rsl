# JSON output schema

Every command that takes `--json` prints one JSON document on stdout. Every
document carries `"schema_version": 1`. The version changes only on an
incompatible change: removing or renaming a field, or changing its type or
meaning. New fields may appear in any version, so consumers must ignore
fields they do not know.

Golden files under `cmd/entire-rsl/cli/testdata/*.json` pin every document
shape.

Conventions:

- Object IDs are full lowercase hex strings (40 characters for SHA-1, 64 for
  SHA-256).
- Times are RFC 3339 in UTC. An entry's `time` is its commit time, which the
  signer asserts. Nothing else vouches for it.
- Strings carry repository data verbatim, with standard JSON escaping.
  Control characters are escaped as `\u00XX`, but other characters, such as
  Unicode bidirectional overrides, are not. Sanitize before printing them to a
  terminal.
- Fields ending in `_unverified`, `custom_fields` and `entire` are recorded in
  the entry but not verified by entire-rsl. Never base a security decision on
  them.

## Entry

Used by `log` and `entry`.

| Field | Type | Meaning |
| --- | --- | --- |
| `number` | integer | Entry number, increasing with each entry. 0 for unnumbered entries. |
| `id` | string | Entry ID (the RSL commit). |
| `type` | string | `reference`, `annotation` or `propagation`. |
| `ref` | string | Ref the entry records. Absent for annotations. |
| `target` | string | Object the ref was set to. Absent for annotations. |
| `time` | string | Commit time, asserted by the signer. |
| `signer` | object | See [Signer](#signer). |
| `committer_unverified` | object | `name` and `email` from the commit. Not verified. |
| `skipped` | boolean | True when a skip annotation revokes this entry. |
| `skipped_by` | array of string | IDs of the skip annotations revoking this entry. |
| `annotated_by` | array of string | IDs of every annotation referring to this entry, newest first. |
| `skip` | boolean | Annotations only: whether the annotation revokes the entries it names. |
| `annotates` | array of string | Annotations only: IDs of the entries it refers to. |
| `message` | string | Annotations only: the annotation message. |
| `upstream_repository` | string | Propagation entries only. |
| `upstream_entry_id` | string | Propagation entries only. |
| `entire` | object | Entire's interpretation of `custom_fields`. See [Entire fields](#entire-fields). |
| `custom_fields` | object | Every custom field, key to value, as recorded. |

## Signer

| Field | Type | Meaning |
| --- | --- | --- |
| `method` | string | `ssh`, `gpg`, `x509`, `none` (unsigned) or `unknown`. |
| `signature_valid` | boolean | True when entire-rsl checked the signature against the key it names. Only SSH signatures are checked. |
| `key_id` | string | SSH SHA256 fingerprint of the signing key. Present only when `signature_valid` is true. |
| `name` | string | Entire cluster that owns the key, when known. Best effort. |
| `problem` | string | Why the signature names no key. |

A valid signature proves who signed the entry, not that they were allowed
to. Only verification decides that.

## Entire fields

| Field | Type | Meaning |
| --- | --- | --- |
| `pusher` | object | The end user Entire observed pushing: `account_id`, `handle` and `raw`. `account_id` and `handle` are absent when `raw` does not have the `<account ULID> (<handle>)` shape. |
| `actor` | string | The non-human service that performed the write, such as `entire-mirror-service`. |
| `actor_label` | string | What `actor` means, for known actors. |
| `repository` | string | Entire repository ID. |
| `force_push` | boolean | True for non-fast-forward branch updates and any tag update. |
| `operation` | string | `default-branch-change` or `default-branch-baseline`. Absent for an ordinary push. |
| `operation_label` | string | What `operation` means. |
| `symref_target` | string | For a default branch change, the new default branch. |
| `symref_previous_target` | string | For a default branch change, the previous default branch. |

## `version`

```json
{"schema_version": 1, "version": "...", "commit": "...", "go_version": "...", "gittuf_version": "..."}
```

## `log`

```json
{"schema_version": 1, "entries": [Entry, ...]}
```

Entries are newest first. `entries` is an empty array when nothing matches.

## `entry`

```json
{"schema_version": 1, "entry": Entry}
```

## `branch` and `tag`

```json
{
  "schema_version": 1,
  "ref": "refs/heads/main",
  "local_tip": "...",
  "rsl_tip": "...",
  "current_entry": Entry,
  "history": [Entry, ...],
  "history_total": 3,
  "warnings": [{"code": "...", "message": "..."}]
}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `ref` | string | Full ref name. |
| `local_tip` | string or null | Local value of the ref. Null when the ref does not exist locally. |
| `rsl_tip` | string or null | Target of `current_entry`. Null when the RSL has no unskipped entry for the ref. |
| `current_entry` | Entry or null | Newest entry for the ref that no annotation skips. |
| `history` | array of Entry | Newest entries for the ref, newest first, up to `--history`, skipped ones included. |
| `history_total` | integer | Number of entries the RSL holds for the ref. |
| `warnings` | array | Each with a stable `code` and a human `message`. |

Warning codes:

| Code | Meaning |
| --- | --- |
| `no_rsl_entries` | The RSL has no entry for the ref. Coverage may exclude it. |
| `all_entries_skipped` | Every entry for the ref is skipped. |
| `local_ref_missing` | The ref does not exist locally. |
| `local_drift` | The local ref differs from `rsl_tip`. |
| `tag_moved` | A tag has more than one unskipped entry. |

## `checkpoint`

The `branch` document for the checkpoint's ref, plus:

```json
"checkpoint": {"id": "01JC0FFEE0BADC0DE00000007Z", "resolved_from": "commit", "commit": "..."}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `checkpoint.id` | string | The checkpoint ID. |
| `checkpoint.resolved_from` | string | `id` when given directly, `commit` when read from a commit's `Entire-Checkpoint` trailer. |
| `checkpoint.commit` | string | The commit the ID was read from, for `resolved_from: commit`. |
