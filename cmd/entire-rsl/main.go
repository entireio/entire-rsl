// Command entire-rsl inspects the gittuf Reference State Log of a repository.
// The Entire CLI dispatches it as `entire rsl`.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/entireio/entire-rsl/cmd/entire-rsl/cli"
)

func main() {
	os.Exit(run())
}

func run() int {
	// The Entire CLI forwards Ctrl-C and sends SIGINT with a five second grace
	// period when it is cancelled. The first signal cancels the context so work
	// unwinds. stop then restores the default disposition, so a second signal
	// terminates the process.
	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	root := cli.NewRootCmd(cli.Options{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Getenv: os.Getenv,

		SetDefaultLogger: slog.SetDefault,
	})

	return cli.Report(os.Stderr, root.ExecuteContext(ctx))
}

func terminationSignals() []os.Signal {
	if runtime.GOOS == "windows" {
		return []os.Signal{os.Interrupt}
	}
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
