package cli

import (
	"regexp"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func TestVerificationIsColouredOnATerminal(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	ok := executeColor(t, inRepo(sc.Dir), "verify", "main")
	require.Equal(t, ExitOK, ok.code, ok.stderr)
	assert.Contains(t, ok.stdout, ansiGreen+"verified"+ansiReset)
	assert.Contains(t, ok.stdout, ansiGreen+"pass"+ansiReset)
	assert.NotContains(t, ok.stdout, ansiRed)

	failed := executeColor(t, inRepo(sc.Dir), "verify", "rogue")
	assert.Contains(t, failed.stdout, ansiRed+"failed"+ansiReset)

	unprotected := executeColor(t, inRepo(sc.Dir), "verify", "refs/heads/never")
	assert.Contains(t, unprotected.stdout, ansiYellow+"unprotected"+ansiReset)

	branch := executeColor(t, inRepo(sc.Dir), "branch", "main")
	assert.Contains(t, branch.stdout, ansiGreen+"SHA256:")

	entry := executeColor(t, inRepo(sc.Dir), "entry", sc.RogueEntry.String())
	assert.Contains(t, entry.stdout, ansiGreen, "a valid signature is green even from an unauthorized key")

	unsigned := executeColor(t, inRepo(sc.Dir), "entry", "1")
	assert.Contains(t, unsigned.stdout, ansiRed+"none: unsigned"+ansiReset)
}

func TestNoColourWhenAskedOrNotATerminal(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	for name, res := range map[string]result{
		"not a terminal": execute(t, inRepo(sc.Dir), "verify", "main"),
		"NO_COLOR":       executeColor(t, testEnv{"ENTIRE_REPO_ROOT": sc.Dir, "NO_COLOR": "1"}, "verify", "main"),
		"dumb terminal":  executeColor(t, testEnv{"ENTIRE_REPO_ROOT": sc.Dir, "TERM": "dumb"}, "verify", "main"),
		"accessible":     executeColor(t, testEnv{"ENTIRE_REPO_ROOT": sc.Dir, "ACCESSIBLE": "1"}, "verify", "main"),
		"json":           executeColor(t, inRepo(sc.Dir), "verify", "--json", "main"),
	} {
		assert.NotContains(t, res.stdout, "\x1b", name)
	}
}

func TestColourKeepsTablesAligned(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	for _, args := range [][]string{{"log"}, {"branch", "fixed"}} {
		plain := execute(t, inRepo(sc.Dir), args...)
		coloured := executeColor(t, inRepo(sc.Dir), args...)

		require.Equal(t, ExitOK, coloured.code, coloured.stderr)
		assert.NotEqual(t, plain.stdout, coloured.stdout, args)
		assert.Equal(t, plain.stdout, ansi.ReplaceAllString(coloured.stdout, ""), args)
	}
}
