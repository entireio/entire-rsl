package rslread

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/entire-rsl/internal/store"
	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/gittuf/gittuf/pkg/githash"
	gogit "github.com/go-git/go-git/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	testutil.IsolateGitConfig()
	os.Exit(m.Run())
}

func openStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	return openStoreTB(t, dir)
}

func openStoreTB(t testing.TB, dir string) *store.Store {
	t.Helper()

	g, err := gogit.PlainOpen(dir)
	require.NoError(t, err)
	st, err := store.New(g)
	require.NoError(t, err)
	return st
}

func openReader(t *testing.T, dir string) *Reader {
	t.Helper()

	r, err := Open(openStore(t, dir))
	require.NoError(t, err)
	return r
}

func ids(entries []Entry) []githash.Hash {
	out := make([]githash.Hash, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

func TestOpenWithoutRSL(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Commit("refs/heads/main", "only")

	_, err := Open(openStore(t, r.Dir))

	require.ErrorIs(t, err, ErrNoRSL)
}

func TestLogNewestFirstWithAnnotations(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	r := openReader(t, sc.Dir)

	entries, err := r.Log(t.Context(), LogOptions{Limit: 100})
	require.NoError(t, err)

	require.Equal(t, sc.FixedEntry, entries[0].ID)
	assert.Equal(t, r.Tip(), entries[0].ID)
	for i := 1; i < len(entries); i++ {
		assert.Greater(t, entries[i-1].Number, entries[i].Number, "numbers decrease")
	}

	byID := map[string]Entry{}
	for _, e := range entries {
		byID[e.ID.String()] = e
	}

	annot := byID[sc.RevokeAnnot.String()]
	assert.Equal(t, KindAnnotation, annot.Kind)
	assert.True(t, annot.Skip)
	assert.Equal(t, "revoked: signed by an unknown key", annot.Message)
	assert.Equal(t, []githash.Hash{sc.RevokedEntry}, annot.Annotates)

	revoked := byID[sc.RevokedEntry.String()]
	assert.True(t, revoked.Skipped())
	assert.Equal(t, []githash.Hash{sc.RevokeAnnot}, revoked.SkippedBy)

	main := byID[sc.MainLastEntry.String()]
	assert.Equal(t, KindReference, main.Kind)
	assert.Equal(t, "refs/heads/main", main.RefName)
	assert.Equal(t, sc.TrailerCommit, main.TargetID)
	assert.Equal(t, testutil.PusherJane, main.CustomFields["custom.entire.io/pusher"])
	assert.False(t, main.Time.IsZero())
	assert.Equal(t, "Jane Doe", main.Committer.Name)
}

func TestSignatureAttribution(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	r := openReader(t, sc.Dir)

	entries, err := r.Log(t.Context(), LogOptions{Limit: 100})
	require.NoError(t, err)
	byID := map[string]Entry{}
	for _, e := range entries {
		byID[e.ID.String()] = e
	}

	cluster := byID[sc.MainLastEntry.String()].Signature
	assert.Equal(t, Signature{Method: MethodSSH, KeyID: sc.Cluster.Fingerprint(t), Valid: true}, cluster)

	rogue := byID[sc.RogueEntry.String()].Signature
	assert.Equal(t, Signature{Method: MethodSSH, KeyID: sc.Rogue.Fingerprint(t), Valid: true}, rogue)

	policyEntry := entries[len(entries)-1].Signature
	assert.Equal(t, MethodNone, policyEntry.Method)
	assert.False(t, policyEntry.Valid)
	assert.Empty(t, policyEntry.KeyID)
}

func TestTamperedPayloadIsNotAttributed(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	payload, sig, err := sc.Git.GetObjectSignature(sc.MainLastEntry)
	require.NoError(t, err)

	got := checkSignature(append(payload, []byte("tampered")...), sig)

	assert.Equal(t, MethodSSH, got.Method)
	assert.False(t, got.Valid)
	assert.Empty(t, got.KeyID, "an unverified signature must not name a key")
	assert.NotEmpty(t, got.Problem)
}

func TestCheckSignatureFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sig  string
		want string
	}{
		{name: "none", sig: "", want: MethodNone},
		{name: "gpg", sig: "-----BEGIN PGP SIGNATURE-----\nabc\n-----END PGP SIGNATURE-----\n", want: MethodGPG},
		{name: "x509", sig: "-----BEGIN SIGNED MESSAGE-----\nabc\n-----END SIGNED MESSAGE-----\n", want: MethodX509},
		{name: "garbage", sig: "hello", want: MethodUnknown},
		{name: "malformed ssh", sig: "-----BEGIN SSH SIGNATURE-----\nnotbase64\n-----END SSH SIGNATURE-----\n", want: MethodSSH},
		{name: "two blocks", sig: "-----BEGIN SSH SIGNATURE-----\na\n-----END SSH SIGNATURE-----\n-----BEGIN SSH SIGNATURE-----\nb\n-----END SSH SIGNATURE-----\n", want: MethodUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := checkSignature([]byte("payload"), []byte(tt.sig))

			assert.Equal(t, tt.want, got.Method)
			assert.False(t, got.Valid)
			assert.Empty(t, got.KeyID)
		})
	}
}

