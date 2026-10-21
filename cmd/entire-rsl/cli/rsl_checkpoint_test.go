package cli

import (
	"encoding/json"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckpointGolden(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	tests := []struct {
		name string
		args []string
	}{
		{name: "checkpoint_ulid.txt", args: []string{"checkpoint", testutil.CheckpointULID}},
		{name: "checkpoint_ulid.json", args: []string{"checkpoint", "--json", testutil.CheckpointULID}},
		{name: "checkpoint_from_commit.txt", args: []string{"checkpoint", sc.TrailerCommit.String()}},
		{name: "checkpoint_from_commit.json", args: []string{"checkpoint", "--json", sc.TrailerCommit.String()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, inRepo(sc.Dir), tt.args...)

			require.Equal(t, ExitOK, res.code, res.stderr)
			golden(t, tt.name, res.stdout)
		})
	}
}

func TestCheckpointErrors(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	tests := []struct {
		name     string
		arg      string
		wantCode int
		wantErr  string
	}{
		{name: "not an id", arg: "main", wantCode: ExitUsage, wantErr: "not a checkpoint ID"},
		{name: "lowercase ulid", arg: "01jc0ffee0badc0de00000007z", wantCode: ExitUsage, wantErr: "not a checkpoint ID"},
		{name: "abbreviated sha", arg: sc.TrailerCommit.String()[:20], wantCode: ExitUsage, wantErr: "not a checkpoint ID"},
		{name: "unknown checkpoint", arg: testutil.CheckpointHex, wantCode: ExitFailure, wantErr: "checkpoint not found"},
		{name: "commit without trailer", arg: sc.MainFirst.String(), wantCode: ExitFailure, wantErr: "no Entire-Checkpoint trailer"},
		{name: "unknown commit", arg: "0123456789abcdef0123456789abcdef01234567", wantCode: ExitFailure, wantErr: "read commit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, inRepo(sc.Dir), "checkpoint", tt.arg)

			assert.Equal(t, tt.wantCode, res.code, res.stderr)
			assert.Contains(t, res.stderr, tt.wantErr)
			assert.Empty(t, res.stdout)
		})
	}
}

func TestCheckpointLocalRefWithDifferentShardCase(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Bootstrap()
	tip := r.Commit("refs/entire/checkpoints/7z/"+testutil.CheckpointULID, "checkpoint")

	res := execute(t, inRepo(r.Dir), "checkpoint", "--json", testutil.CheckpointULID)
	require.Equal(t, ExitOK, res.code, res.stderr)

	var doc struct {
		LocalTip string    `json:"local_tip"`
		Ref      string    `json:"ref"`
		Warnings []warning `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
	assert.Equal(t, tip.String(), doc.LocalTip)
	assert.Equal(t, testutil.CheckpointRef(testutil.CheckpointULID), doc.Ref, "the canonical ref is reported")
	require.Len(t, doc.Warnings, 1)
	assert.Equal(t, warnNoRSLEntries, doc.Warnings[0].Code)
}

func TestCheckpointCommitWithSeveralTrailers(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	c := r.Commit("refs/heads/main", "squash\n\nEntire-Checkpoint: "+testutil.CheckpointHex+"\nEntire-Checkpoint: "+testutil.CheckpointULID)
	r.Bootstrap()

	res := execute(t, inRepo(r.Dir), "checkpoint", c.String())

	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, testutil.CheckpointHex+", "+testutil.CheckpointULID)
}
