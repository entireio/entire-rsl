package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"text/tabwriter"

	"github.com/entireio/entire-rsl/internal/gitcli"
	"github.com/entireio/entire-rsl/internal/sanitize"
	"github.com/gittuf/gittuf/pkg/githash"
	pkgrsl "github.com/gittuf/gittuf/pkg/rsl"
	"github.com/spf13/cobra"
)

type rslEntriesJSON struct {
	Old uint64 `json:"old,omitempty"`
	New uint64 `json:"new"`
}

type refreshRefJSON struct {
	RSLEntries   *rslEntriesJSON `json:"rsl_entries,omitempty"`
	Ref          string          `json:"ref"`
	Status       string          `json:"status"`
	Old          string          `json:"old,omitempty"`
	New          string          `json:"new"`
	Commits      int             `json:"commits"`
	LocalCommits int             `json:"local_commits,omitempty"`
	Symbolic     bool            `json:"symbolic,omitempty"`
}

// rslEntries are the RSL entry numbers before and after a refresh. Old is 0
// when the RSL was created.
type rslEntries struct {
	old, new uint64
}

type refreshJSON struct {
	SchemaVersion int              `json:"schema_version"`
	Remote        string           `json:"remote"`
	Refs          []refreshRefJSON `json:"refs"`
}

func newRefreshCmd(a *app) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "refresh [remote]",
		Short: "Fetch the latest gittuf RSL and policy from a remote",
		Long: `Fetch the remote's gittuf refs (the RSL, the policy and the other
refs/gittuf/* refs) into refs/remotes/<remote>/gittuf/*, then update the local
refs/gittuf/* refs:

  created    the ref did not exist locally
  updated    the remote's ref fast-forwarded the local one
  unchanged  the local ref already matched
  diverged   the remote's ref does not descend from the local one, so the
             local ref was left as it was. The table is still printed, then
             an error names each diverged ref (exit 1)

Only gittuf refs are written. Branches, tags and checkpoint refs are not
fetched: fetch them with git to compare them with the RSL.

With no remote named, the only remote is used, or one is picked on a
terminal.`,
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.openRepo(cmd.Context())
			if err != nil {
				return err
			}

			want := ""
			if len(args) == 1 {
				want = args[0]
			}
			choice, err := a.chooseRemote(r, want,
				errors.New("no remote is configured to refresh from"),
				"several remotes are configured, name one: entire rsl refresh <remote>")
			if err != nil {
				return err
			}

			updates, err := a.syncGittufRefs(cmd.Context(), r, choice.name, true)
			if err != nil {
				return err
			}
			entries := a.rslEntryNumbers(cmd.Context(), updates)

			if asJSON {
				out := refreshJSON{SchemaVersion: schemaVersion, Remote: choice.name, Refs: make([]refreshRefJSON, 0, len(updates))}
				for _, u := range updates {
					ref := refreshRefJSON{Ref: u.Ref, Status: u.Status, Old: u.Old, New: u.New, Commits: u.Commits, LocalCommits: u.LocalCommits, Symbolic: u.Symbolic}
					if e, ok := entries[u.Ref]; ok {
						ref.RSLEntries = &rslEntriesJSON{Old: e.old, New: e.new}
					}
					out.Refs = append(out.Refs, ref)
				}
				if err := writeJSON(cmd.OutOrStdout(), out); err != nil {
					return err
				}
			} else if err := writeRefreshTable(cmd, a.styles(), updates, entries); err != nil {
				return err
			}

			return divergedError(choice.name, updates)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")

	return cmd
}

// rslEntryNumbers reads the RSL entry numbers before and after the refresh
// for the RSL ref, from a repository opened afresh to see the fetched
// objects. Numbers only label the change, so when one cannot be read, or an
// entry is unnumbered (gittuf numbers entries from 1, and 0 means none), both
// are left out.
func (a *app) rslEntryNumbers(ctx context.Context, updates []gitcli.RefUpdate) map[string]rslEntries {
	out := map[string]rslEntries{}
	idx := slices.IndexFunc(updates, func(u gitcli.RefUpdate) bool { return u.Ref == pkgrsl.Ref })
	if idx < 0 || updates[idx].Status == gitcli.RefUnchanged {
		return out
	}
	u := updates[idx]

	r, err := a.openRepo(ctx)
	if err != nil {
		a.logger.DebugContext(ctx, "reopen repository for RSL entry numbers", slog.String("error", err.Error()))
		return out
	}
	number := func(id string) (uint64, bool) {
		h, err := githash.NewHash(id)
		if err != nil {
			return 0, false
		}
		entry, err := pkgrsl.GetEntry(r.Store, h)
		if err != nil {
			a.logger.DebugContext(ctx, "read RSL entry number", slog.String("entry", id), slog.String("error", err.Error()))
			return 0, false
		}
		n := entry.GetNumber()
		return n, n > 0
	}

	var e rslEntries
	var ok bool
	if e.new, ok = number(u.New); !ok {
		return out
	}
	if u.Old != "" {
		if e.old, ok = number(u.Old); !ok {
			return out
		}
	}
	out[u.Ref] = e
	return out
}

// commitsText describes how many commits a refresh brought, with the RSL
// entry numbers for the RSL.
func commitsText(u gitcli.RefUpdate, entries map[string]rslEntries) string {
	var text string
	switch u.Status {
	case gitcli.RefCreated:
		text = plural(u.Commits, "commit", "commits")
	case gitcli.RefUpdated:
		text = "+" + plural(u.Commits, "commit", "commits")
	case gitcli.RefDiverged:
		text = fmt.Sprintf("remote +%d, local +%d", u.Commits, u.LocalCommits)
		if u.Symbolic {
			text += ", local ref is symbolic"
		}
	default:
		return none
	}

	if e, ok := entries[u.Ref]; ok {
		if e.old == 0 {
			text += fmt.Sprintf(" (entry #%d)", e.new)
		} else {
			text += fmt.Sprintf(" (entry #%d \u2192 #%d)", e.old, e.new)
		}
	}
	return text
}

func writeRefreshTable(cmd *cobra.Command, st styles, updates []gitcli.RefUpdate, entries map[string]rslEntries) error {
	w := cmd.OutOrStdout()
	if len(updates) == 0 {
		fmt.Fprintln(w, "The remote has no gittuf refs.")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "REF\t"+st.plain("STATUS")+"\tLOCAL\tREMOTE\tCOMMITS")
	for _, u := range updates {
		old := none
		if u.Old != "" {
			old = u.Old[:min(shortIDLen, len(u.Old))]
		}
		status := u.Status
		switch u.Status {
		case gitcli.RefDiverged:
			status = st.bad(status)
		case gitcli.RefCreated, gitcli.RefUpdated:
			status = st.good(status)
		default:
			status = st.plain(status)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", sanitize.Line(u.Ref), status, old, u.New[:min(shortIDLen, len(u.New))], commitsText(u, entries))
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write table: %w", err)
	}
	return nil
}
