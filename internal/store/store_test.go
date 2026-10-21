package store

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/gitinterface"
	"github.com/gittuf/gittuf/pkg/gitstore"
	"github.com/gittuf/gittuf/pkg/policy"
	"github.com/gittuf/gittuf/pkg/rsl"
	gogit "github.com/go-git/go-git/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutil.IsolateGitConfig()
	os.Exit(m.Run())
}

func open(t *testing.T, dir string) *Store {
	t.Helper()

	repo, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	s, err := New(repo)
	require.NoError(t, err)
	return s
}

// rslEntryIDs walks the RSL from its tip and returns every entry ID.
func rslEntryIDs(t *testing.T, s gitstore.Storer) []githash.Hash {
	t.Helper()

	entry, err := rsl.GetLatestEntry(s)
	require.NoError(t, err)

	var ids []githash.Hash
	for {
		ids = append(ids, entry.GetID())
		entry, err = rsl.GetParentForEntry(s, entry)
		if errors.Is(err, rsl.ErrRSLEntryNotFound) {
			return ids
		}
		require.NoError(t, err)
	}
}

// requireSame asserts that both stores return the same value and error
// presence for one call.
func requireSame[T any](t *testing.T, call string, want T, wantErr error, got T, gotErr error) {
	t.Helper()

	if wantErr != nil {
		require.Error(t, gotErr, call)
		return
	}
	require.NoError(t, gotErr, call)
	assert.Equal(t, want, got, call)
}

