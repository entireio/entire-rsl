package cli

import (
	"bytes"
	"testing"
)

type testEnv map[string]string

func (e testEnv) get(key string) string { return e[key] }

type result struct {
	stdout string
	stderr string
	code   int
}

// execute runs the command tree with args and reports what a process would
// print and exit with.
func execute(t *testing.T, env testEnv, args ...string) result {
	t.Helper()

	var stdout, stderr bytes.Buffer
	root := NewRootCmd(Options{
		Stdin:  &bytes.Buffer{},
		Stdout: &stdout,
		Stderr: &stderr,
		Getenv: env.get,
	})
	root.SetArgs(args)

	code := Report(&stderr, root.ExecuteContext(t.Context()))

	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}
