package repo

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	cleanup := testutil.IsolateGitConfig()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func env(vars map[string]string, cwd string) Env {
	return Env{
		Getenv: func(k string) string { return vars[k] },
		Getwd:  func() (string, error) { return cwd, nil },
	}
}

func discard() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestOpenDiscoversFromSubdirectory(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Commit("refs/heads/main", "only")
	sub := filepath.Join(r.Dir, "a", "b")
	require.NoError(t, os.MkdirAll(sub, 0o750))

	got, err := Open(t.Context(), env(nil, sub), discard())

	require.NoError(t, err)
	assert.True(t, sameDir(r.Dir, got.Root))
	_, err = got.Store.GetReference("refs/heads/main")
	require.NoError(t, err)
}

func TestOpenLinkedWorktree(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Commit("refs/heads/main", "only")
	wt := r.AddWorktree(t, "main")

	for name, e := range map[string]Env{
		"discovered":       env(nil, wt),
		"ENTIRE_REPO_ROOT": env(map[string]string{EnvRepoRoot: wt}, t.TempDir()),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := Open(t.Context(), e, discard())

			require.NoError(t, err)
			assert.True(t, sameDir(wt, got.Root))
			assert.True(t, sameDir(filepath.Join(r.Dir, ".git"), got.CommonDir), "the common git directory holds the config, objects and shared refs: %s", got.CommonDir)
		})
	}
}

func TestOpenCommonDirOfMainWorktree(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)

	got, err := Open(t.Context(), env(nil, r.Dir), discard())

	require.NoError(t, err)
	assert.True(t, sameDir(filepath.Join(r.Dir, ".git"), got.CommonDir))
}

func TestOpenPrefersEntireRepoRoot(t *testing.T) {
	t.Parallel()

	target := testutil.NewRepo(t)
	other := testutil.NewRepo(t)

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	got, err := Open(t.Context(), env(map[string]string{EnvRepoRoot: target.Dir}, other.Dir), logger)

	require.NoError(t, err)
	assert.True(t, sameDir(target.Dir, got.Root))
	assert.Contains(t, logs.String(), "ENTIRE_REPO_ROOT differs")
}

func TestOpenRejectsRelativeEntireRepoRoot(t *testing.T) {
	t.Parallel()

	_, err := Open(t.Context(), env(map[string]string{EnvRepoRoot: "relative/path"}, t.TempDir()), discard())

	require.ErrorContains(t, err, "must be an absolute path")
}

func TestOpenOutsideRepository(t *testing.T) {
	t.Parallel()

	_, err := Open(t.Context(), env(nil, t.TempDir()), discard())

	require.ErrorIs(t, err, ErrNotRepository)
}

func TestOpenEntireRepoRootThatIsNotARepository(t *testing.T) {
	t.Parallel()

	_, err := Open(t.Context(), env(map[string]string{EnvRepoRoot: t.TempDir()}, t.TempDir()), discard())

	require.ErrorIs(t, err, ErrNotRepository)
}
