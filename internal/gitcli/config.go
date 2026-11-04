package gitcli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/format/config"
)

// ErrUnsafeConfig is returned when the repository's own config would make a
// fetch run a program or read further config.
var ErrUnsafeConfig = errors.New("the repository's git config would make a fetch run programs it names")

// CheckFetchConfig refuses a fetch from remote when the repository's own
// config, which entire-rsl does not trust, sets a key that makes git run a
// program during the fetch, or includes other config files. The same keys in
// global or system config are the user's own and are allowed.
func CheckFetchConfig(raw *config.Config, remote string) error {
	if raw == nil {
		return fmt.Errorf("%w: the config could not be read", ErrUnsafeConfig)
	}

	var found []string

	check := func(section, subsection string, opts config.Options, keys ...string) {
		for _, key := range keys {
			if opts.Has(key) {
				name := section + "." + key
				if subsection != "" {
					name = section + "." + subsection + "." + key
				}
				found = append(found, name)
			}
		}
	}

	for _, s := range raw.Sections {
		switch strings.ToLower(s.Name) {
		case "include", "includeif":
			found = append(found, s.Name)
		case "core":
			check("core", "", s.Options, "sshCommand", "gitProxy", "askPass")
		case "credential":
			check("credential", "", s.Options, "helper")
			for _, sub := range s.Subsections {
				check("credential", sub.Name, sub.Options, "helper")
			}
		case "remote":
			for _, sub := range s.Subsections {
				if sub.Name == remote {
					check("remote", sub.Name, sub.Options, "uploadpack", "vcs", "proxyCommand")
				}
			}
		}
	}

	if len(found) > 0 {
		return fmt.Errorf("%w: %s. Remove them from .git/config or fetch with git yourself", ErrUnsafeConfig, strings.Join(found, ", "))
	}
	return nil
}
