package verify

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entireio/entire-rsl/internal/clusters"
	"github.com/entireio/entire-rsl/internal/store"
	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/gitstore"
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
	return runWithKeys(ctx, t, dir, ref, timeout, testutil.TrustedKeys(t))
}

func runWithKeys(ctx context.Context, t *testing.T, dir, ref string, timeout time.Duration, keys []clusters.Key) Result {
	t.Helper()

	g, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	st, err := store.New(g)
	require.NoError(t, err)

	localTip := func(ref string) (githash.Hash, error) {
		tip, err := st.GetReference(ref)
		if errors.Is(err, gitstore.ErrReferenceNotFound) {
			return nil, nil
		}
		return tip, err
	}

	return Verify(ctx, st, ref, Options{
		Logger:   slog.New(slog.DiscardHandler),
		RepoPath: dir,
		Timeout:  timeout,
		LocalTip: localTip,
		Keys:     keys,
	})
}

func checkByName(t *testing.T, res Result, name string) Check {
	t.Helper()

	for _, c := range res.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s check in %+v", name, res.Checks)
	return Check{}
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

func TestChecksPassOnTheScenario(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	res := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)

	require.Equal(t, Verified, res.State, res.Reason)
	assert.Equal(t, CheckPass, checkByName(t, res, CheckTip).Status)
	root := checkByName(t, res, CheckRoot)
	assert.Equal(t, CheckPass, root.Status, root.Detail)
	assert.Contains(t, root.Detail, "Entire test root")
	signer := checkByName(t, res, CheckSigner)
	assert.Equal(t, CheckPass, signer.Status, signer.Detail)
	assert.Contains(t, signer.Detail, "Entire test test-cluster (local)")
}

func TestLocalRefBehindAtARecordedTipVerifies(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	sc.SetRef("refs/heads/main", sc.MainSecond)

	res := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Verified, res.State, res.Reason)
	assert.Contains(t, res.Reason, "behind the RSL tip")
	tip := checkByName(t, res, CheckTip)
	assert.Equal(t, CheckBehind, tip.Status)
	assert.Contains(t, tip.Detail, "at entry #4, the RSL tip is entry #9 (1 entry, 1 commit behind)")
}

func TestLocalRefBehindInsideAPushVerifies(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Bootstrap()
	inside := r.Commit("refs/heads/main", "pushed together")
	r.Commit("refs/heads/main", "with this one")
	r.Record(r.Cluster, "refs/heads/main", nil)
	r.Commit("refs/heads/main", "pushed later")
	r.Record(r.Cluster, "refs/heads/main", nil)
	r.SetRef("refs/heads/main", inside)

	res := run(t.Context(), t, r.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Verified, res.State, res.Reason)
	tip := checkByName(t, res, CheckTip)
	assert.Equal(t, CheckBehind, tip.Status)
	assert.Contains(t, tip.Detail, "in the push recorded by entry #3, the RSL tip is entry #4 (1 entry, 2 commits behind)")
}

func TestLocalRefAheadFailsVerification(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	sc.Commit("refs/heads/main", "not pushed")

	res := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Failed, res.State)
	tip := checkByName(t, res, CheckTip)
	assert.Equal(t, CheckFail, tip.Status)
	assert.Contains(t, tip.Detail, "ahead of the RSL tip (entry #9) by 1 commit")
}

func TestLocalRefDivergedFailsVerification(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	feature, err := sc.Git.GetReference("refs/heads/feature")
	require.NoError(t, err)
	sc.SetRef("refs/heads/main", feature)

	res := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Failed, res.State)
	assert.Contains(t, checkByName(t, res, CheckTip).Detail, "diverged")
}

func TestMissingLocalRefSkipsTheTipCheck(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	require.NoError(t, sc.Git.DeleteReference("refs/heads/feature"))

	res := run(t.Context(), t, sc.Dir, "refs/heads/feature", time.Minute)

	assert.Equal(t, Verified, res.State, res.Reason)
	assert.Equal(t, CheckSkip, checkByName(t, res, CheckTip).Status)
}

func TestUnknownRootFailsVerification(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Root = testutil.NewKey(t, "someone else's root")
	r.Bootstrap()
	r.Commit("refs/heads/main", "only")
	r.Record(r.Cluster, "refs/heads/main", nil)

	res := run(t.Context(), t, r.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Failed, res.State)
	root := checkByName(t, res, CheckRoot)
	assert.Equal(t, CheckFail, root.Status)
	assert.Contains(t, root.Detail, "not signed by a known root key")
	assert.Equal(t, CheckSkip, checkByName(t, res, CheckSigner).Status)
}

func TestSignerOutsideTheClusterTableFailsVerification(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	keys := testutil.TrustedKeys(t)[:1] // the root only

	res := runWithKeys(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute, keys)

	assert.Equal(t, Failed, res.State)
	signer := checkByName(t, res, CheckSigner)
	assert.Equal(t, CheckFail, signer.Status)
	assert.Contains(t, signer.Detail, "not a known Entire cluster key")
}

