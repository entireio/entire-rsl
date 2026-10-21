package cli

import (
	"encoding/json"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refreshStatuses(t *testing.T, stdout string) map[string]string {
	t.Helper()

	var doc refreshJSON
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	out := map[string]string{}
	for _, r := range doc.Refs {
		out[r.Ref] = r.Status
	}
	return out
}

func TestRefreshFastForwardsTheLocalRSL(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")

	first := execute(t, inRepo(clone), "refresh", "--json")
	require.Equal(t, ExitOK, first.code, first.stderr)
	assert.Equal(t, "created", refreshStatuses(t, first.stdout)["refs/gittuf/reference-state-log"])

	again := execute(t, inRepo(clone), "refresh", "--json", "origin")
	require.Equal(t, ExitOK, again.code, again.stderr)
	assert.Equal(t, "unchanged", refreshStatuses(t, again.stdout)["refs/gittuf/reference-state-log"])

	sc.Commit("refs/heads/main", "pushed later")
	newTip := sc.Record(sc.Cluster, "refs/heads/main", nil)

	updated := execute(t, inRepo(clone), "refresh", "--json")
	require.Equal(t, ExitOK, updated.code, updated.stderr)
	assert.Equal(t, "updated", refreshStatuses(t, updated.stdout)["refs/gittuf/reference-state-log"])
	assert.Equal(t, newTip.String(), gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log"))
	assert.Equal(t, "unchanged", refreshStatuses(t, updated.stdout)["refs/gittuf/policy"])
}

func TestRefreshLeavesDivergedRefsAlone(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	require.Equal(t, ExitOK, execute(t, inRepo(clone), "refresh").code)

	// A local entry the remote never saw, then a remote entry: the histories
	// diverge.
	tree := gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log^{tree}")
	localTip := gitOut(t, clone, "-c", "user.name=Local", "-c", "user.email=local@example.com", "commit-tree", tree, "-p", "refs/gittuf/reference-state-log", "-m", "local only")
	gitOut(t, clone, "update-ref", "refs/gittuf/reference-state-log", localTip)
	sc.Commit("refs/heads/main", "remote only")
	sc.Record(sc.Cluster, "refs/heads/main", nil)

	res := execute(t, inRepo(clone), "refresh")

	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stdout, "diverged")
	assert.Contains(t, res.stderr, "Warning: refs/gittuf/reference-state-log was left at "+localTip)
	assert.Equal(t, localTip, gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log"))
}

func TestRefreshRemoteSelection(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	testutil.AddRemote(t, clone, "mirror", sc.Dir)

	res := execute(t, inRepo(clone), "refresh")
	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, "name one: entire rsl refresh <remote>")

	res = execute(t, inRepo(clone), "refresh", "nope")
	assert.Equal(t, ExitUsage, res.code)

	res = execute(t, inRepo(clone), "refresh", "mirror")
	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Contains(t, refs(t, clone, "refs/remotes/mirror/gittuf/"), "refs/remotes/mirror/gittuf/reference-state-log")
}

func TestRefreshRefusesUnsafeConfig(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	gitOut(t, clone, "config", "core.sshCommand", "touch /tmp/pwned")

	res := execute(t, inRepo(clone), "refresh")

	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, "would make a fetch run programs")
}
