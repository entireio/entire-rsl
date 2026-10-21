package cli

import (
	"encoding/json"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogGolden(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	tests := []struct {
		name string
		args []string
	}{
		{name: "log.txt", args: []string{"log"}},
		{name: "log.json", args: []string{"log", "--json"}},
		{name: "log_ref_main.txt", args: []string{"log", "--ref", "refs/heads/main"}},
		{name: "log_limit_before.txt", args: []string{"log", "--limit", "2", "--before", sc.RevokeAnnot.String()}},
		{name: "log_no_match.txt", args: []string{"log", "--ref", "refs/heads/never"}},
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

func TestLogJSONIsValid(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	res := execute(t, inRepo(sc.Dir), "log", "--json", "--limit", "3")
	require.Equal(t, ExitOK, res.code, res.stderr)

	var doc struct {
		Entries       []map[string]any `json:"entries"`
		SchemaVersion int              `json:"schema_version"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
	assert.Equal(t, 1, doc.SchemaVersion)
	assert.Len(t, doc.Entries, 3)
}

func TestLogUsageErrors(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	for name, args := range map[string][]string{
		"limit zero":   {"log", "--limit", "0"},
		"limit huge":   {"log", "--limit", "100001"},
		"bad ref":      {"log", "--ref", "-x"},
		"bad before":   {"log", "--before", "xyz"},
		"positional":   {"log", "extra"},
		"unknown flag": {"log", "--nope"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, inRepo(sc.Dir), args...)

			assert.Equal(t, ExitUsage, res.code, res.stderr)
			assert.Empty(t, res.stdout)
		})
	}
}

func TestLogWithoutRSL(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Commit("refs/heads/main", "only")

	res := execute(t, inRepo(r.Dir), "log")

	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, "no gittuf RSL found")
	assert.Empty(t, res.stdout)
}

func TestLogOutsideRepository(t *testing.T) {
	t.Parallel()

	res := execute(t, testEnv{}, "log")

	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, "not a git repository")
}
