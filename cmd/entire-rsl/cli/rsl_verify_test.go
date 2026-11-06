package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/entireio/entire-rsl/internal/verify"
	"github.com/gittuf/gittuf/pkg/githash"
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
		{name: "verify_rogue.txt", args: []string{"verify", "rogue"}, wantCode: ExitVerification},
		{name: "verify_rogue.json", args: []string{"verify", "--json", "rogue"}, wantCode: ExitVerification},
		{name: "verify_tag_moved.txt", args: []string{"verify", "v1.0.0"}, wantCode: ExitOK},
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

func TestVerifyCommitGolden(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	trailer := "\n\nEntire-Checkpoint: " + testutil.CheckpointULID
	onRogue := sc.Commit("refs/heads/rogue", "rogue claims a checkpoint"+trailer)
	sc.Record(sc.Rogue, "refs/heads/rogue", nil)
	dangling := sc.Commit("refs/heads/scratch", "dangling claims a checkpoint"+trailer)
	sc.SetRef("refs/heads/scratch", sc.MainFirst)
	sc.Commit("refs/heads/healed", "healed work")
	sc.Record(sc.Cluster, "refs/heads/healed", nil)
	revoked := sc.Commit("refs/heads/healed", "rogue work on healed")
	sc.Annotate(sc.Cluster, true, "revoked: signed by an unknown key", sc.Record(sc.Rogue, "refs/heads/healed", nil))
	sc.Commit("refs/heads/healed", "healed work")
	sc.Record(sc.Cluster, "refs/heads/healed", nil)

	tests := []struct {
		name     string
		args     []string
		wantCode int
	}{
		{name: "verify_commit_tip.txt", args: []string{"verify", sc.TrailerCommit.String()}, wantCode: ExitOK},
		{name: "verify_commit_tip.json", args: []string{"verify", "--json", sc.TrailerCommit.String()}, wantCode: ExitOK},
		{name: "verify_commit_behind.txt", args: []string{"verify", sc.MainFirst.String()}, wantCode: ExitOK},
		{name: "verify_commit_rogue.txt", args: []string{"verify", onRogue.String()}, wantCode: ExitVerification},
		{name: "verify_commit_rogue.json", args: []string{"verify", "--json", onRogue.String()}, wantCode: ExitVerification},
		{name: "verify_commit_dangling.txt", args: []string{"verify", dangling.String()}, wantCode: ExitVerification},
		{name: "verify_commit_skipped.txt", args: []string{"verify", sc.RevokedCommit.String()}, wantCode: ExitVerification},
		{name: "verify_commit_revoked.txt", args: []string{"verify", revoked.String()}, wantCode: ExitVerification},
		{name: "verify_commit_revoked.json", args: []string{"verify", "--json", revoked.String()}, wantCode: ExitVerification},
		{name: "verify_healed.txt", args: []string{"verify", "healed"}, wantCode: ExitOK},
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

func TestVerifyCommitNeedsItsCheckpoint(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	commit := sc.Commit("refs/heads/main", "fourth\n\nEntire-Checkpoint: "+testutil.CheckpointHex)
	sc.Record(sc.Cluster, "refs/heads/main", nil)

	res := execute(t, inRepo(sc.Dir), "verify", "--json", commit.String())

	assert.Equal(t, ExitVerification, res.code, res.stderr)
	var doc verifyCommitJSON
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
	assert.Equal(t, "unprotected", doc.Verification.State)
	require.Len(t, doc.ContainedIn, 1)
	require.NotNil(t, doc.ContainedIn[0].Verification)
	assert.Equal(t, "verified", doc.ContainedIn[0].Verification.State)
	require.NotNil(t, doc.Checkpoint)
	assert.Equal(t, testutil.CheckpointHex, doc.Checkpoint.ID)
	assert.Equal(t, "unprotected", doc.Checkpoint.Verification.State)
}

func TestVerifyCommitWithUnknownContainment(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	sc.RecordAt(sc.Cluster, "refs/heads/ghost", mustHash(t, "0123456789abcdef0123456789abcdef01234567"), nil)
	dangling := sc.Commit("refs/heads/scratch", "dangling")
	sc.SetRef("refs/heads/scratch", sc.MainFirst)

	res := execute(t, inRepo(sc.Dir), "verify", "--json", dangling.String())

	assert.Equal(t, ExitFailure, res.code, "a ref that may hold the commit makes the outcome an error: %s", res.stderr)
	var doc verifyCommitJSON
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
	assert.Equal(t, "error", doc.Verification.State)
	require.Len(t, doc.UncheckedRefs, 1)
	assert.Contains(t, doc.UncheckedRefs[0], "refs/heads/ghost")

	res = execute(t, inRepo(sc.Dir), "verify", sc.MainFirst.String())
	assert.Equal(t, ExitOK, res.code, "a verified container decides even when another ref could not be compared: %s", res.stderr)
}

func TestVerifyCommitErrors(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	twoTrailers := sc.Commit("refs/heads/main", "fifth\n\nEntire-Checkpoint: "+testutil.CheckpointULID+"\nEntire-Checkpoint: "+testutil.CheckpointHex)
	sc.Record(sc.Cluster, "refs/heads/main", nil)

	tests := []struct {
		name    string
		target  string
		wantErr string
	}{
		{name: "unknown commit", target: "0123456789abcdef0123456789abcdef01234567", wantErr: "read commit 0123456789abcdef0123456789abcdef01234567"},
		{name: "several trailers", target: twoTrailers.String(), wantErr: "several Entire-Checkpoint trailers"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, inRepo(sc.Dir), "verify", tt.target)

			assert.Equal(t, ExitFailure, res.code, res.stderr)
			assert.Empty(t, res.stdout)
			assert.Contains(t, res.stderr, tt.wantErr)
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

func TestCheckedText(t *testing.T) {
	t.Parallel()

	first := rslread.Target{Number: 3, ID: mustHash(t, "d424aa2b5963fed76767d1335a83938adc61a300")}
	last := rslread.Target{Number: 9, ID: mustHash(t, "2f3106dcfec619b4807b8fc24927c954cf17565e")}
	legacy := rslread.Target{ID: mustHash(t, "0123456789abcdef0123456789abcdef01234567")}

	assert.Equal(t, "#3 d424aa2b5963 → #9 2f3106dcfec6 (3 entries)", checkedText(verify.Checked{First: first, Last: last, Count: 3}))
	assert.Equal(t, "#3 d424aa2b5963 → #9 2f3106dcfec6 (3 entries, 1 skipped)", checkedText(verify.Checked{First: first, Last: last, Count: 3, Skipped: 1}))
	assert.Equal(t, "#9 2f3106dcfec6 (1 entry)", checkedText(verify.Checked{First: last, Last: last, Count: 1}))
	assert.Equal(t, "0123456789ab → #9 2f3106dcfec6 (2 entries)", checkedText(verify.Checked{First: legacy, Last: last, Count: 2}))
}

func mustHash(t *testing.T, s string) githash.Hash {
	t.Helper()

	h, err := githash.NewHash(s)
	require.NoError(t, err)
	return h
}
