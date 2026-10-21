package cli

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentHelpGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "agent_help.txt", args: []string{"agent-help"}},
		{name: "agent_help.json", args: []string{"agent-help", "--json"}},
		{name: "agent_help_verify.json", args: []string{"agent-help", "verify", "--json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, testEnv{"ENTIRE_CLI_VERSION": "0.42.0"}, tt.args...)

			require.Equal(t, ExitOK, res.code, res.stderr)
			golden(t, tt.name, res.stdout)
		})
	}
}

func TestAgentHelpListsEveryCommandAsReadOnly(t *testing.T) {
	t.Parallel()

	res := execute(t, testEnv{}, "agent-help", "--json")
	require.Equal(t, ExitOK, res.code)

	var doc agentHelpJSON
	require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))

	var names []string
	for _, s := range doc.Subcommands {
		names = append(names, s.Name)
		assert.Equal(t, "read-only", s.Audience, s.Name)
	}
	assert.ElementsMatch(t, []string{"branch", "tag", "checkpoint", "verify", "log", "entry", "version"}, names)
}

func TestAgentHelpUnknownPath(t *testing.T) {
	t.Parallel()

	res := execute(t, testEnv{}, "agent-help", "nope")

	assert.Equal(t, ExitUsage, res.code)
}