func TestLogFilters(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	r := openReader(t, sc.Dir)

	main, err := r.Log(t.Context(), LogOptions{Ref: "refs/heads/main", Limit: 100})
	require.NoError(t, err)
	require.Len(t, main, 3)
	assert.Equal(t, sc.MainLastEntry, main[0].ID)
	assert.Equal(t, sc.MainFirstEntry, main[2].ID)

	limited, err := r.Log(t.Context(), LogOptions{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, []githash.Hash{sc.FixedEntry, sc.RevokeAnnot}, ids(limited))

	before, err := ParseSelector(sc.RevokeAnnot.String())
	require.NoError(t, err)
	below, err := r.Log(t.Context(), LogOptions{Before: &before, Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, []githash.Hash{sc.RevokedEntry}, ids(below))

	missing, err := ParseSelector("ffffffffff")
	require.NoError(t, err)
	_, err = r.Log(t.Context(), LogOptions{Before: &missing, Limit: 1})
	require.ErrorIs(t, err, ErrEntryNotFound)
}

func TestFind(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	r := openReader(t, sc.Dir)

	byID, err := ParseSelector(sc.CheckpointEntry.String())
	require.NoError(t, err)
	e, err := r.Find(t.Context(), byID)
	require.NoError(t, err)
	assert.Equal(t, testutil.CheckpointRef(testutil.CheckpointULID), e.RefName)

	byPrefix, err := ParseSelector(sc.CheckpointEntry.String()[:10])
	require.NoError(t, err)
	e, err = r.Find(t.Context(), byPrefix)
	require.NoError(t, err)
	assert.Equal(t, sc.CheckpointEntry, e.ID)

	byNumber, err := ParseSelector("1")
	require.NoError(t, err)
	first, err := r.Find(t.Context(), byNumber)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), first.Number)

	unknown, err := ParseSelector("0123456789abcdef0123456789abcdef01234567")
	require.NoError(t, err)
	_, err = r.Find(t.Context(), unknown)
	require.ErrorIs(t, err, ErrEntryNotFound)
}

func TestFindRefusesCommitsOutsideTheRSL(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	r := openReader(t, sc.Dir)

	// MainFirst is a real commit, but not an RSL entry.
	sel, err := ParseSelector(sc.MainFirst.String())
	require.NoError(t, err)
	_, err = r.Find(t.Context(), sel)

	require.ErrorIs(t, err, ErrEntryNotFound)
}