func TestMatchesGitinterfaceOnScenario(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	ref := sc.Git
	got := open(t, sc.Dir)

	ids := rslEntryIDs(t, ref)
	require.NotEmpty(t, ids)

	var commits []githash.Hash
	for _, id := range ids {
		commits = append(commits, id)

		wantMsg, wantErr := ref.GetCommitMessage(id)
		gotMsg, gotErr := got.GetCommitMessage(id)
		requireSame(t, "GetCommitMessage "+id.String(), wantMsg, wantErr, gotMsg, gotErr)

		wantTree, wantErr := ref.GetCommitTreeID(id)
		gotTree, gotErr := got.GetCommitTreeID(id)
		requireSame(t, "GetCommitTreeID", wantTree, wantErr, gotTree, gotErr)

		wantParents, wantErr := ref.GetCommitParentIDs(id)
		gotParents, gotErr := got.GetCommitParentIDs(id)
		requireSame(t, "GetCommitParentIDs", wantParents, wantErr, gotParents, gotErr)

		wantPayload, wantSig, wantErr := ref.GetObjectSignature(id)
		gotPayload, gotSig, gotErr := got.GetObjectSignature(id)
		requireSame(t, "GetObjectSignature payload", wantPayload, wantErr, gotPayload, gotErr)
		requireSame(t, "GetObjectSignature signature", wantSig, wantErr, gotSig, gotErr)
	}

	refs := []string{"refs/heads/main", "refs/heads/feature", "refs/heads/rogue", "refs/heads/fixed", "refs/tags/v1.0.0", rsl.Ref, policy.Ref, policy.StagingRef, testutil.CheckpointRef(testutil.CheckpointULID), "refs/heads/missing", "HEAD"}
	for _, name := range refs {
		wantTip, wantErr := ref.GetReference(name)
		gotTip, gotErr := got.GetReference(name)
		requireSame(t, "GetReference "+name, wantTip, wantErr, gotTip, gotErr)
		if errors.Is(wantErr, gitstore.ErrReferenceNotFound) {
			require.ErrorIs(t, gotErr, gitstore.ErrReferenceNotFound, name)
		}
		if wantErr == nil {
			commits = append(commits, wantTip)
		}
	}

	policyTip, err := ref.GetReference(policy.Ref)
	require.NoError(t, err)
	policyTree, err := ref.GetCommitTreeID(policyTip)
	require.NoError(t, err)

	wantFiles, wantErr := ref.GetAllFilesInTree(policyTree)
	gotFiles, gotErr := got.GetAllFilesInTree(policyTree)
	requireSame(t, "GetAllFilesInTree", wantFiles, wantErr, gotFiles, gotErr)

	wantEntries, wantErr := ref.GetEntriesInTree(policyTree)
	gotEntries, gotErr := got.GetEntriesInTree(policyTree)
	requireSame(t, "GetEntriesInTree", wantEntries, wantErr, gotEntries, gotErr)

	for path, blob := range wantFiles {
		wantID, wantErr := ref.GetPathIDInTree(policyTree, path)
		gotID, gotErr := got.GetPathIDInTree(policyTree, path)
		requireSame(t, "GetPathIDInTree "+path, wantID, wantErr, gotID, gotErr)

		wantBlob, wantErr := ref.ReadBlob(blob)
		gotBlob, gotErr := got.ReadBlob(blob)
		requireSame(t, "ReadBlob "+path, wantBlob, wantErr, gotBlob, gotErr)
	}
	_, wantErr = ref.GetPathIDInTree(policyTree, "no/such/path")
	_, gotErr = got.GetPathIDInTree(policyTree, "no/such/path")
	require.Error(t, wantErr)
	require.ErrorIs(t, gotErr, gitinterface.ErrTreeDoesNotHavePath)

	for _, a := range commits {
		for _, b := range commits {
			wantKnows, wantErr := ref.KnowsCommit(a, b)
			gotKnows, gotErr := got.KnowsCommit(a, b)
			requireSame(t, "KnowsCommit "+a.String()+" "+b.String(), wantKnows, wantErr, gotKnows, gotErr)
		}
	}

	for _, pair := range [][2]githash.Hash{{sc.TrailerCommit, sc.MainFirst}, {sc.TrailerCommit, nil}, {sc.MainSecond, sc.TrailerCommit}, {ids[0], ids[len(ids)-1]}} {
		wantRange, wantErr := ref.GetCommitsBetweenRange(pair[0], pair[1])
		gotRange, gotErr := got.GetCommitsBetweenRange(pair[0], pair[1])
		requireSame(t, "GetCommitsBetweenRange", wantRange, wantErr, gotRange, gotErr)
	}

	for _, c := range []githash.Hash{sc.MainFirst, sc.MainSecond, sc.TrailerCommit, policyTip} {
		wantPaths, wantErr := ref.GetFilePathsChangedByCommit(c)
		gotPaths, gotErr := got.GetFilePathsChangedByCommit(c)
		requireSame(t, "GetFilePathsChangedByCommit "+c.String(), wantPaths, wantErr, gotPaths, gotErr)
	}

	wantEmpty, err := ref.EmptyTree()
	require.NoError(t, err)
	gotEmpty, err := got.EmptyTree()
	require.NoError(t, err)
	assert.Equal(t, wantEmpty, gotEmpty)
	assert.Equal(t, ref.ZeroHash(), got.ZeroHash())
}

func TestTagTargetAndSignatureOnTagObjects(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	commit := r.Commit("refs/heads/main", "tagged")
	gitCmd(t, r.Dir, "tag", "-a", "-m", "annotated", "v1", commit.String())
	gitCmd(t, r.Dir, "tag", "-a", "-m", "chained", "v1-chain", "v1")

	got := open(t, r.Dir)
	for _, name := range []string{"refs/tags/v1", "refs/tags/v1-chain"} {
		tagID, err := r.Git.GetReference(name)
		require.NoError(t, err)

		wantTarget, wantErr := r.Git.GetTagTarget(tagID)
		gotTarget, gotErr := got.GetTagTarget(tagID)
		requireSame(t, "GetTagTarget "+name, wantTarget, wantErr, gotTarget, gotErr)
		assert.Equal(t, commit, gotTarget)

		wantPayload, wantSig, wantErr := r.Git.GetObjectSignature(tagID)
		gotPayload, gotSig, gotErr := got.GetObjectSignature(tagID)
		requireSame(t, "GetObjectSignature payload", wantPayload, wantErr, gotPayload, gotErr)
		requireSame(t, "GetObjectSignature signature", wantSig, wantErr, gotSig, gotErr)
	}

	blob, err := r.Git.WriteBlob([]byte("x"))
	require.NoError(t, err)
	_, _, err = got.GetObjectSignature(blob)
	assert.ErrorIs(t, err, gitinterface.ErrNotCommitOrTag)
}

