package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	cleanup := testutil.IsolateGitConfig()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

type outcome struct {
	stdout string
	stderr string
	code   int
}

func runIn(t *testing.T, dir string, env []string, name string, args ...string) outcome {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else {
		require.NoError(t, err)
	}
	return outcome{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// TestDispatchedThroughEntire is SPEC.md section 4's acceptance test: once
// installed with `entire plugin install`, `entire rsl ...` prints exactly
// what running the binary directly prints, with the same exit code.
func TestDispatchedThroughEntire(t *testing.T) {
	t.Parallel()

	entire, err := exec.LookPath("entire")
	if err != nil {
		t.Skip("the entire CLI is not on PATH")
	}

	binDir := t.TempDir()
	binary := filepath.Join(binDir, "entire-rsl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", binary, ".")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	pluginDir := t.TempDir()
	env := []string{"ENTIRE_PLUGIN_DIR=" + pluginDir}
	install := runIn(t, binDir, env, entire, "plugin", "install", binary)
	require.Equal(t, 0, install.code, install.stderr)

	sc := testutil.NewScenario(t)

	for _, args := range [][]string{
		{"branch", "main"},
		{"branch", "--json", "main"},
		{"tag", "v1.0.0"},
		{"verify", "rogue"},
		{"log", "--limit", "3"},
		{"agent-help", "--json"},
	} {
		viaEntire := runIn(t, sc.Dir, env, entire, append([]string{"rsl"}, args...)...)
		direct := runIn(t, sc.Dir, nil, binary, args...)

		assert.Equal(t, direct.stdout, viaEntire.stdout, args)
		assert.Equal(t, direct.code, viaEntire.code, args)
	}
}
