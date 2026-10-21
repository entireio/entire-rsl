package cli

import (
	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/spf13/cobra"
)

type entryDocJSON struct {
	SchemaVersion int       `json:"schema_version"`
	Entry         entryJSON `json:"entry"`
}

func newEntryCmd(a *app) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "entry <id|number>",
		Short: "Show one RSL entry in full",
		Long: `Show one RSL entry in full, including the annotations that refer to it
and its custom fields.

Name the entry by its number, or by its ID or an ID prefix of at least seven
hex characters. Only entries reachable from the RSL tip are found.`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			sel, err := rslread.ParseSelector(args[0])
			if err != nil {
				return usageError(err)
			}

			s, err := a.open(cmd.Context())
			if err != nil {
				return err
			}

			e, err := s.reader.Find(cmd.Context(), sel)
			if err != nil {
				return err
			}

			if asJSON {
				return writeJSON(cmd.OutOrStdout(), entryDocJSON{SchemaVersion: schemaVersion, Entry: toEntryJSON(e)})
			}
			return writeFields(cmd.OutOrStdout(), entryFields(e))
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")

	return cmd
}
