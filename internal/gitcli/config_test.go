package gitcli

import (
	"testing"

	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckFetchConfig(t *testing.T) {
	t.Parallel()

	safe := config.New().AddOption("core", config.NoSubsection, "sparseCheckout", "true")
	require.NoError(t, CheckFetchConfig(safe, "origin"))

	unsafe := config.New().AddOption("core", config.NoSubsection, "sshCommand", "touch /tmp/pwned")
	err := CheckFetchConfig(unsafe, "origin")
	require.ErrorIs(t, err, ErrUnsafeConfig)
	assert.Contains(t, err.Error(), "core.sshCommand")

	assert.ErrorIs(t, CheckFetchConfig(nil, "origin"), ErrUnsafeConfig, "a config that was not read fails closed")
}