func TestFilePathsChangedByMergeMatchesGitinterface(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	base := r.Commit("refs/heads/main", "base")
	left := r.Commit("refs/heads/main", "left")
	r.SetRef("refs/heads/side", base)
	right := r.Commit("refs/heads/side", "right")

	leftTree, err := r.Git.GetCommitTreeID(left)
	require.NoError(t, err)
	rightTree, err := r.Git.GetCommitTreeID(right)
	require.NoError(t, err)

	got := open(t, r.Dir)
	for name, tree := range map[string]githash.Hash{"tree of last parent": rightTree, "tree of first parent": leftTree} {
		merge := gitCmd(t, r.Dir, "commit-tree", tree.String(), "-p", left.String(), "-p", right.String(), "-m", "merge")
		mergeID, err := githash.NewHash(merge)
		require.NoError(t, err)

		wantPaths, wantErr := r.Git.GetFilePathsChangedByCommit(mergeID)
		gotPaths, gotErr := got.GetFilePathsChangedByCommit(mergeID)
		requireSame(t, name, wantPaths, wantErr, gotPaths, gotErr)
	}
}

func TestVerificationMatchesGitinterface(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	got := open(t, sc.Dir)

	for _, ref := range []string{"refs/heads/main", "refs/heads/feature", "refs/heads/rogue", "refs/heads/fixed", "refs/tags/v1.0.0", testutil.CheckpointRef(testutil.CheckpointULID)} {
		wantTip, wantErr := policy.VerifyRef(t.Context(), sc.Git, ref)
		gotTip, gotErr := policy.VerifyRef(t.Context(), got, ref)
		requireSame(t, "VerifyRef "+ref, wantTip, wantErr, gotTip, gotErr)

		first, _, err := rsl.GetFirstReferenceUpdaterEntryForRef(sc.Git, ref)
		require.NoError(t, err)
		wantTip, wantErr = policy.VerifyRefFromEntry(t.Context(), sc.Git, ref, first.GetID())
		gotTip, gotErr = policy.VerifyRefFromEntry(t.Context(), got, ref, first.GetID())
		requireSame(t, "VerifyRefFromEntry "+ref, wantTip, wantErr, gotTip, gotErr)
	}
}

func TestWritesAreRefused(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	tip := r.Commit("refs/heads/main", "only")
	s := open(t, r.Dir)

	_, err := s.WriteBlob([]byte("x"))
	require.ErrorIs(t, err, ErrReadOnly)
	_, err = s.WriteTree(nil)
	require.ErrorIs(t, err, ErrReadOnly)
	_, err = s.Commit(tip, "refs/heads/main", "m", false)
	require.ErrorIs(t, err, ErrReadOnly)
	_, err = s.CommitUsingSpecificKey(tip, "refs/heads/main", "m", nil)
	require.ErrorIs(t, err, ErrReadOnly)
	_, err = s.GetMergeTree(tip, tip)
	require.ErrorIs(t, err, ErrReadOnly)
	require.ErrorIs(t, s.SetReference("refs/heads/x", tip), ErrReadOnly)
	require.ErrorIs(t, s.DeleteReference("refs/heads/main"), ErrReadOnly)
	require.ErrorIs(t, s.CheckAndSetReference("refs/heads/main", tip, tip), ErrReadOnly)
	require.ErrorIs(t, s.CheckAndDeleteReference("refs/heads/main", tip), ErrReadOnly)

	cause := errors.New("cause")
	err = s.ResetDueToError(cause, "refs/heads/main", tip)
	require.ErrorIs(t, err, cause)
	require.ErrorIs(t, err, ErrReadOnly)

	stillTip, err := r.Git.GetReference("refs/heads/main")
	require.NoError(t, err)
	assert.Equal(t, tip, stillTip)
}

