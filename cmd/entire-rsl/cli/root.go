// Package cli implements the entire-rsl commands.
package cli

import (
	"io"
	"log/slog"
	"runtime"

	"github.com/entireio/entire-rsl/internal/versioninfo"
	"github.com/spf13/cobra"
)

// envLogLevel is the Entire CLI's log level variable. The parent CLI forwards
// every ENTIRE_ variable, so plugins share it.
const envLogLevel = "ENTIRE_LOG_LEVEL"

// Options are the process resources the commands use. Tests substitute them.
type Options struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(key string) string
	Getwd  func() (string, error)

	// SetDefaultLogger installs the debug logger as the process default so
	// gittuf's own slog output reaches it. Nil leaves the default alone.
	SetDefaultLogger func(l *slog.Logger)
}

// app is the state shared by every command of one invocation.
type app struct {
	opts   Options
	logger *slog.Logger
	debug  bool
}

// NewRootCmd builds the entire-rsl command tree.
func NewRootCmd(opts Options) *cobra.Command {
	a := &app{opts: opts, logger: slog.New(slog.DiscardHandler)}

	root := &cobra.Command{
		Use:   "entire-rsl",
		Short: "Inspect the gittuf Reference State Log of a repository",
		Long: `Inspect the gittuf Reference State Log (RSL) of a repository: who pushed
a branch, tag or Entire checkpoint, when, and whether gittuf verification
passes for it.

entire-rsl is read-only. It never writes refs, objects or git config, except
for the remote-tracking RSL refs written by a fetch you confirm.

Exit codes:
  0  success, or verification passed
  1  error
  2  usage error
  3  verification failed, or the ref is not protected by gittuf policy`,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			a.configureLogging()
		},
	}

	if opts.Getenv("ENTIRE_CLI_VERSION") != "" {
		root.Annotations = map[string]string{cobra.CommandDisplayNameAnnotation: "entire rsl"}
	}

	root.SetIn(opts.Stdin)
	root.SetOut(opts.Stdout)
	root.SetErr(opts.Stderr)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return usageError(err)
	})
	root.PersistentFlags().BoolVar(&a.debug, "debug", false, "print diagnostics to stderr (also ENTIRE_LOG_LEVEL=debug)")

	root.AddCommand(
		newBranchCmd(a),
		newTagCmd(a),
		newCheckpointCmd(a),
		newVerifyCmd(a),
		newLogCmd(a),
		newEntryCmd(a),
		newAgentHelpCmd(),
		newVersionCmd(),
	)

	return root
}

// configureLogging sends debug logs to stderr when asked. gittuf logs through
// the default slog logger, so debug mode also installs the logger there, via
// Options.SetDefaultLogger, to surface gittuf's own diagnostics.
func (a *app) configureLogging() {
	if !a.debug && a.opts.Getenv(envLogLevel) != "debug" {
		return
	}

	handler := slog.NewTextHandler(a.opts.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	a.logger = slog.New(handler)
	if a.opts.SetDefaultLogger != nil {
		a.opts.SetDefaultLogger(a.logger)
	}

	info := versioninfo.Get()
	a.logger.Debug("entire-rsl starting",
		slog.String("version", info.Version),
		slog.String("commit", info.Commit),
		slog.String("go", info.GoVersion),
		slog.String("gittuf", info.GittufVersion),
		slog.String("os", runtime.GOOS),
		slog.String("arch", runtime.GOARCH),
		slog.String("entire_cli_version", a.opts.Getenv("ENTIRE_CLI_VERSION")),
	)
}

// usageArgs maps a positional-argument validator failure to ExitUsage.
func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return usageError(err)
		}
		return nil
	}
}
