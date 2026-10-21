package refresolve

import (
	"errors"
	"testing"

	"github.com/entireio/entire-rsl/internal/refname"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchAndTag(t *testing.T) {
	t.Parallel()

	got, err := Branch("main")
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/main", got)

	got, err = Branch("refs/heads/feature/x")
	require.NoError(t, err)
	assert.Equal(t, "refs/heads/feature/x", got)

	got, err = Tag("v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "refs/tags/v1.0.0", got)

	_, err = Branch("refs/tags/v1")
	require.ErrorIs(t, err, ErrWrongNamespace)
	_, err = Tag("refs/heads/main")
	require.ErrorIs(t, err, ErrWrongNamespace)
	_, err = Branch("HEAD")
	require.ErrorIs(t, err, ErrWrongNamespace)
	_, err = Branch("refs/heads/")
	require.ErrorIs(t, err, refname.ErrInvalid)
	_, err = Branch("-x")
	require.ErrorIs(t, err, refname.ErrInvalid)
	_, err = Tag("a..b")
	require.ErrorIs(t, err, refname.ErrInvalid)
}

func existing(refs ...string) Exists {
	set := map[string]bool{}
	for _, r := range refs {
		set[r] = true
	}
	return func(ref string) (bool, error) { return set[ref], nil }
}

func TestResolve(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		exists  Exists
		want    string
		wantErr error
	}{
		{name: "full name as is", input: "refs/heads/main", exists: existing(), want: "refs/heads/main"},
		{name: "HEAD as is", input: "HEAD", exists: existing(), want: "HEAD"},
		{name: "branch", input: "main", exists: existing("refs/heads/main"), want: "refs/heads/main"},
		{name: "tag", input: "v1", exists: existing("refs/tags/v1"), want: "refs/tags/v1"},
		{name: "remote", input: "origin/main", exists: existing("refs/remotes/origin/main"), want: "refs/remotes/origin/main"},
		{name: "remote head", input: "origin", exists: existing("refs/remotes/origin/HEAD"), want: "refs/remotes/origin/HEAD"},
		{name: "refs relative", input: "heads/main", exists: existing("refs/heads/main"), want: "refs/heads/main"},
		{name: "branch and tag", input: "x", exists: existing("refs/heads/x", "refs/tags/x"), wantErr: ErrAmbiguous},
		{name: "missing", input: "nope", exists: existing(), wantErr: ErrNotFound},
		{name: "invalid", input: "-x", exists: existing(), wantErr: refname.ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Resolve(tt.input, tt.exists)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveAmbiguityListsCandidates(t *testing.T) {
	t.Parallel()

	_, err := Resolve("x", existing("refs/heads/x", "refs/tags/x"))

	require.ErrorContains(t, err, "refs/tags/x, refs/heads/x")
}

func TestResolvePropagatesLookupErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	_, err := Resolve("x", func(string) (bool, error) { return false, boom })

	require.ErrorIs(t, err, boom)
}
