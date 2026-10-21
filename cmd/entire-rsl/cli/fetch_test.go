package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refs(t *testing.T, dir, prefix string) []string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", "-C", dir, "for-each-ref", "--format=%(refname)", prefix).Output()
	require.NoError(t, err)
	return strings.Fields(string(out))
}

func TestNoRSLWithoutConsentDoesNotFetch(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")

	res := execute(t, inRepo(clone), "branch", "main")

	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, "no gittuf RSL found")
	assert.Contains(t, res.stderr, "--fetch")
	assert.Empty(t, refs(t, clone, "refs/gittuf/"))
	assert.Empty(t, refs(t, clone, "refs/remotes/origin/gittuf/"))
}

func TestFetchFlagFetchesAndVerifies(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")

	res := execute(t, inRepo(clone), "verify", "--fetch", "refs/heads/main")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Contains(t, res.stdout, "verified")
	assert.Contains(t, res.stderr, "Fetching gittuf refs from origin")
	assert.Contains(t, refs(t, clone, "refs/gittuf/"), "refs/gittuf/reference-state-log")
	assert.Contains(t, refs(t, clone, "refs/gittuf/"), "refs/gittuf/policy")
	assert.Contains(t, refs(t, clone, "refs/remotes/origin/gittuf/"), "refs/remotes/origin/gittuf/reference-state-log")

	again := execute(t, inRepo(clone), "verify", "refs/heads/main")
	assert.Equal(t, ExitOK, again.code, "the fetched RSL is local now: %s", again.stderr)
	assert.NotContains(t, again.stderr, "Fetching")
}

func TestFetchFlagWithUnknownRemote(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")

	res := execute(t, inRepo(clone), "log", "--fetch=nope")

	assert.Equal(t, ExitUsage, res.code)
	assert.Contains(t, res.stderr, `no remote named "nope"`)
}

func TestPromptToFetch(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	declined := sc.Clone(t, "origin")
	res := executeInteractive(t, inRepo(declined), true, "n\n", "log", "--limit", "1")
	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, "Fetch the gittuf refs from origin")
	assert.Contains(t, res.stderr, "the RSL was not fetched")
	assert.Empty(t, refs(t, declined, "refs/gittuf/"))

	accepted := sc.Clone(t, "origin")
	res = executeInteractive(t, inRepo(accepted), true, "y\n", "log", "--limit", "1")
	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Contains(t, res.stdout, "refs/heads/fixed")
}

func TestPickRemoteWhenSeveral(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	empty := testutil.NewRepo(t)
	empty.Commit("refs/heads/main", "unrelated")

	clone := empty.Clone(t, "aaa-empty")
	testutil.AddRemote(t, clone, "zzz-scenario", sc.Dir)

	res := execute(t, inRepo(clone), "log")
	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, "--fetch=<remote> (remotes: aaa-empty, zzz-scenario)")

	res = executeInteractive(t, inRepo(clone), true, "2\n", "log", "--limit", "1")
	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Contains(t, res.stderr, "Fetch from which remote?")
	assert.Contains(t, refs(t, clone, "refs/remotes/zzz-scenario/gittuf/"), "refs/remotes/zzz-scenario/gittuf/reference-state-log")
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

func TestFetchNeverOverwritesExistingLocalGittufRefs(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	local := testutil.NewRepo(t)
	// Different patterns give a policy commit unrelated to the scenario's.
	local.Bootstrap("git:refs/heads/elsewhere")
	gitOut(t, clone, "fetch", "--quiet", local.Dir, "refs/gittuf/policy:refs/gittuf/policy")
	before := gitOut(t, clone, "rev-parse", "refs/gittuf/policy")

	res := execute(t, inRepo(clone), "log", "--fetch", "--limit", "1")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Equal(t, before, gitOut(t, clone, "rev-parse", "refs/gittuf/policy"), "the existing local policy ref is untouched")
	assert.Equal(t, gitOut(t, sc.Dir, "rev-parse", "refs/gittuf/policy"), gitOut(t, clone, "rev-parse", "refs/remotes/origin/gittuf/policy"))
	assert.Contains(t, res.stderr, "Created refs/gittuf/reference-state-log")
	assert.NotContains(t, res.stderr, "Created refs/gittuf/policy\n")
}

func TestFetchIgnoresConfiguredRefmapAndPruning(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	mainBefore := gitOut(t, clone, "rev-parse", "refs/heads/main")
	gitOut(t, clone, "config", "--add", "remote.origin.fetch", "+refs/gittuf/policy:refs/heads/main")
	gitOut(t, clone, "config", "remote.origin.prune", "true")
	gitOut(t, clone, "config", "remote.origin.pruneTags", "true")
	gitOut(t, clone, "update-ref", "refs/remotes/origin/gittuf/stale", mainBefore)
	gitOut(t, clone, "tag", "local-only", mainBefore)

	res := execute(t, inRepo(clone), "log", "--fetch", "--limit", "1")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Equal(t, mainBefore, gitOut(t, clone, "rev-parse", "refs/heads/main"), "a configured mapping must not redirect the fetch")
	assert.Contains(t, refs(t, clone, "refs/remotes/origin/gittuf/"), "refs/remotes/origin/gittuf/stale", "configured pruning must not delete refs")
	assert.Contains(t, refs(t, clone, "refs/tags/"), "refs/tags/local-only")
}

func TestFetchRefusesUnsafeRepositoryConfig(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	for name, kv := range map[string][2]string{
		"ssh command":       {"core.sshCommand", "touch /tmp/pwned"},
		"upload pack":       {"remote.origin.uploadpack", "touch /tmp/pwned; git-upload-pack"},
		"credential helper": {"credential.helper", "!touch /tmp/pwned"},
		"include":           {"include.path", "/tmp/evil.config"},
		"askpass":           {"core.askPass", "/tmp/evil"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			clone := sc.Clone(t, "origin")
			gitOut(t, clone, "config", kv[0], kv[1])

			res := execute(t, inRepo(clone), "log", "--fetch")

			assert.Equal(t, ExitFailure, res.code)
			assert.Contains(t, res.stderr, "would make a fetch run programs")
			assert.Empty(t, refs(t, clone, "refs/gittuf/"))
			assert.Empty(t, refs(t, clone, "refs/remotes/origin/gittuf/"))
		})
	}
}
