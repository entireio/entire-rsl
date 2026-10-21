package cli

import (
	"fmt"

	"github.com/entireio/entire-rsl/internal/versioninfo"
	"github.com/spf13/cobra"
)

type versionJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	GoVersion     string `json:"go_version"`
	GittufVersion string `json:"gittuf_version"`
}

func newVersionCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the entire-rsl version",
		Args:  usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error {
			info := versioninfo.Get()
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), versionJSON{
					SchemaVersion: schemaVersion,
					Version:       info.Version,
					Commit:        info.Commit,
					GoVersion:     info.GoVersion,
					GittufVersion: info.GittufVersion,
				})
			}

			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "entire-rsl %s (commit %s)\n", info.Version, info.Commit)
			fmt.Fprintf(w, "Go:     %s\n", info.GoVersion)
			fmt.Fprintf(w, "gittuf: %s\n", info.GittufVersion)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")

	return cmd
}
