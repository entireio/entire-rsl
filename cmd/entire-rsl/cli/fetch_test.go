package cli

import (
	"os"
	"os/exec"
	"path/filepath"
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

func TestFetchWithWorktreeConfig(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	t.Run("allowed keys", func(t *testing.T) {
		t.Parallel()

		clone := sc.Clone(t, "origin")
		enableWorktreeConfig(t, clone)
		gitOut(t, clone, "config", "--worktree", "user.name", "Jane Doe")

		res := execute(t, inRepo(clone), "verify", "--fetch", "refs/heads/main")

		require.Equal(t, ExitOK, res.code, res.stderr)
		assert.Contains(t, res.stdout, "verified")
	})

	t.Run("refused key", func(t *testing.T) {
		t.Parallel()

		clone := sc.Clone(t, "origin")
		enableWorktreeConfig(t, clone)
		gitOut(t, clone, "config", "--worktree", "core.sshCommand", "touch /tmp/pwned")

		res := execute(t, inRepo(clone), "refresh")

		assert.Equal(t, ExitFailure, res.code)
		assert.Contains(t, res.stderr, "will not fetch with: core.sshcommand. Remove them, or fetch the gittuf refs with git yourself")
		assert.Empty(t, refs(t, clone, "refs/remotes/origin/gittuf/"))
	})
}

func TestFetchRefusesAlternateRefsCommand(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	other := testutil.NewRepo(t)
	other.Commit("refs/heads/main", "other")
	marker := filepath.Join(t.TempDir(), "marker")
	require.NoError(t, os.WriteFile(filepath.Join(clone, ".git", "objects", "info", "alternates"), []byte(filepath.Join(other.Dir, ".git", "objects")+"\n"), 0o600))
	gitOut(t, clone, "config", "core.alternateRefsCommand", "touch '"+filepath.ToSlash(marker)+"'; true")

	for _, args := range [][]string{{"refresh"}, {"verify", "--fetch", "main"}} {
		res := execute(t, inRepo(clone), args...)

		assert.Equal(t, ExitFailure, res.code, args)
		assert.Contains(t, res.stderr, "will not fetch with: core.alternaterefscommand.", args)
		assert.NoFileExists(t, marker, args)
		assert.Empty(t, refs(t, clone, "refs/remotes/origin/gittuf/"), args)
	}
}

func TestVerifyWithWorktreeConfig(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	enableWorktreeConfig(t, sc.Dir)
	gitOut(t, sc.Dir, "config", "--worktree", "core.sparseCheckout", "true")

	res := execute(t, inRepo(sc.Dir), "verify", "refs/heads/main")

	require.Equal(t, ExitOK, res.code, res.stdout+res.stderr)
}

func TestLinkedWorktree(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	wt := sc.AddWorktree(t, "main")

	for _, args := range [][]string{
		{"verify", "refs/heads/main"},
		{"verify", sc.TrailerCommit.String()},
		{"verify", testutil.CheckpointULID},
		{"branch", "main"},
	} {
		res := execute(t, inRepo(wt), args...)
		assert.Equal(t, ExitOK, res.code, "%v: %s%s", args, res.stdout, res.stderr)
		assert.Contains(t, res.stdout, "verified", args)
	}
}

func TestLinkedWorktreeFetch(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	wt := filepath.Join(t.TempDir(), "worktree")
	gitOut(t, clone, "worktree", "add", "--quiet", "--detach", "--", wt, "main")

	res := execute(t, inRepo(wt), "verify", "--fetch", "refs/heads/main")

	require.Equal(t, ExitOK, res.code, res.stdout+res.stderr)
	assert.Contains(t, refs(t, clone, "refs/gittuf/"), "refs/gittuf/reference-state-log", "gittuf refs are shared by every worktree")
}

func enableWorktreeConfig(t *testing.T, dir string) {
	t.Helper()

	gitOut(t, dir, "config", "core.repositoryformatversion", "1")
	gitOut(t, dir, "config", "extensions.worktreeConfig", "true")
}

func TestFetchFlagFetchesAndVerifies(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")

	res := execute(t, inRepo(clone), "verify", "--fetch", "refs/heads/main")
	t.Log(res.stdout)

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

func TestFetchFlagFastForwardsAnExistingRSL(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	require.Equal(t, ExitOK, execute(t, inRepo(clone), "refresh").code)

	sc.Commit("refs/heads/main", "pushed later")
	newTip := sc.Record(sc.Cluster, "refs/heads/main", nil)

	res := execute(t, inRepo(clone), "verify", "--fetch", "refs/heads/main")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.Contains(t, res.stderr, "Fetching gittuf refs from origin")
	assert.Contains(t, res.stderr, "Updated refs/gittuf/reference-state-log")
	assert.Equal(t, newTip.String(), gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log"))
	assert.Contains(t, res.stdout, "→ #15")
	assert.Contains(t, res.stdout, "behind: the local ref is behind: it is at entry #9, the RSL tip is entry #15 (1 entry behind, and the tip's object is not in the local repository)")
}

func TestFetchFlagCannotCompareALocalCommitWithAnUnfetchedTip(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	require.Equal(t, ExitOK, execute(t, inRepo(clone), "refresh").code)
	gitOut(t, clone, "-c", "user.name=Local", "-c", "user.email=local@example.com", "commit", "--allow-empty", "-m", "local only")
	sc.Commit("refs/heads/main", "pushed later")
	sc.Record(sc.Cluster, "refs/heads/main", nil)

	res := execute(t, inRepo(clone), "verify", "--fetch", "refs/heads/main")

	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stdout, "the object the RSL tip records is not in the local repository")
}

func TestFetchFlagFailsOnADivergedRSL(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"branch", "main"},
		{"branch", "main", "--json"},
		{"tag", "v1.0.0"},
		{"checkpoint", testutil.CheckpointULID},
		{"verify", "refs/heads/main"},
		{"verify", "refs/heads/main", "--json"},
		{"log", "--limit", "1"},
		{"entry", "1"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()

			sc := testutil.NewScenario(t)
			clone := sc.Clone(t, "origin")
			localTip := divergeRSL(t, sc, clone)

			res := execute(t, inRepo(clone), append(args, "--fetch")...)

			assert.Equal(t, ExitFailure, res.code)
			assert.Empty(t, res.stdout, "the command must not run on a diverged RSL")
			assert.Contains(t, res.stderr, "Error: local gittuf refs have diverged from origin and were left unchanged:\n  refs/gittuf/reference-state-log: local "+localTip)
			assert.Equal(t, localTip, gitOut(t, clone, "rev-parse", "refs/gittuf/reference-state-log"))
		})
	}
}

