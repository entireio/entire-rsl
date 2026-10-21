package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyGolden(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{name: "verify_main.txt", args: []string{"verify", "main"}, wantCode: ExitOK},
		{name: "verify_main.json", args: []string{"verify", "--json", "refs/heads/main"}, wantCode: ExitOK},
		{name: "verify_fixed.txt", args: []string{"verify", "fixed"}, wantCode: ExitOK},
		{name: "verify_checkpoint.txt", args: []string{"verify", testutil.CheckpointULID}, wantCode: ExitOK},
		{name: "verify_checkpoint_commit.txt", args: []string{"verify", sc.TrailerCommit.String()}, wantCode: ExitOK},
		{name: "verify_rogue.txt", args: []string{"verify", "rogue"}, wantCode: ExitVerification},
		{name: "verify_rogue.json", args: []string{"verify", "--json", "rogue"}, wantCode: ExitVerification},
		{name: "verify_tag_moved.txt", args: []string{"verify", "v1.0.0"}, wantCode: ExitVerification},
		{name: "verify_unprotected.txt", args: []string{"verify", "refs/heads/never"}, wantCode: ExitVerification},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, inRepo(sc.Dir), tt.args...)

			require.Equal(t, tt.wantCode, res.code, res.stderr)
			assert.Empty(t, res.stderr, "the result is on stdout and the exit code carries it")
			golden(t, tt.name, res.stdout)
		})
	}
}

func TestVerifyErrorExitsOne(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	id := sc.MainFirstEntry.String()
	path := filepath.Join(sc.Dir, ".git", "objects", id[:2], id[2:])
	require.NoError(t, os.Chmod(path, 0o600))
	require.NoError(t, os.Remove(path))

	res := execute(t, inRepo(sc.Dir), "verify", "--json", "refs/heads/main")

	assert.Equal(t, ExitFailure, res.code, res.stderr)
	var doc verifyJSON
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
	assert.Equal(t, "error", doc.Verification.State)
}

func TestVerifyResolution(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	sc.SetRef("refs/tags/main", sc.MainFirst)
	sc.SetRef("refs/heads/"+testutil.CheckpointULID, sc.MainFirst)

	tests := []struct {
		name     string
		target   string
		wantCode int
		wantErr  string
	}{
		{name: "branch and tag share a name", target: "main", wantCode: ExitUsage, wantErr: "refs/tags/main, refs/heads/main"},
		{name: "checkpoint id is also a branch", target: testutil.CheckpointULID, wantCode: ExitUsage, wantErr: "is both a ref"},
		{name: "nothing matches", target: "nope", wantCode: ExitFailure, wantErr: "no ref matches"},
		{name: "invalid name", target: "a..b", wantCode: ExitUsage, wantErr: "invalid ref name"},
		{name: "option injection", target: "-x", wantCode: ExitUsage, wantErr: "must not begin with '-'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, inRepo(sc.Dir), "verify", "--", tt.target)

			assert.Equal(t, tt.wantCode, res.code, res.stderr)
			assert.Contains(t, res.stderr, tt.wantErr)
		})
	}

	res := execute(t, inRepo(sc.Dir), "verify", "refs/heads/main")
	assert.Equal(t, ExitOK, res.code, "a full name is never ambiguous: %s", res.stderr)
}

func TestVerifyResolutionFailureIsNotUsage(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	id := sc.MainFirstEntry.String()
	path := filepath.Join(sc.Dir, ".git", "objects", id[:2], id[2:])
	require.NoError(t, os.Chmod(path, 0o600))
	require.NoError(t, os.Remove(path))

	res := execute(t, inRepo(sc.Dir), "verify", "main")

	assert.Equal(t, ExitFailure, res.code, "an unreadable RSL is an error, not a usage mistake: %s", res.stderr)
}

func TestDetailCommandsVerifyByDefault(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	for _, args := range [][]string{{"branch", "--json", "main"}, {"tag", "--json", "v1.0.0"}, {"checkpoint", "--json", testutil.CheckpointULID}} {
		res := execute(t, inRepo(sc.Dir), args...)
		require.Equal(t, ExitOK, res.code, res.stderr)

		var doc refReportJSON
		require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
		require.NotNil(t, doc.Verification, args)
		assert.NotEmpty(t, doc.Verification.State, args)
	}

	res := execute(t, inRepo(sc.Dir), "branch", "--json", "--no-verify", "main")
	require.Equal(t, ExitOK, res.code)
	assert.Contains(t, res.stdout, `"verification": null`)

	res = execute(t, inRepo(sc.Dir), "branch", "--no-verify", "main")
	assert.Contains(t, res.stdout, "skipped (--no-verify)")

	res = execute(t, inRepo(sc.Dir), "branch", "--timeout", "0s", "main")
	assert.Equal(t, ExitUsage, res.code)
}
