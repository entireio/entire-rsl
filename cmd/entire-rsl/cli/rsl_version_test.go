package cli

import (
	"encoding/json"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionText(t *testing.T) {
	t.Parallel()

	res := execute(t, testEnv{}, "version")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Contains(t, res.stdout, "entire-rsl dev (commit unknown)\n")
	assert.Contains(t, res.stdout, "Go:     "+runtime.Version()+"\n")
	assert.Contains(t, res.stdout, "gittuf: ")
	assert.Empty(t, res.stderr)
}

func TestVersionJSON(t *testing.T) {
	t.Parallel()

	res := execute(t, testEnv{}, "version", "--json")
	require.Equal(t, ExitOK, res.code, res.stderr)

	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &got))

	assert.InDelta(t, 1, got["schema_version"], 0)
	assert.Equal(t, "dev", got["version"])
	assert.Equal(t, runtime.Version(), got["go_version"])
	assert.Contains(t, got, "gittuf_version")
}

func TestDebugLogsToStderrOnly(t *testing.T) {
	t.Parallel()

	for name, env := range map[string]testEnv{
		"flag": {},
		"env":  {envLogLevel: "debug"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			args := []string{"version", "--json"}
			if name == "flag" {
				args = append(args, "--debug")
			}
			res := execute(t, env, args...)

			require.Equal(t, ExitOK, res.code)
			assert.Contains(t, res.stderr, "entire-rsl starting")
			assert.Contains(t, res.stderr, "gittuf=")
			assert.True(t, json.Valid([]byte(res.stdout)), "stdout must stay pure JSON in debug mode")
		})
	}
}

func TestDisplayNameUnderEntire(t *testing.T) {
	t.Parallel()

	res := execute(t, testEnv{"ENTIRE_CLI_VERSION": "0.42.0"}, "--help")

	require.Equal(t, ExitOK, res.code)
	assert.Contains(t, res.stdout, "entire rsl [command]")
}
