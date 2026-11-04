package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestMain(m *testing.M) {
	cleanup := testutil.IsolateGitConfig()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

type testEnv map[string]string

func (e testEnv) get(key string) string { return e[key] }

// inRepo is the environment of a command run by the Entire CLI inside dir.
func inRepo(dir string) testEnv {
	return testEnv{"ENTIRE_REPO_ROOT": dir, "ENTIRE_CLI_VERSION": "0.42.0"}
}

type result struct {
	stdout string
	stderr string
	code   int
}

// execute runs the command tree with args and reports what a process would
// print and exit with.
func execute(t *testing.T, env testEnv, args ...string) result {
	t.Helper()
	return executeInteractive(t, env, false, "", args...)
}

// executeInteractive is execute on a terminal when interactive is true, with
// stdin as what the user types.
func executeInteractive(t *testing.T, env testEnv, interactive bool, stdin string, args ...string) result {
	t.Helper()
	return executeWith(t, env, interactive, false, stdin, args...)
}

// executeColor is execute with stdout on a terminal that may be coloured.
func executeColor(t *testing.T, env testEnv, args ...string) result {
	t.Helper()
	return executeWith(t, env, false, true, "", args...)
}

func executeWith(t *testing.T, env testEnv, interactive, color bool, stdin string, args ...string) result {
	t.Helper()

	var stdout, stderr bytes.Buffer
	cwd := t.TempDir()
	root := NewRootCmd(Options{
		Stdin:       bytes.NewBufferString(stdin),
		Stdout:      &stdout,
		Stderr:      &stderr,
		Getenv:      env.get,
		Getwd:       func() (string, error) { return cwd, nil },
		Interactive: func() bool { return interactive },
		ColorStdout: func() bool { return color },
		TrustedKeys: testutil.TrustedKeys(t),
	})
	root.SetArgs(args)

	code := Report(&stderr, root.ExecuteContext(t.Context()))

	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// golden compares got with testdata/<name>, rewriting it under -update.
func golden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)
	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file, run go test ./cmd/entire-rsl/cli -update")
	assert.Equal(t, string(want), got)
}