func TestPromptToFetchFailsOnADivergedRef(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	gitOut(t, clone, "symbolic-ref", "refs/gittuf/policy", "refs/heads/main")

	res := executeInteractive(t, inRepo(clone), true, "y\n", "log", "--limit", "1")

	assert.Equal(t, ExitFailure, res.code)
	assert.Empty(t, res.stdout)
	assert.Contains(t, res.stderr, "Error: local gittuf refs have diverged from origin and were left unchanged:\n  refs/gittuf/policy: ")
	assert.Equal(t, "refs/heads/main", gitOut(t, clone, "symbolic-ref", "refs/gittuf/policy"))
}

func TestFetchFlagWithSeveralRemotes(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")
	testutil.AddRemote(t, clone, "upstream", testutil.LocalURL(sc.Dir))

	res := execute(t, inRepo(clone), "log", "--fetch")

	assert.Equal(t, ExitFailure, res.code)
	assert.Contains(t, res.stderr, "--fetch=<remote> (remotes: origin, upstream)")
	assert.Empty(t, refs(t, clone, "refs/gittuf/"))
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

func TestFetchWarnsWhenTheRemoteHasNoRSL(t *testing.T) {
	t.Parallel()

	plain := testutil.NewRepo(t)
	plain.Commit("refs/heads/main", "no gittuf")
	clone := plain.Clone(t, "origin")

	for _, args := range [][]string{{"refresh"}, {"refresh", "--json"}, {"verify", "--fetch", "main"}} {
		res := execute(t, inRepo(clone), args...)

		assert.Contains(t, res.stderr, "Warning: origin has no refs/gittuf/reference-state-log or refs/gittuf/policy. RSL may not be enabled for the repository, or origin may not be hosted on Entire.", args)
	}

	res := executeInteractive(t, inRepo(clone), true, "y\n", "log", "--limit", "1")
	assert.Contains(t, res.stderr, "Warning: origin has no refs/gittuf/reference-state-log or refs/gittuf/policy.")
}

func TestFetchWarnsAboutEachMissingGittufRef(t *testing.T) {
	t.Parallel()

	plain := testutil.NewRepo(t)
	main := plain.Commit("refs/heads/main", "policy only")
	plain.SetRef("refs/gittuf/policy", main)
	clone := plain.Clone(t, "origin")

	res := execute(t, inRepo(clone), "refresh")

	assert.Contains(t, res.stderr, "Warning: origin has no refs/gittuf/reference-state-log. RSL may not be enabled")
	assert.NotContains(t, res.stderr, "or refs/gittuf/policy")
}

func TestFetchDoesNotWarnWhenTheRemoteHasTheRSLAndPolicy(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	clone := sc.Clone(t, "origin")

	res := execute(t, inRepo(clone), "refresh")

	require.Equal(t, ExitOK, res.code, res.stderr)
	assert.NotContains(t, res.stderr, "Warning")
}

func TestPickRemoteWhenSeveral(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)
	empty := testutil.NewRepo(t)
	empty.Commit("refs/heads/main", "unrelated")

	clone := empty.Clone(t, "aaa-empty")
	testutil.AddRemote(t, clone, "zzz-scenario", testutil.LocalURL(sc.Dir))

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

	assert.Equal(t, ExitFailure, res.code)
	assert.Equal(t, before, gitOut(t, clone, "rev-parse", "refs/gittuf/policy"), "the existing local policy ref is untouched")
	assert.Equal(t, gitOut(t, sc.Dir, "rev-parse", "refs/gittuf/policy"), gitOut(t, clone, "rev-parse", "refs/remotes/origin/gittuf/policy"))
	assert.Contains(t, res.stderr, "Created refs/gittuf/reference-state-log")
	assert.NotContains(t, res.stderr, "Created refs/gittuf/policy\n")
	assert.Contains(t, res.stderr, "Error: local gittuf refs have diverged from origin and were left unchanged:\n  refs/gittuf/policy: local "+before)
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
		"local path remote": {"remote.origin.url", sc.Dir},
		"file remote":       {"remote.origin.url", "file://" + filepath.ToSlash(sc.Dir)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			clone := sc.Clone(t, "origin")
			gitOut(t, clone, "config", kv[0], kv[1])

			res := execute(t, inRepo(clone), "log", "--fetch")

			assert.Equal(t, ExitFailure, res.code)
			assert.Contains(t, res.stderr, "will not fetch with")
			assert.Empty(t, refs(t, clone, "refs/gittuf/"))
			assert.Empty(t, refs(t, clone, "refs/remotes/origin/gittuf/"))
		})
	}
}

func TestSortRemotesPutsOriginFirst(t *testing.T) {
	t.Parallel()

	names := []string{"zzz", "upstream", "origin", "aaa"}
	sortRemotes(names)

	assert.Equal(t, []string{"origin", "aaa", "upstream", "zzz"}, names)
}
