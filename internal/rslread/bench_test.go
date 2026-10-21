package rslread

import (
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
)

// largeRSL is the entry count SPEC.md section 8 benchmarks against.
const largeRSL = 100_000

// largeRepo builds a repository whose RSL holds largeRSL entries for
// refs/heads/main after the policy entries.
func largeRepo(b *testing.B) *testutil.Repo {
	b.Helper()

	r := testutil.NewBenchRepo(b)
	r.Bootstrap()
	target := r.Commit("refs/heads/main", "only")
	r.Record(r.Cluster, "refs/heads/main", nil)
	r.AppendEntries(b, r.Cluster, "refs/heads/main", target, largeRSL-1)
	return r
}

func BenchmarkLargeRSL(b *testing.B) {
	r := largeRepo(b)

	b.Run("log first page", func(b *testing.B) {
		for b.Loop() {
			reader := openReaderB(b, r.Dir)
			if _, err := reader.Log(b.Context(), LogOptions{Limit: 50}); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("history full scan", func(b *testing.B) {
		for b.Loop() {
			reader := openReaderB(b, r.Dir)
			if _, err := reader.History(b.Context(), "refs/heads/never", 10); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("check complete", func(b *testing.B) {
		for b.Loop() {
			reader := openReaderB(b, r.Dir)
			if err := reader.CheckComplete(b.Context()); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func openReaderB(b *testing.B, dir string) *Reader {
	b.Helper()

	st := openStoreTB(b, dir)
	r, err := Open(st)
	if err != nil {
		b.Fatal(err)
	}
	return r
}
