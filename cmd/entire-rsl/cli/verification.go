package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/entireio/entire-rsl/internal/redact"
	"github.com/entireio/entire-rsl/internal/sanitize"
	"github.com/entireio/entire-rsl/internal/verify"
	"github.com/spf13/cobra"
)

const defaultVerifyTimeout = 5 * time.Minute

type verificationJSON struct {
	State       string   `json:"state"`
	Reason      string   `json:"reason"`
	Error       string   `json:"error,omitempty"`
	VerifiedTip string   `json:"verified_tip,omitempty"`
	FromEntry   string   `json:"from_entry,omitempty"`
	Rules       []string `json:"rules,omitempty"`
}

func toVerificationJSON(r verify.Result) verificationJSON {
	j := verificationJSON{State: string(r.State), Reason: r.Reason, Rules: r.Rules}
	if r.Err != nil {
		j.Error = redact.URLs(r.Err.Error())
	}
	if len(r.ExpectedTip) != 0 {
		j.VerifiedTip = r.ExpectedTip.String()
	}
	if len(r.FromEntry) != 0 {
		j.FromEntry = r.FromEntry.String()
	}
	return j
}

// verificationFields describes a result for a detail view.
func verificationFields(r verify.Result) []field {
	fields := []field{{"Verification", string(r.State) + ": " + r.Reason}}
	if r.Err != nil {
		fields = append(fields, field{"Detail", sanitize.Lines(redact.URLs(r.Err.Error()))})
	}
	if len(r.ExpectedTip) != 0 {
		fields = append(fields, field{"Verified tip", r.ExpectedTip.String()})
	}
	if len(r.FromEntry) != 0 {
		fields = append(fields, field{"Checked from", r.FromEntry.String()})
	}
	if len(r.Rules) != 0 {
		fields = append(fields, field{"Rules", sanitize.Line(strings.Join(r.Rules, ", "))})
	}
	return fields
}

// verifyFlags are the verification flags of the detail commands.
type verifyFlags struct {
	timeout  time.Duration
	noVerify bool
}

func (f *verifyFlags) register(cmd *cobra.Command, optional bool) {
	if optional {
		cmd.Flags().BoolVar(&f.noVerify, "no-verify", false, "skip gittuf verification")
	}
	cmd.Flags().DurationVar(&f.timeout, "timeout", defaultVerifyTimeout, "maximum time verification may take")
}

func (f *verifyFlags) validate() error {
	if f.timeout <= 0 {
		return usageError(fmt.Errorf("--timeout must be positive, got %s", f.timeout))
	}
	return nil
}

// verify runs full gittuf verification for ref.
func (a *app) verify(ctx context.Context, s *session, ref string, flags verifyFlags) verify.Result {
	return verify.Verify(ctx, s.repo.Store, ref, verify.Options{
		Logger:   a.logger,
		RepoPath: s.repo.Root,
		Timeout:  flags.timeout,
	})
}

// exitCodeFor maps a verification state to the verify command's exit code.
func exitCodeFor(state verify.State) int {
	switch state {
	case verify.Verified:
		return ExitOK
	case verify.Failed, verify.Unprotected:
		return ExitVerification
	case verify.Error:
		return ExitFailure
	default:
		return ExitFailure
	}
}
