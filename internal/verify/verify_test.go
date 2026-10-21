package verify

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entireio/entire-rsl/internal/store"
	"github.com/entireio/entire-rsl/internal/testutil"
	gogit "github.com/go-git/go-git/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutil.IsolateGitConfig()
	os.Exit(m.Run())
}

func run(ctx context.Context, t *testing.T, dir, ref string, timeout time.Duration) Result {
	t.Helper()

	g, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	st, err := store.New(g)
	require.NoError(t, err)

	return Verify(ctx, st, ref, Options{Logger: slog.New(slog.DiscardHandler), RepoPath: dir, Timeout: timeout})
}

func TestScenarioStates(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	tests := []struct {
		ref  string
		want State
	}{
		{ref: "refs/heads/main", want: Verified},
		{ref: "refs/heads/feature", want: Verified},
		{ref: "refs/heads/fixed", want: Verified},
		{ref: testutil.CheckpointRef(testutil.CheckpointULID), want: Verified},
		{ref: "refs/heads/rogue", want: Failed},
		{ref: "refs/tags/v1.0.0", want: Failed},
		{ref: "refs/heads/never", want: Unprotected},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			t.Parallel()

			res := run(t.Context(), t, sc.Dir, tt.ref, time.Minute)

			assert.Equal(t, tt.want, res.State, "%s: %v", res.Reason, res.Err)
			if tt.want == Verified {
				require.NoError(t, res.Err)
				assert.NotEmpty(t, res.ExpectedTip)
				assert.Equal(t, []string{"authorize-rsl-signers"}, res.Rules)
			} else {
				assert.Empty(t, res.ExpectedTip)
			}
		})
	}
}

func TestVerifiedTipIsTheRecordedTarget(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	res := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)

	require.Equal(t, Verified, res.State)
	assert.Equal(t, sc.TrailerCommit, res.ExpectedTip)
}

func TestNoPolicyIsUnprotectedNeverVerified(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Commit("refs/heads/main", "only")
	r.Record(r.Cluster, "refs/heads/main", nil)

	res := run(t.Context(), t, r.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Unprotected, res.State)
	assert.Contains(t, res.Reason, "no applied gittuf policy")
}

func TestRefNoRuleCoversIsUnprotected(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Bootstrap("git:refs/heads/main")
	r.Commit("refs/heads/other", "unprotected work")
	r.Record(r.Rogue, "refs/heads/other", nil)

	res := run(t.Context(), t, r.Dir, "refs/heads/other", time.Minute)

	assert.Equal(t, Unprotected, res.State, "gittuf alone would pass this rogue-signed ref")
	assert.Contains(t, res.Reason, "no gittuf policy rule covers")
}

func TestNoEntriesIsUnprotected(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	res := run(t.Context(), t, sc.Dir, "refs/heads/never", time.Minute)

	assert.Equal(t, Unprotected, res.State)
	assert.Contains(t, res.Reason, "no entries")
}

func TestErrorsAreNeverVerified(t *testing.T) {
	t.Parallel()

	t.Run("cancelled", func(t *testing.T) {
		t.Parallel()

		sc := testutil.NewScenario(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		res := run(ctx, t, sc.Dir, "refs/heads/main", time.Minute)

		assert.Equal(t, Error, res.State)
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()

		sc := testutil.NewScenario(t)

		res := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Nanosecond)

		assert.Equal(t, Error, res.State)
		assert.Equal(t, "verification timed out", res.Reason)
	})

	t.Run("missing object in history", func(t *testing.T) {
		t.Parallel()

		sc := testutil.NewScenario(t)
		removeLooseObject(t, sc.Dir, sc.MainFirstEntry.String())

		res := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)

		assert.Equal(t, Error, res.State, res.Reason)
	})

	t.Run("shallow RSL", func(t *testing.T) {
		t.Parallel()

		sc := testutil.NewScenario(t)
		require.NoError(t, os.WriteFile(filepath.Join(sc.Dir, ".git", "shallow"), []byte(sc.TagMovedEntry.String()+"\n"), 0o600))

		res := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)

		assert.Equal(t, Error, res.State)
		assert.Contains(t, res.Reason, "incomplete")
	})

	t.Run("missing local tag", func(t *testing.T) {
		t.Parallel()

		r := testutil.NewRepo(t)
		r.Bootstrap()
		c := r.Commit("refs/heads/main", "only")
		r.SetRef("refs/tags/v1", c)
		r.Record(r.Cluster, "refs/tags/v1", nil)
		require.NoError(t, r.Git.DeleteReference("refs/tags/v1"))

		res := run(t.Context(), t, r.Dir, "refs/tags/v1", time.Minute)

		assert.NotEqual(t, Verified, res.State)
	})
}

func removeLooseObject(t *testing.T, dir, id string) {
	t.Helper()

	path := filepath.Join(dir, ".git", "objects", id[:2], id[2:])
	require.NoError(t, os.Chmod(path, 0o600))
	require.NoError(t, os.Remove(path))
}

func TestNoRSLIsUnprotected(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Commit("refs/heads/main", "only")

	res := run(t.Context(), t, r.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Unprotected, res.State)
}

func TestFirstEntryViolationFailsEvenWhenSkipped(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Bootstrap()
	r.Commit("refs/heads/main", "rogue first")
	rogue := r.Record(r.Rogue, "refs/heads/main", nil)
	r.Annotate(r.Cluster, true, "revoked", rogue)
	r.Record(r.Cluster, "refs/heads/main", nil)

	res := run(t.Context(), t, r.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Failed, res.State, "gittuf has no last good state to recover to")
}
