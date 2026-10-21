package cli

import (
	"github.com/entireio/entire-rsl/internal/refresolve"
	"github.com/spf13/cobra"
)

func newBranchCmd(a *app) *cobra.Command {
	var flags refCommandFlags

	cmd := &cobra.Command{
		Use:   "branch <name>",
		Short: "Show what the RSL records for a branch",
		Long: `Show what the RSL records for a branch: the tip the RSL records next to the
local tip, the latest entry with who pushed it, when and the signer, and the
recent history.

The branch is named short (main) or in full (refs/heads/main).`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.validate(); err != nil {
				return err
			}
			ref, err := refresolve.Branch(args[0])
			if err != nil {
				return usageError(err)
			}
			return a.runRefReport(cmd, &flags, ref)
		},
	}
	flags.register(cmd)

	return cmd
}
