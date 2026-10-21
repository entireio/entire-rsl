package cli

import (
	"github.com/entireio/entire-rsl/internal/refresolve"
	"github.com/spf13/cobra"
)

func newTagCmd(a *app) *cobra.Command {
	var flags refCommandFlags

	cmd := &cobra.Command{
		Use:   "tag <name>",
		Short: "Show what the RSL records for a tag",
		Long: `Show what the RSL records for a tag, like 'branch', and warn when the tag has
more than one unskipped RSL entry, which means it was moved.

The tag is named short (v1.0.0) or in full (refs/tags/v1.0.0).`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.validate(); err != nil {
				return err
			}
			ref, err := refresolve.Tag(args[0])
			if err != nil {
				return usageError(err)
			}
			return a.runRefReport(cmd, &flags, ref)
		},
	}
	flags.register(cmd)

	return cmd
}
