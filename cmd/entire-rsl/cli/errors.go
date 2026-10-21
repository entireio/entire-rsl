package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/entireio/entire-rsl/internal/redact"
	"github.com/entireio/entire-rsl/internal/sanitize"
)

// Exit codes. Only ExitOK means the command succeeded. ExitVerification is
// reserved for a verification outcome other than verified.
const (
	ExitOK           = 0
	ExitFailure      = 1
	ExitUsage        = 2
	ExitVerification = 3
	exitInterrupted  = 130
)

// ExitError carries the process exit code for an error. Silent marks an error
// whose message the command already printed.
type ExitError struct {
	Err    error
	Code   int
	Silent bool
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

func usageError(err error) error {
	return &ExitError{Err: err, Code: ExitUsage}
}

// silentExit ends the command with code after it has printed its own output.
func silentExit(code int, err error) error {
	return &ExitError{Err: err, Code: code, Silent: true}
}

// Report prints err to w and returns the exit code for it.
func Report(w io.Writer, err error) int {
	if err == nil {
		return ExitOK
	}

	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(w, "Interrupted.")
		return exitInterrupted
	}

	code := ExitFailure
	silent := false

	var exitErr *ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.Code
		silent = exitErr.Silent
	case isCobraUsageError(err):
		code = ExitUsage
	}

	if !silent {
		fmt.Fprintln(w, "Error: "+sanitize.Lines(redact.URLs(err.Error())))
	}
	if code == ExitUsage {
		fmt.Fprintln(w, "Run 'entire rsl --help' for usage.")
	}

	return code
}

// isCobraUsageError recognises the errors cobra raises before any command runs.
// Cobra exposes no type for them, so the stable message prefixes are the
// discriminator.
func isCobraUsageError(err error) bool {
	msg := err.Error()
	return strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "unknown flag") ||
		strings.HasPrefix(msg, "unknown shorthand flag")
}
