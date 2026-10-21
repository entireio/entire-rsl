# Benchmarks

SPEC.md section 8 asks for benchmarks on a 100,000-entry RSL. The fixture
(`testutil.AppendEntries`) writes signed reference entries for one branch,
exactly as gittuf formats them, and repacks the repository as real ones are.

```sh
go test -run '^$' -bench BenchmarkLargeRSL -benchtime 3x -benchmem ./internal/rslread/
go test -run '^$' -bench BenchmarkVerifyLargeRSL -benchtime 1x -benchmem -timeout 30m ./internal/verify/
```

Results on 2026-10-21, AMD Ryzen AI 9 HX PRO 370, Go 1.27.3, linux/amd64:

| Benchmark | Time | Allocated |
| --- | --- | --- |
| `log` first page (50 entries) | 4.0 ms | 2.1 MB |
| History scan of all 100,000 entries | 2.4 s | 1.3 GB |
| Completeness walk of all 100,000 entries | 2.5 s | 1.3 GB |
| Full verification of a ref with 100,000 entries | 10.1 s | 4.1 GB |

Notes:

- Commands that show a ref's history walk the whole RSL to count entries and
  find the current one, so they cost about one history scan.
- Verification walks the RSL several times: entire-rsl's completeness check,
  then gittuf's own walks for the policy, the ref's first entry and the
  verification itself. The default `--timeout` of five minutes leaves room
  for RSLs far larger than this.
- Most of the remaining time is go-git's pack index lookups and commit
  decoding. The store caches decoded commits (4,096) and the shallow list per
  invocation.
