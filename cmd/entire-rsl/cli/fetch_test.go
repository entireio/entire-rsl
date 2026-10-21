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

func TestFetchNeverOverwritesExistingLocalGittufRefs(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	local := testutil.NewRepo(t)
	// Different patterns give a policy commit unrelated to the scenario's.
	local.Bootstrap("git:refs/heads/elsewhere")
	// The clone gets a local policy ref unrelated to the remote's, but no RSL.
	out, err := exec.CommandContext(t.Context(), "git", "-C", clone, "fetch", "--quiet", local.Dir, "refs/gittuf/policy:refs/gittuf/policy").CombinedOutput()
	require.NoError(t, err, string(out))
	before := refs(t, clone, "refs/gittuf/policy")

	res := execute(t, inRepo(clone), "log", "--fetch")

	assert.Equal(t, ExitFailure, res.code, "a non fast-forward gittuf ref fails the fetch: %s\n%s", res.stderr, res.stdout)
	afterOut, err := exec.CommandContext(t.Context(), "git", "-C", clone, "rev-parse", "refs/gittuf/policy").Output()
	require.NoError(t, err)
	localTip, err := exec.CommandContext(t.Context(), "git", "-C", local.Dir, "rev-parse", "refs/gittuf/policy").Output()
	require.NoError(t, err)
	assert.Equal(t, string(localTip), string(afterOut), "the existing local policy ref is untouched")
	assert.Equal(t, before, refs(t, clone, "refs/gittuf/policy"))
}