func TestGetReferenceRejectsRevisionExpressions(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Commit("refs/heads/main", "only")
	s := open(t, r.Dir)

	for _, name := range []string{"main", "main~1", "refs/heads/main..x", "-main", ""} {
		_, err := s.GetReference(name)
		require.ErrorIs(t, err, ErrNotFullReference, name)
	}
}

func TestLookupConfig(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	gitCmd(t, r.Dir, "config", "gitsign.rekor", "https://rekor.example")
	s := open(t, r.Dir)

	for _, key := range []gitstore.ConfigKey{gitstore.ConfigUserName, gitstore.ConfigGPGFormat, gitstore.ConfigGitsignRekor, gitstore.ConfigCacheAutomatic, "remote.origin.url"} {
		want, wantOK, wantErr := r.Git.LookupConfig(key)
		got, gotOK, gotErr := s.LookupConfig(key)
		requireSame(t, "LookupConfig "+string(key), want, wantErr, got, gotErr)
		assert.Equal(t, wantOK, gotOK, string(key))
	}
}

func TestReferencesAndCommitInfo(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	s := open(t, sc.Dir)

	refs, err := s.References("refs/heads/")
	require.NoError(t, err)
	var names []string
	for _, ref := range refs {
		names = append(names, ref.Name)
	}
	assert.Equal(t, []string{"refs/heads/feature", "refs/heads/fixed", "refs/heads/main", "refs/heads/rogue"}, names)

	info, err := s.CommitInfo(sc.TrailerCommit)
	require.NoError(t, err)
	assert.Equal(t, "Jane Doe", info.Committer.Name)
	assert.Contains(t, info.Message, "Entire-Checkpoint: "+testutil.CheckpointULID)
}

func TestOversizedBlobIsRefused(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	blob, err := r.Git.WriteBlob(make([]byte, 1024))
	require.NoError(t, err)
	s := open(t, r.Dir)
	s.limits.blob = 1023

	_, err = s.ReadBlob(blob)

	require.ErrorIs(t, err, ErrObjectTooLarge)
	var objErr *ObjectError
	require.ErrorAs(t, err, &objErr)
}

func TestOversizedNestedTreeIsRefusedWhenDiffing(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	parent := r.Commit("refs/heads/main", "parent")

	var entries []gitinterface.TreeEntry
	for i := range 50 {
		blob, err := r.Git.WriteBlob([]byte{byte(i)})
		require.NoError(t, err)
		entries = append(entries, gitinterface.NewEntryBlob("dir/sub/file"+strings.Repeat("x", i), blob))
	}
	tree, err := r.Git.WriteTree(entries)
	require.NoError(t, err)
	merge := gitCmd(t, r.Dir, "commit-tree", tree.String(), "-p", parent.String(), "-m", "big subtree")
	commit, err := githash.NewHash(merge)
	require.NoError(t, err)

	s := open(t, r.Dir)
	s.limits.tree = 512

	_, err = s.GetFilePathsChangedByCommit(commit)

	require.ErrorIs(t, err, ErrObjectTooLarge)
}

func TestWalksStopWhenContextIsDone(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	s := open(t, sc.Dir).WithContext(cancelled(t))

	_, err := s.GetCommitsBetweenRange(sc.TrailerCommit, nil)
	require.ErrorIs(t, err, context.Canceled)

	_, err = s.KnowsCommit(sc.TrailerCommit, sc.MainFirst)
	require.ErrorIs(t, err, context.Canceled)

	policyTip, err := sc.Git.GetReference(policy.Ref)
	require.NoError(t, err)
	_, err = s.GetFilePathsChangedByCommit(policyTip)
	require.ErrorIs(t, err, context.Canceled)
}

func cancelled(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}
