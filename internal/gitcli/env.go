// Package gitcli runs the git binary safely: an absolute path resolved once,
// arguments as a slice with "--" before positionals, no shell, and an
// environment that cannot redirect git to another repository or make it run
// repository-configured programs.
package gitcli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// repoSelectors are inherited variables that would point git at a repository,
// index or object store other than the one entire-rsl opened.
var repoSelectors = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_NAMESPACE",
}

// configOverrides neutralise repository config that makes git execute
// programs. They apply to every git process entire-rsl starts, including the
// ones gittuf starts.
var configOverrides = [][2]string{
	{"core.fsmonitor", "false"},
	{"core.hooksPath", os.DevNull},
}

// HardenProcessEnv removes repository selectors from the process environment
// and installs configOverrides through GIT_CONFIG_COUNT, replacing any
// environment-provided config. It runs once, at startup, before any git
// process can start: gittuf starts git itself and inherits this environment.
func HardenProcessEnv() error {
	for _, key := range repoSelectors {
		if err := os.Unsetenv(key); err != nil {
			return fmt.Errorf("unset %s: %w", key, err)
		}
	}

	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if key == "GIT_CONFIG_PARAMETERS" || key == "GIT_CONFIG_COUNT" ||
			strings.HasPrefix(key, "GIT_CONFIG_KEY_") || strings.HasPrefix(key, "GIT_CONFIG_VALUE_") {
			if err := os.Unsetenv(key); err != nil {
				return fmt.Errorf("unset %s: %w", key, err)
			}
		}
	}

	for i, kv := range configOverrides {
		n := strconv.Itoa(i)
		if err := os.Setenv("GIT_CONFIG_KEY_"+n, kv[0]); err != nil {
			return fmt.Errorf("set git config override: %w", err)
		}
		if err := os.Setenv("GIT_CONFIG_VALUE_"+n, kv[1]); err != nil {
			return fmt.Errorf("set git config override: %w", err)
		}
	}
	if err := os.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(len(configOverrides))); err != nil {
		return fmt.Errorf("set git config override: %w", err)
	}
	return nil
}
