package cli

import (
	"strings"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hostile carries an OSC title change, a screen clear, a colour and a
// right-to-left override.
var hostile = "\x1b]0;pwned\x07\x1b[2J\x1b[31m" + string(rune(0x202e)) + "txt.exe"

func TestHostileRepositoryStringsRenderInert(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Bootstrap()
	target := r.Commit("refs/heads/main", "only")
	evil := r.RecordAt(r.Cluster, "refs/heads/evil"+hostile, target, nil)
	annot := r.Annotate(r.Cluster, false, "note"+hostile+"\r\nsecond line", evil)

	for _, args := range [][]string{
		{"log"},
		{"entry", evil.String()},
		{"entry", annot.String()},
	} {
		res := execute(t, inRepo(r.Dir), args...)
		require.Equal(t, ExitOK, res.code, res.stderr)

		assert.NotContains(t, res.stdout, "\x1b", args)
		assert.NotContains(t, res.stdout, "\x07", args)
		assert.NotContains(t, res.stdout, "\r", args)
		assert.NotContains(t, res.stdout, string(rune(0x202e)), args)
		assert.Contains(t, res.stdout, "txt.exe", args)
	}

	res := execute(t, inRepo(r.Dir), "entry", annot.String())
	assert.Contains(t, res.stdout, "second line")

	res = execute(t, inRepo(r.Dir), "entry", "--json", evil.String())
	require.Equal(t, ExitOK, res.code)
	assert.NotContains(t, res.stdout, "\x1b", "JSON escapes control characters")
	assert.True(t, strings.Contains(res.stdout, `\u001b`), "the raw value survives in JSON, escaped")
}
