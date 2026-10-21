package cli

import (
	"fmt"

	"github.com/entireio/entire-rsl/internal/refname"
	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/spf13/cobra"
)

const (
	defaultLogLimit = 50
	maxLogLimit     = 100_000
)

type logJSON struct {
	SchemaVersion int         `json:"schema_version"`
	Entries       []entryJSON `json:"entries"`
}

func newLogCmd(a *app) *cobra.Command {
	var (
		asJSON bool
		ref    string
		before string
		limit  int
	)

	cmd := &cobra.Command{
		Use:   "log",
		Short: "List RSL entries, newest first",
		Long: `List RSL entries, newest first: number, entry ID, type, ref, target, the
time the signer asserted, the signer, and whether an annotation skips the
entry.

The signer column names a key only when the entry's signature checks out
against it. Whether that key may sign the entry is gittuf policy's decision:
use 'entire rsl verify'.`,
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			if limit < 1 || limit > maxLogLimit {
				return usageError(fmt.Errorf("--limit must be between 1 and %d", maxLogLimit))
			}

			opts := rslread.LogOptions{Limit: limit}
			if ref != "" {
				if err := refname.Validate(ref); err != nil {
					return usageError(fmt.Errorf("--ref: %w", err))
				}
				opts.Ref = ref
			}
			if before != "" {
				sel, err := rslread.ParseSelector(before)
				if err != nil {
					return usageError(fmt.Errorf("--before: %w", err))
				}
				opts.Before = &sel
			}

			s, err := a.open(cmd.Context())
			if err != nil {
				return err
			}

			entries, err := s.reader.Log(cmd.Context(), opts)
			if err != nil {
				return err
			}

			if asJSON {
				out := logJSON{SchemaVersion: schemaVersion, Entries: make([]entryJSON, 0, len(entries))}
				for _, e := range entries {
					out.Entries = append(out.Entries, toEntryJSON(e))
				}
				return writeJSON(cmd.OutOrStdout(), out)
			}

			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No RSL entries match.")
				return nil
			}
			return writeLogTable(cmd.OutOrStdout(), entries)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	cmd.Flags().StringVar(&ref, "ref", "", "only entries for this full ref name, such as refs/heads/main")
	cmd.Flags().StringVar(&before, "before", "", "start below this entry (number or ID)")
	cmd.Flags().IntVar(&limit, "limit", defaultLogLimit, "maximum number of entries to list")

	return cmd
}
