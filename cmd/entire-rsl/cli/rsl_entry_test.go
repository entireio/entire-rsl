package cli

import (
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntryGolden(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	tests := []struct {
		name string
		args []string
	}{
		{name: "entry_main.txt", args: []string{"entry", sc.MainLastEntry.String()}},
		{name: "entry_main.json", args: []string{"entry", "--json", sc.MainLastEntry.String()}},
		{name: "entry_feature_actor.txt", args: []string{"entry", sc.FeatureEntry.String()[:10]}},
		{name: "entry_revoked.txt", args: []string{"entry", sc.RevokedEntry.String()}},
		{name: "entry_annotation.txt", args: []string{"entry", sc.RevokeAnnot.String()}},
		{name: "entry_annotation.json", args: []string{"entry", "--json", sc.RevokeAnnot.String()}},
		{name: "entry_first.txt", args: []string{"entry", "1"}},
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

func TestEntryErrors(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	res := execute(t, inRepo(sc.Dir), "entry", "zzz")
	assert.Equal(t, ExitUsage, res.code)

	res = execute(t, inRepo(sc.Dir), "entry")
	assert.Equal(t, ExitUsage, res.code)

	res = execute(t, inRepo(sc.Dir), "entry", "999")
	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, "RSL entry not found")

	res = execute(t, inRepo(sc.Dir), "entry", sc.MainFirst.String())
	assert.Equal(t, ExitFailure, res.code, "a commit outside the RSL is not an entry")
}
