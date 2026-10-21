package cli

import (
	"encoding/json"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchAndTagGolden(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	tests := []struct {
		name string
		args []string
	}{
		{name: "branch_main.txt", args: []string{"branch", "main"}},
		{name: "branch_main.json", args: []string{"branch", "--json", "refs/heads/main"}},
		{name: "branch_fixed.txt", args: []string{"branch", "fixed"}},
		{name: "branch_main_history1.txt", args: []string{"branch", "--history", "1", "main"}},
		{name: "branch_never.txt", args: []string{"branch", "never"}},
		{name: "branch_never.json", args: []string{"branch", "--json", "never"}},
		{name: "tag_moved.txt", args: []string{"tag", "v1.0.0"}},
		{name: "tag_moved.json", args: []string{"tag", "--json", "v1.0.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, inRepo(sc.Dir), tt.args...)

			require.Equal(t, ExitOK, res.code, res.stderr)
			assert.Empty(t, res.stderr)
			golden(t, tt.name, res.stdout)
		})
	}
}

func warningCodes(t *testing.T, stdout string) []string {
	t.Helper()

	var doc struct {
		Warnings []warning `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	codes := []string{}
	for _, w := range doc.Warnings {
		codes = append(codes, w.Code)
	}
	return codes
}

func TestBranchWarnsOnLocalDrift(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	sc.SetRef("refs/heads/main", sc.MainFirst)

	res := execute(t, inRepo(sc.Dir), "branch", "--json", "main")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Equal(t, []string{warnLocalDrift}, warningCodes(t, res.stdout))

	text := execute(t, inRepo(sc.Dir), "branch", "main")
	assert.Contains(t, text.stdout, "Warning: local refs/heads/main is at "+sc.MainFirst.String()[:12]+" but the RSL records "+sc.TrailerCommit.String()[:12])
}

func TestBranchWarnsWhenMissingLocally(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	require.NoError(t, sc.Git.DeleteReference("refs/heads/feature"))

	res := execute(t, inRepo(sc.Dir), "branch", "--json", "feature")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Equal(t, []string{warnLocalMissing}, warningCodes(t, res.stdout))
}

func TestTagUnmovedHasNoWarning(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Bootstrap()
	c := r.Commit("refs/heads/main", "only")
	r.SetRef("refs/tags/v1", c)
	r.Record(r.Cluster, "refs/tags/v1", nil)

	res := execute(t, inRepo(r.Dir), "tag", "--json", "v1")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Empty(t, warningCodes(t, res.stdout))
}

func TestBranchAndTagUsageErrors(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	for name, args := range map[string][]string{
		"branch in tags":   {"branch", "refs/tags/v1.0.0"},
		"tag in heads":     {"tag", "refs/heads/main"},
		"option injection": {"branch", "--", "-main"},
		"bad name":         {"tag", "a..b"},
		"no name":          {"branch"},
		"two names":        {"branch", "a", "b"},
		"history negative": {"branch", "--history", "-1", "main"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, inRepo(sc.Dir), args...)

			assert.Equal(t, ExitUsage, res.code, res.stderr)
			assert.Empty(t, res.stdout)
		})
	}
}