func TestHistory(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	r := openReader(t, sc.Dir)

	tag, err := r.History(t.Context(), "refs/tags/v1.0.0", 10)
	require.NoError(t, err)
	assert.Equal(t, 2, tag.Total)
	assert.Equal(t, 2, tag.Unskipped)
	assert.Equal(t, []githash.Hash{sc.TagMovedEntry, sc.TagFirstEntry}, ids(tag.Entries))

	fixed, err := r.History(t.Context(), "refs/heads/fixed", 1)
	require.NoError(t, err)
	assert.Equal(t, 3, fixed.Total)
	assert.Equal(t, 2, fixed.Unskipped)
	assert.Equal(t, []githash.Hash{sc.FixedEntry}, ids(fixed.Entries))
	require.NotNil(t, fixed.Current)
	assert.Equal(t, sc.FixedEntry, fixed.Current.ID)

	beyond, err := r.History(t.Context(), "refs/heads/main", 0)
	require.NoError(t, err)
	assert.Empty(t, beyond.Entries)
	require.NotNil(t, beyond.Current, "the current entry is found beyond the limit")
	assert.Equal(t, sc.MainLastEntry, beyond.Current.ID)

	none, err := r.History(t.Context(), "refs/heads/never", 10)
	require.NoError(t, err)
	assert.Zero(t, none.Total)
	assert.Empty(t, none.Entries)
}

func TestWalkStopsOnCancelledContext(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	r := openReader(t, sc.Dir)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := r.Log(ctx, LogOptions{Limit: 100})
	require.ErrorIs(t, err, context.Canceled)
}

func TestParseSelector(t *testing.T) {
	t.Parallel()

	for _, bad := range []string{"", "0", "abc", "abcdef", "xyz1234567", "-1234567", "18446744073709551616", "a" + string(make([]byte, 70))} {
		_, err := ParseSelector(bad)
		require.ErrorIs(t, err, ErrInvalidEntryID, bad)
	}

	sel, err := ParseSelector("42")
	require.NoError(t, err)
	assert.Equal(t, "#42", sel.String())

	sel, err = ParseSelector("ABCDEF0")
	require.NoError(t, err)
	assert.Equal(t, "abcdef0", sel.String())
}

func FuzzParseSelector(f *testing.F) {
	for _, seed := range []string{"1", "42", "abcdef0", "0123456789abcdef0123456789abcdef01234567", "", "-x", "#1"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		sel, err := ParseSelector(in)
		if err != nil {
			return
		}
		if sel.byNumber {
			assert.NotZero(t, sel.number)
			return
		}
		assert.GreaterOrEqual(t, len(sel.idPrefix), minPrefix)
		assert.True(t, isHex(sel.idPrefix))
	})
}

func TestWalkFailsOnMissingParentInsteadOfStopping(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	removeLooseObject(t, sc.Dir, sc.TagFirstEntry.String())

	r := openReader(t, sc.Dir)
	_, err := r.Log(t.Context(), LogOptions{Limit: 100})

	require.Error(t, err)
	assert.Contains(t, err.Error(), sc.TagFirstEntry.String())
}

func TestWalkFailsAtShallowBoundaryInsideTheRSL(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	require.NoError(t, os.WriteFile(filepath.Join(sc.Dir, ".git", "shallow"), []byte(sc.CheckpointEntry.String()+"\n"), 0o600))

	r := openReader(t, sc.Dir)
	_, err := r.Log(t.Context(), LogOptions{Limit: 100})

	require.ErrorIs(t, err, ErrShallowRSL)
}

func TestLogBeforeRejectsAmbiguousPrefix(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	r := openReader(t, sc.Dir)

	// An empty prefix matches every entry, standing in for two entries that
	// share a seven character prefix.
	ambiguous := Selector{}
	_, err := r.Log(t.Context(), LogOptions{Before: &ambiguous, Limit: 1})

	require.ErrorIs(t, err, ErrAmbiguousEntry)
}

func removeLooseObject(t *testing.T, dir, id string) {
	t.Helper()

	path := filepath.Join(dir, ".git", "objects", id[:2], id[2:])
	require.FileExists(t, path)
	require.NoError(t, os.Chmod(path, 0o600))
	require.NoError(t, os.Remove(path))
}
