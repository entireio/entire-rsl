package gitcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckFetchConfigAllowsAFreshClone(t *testing.T) {
	t.Parallel()

	dir := newClone(t)
	for _, kv := range [][2]string{
		{"core.ignorecase", "false"},
		{"core.precomposeunicode", "true"},
		{"core.symlinks", "true"},
		{"core.repositoryformatversion", "1"},
		{"extensions.objectformat", "sha1"},
		{"extensions.refstorage", "files"},
		{"extensions.worktreeConfig", "true"},
		{"remote.origin.pushurl", "https://example.com/push"},
		{"remote.origin.push", "refs/heads/main"},
		{"remote.origin.tagopt", "--no-tags"},
		{"remote.origin.mirror", "false"},
		{"remote.origin.prune", "true"},
		{"remote.origin.prunetags", "true"},
		{"remote.origin.promisor", "true"},
		{"remote.origin.partialclonefilter", "blob:none"},
		{"remote.origin.entiredb-original-url", "https://example.com/repo"},
		{"branch.main.rebase", "true"},
		{"branch.main.vscode-merge-base", "origin/main"},
		{"user.name", "Jane Doe"},
		{"user.email", "jane@example.com"},
		{"user.signingkey", "~/.ssh/id_ed25519.pub"},
		{"commit.gpgsign", "true"},
		{"tag.gpgsign", "true"},
		{"gpg.format", "ssh"},
	} {
		gitIn(t, dir, "config", kv[0], kv[1])
	}

	require.NoError(t, newGit(t).CheckFetchConfig(t.Context(), dir))
}

func TestCheckFetchConfigRefuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key, value, want string
	}{
		{key: "core.alternateRefsCommand", value: "touch /tmp/pwned", want: "core.alternaterefscommand"},
		{key: "core.sshCommand", value: "touch /tmp/pwned", want: "core.sshcommand"},
		{key: "credential.helper", value: "!touch /tmp/pwned", want: "credential.helper"},
		{key: "credential.https://example.com.helper", value: "!touch /tmp/pwned", want: "credential.https://example.com.helper"},
		{key: "url.https://evil.example/.insteadOf", value: "https://example.com/", want: "url.https://evil.example/.insteadof"},
		{key: "remote.other.uploadpack", value: "touch /tmp/pwned; git-upload-pack", want: "remote.other.uploadpack"},
		{key: "include.path", value: "/tmp/evil.config", want: "include.path"},
		{key: "includeIf.gitdir:/tmp/.path", value: "/tmp/evil.config", want: "includeif.gitdir:/tmp/.path"},
		{key: "core.foo", value: "bar", want: "core.foo"},
		{key: "core.fsmonitor", value: "touch /tmp/pwned", want: "core.fsmonitor"},
		{key: "remote.origin", value: "x", want: "remote.origin"},
		{key: "branch.rebase", value: "true", want: "branch.rebase"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()

			dir := newClone(t)
			gitIn(t, dir, "config", tt.key, tt.value)

			err := newGit(t).CheckFetchConfig(t.Context(), dir)

			require.ErrorIs(t, err, ErrUnsafeConfig)
			assert.Contains(t, err.Error(), ": "+tt.want+". Remove them, or fetch the gittuf refs with git yourself")
			assert.NotContains(t, err.Error(), tt.value, "values can hold secrets and are never shown")
		})
	}
}

func TestCheckFetchConfigMatchesNamesAsGitDoes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, config, want string
	}{
		{name: "mixed case", config: "[CORE]\n\tSSHCOMMAND = touch /tmp/pwned\n", want: "core.sshcommand"},
		{name: "deprecated subsection", config: "[remote.Origin]\n\tuploadpack = touch /tmp/pwned\n", want: "remote.origin.uploadpack"},
		{name: "mixed case subsection", config: "[Remote \"Up\"]\n\tUploadPack = touch /tmp/pwned\n", want: "remote.Up.uploadpack"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := newClone(t)
			appendConfig(t, dir, tt.config)

			err := newGit(t).CheckFetchConfig(t.Context(), dir)

			require.ErrorIs(t, err, ErrUnsafeConfig)
			assert.Contains(t, err.Error(), ": "+tt.want+".")
		})
	}

	dir := newClone(t)
	appendConfig(t, dir, "[CORE]\n\tFileMode = true\n[remote.mirror]\n\tURL = https://example.com\n[Branch \"Main\"]\n\tAnything = x\n")
	assert.NoError(t, newGit(t).CheckFetchConfig(t.Context(), dir), "allowed keys pass in any spelling git accepts")
}

func TestCheckFetchConfigListsEveryKeyOnce(t *testing.T) {
	t.Parallel()

	dir := newClone(t)
	appendConfig(t, dir, "[core]\n\tsshCommand = a\n[credential]\n\thelper = b\n\thelper = c\n[remote \"\x1b[31mred\"]\n\tuploadpack = d\n")

	err := newGit(t).CheckFetchConfig(t.Context(), dir)

	require.ErrorIs(t, err, ErrUnsafeConfig)
	assert.Contains(t, err.Error(), ": core.sshcommand, credential.helper, remote.")
	assert.NotContains(t, err.Error(), "\x1b", "keys are sanitized")
}