func TestSignerFromAnotherEnvironmentFailsVerification(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	keys := testutil.TrustedKeys(t)
	keys[1].Environment = "staging"

	res := runWithKeys(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute, keys)

	assert.Equal(t, Failed, res.State)
	assert.Contains(t, checkByName(t, res, CheckSigner).Detail, "rooted in test")
}

func TestUserSuppliedRootSkipsTheSignerCheck(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	keys := testutil.TrustedKeys(t)[:1]
	keys[0].Environment = clusters.EnvironmentUser

	res := runWithKeys(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute, keys)

	assert.Equal(t, Verified, res.State, res.Reason)
	assert.Equal(t, CheckSkip, checkByName(t, res, CheckSigner).Status)
}

func TestLocalRefReadErrorIsAnError(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	g, err := gogit.PlainOpen(sc.Dir)
	require.NoError(t, err)
	st, err := store.New(g)
	require.NoError(t, err)

	res := Verify(t.Context(), st, "refs/heads/main", Options{
		Logger:   slog.New(slog.DiscardHandler),
		RepoPath: sc.Dir,
		Timeout:  time.Minute,
		Keys:     testutil.TrustedKeys(t),
		LocalTip: func(string) (githash.Hash, error) { return nil, errors.New("disk on fire") },
	})

	assert.Equal(t, Error, res.State, "a check that cannot run is an error, not a failure")
	require.ErrorContains(t, res.Err, "disk on fire")
}

func TestRevokedCommitKeptByARecoveryCommitFailsVerification(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Bootstrap()
	base := r.Commit("refs/heads/main", "base")
	good := r.Commit("refs/heads/main", "good")
	r.Record(r.Cluster, "refs/heads/main", nil)
	rogue := r.Commit("refs/heads/main", "rogue")
	rogueEntry := r.Record(r.Rogue, "refs/heads/main", nil)
	r.Annotate(r.Cluster, true, "revoked", rogueEntry)

	// gittuf accepts a fix tree-same with the last good state, here one that
	// keeps the rogue commit as its parent.
	goodTree, err := r.Git.GetCommitTreeID(good)
	require.NoError(t, err)
	out, err := exec.CommandContext(t.Context(), "git", "-C", r.Dir, "commit-tree", goodTree.String(), "-p", rogue.String(), "-m", "fix").CombinedOutput()
	require.NoError(t, err, string(out))
	fix, err := githash.NewHash(strings.TrimSpace(string(out)))
	require.NoError(t, err)
	r.SetRef("refs/heads/main", fix)
	r.Record(r.Cluster, "refs/heads/main", nil)

	atFix := run(t.Context(), t, r.Dir, "refs/heads/main", time.Minute)
	require.Equal(t, Verified, atFix.State, "%s: %v", atFix.Reason, atFix.Err)

	r.SetRef("refs/heads/main", rogue)
	atRogue := run(t.Context(), t, r.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Failed, atRogue.State)
	tip := checkByName(t, atRogue, CheckTip)
	assert.Equal(t, CheckFail, tip.Status)
	assert.Contains(t, tip.Detail, "brought in by skipped entry")

	r.SetRef("refs/heads/main", base)
	atBase := run(t.Context(), t, r.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Verified, atBase.State, "an unskipped push brought base in before the skipped one: %s: %v", atBase.Reason, atBase.Err)
}

func TestBehindWithAMissingObjectIsAnError(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	sc.SetRef("refs/heads/main", sc.MainFirst)
	removeLooseObject(t, sc.Dir, sc.MainFirst.String())

	res := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)

	assert.Equal(t, Error, res.State, "%s: %v", res.Reason, res.Err)
}

func TestEntriesCheckedOnlyWhenGittufVerified(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	verified := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)
	require.Equal(t, Verified, verified.State)
	require.NotNil(t, verified.Checked)
	assert.Equal(t, uint64(3), verified.Checked.First.Number)
	assert.Equal(t, uint64(9), verified.Checked.Last.Number)
	assert.Equal(t, 3, verified.Checked.Count)

	failed := run(t.Context(), t, sc.Dir, "refs/heads/rogue", time.Minute)
	require.Equal(t, Failed, failed.State)
	assert.Nil(t, failed.Checked, "gittuf stops at the first violation, so no range was checked")

	r := testutil.NewRepo(t)
	r.Bootstrap("git:refs/heads/main")
	r.Commit("refs/heads/other", "uncovered")
	r.Record(r.Cluster, "refs/heads/other", nil)
	uncovered := run(t.Context(), t, r.Dir, "refs/heads/other", time.Minute)
	require.Equal(t, Unprotected, uncovered.State)
	assert.Nil(t, uncovered.Checked)

	sc.Commit("refs/heads/main", "not pushed")
	checkFailed := run(t.Context(), t, sc.Dir, "refs/heads/main", time.Minute)
	require.Equal(t, Failed, checkFailed.State)
	assert.NotNil(t, checkFailed.Checked, "gittuf verified the range even though the tip check failed")
}
