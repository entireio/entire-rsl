package gitcli

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Not parallel: HardenProcessEnv changes the process environment.
func TestHardenProcessEnv(t *testing.T) {
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	t.Setenv("GIT_WORK_TREE", "/elsewhere")
	t.Setenv("GIT_INDEX_FILE", "/elsewhere/index")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'core.fsmonitor'='evil'")
	t.Setenv("GIT_CONFIG_COUNT", "3")
	t.Setenv("GIT_CONFIG_KEY_2", "core.sshCommand")
	t.Setenv("GIT_CONFIG_VALUE_2", "evil")
	t.Setenv("SSH_AUTH_SOCK", "/run/agent")

	require.NoError(t, HardenProcessEnv())

	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_KEY_2", "GIT_CONFIG_VALUE_2"} {
		_, set := os.LookupEnv(key)
		assert.False(t, set, key)
	}
	assert.Equal(t, "2", os.Getenv("GIT_CONFIG_COUNT"))
	assert.Equal(t, "core.fsmonitor", os.Getenv("GIT_CONFIG_KEY_0"))
	assert.Equal(t, "false", os.Getenv("GIT_CONFIG_VALUE_0"))
	assert.Equal(t, "core.hooksPath", os.Getenv("GIT_CONFIG_KEY_1"))
	assert.Equal(t, os.DevNull, os.Getenv("GIT_CONFIG_VALUE_1"))
	assert.Equal(t, "/run/agent", os.Getenv("SSH_AUTH_SOCK"), "credentials plumbing is kept")
	assert.Equal(t, "1", os.Getenv("GIT_NO_REPLACE_OBJECTS"))
	assert.Equal(t, os.DevNull, os.Getenv("GIT_GRAFT_FILE"))
}
