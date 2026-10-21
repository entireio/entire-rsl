package verify

import (
	"log/slog"
	"testing"
	"time"

	"github.com/entireio/entire-rsl/internal/store"
	"github.com/entireio/entire-rsl/internal/testutil"
	gogit "github.com/go-git/go-git/v6"
)

// BenchmarkVerifyLargeRSL runs full verification of a ref with 100,000 RSL
// entries, the size SPEC.md section 8 benchmarks against.
func BenchmarkVerifyLargeRSL(b *testing.B) {
	r := testutil.NewBenchRepo(b)
	r.Bootstrap()
	target := r.Commit("refs/heads/main", "only")
	r.Record(r.Cluster, "refs/heads/main", nil)
	r.AppendEntries(b, r.Cluster, "refs/heads/main", target, 99_999)

	for b.Loop() {
		g, err := gogit.PlainOpen(r.Dir)
		if err != nil {
			b.Fatal(err)
		}
		st, err := store.New(g)
		if err != nil {
			b.Fatal(err)
		}

		res := Verify(b.Context(), st, "refs/heads/main", Options{
			Logger:   slog.New(slog.DiscardHandler),
			RepoPath: r.Dir,
			Timeout:  30 * time.Minute,
			Keys:     testutil.TrustedKeys(b),
		})
		if res.State != Verified {
			b.Fatalf("%s: %s: %v", res.State, res.Reason, res.Err)
		}
	}
}
