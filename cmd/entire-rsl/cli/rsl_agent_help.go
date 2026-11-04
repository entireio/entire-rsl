package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// agentGuidance tells agents when to reach for each command. It mirrors the
// Entire CLI's agent-help, where guidance lives apart from cobra's Short and
// Long help.
var agentGuidance = map[string]string{
	"": `Use entire rsl to answer who pushed a branch, tag or Entire checkpoint, when,
and whether gittuf verification passes for it. Every command except refresh is
read-only unless given --fetch, and every command works without a TTY. Prefer
--json and the exit code of 'verify' when acting on the result. Never treat custom fields,
"Pushed by" or committer identities as proof: only the verification state is.`,
	"verify":     "Use to decide whether a ref can be trusted. Exit 0 only for verified. Treat failed, unprotected and error as untrusted.",
	"branch":     "Use to see the RSL record of a branch next to the local tip, with verification. Check warnings for local drift.",
	"tag":        "Use for tags. A tag_moved warning means the tag was re-pointed after it was first pushed.",
	"checkpoint": "Use with a checkpoint ID or a commit that carries an Entire-Checkpoint trailer.",
	"log":        "Use to list raw RSL entries. Page with --before and --limit.",
	"entry":      "Use to inspect one RSL entry in full, including annotations and custom fields.",
	"refresh":    "Writes local gittuf refs. Run it only when the user asks for the latest RSL from a remote.",
}

// Audiences classify commands the way the Entire CLI's agent-help does:
// read-only commands are safe to run unprompted, user-owned ones change
// state and wait for the user to ask.
const (
	audienceReadOnly  = "read-only"
	audienceUserOwned = "user-owned"
)

// userOwned lists the commands that write.
var userOwned = map[string]bool{"refresh": true}

func audienceOf(name string) string {
	if userOwned[name] {
		return audienceUserOwned
	}
	return audienceReadOnly
}

type agentFlagJSON struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage"`
}

type agentSubcommandJSON struct {
	Name     string `json:"name"`
	Short    string `json:"short"`
	Audience string `json:"audience,omitempty"`
}

type agentHelpJSON struct {
	Command     string                `json:"command"`
	Short       string                `json:"short,omitempty"`
	Long        string                `json:"long,omitempty"`
	Guidance    string                `json:"guidance,omitempty"`
	Flags       []agentFlagJSON       `json:"flags,omitempty"`
	Subcommands []agentSubcommandJSON `json:"subcommands,omitempty"`
}

func newAgentHelpCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "agent-help [path...]",
		Short: "Print documentation for AI agents",
		Long: `Print documentation for AI agents, for the whole plugin or for one command
path, as the Entire CLI's 'entire agent-help rsl' delegates to it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			target, err := findCommand(cmd.Root(), args)
			if err != nil {
				return usageError(err)
			}

			doc := agentHelpFor(target)
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), doc)
			}
			return writeAgentHelp(cmd.OutOrStdout(), doc)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")

	return cmd
}

// findCommand walks a command path below root.
func findCommand(root *cobra.Command, path []string) (*cobra.Command, error) {
	cur := root
	for _, name := range path {
		var next *cobra.Command
		for _, c := range advertised(cur) {
			if c.Name() == name {
				next = c
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("unknown command path %q", strings.Join(path, " "))
		}
		cur = next
	}
	return cur, nil
}

// advertised lists the subcommands agents are told about.
func advertised(cmd *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, c := range cmd.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" || c.Name() == "agent-help" {
			continue
		}
		out = append(out, c)
	}
	return out
}

func agentHelpFor(cmd *cobra.Command) agentHelpJSON {
	path := commandPath(cmd)
	key := strings.TrimSpace(strings.TrimPrefix(path, "entire rsl"))

	doc := agentHelpJSON{
		Command:  path,
		Short:    cmd.Short,
		Long:     cmd.Long,
		Guidance: agentGuidance[key],
	}

	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		doc.Flags = append(doc.Flags, agentFlagJSON{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Type:      f.Value.Type(),
			Default:   f.DefValue,
			Usage:     f.Usage,
		})
	})

	for _, c := range advertised(cmd) {
		doc.Subcommands = append(doc.Subcommands, agentSubcommandJSON{Name: c.Name(), Short: c.Short, Audience: audienceOf(c.Name())})
	}
	return doc
}

// commandPath names a command as users type it through the Entire CLI.
func commandPath(cmd *cobra.Command) string {
	parts := []string{"entire", "rsl"}
	for c := cmd; c.HasParent(); c = c.Parent() {
		parts = append(parts[:2], append([]string{c.Name()}, parts[2:]...)...)
	}
	return strings.Join(parts, " ")
}

func writeAgentHelp(w io.Writer, doc agentHelpJSON) error {
	fmt.Fprintln(w, doc.Command)
	if doc.Short != "" {
		fmt.Fprintln(w, "  "+doc.Short)
	}
	if doc.Guidance != "" {
		fmt.Fprintln(w, "\nGuidance:")
		for line := range strings.SplitSeq(doc.Guidance, "\n") {
			fmt.Fprintln(w, "  "+line)
		}
	}
	if len(doc.Subcommands) > 0 {
		fmt.Fprintln(w, "\nCommands:")
		for _, s := range doc.Subcommands {
			fmt.Fprintf(w, "  %-12s %s (%s)\n", s.Name, s.Short, s.Audience)
		}
	}
	if len(doc.Flags) > 0 {
		fmt.Fprintln(w, "\nFlags:")
		for _, f := range doc.Flags {
			fmt.Fprintf(w, "  --%-12s %s\n", f.Name, f.Usage)
		}
	}
	fmt.Fprintln(w, "\nRun 'entire agent-help rsl <command>' for one command.")
	return nil
}