func TestCheckFetchConfigReadsWorktreeConfig(t *testing.T) {
	t.Parallel()

	dir := newClone(t)
	gitIn(t, dir, "config", "extensions.worktreeConfig", "true")
	gitIn(t, dir, "config", "--worktree", "core.sshCommand", "touch /tmp/pwned")
	require.NotContains(t, readFile(t, filepath.Join(dir, ".git", "config")), "sshCommand")

	err := newGit(t).CheckFetchConfig(t.Context(), dir)

	require.ErrorIs(t, err, ErrUnsafeConfig)
	assert.Contains(t, err.Error(), ": core.sshcommand.", "the worktree key is refused and the extension is not")
}

func TestCheckFetchConfigTrustsGlobalConfig(t *testing.T) {
	t.Parallel()

	global := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(global, []byte("[core]\n\tsshCommand = ssh -v\n[credential]\n\thelper = cache\n[url \"https://example.com/\"]\n\tinsteadOf = gh:\n"), 0o600))
	g := newGit(t, "GIT_CONFIG_GLOBAL="+global)
	dir := newClone(t)

	out, err := g.run(t.Context(), dir, "config", "--get", "core.sshCommand")
	require.NoError(t, err)
	require.Equal(t, "ssh -v\n", string(out), "git reads the global config")

	assert.NoError(t, g.CheckFetchConfig(t.Context(), dir))
}

func TestCheckFetchConfigFailsWhenGitCannotReadTheConfig(t *testing.T) {
	t.Parallel()

	dir := newClone(t)
	appendConfig(t, dir, "[core\n")

	err := newGit(t).CheckFetchConfig(t.Context(), dir)

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrUnsafeConfig)
	assert.Contains(t, err.Error(), "read repository config")
}

func TestRepositoryConfigKeys(t *testing.T) {
	t.Parallel()

	out := []byte("local\x00core.bare\nfalse\x00worktree\x00core.sparsecheckout\x00global\x00core.sshcommand\nssh\x00command\x00core.hookspath\n/dev/null\x00local\x00remote.origin.url\nhttps://example.com\nwith a newline\x00")

	keys, err := repositoryConfigKeys(out)

	require.NoError(t, err)
	assert.Equal(t, []string{"core.bare", "core.sparsecheckout", "remote.origin.url"}, keys)

	_, err = repositoryConfigKeys([]byte("local\x00core.bare\nfalse\x00local\x00"))
	require.Error(t, err)

	keys, err = repositoryConfigKeys(nil)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestSyncSkipsAlternateRefsCommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	upstream := filepath.Join(root, "upstream")
	gitIn(t, root, "init", "--quiet", "--", upstream)
	gitIn(t, upstream, "commit", "--quiet", "--allow-empty", "-m", "first")
	gitIn(t, upstream, "update-ref", "refs/gittuf/reference-state-log", "HEAD")
	alternate := filepath.Join(root, "alternate")
	gitIn(t, root, "init", "--quiet", "--", alternate)
	gitIn(t, alternate, "commit", "--quiet", "--allow-empty", "-m", "other")
	clone := filepath.Join(root, "clone")
	gitIn(t, root, "clone", "--quiet", "--", upstream, clone)

	marker := filepath.Join(root, "marker")
	require.NoError(t, os.WriteFile(filepath.Join(clone, ".git", "objects", "info", "alternates"), []byte(filepath.Join(alternate, ".git", "objects")+"\n"), 0o600))
	gitIn(t, clone, "config", "core.alternateRefsCommand", "touch "+marker+"; true")

	_, err := newGit(t).SyncGittufRefs(t.Context(), clone, "origin", true)

	require.NoError(t, err)
	assert.NoFileExists(t, marker, "the empty override stops git running the repository's command")

	gitIn(t, clone, "fetch", "--quiet", "--", "origin", "+refs/gittuf/*:refs/remotes/upstream/gittuf/*")
	assert.FileExists(t, marker, "a plain fetch runs it, so the override is what stopped it")
}

func newGit(t *testing.T, env ...string) *Git {
	t.Helper()

	g, err := Find()
	require.NoError(t, err)
	g.env = slices.Concat(isolatedEnv, env)
	return g
}

// isolatedEnv keeps every git process away from the developer's global and
// system config. Later entries override it.
var isolatedEnv = []string{
	"GIT_CONFIG_GLOBAL=" + os.DevNull,
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_CONFIG_COUNT=0",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_AUTHOR_NAME=Jane Doe",
	"GIT_AUTHOR_EMAIL=jane.doe@example.com",
	"GIT_COMMITTER_NAME=Jane Doe",
	"GIT_COMMITTER_EMAIL=jane.doe@example.com",
}

// newClone returns a repository whose config is what git clone writes.
func newClone(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	upstream := filepath.Join(root, "upstream")
	gitIn(t, root, "init", "--quiet", "--initial-branch=main", "--", upstream)
	gitIn(t, upstream, "commit", "--quiet", "--allow-empty", "-m", "first")
	clone := filepath.Join(root, "clone")
	gitIn(t, root, "clone", "--quiet", "--", upstream, clone)
	return clone
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), isolatedEnv...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

func appendConfig(t *testing.T, dir, text string) {
	t.Helper()

	f, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(text)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
