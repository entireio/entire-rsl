package cli

import (
	"errors"
	"fmt"
	"text/tabwriter"

	"github.com/entireio/entire-rsl/internal/gitcli"
	"github.com/entireio/entire-rsl/internal/sanitize"
	"github.com/spf13/cobra"
)

// errDiverged is returned when a local gittuf ref was left alone because the
// remote's does not descend from it.
var errDiverged = errors.New("some local gittuf refs have diverged from the remote and were left unchanged")

type refreshRefJSON struct {
	Ref    string `json:"ref"`
	Status string `json:"status"`
	Old    string `json:"old,omitempty"`
	New    string `json:"new"`
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
             local ref was left as it was (exit 1)

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

			diverged := false
			for _, u := range updates {
				diverged = diverged || u.Status == gitcli.RefDiverged
			}

			if asJSON {
				out := refreshJSON{SchemaVersion: schemaVersion, Remote: choice.name, Refs: make([]refreshRefJSON, 0, len(updates))}
				for _, u := range updates {
					out.Refs = append(out.Refs, refreshRefJSON{Ref: u.Ref, Status: u.Status, Old: u.Old, New: u.New})
				}
				if err := writeJSON(cmd.OutOrStdout(), out); err != nil {
					return err
				}
			} else if err := writeRefreshTable(cmd, a.styles(), updates); err != nil {
				return err
			}

			if diverged {
				return silentExit(ExitFailure, errDiverged)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")

	return cmd
}

func writeRefreshTable(cmd *cobra.Command, st styles, updates []gitcli.RefUpdate) error {
	w := cmd.OutOrStdout()
	if len(updates) == 0 {
		fmt.Fprintln(w, "The remote has no gittuf refs.")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "REF\t"+st.plain("STATUS")+"\tLOCAL\tREMOTE")
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
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", sanitize.Line(u.Ref), status, old, u.New[:min(shortIDLen, len(u.New))])
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write table: %w", err)
	}

	for _, u := range updates {
		if u.Status == gitcli.RefDiverged {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s was left at %s: the remote's %s does not descend from it\n", sanitize.Line(u.Ref), u.Old, u.New)
		}
	}
	return nil
}
