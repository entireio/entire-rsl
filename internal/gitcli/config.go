package gitcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/entireio/entire-rsl/internal/sanitize"
)

// ErrUnsafeConfig is returned when the repository's own config has a key
// outside fetchAllowlist.
var ErrUnsafeConfig = errors.New("the repository's git config has keys entire-rsl will not fetch with")

// allowed lists the keys of one config section a fetch may run with.
type allowed struct {
	keys []string
	// anyKey allows every key of the section.
	anyKey bool
	// subsection requires the key to be in a subsection, as in
	// remote.<name>.url, and forbids it otherwise.
	subsection bool
}

// fetchAllowlist is every repository config key a fetch may run with, by
// section, lowercased as git prints them. Any other key refuses the fetch,
// so a setting that makes git run a program fails closed even when it is new
// to entire-rsl. branch.<name>.* is allowed whole: git reads it only to pick
// upstreams, and editors keep their own keys there.
var fetchAllowlist = map[string]allowed{
	"core": {keys: []string{
		"repositoryformatversion", "filemode", "bare", "logallrefupdates",
		"ignorecase", "precomposeunicode", "symlinks",
	}},
	"extensions": {keys: []string{"objectformat", "refstorage"}},
	"remote": {subsection: true, keys: []string{
		"url", "pushurl", "fetch", "push", "tagopt", "mirror", "prune",
		"prunetags", "promisor", "partialclonefilter", "entiredb-original-url",
	}},
	"branch": {subsection: true, anyKey: true},
	"user":   {keys: []string{"name", "email", "signingkey"}},
	"commit": {keys: []string{"gpgsign"}},
	"tag":    {keys: []string{"gpgsign"}},
	"gpg":    {keys: []string{"format"}},
}

// repositoryScopes are the config scopes the repository controls: .git/config
// and config.worktree. Global and system config are the user's own, and the
// command scope holds entire-rsl's own overrides.
var repositoryScopes = []string{"local", "worktree"}

// CheckFetchConfig refuses a fetch in root when the repository's own config
// has a key outside fetchAllowlist. git lists the config, so names match the
// way git matches them, config.worktree is included, and include files are
// not followed: an include key is itself refused.
func (g *Git) CheckFetchConfig(ctx context.Context, root string) error {
	out, err := g.run(ctx, root, "config", "--list", "-z", "--show-scope", "--no-includes")
	if err != nil {
		return fmt.Errorf("read repository config: %w", err)
	}
	keys, err := repositoryConfigKeys(out)
	if err != nil {
		return err
	}

	var refused []string
	for _, key := range keys {
		if !fetchAllowed(key) && !slices.Contains(refused, sanitize.Line(key)) {
			refused = append(refused, sanitize.Line(key))
		}
	}
	if len(refused) > 0 {
		return fmt.Errorf("%w: %s. Remove them, or fetch the gittuf refs with git yourself", ErrUnsafeConfig, strings.Join(refused, ", "))
	}
	return nil
}

// repositoryConfigKeys reads the keys of the repository scopes from
// `git config --list -z --show-scope`, in the order git prints them. Each
// entry is the scope, a NUL, then the key, and its value after a newline when
// it has one, then a NUL.
func repositoryConfigKeys(out []byte) ([]string, error) {
	fields := bytes.Split(out, []byte{0})
	if last := len(fields) - 1; len(fields[last]) == 0 {
		fields = fields[:last]
	}
	if len(fields)%2 != 0 {
		return nil, errors.New("read repository config: git printed an incomplete entry")
	}

	var keys []string
	for i := 0; i < len(fields); i += 2 {
		if !slices.Contains(repositoryScopes, string(fields[i])) {
			continue
		}
		key, _, _ := bytes.Cut(fields[i+1], []byte{'\n'})
		keys = append(keys, string(key))
	}
	return keys, nil
}

// fetchAllowed reports whether a key, as section[.subsection].key, is in
// fetchAllowlist. The section and key compare case-insensitively, as git
// does, and any subsection passes.
func fetchAllowed(key string) bool {
	first, last := strings.Index(key, "."), strings.LastIndex(key, ".")
	if first < 0 {
		return false
	}
	section, name := strings.ToLower(key[:first]), strings.ToLower(key[last+1:])
	hasSubsection := first != last

	a, ok := fetchAllowlist[section]
	if !ok || a.subsection != hasSubsection {
		return false
	}
	return a.anyKey || slices.Contains(a.keys, name)
}
