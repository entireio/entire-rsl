package cli

import (
	"encoding/json"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/gittuf/gittuf/pkg/rsl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refreshRefs(t *testing.T, stdout string) map[string]refreshRefJSON {
	t.Helper()

	var doc refreshJSON
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	out := map[string]refreshRefJSON{}
	for _, r := range doc.Refs {
		out[r.Ref] = r
	}
	return out
}

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
	created := refreshRefs(t, first.stdout)["refs/gittuf/reference-state-log"]
	assert.Equal(t, "created", created.Status)
	assert.Equal(t, 14, created.Commits)
	assert.Equal(t, &rslEntriesJSON{New: 14}, created.RSLEntries)

	again := execute(t, inRepo(clone), "refresh", "--json", "origin")
	require.Equal(t, ExitOK, again.code, again.stderr)
	assert.Equal(t, "unchanged", refreshStatuses(t, again.stdout)["refs/gittuf/reference-state-log"])

	sc.Commit("refs/heads/main", "pushed later")
	newTip := sc.Record(sc.Cluster, "refs/heads/main", nil)

	updated := execute(t, inRepo(clone), "refresh", "--json")
	require.Equal(t, ExitOK, updated.code, updated.stderr)
	rslRef := refreshRefs(t, updated.stdout)["refs/gittuf/reference-state-log"]
	assert.Equal(t, "updated", rslRef.Status)
	assert.Equal(t, 1, rslRef.Commits)
	assert.Equal(t, &rslEntriesJSON{Old: 14, New: 15}, rslRef.RSLEntries)
	assert.Equal(t, newTip.String(), gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log"))
	assert.Equal(t, "unchanged", refreshStatuses(t, updated.stdout)["refs/gittuf/policy"])
}

// divergeRSL refreshes clone, then adds a local RSL entry the remote never
// saw and a remote entry the clone has not fetched, so the histories
// diverge. It returns the local tip.
func divergeRSL(t *testing.T, sc *testutil.Scenario, clone string) string {
	t.Helper()

	require.Equal(t, ExitOK, execute(t, inRepo(clone), "refresh").code)
	tree := gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log^{tree}")
	localTip := gitOut(t, clone, "-c", "user.name=Local", "-c", "user.email=local@example.com", "commit-tree", tree, "-p", "refs/gittuf/reference-state-log", "-m", "local only")
	gitOut(t, clone, "update-ref", "refs/gittuf/reference-state-log", localTip)
	sc.Commit("refs/heads/main", "remote only")
	sc.Record(sc.Cluster, "refs/heads/main", nil)
	return localTip
}

func TestRefreshFailsOnDivergedRefs(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	localTip := divergeRSL(t, sc, clone)
	remoteTip := gitOut(t, sc.Dir, "rev-parse", "refs/gittuf/reference-state-log")

	text := execute(t, inRepo(clone), "refresh")

	assert.Equal(t, ExitFailure, text.code)
	assert.Contains(t, text.stdout, "diverged")
	assert.Contains(t, text.stdout, "remote +1, local +1")
	assert.Contains(t, text.stderr, "Error: local gittuf refs have diverged from origin and were left unchanged:\n"+
		"  refs/gittuf/reference-state-log: local "+localTip+", remote "+remoteTip+" (remote +1, local +1)\n"+
		"entire-rsl never overwrites a local gittuf ref: compare each with refs/remotes/origin/gittuf/* and resolve it with git")
	assert.Equal(t, localTip, gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log"))

	asJSON := execute(t, inRepo(clone), "refresh", "--json")

	assert.Equal(t, ExitFailure, asJSON.code)
	assert.Equal(t, "diverged", refreshStatuses(t, asJSON.stdout)["refs/gittuf/reference-state-log"])
	assert.Contains(t, asJSON.stderr, "Error: local gittuf refs have diverged from origin")
	assert.Equal(t, localTip, gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log"))
}

func TestRefreshRemoteSelection(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	testutil.AddRemote(t, clone, "mirror", testutil.LocalURL(sc.Dir))

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
	assert.Contains(t, res.stderr, "will not fetch with")
}

func TestRefreshNeverWritesThroughASymbolicGittufRef(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	require.Equal(t, ExitOK, execute(t, inRepo(clone), "refresh").code)
	mainBefore := gitOut(t, clone, "rev-parse", "refs/heads/main")
	gitOut(t, clone, "symbolic-ref", "refs/gittuf/policy", "refs/heads/main")

	res := execute(t, inRepo(clone), "refresh", "--json")

	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, ", local ref is symbolic)")
	policy := refreshRefs(t, res.stdout)["refs/gittuf/policy"]
	assert.Equal(t, "diverged", policy.Status)
	assert.True(t, policy.Symbolic)
	assert.Positive(t, policy.Commits, "counts compare with the object the symbolic ref resolves to")
	assert.Positive(t, policy.LocalCommits)
	assert.Equal(t, mainBefore, gitOut(t, clone, "rev-parse", "refs/heads/main"))
	assert.Equal(t, "refs/heads/main", gitOut(t, clone, "symbolic-ref", "refs/gittuf/policy"))
}

func TestRefreshIgnoresReplacementObjects(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	require.Equal(t, ExitOK, execute(t, inRepo(clone), "refresh").code)

	tree := gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log^{tree}")
	localTip := gitOut(t, clone, "-c", "user.name=Local", "-c", "user.email=local@example.com", "commit-tree", tree, "-p", "refs/gittuf/reference-state-log", "-m", "local only")
	gitOut(t, clone, "update-ref", "refs/gittuf/reference-state-log", localTip)
	sc.Commit("refs/heads/main", "remote only")
	remoteTip := sc.Record(sc.Cluster, "refs/heads/main", nil).String()

	// Fake the remote tip descending from the local one.
	gitOut(t, clone, "fetch", "--quiet", "origin", "refs/gittuf/reference-state-log:refs/scratch")
	gitOut(t, clone, "update-ref", "-d", "refs/scratch")
	gitOut(t, clone, "replace", "--graft", remoteTip, localTip)

	res := execute(t, inRepo(clone), "refresh", "--json")

	assert.Equal(t, ExitFailure, res.code, res.stderr)
	assert.Equal(t, "diverged", refreshStatuses(t, res.stdout)["refs/gittuf/reference-state-log"])
	assert.Equal(t, localTip, gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log"))
}

func TestRefreshTextGolden(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")

	created := execute(t, inRepo(clone), "refresh")
	require.Equal(t, ExitOK, created.code, created.stderr)
	golden(t, "refresh_created.txt", created.stdout)

	sc.Commit("refs/heads/main", "pushed later")
	sc.Record(sc.Cluster, "refs/heads/main", nil)
	sc.Commit("refs/heads/main", "pushed even later")
	sc.Record(sc.Cluster, "refs/heads/main", nil)

	updated := execute(t, inRepo(clone), "refresh")
	require.Equal(t, ExitOK, updated.code, updated.stderr)
	golden(t, "refresh_updated.txt", updated.stdout)
}

func TestPickerListsOriginFirst(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	testutil.AddRemote(t, clone, "aaa", testutil.LocalURL(sc.Dir))
	testutil.AddRemote(t, clone, "zzz", testutil.LocalURL(sc.Dir))

	res := executeInteractive(t, inRepo(clone), true, "1\n", "refresh", "--json")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Contains(t, res.stderr, "1) origin")
	assert.Contains(t, res.stderr, "2) aaa")
	assert.Contains(t, res.stderr, "3) zzz")
	assert.Contains(t, res.stdout, `"remote": "origin"`)
}

func TestRefreshOmitsNumbersForUnnumberedEntries(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	target := r.Commit("refs/heads/main", "only")
	require.NoError(t, rsl.NewReferenceEntry("refs/heads/main", target).CommitWithoutNumber(r.Git))
	clone := r.Clone(t, "origin")

	res := execute(t, inRepo(clone), "refresh", "--json")

	require.Equal(t, ExitOK, res.code, res.stderr)
	entry := refreshRefs(t, res.stdout)["refs/gittuf/reference-state-log"]
	assert.Equal(t, "created", entry.Status)
	assert.Nil(t, entry.RSLEntries, "an unnumbered entry has no number to show")

	text := execute(t, inRepo(clone), "refresh")
	assert.NotContains(t, text.stdout, "entry #0")
}
