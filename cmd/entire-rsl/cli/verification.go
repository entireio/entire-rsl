package cli

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/entireio/entire-rsl/internal/clusters"
	"github.com/entireio/entire-rsl/internal/redact"
	"github.com/entireio/entire-rsl/internal/sanitize"
	"github.com/entireio/entire-rsl/internal/verify"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

const defaultVerifyTimeout = 5 * time.Minute

type checkJSON struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type verificationJSON struct {
	State       string      `json:"state"`
	Reason      string      `json:"reason"`
	Error       string      `json:"error,omitempty"`
	VerifiedTip string      `json:"verified_tip,omitempty"`
	FromEntry   string      `json:"from_entry,omitempty"`
	Rules       []string    `json:"rules,omitempty"`
	Checks      []checkJSON `json:"checks,omitempty"`
}

func toVerificationJSON(r verify.Result) verificationJSON {
	j := verificationJSON{State: string(r.State), Reason: redact.URLs(r.Reason), Rules: r.Rules}
	if r.Err != nil {
		j.Error = redact.URLs(r.Err.Error())
	}
	if len(r.ExpectedTip) != 0 {
		j.VerifiedTip = r.ExpectedTip.String()
	}
	if len(r.FromEntry) != 0 {
		j.FromEntry = r.FromEntry.String()
	}
	for _, c := range r.Checks {
		j.Checks = append(j.Checks, checkJSON{Name: c.Name, Status: string(c.Status), Detail: redact.URLs(c.Detail)})
	}
	return j
}

// verificationFields describes a result for a detail view.
func verificationFields(r verify.Result) []field {
	fields := []field{{"Verification", string(r.State) + ": " + sanitize.Line(redact.URLs(r.Reason))}}
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
	for _, c := range r.Checks {
		fields = append(fields, field{"Check " + c.Name, string(c.Status) + ": " + sanitize.Line(redact.URLs(c.Detail))})
	}
	return fields
}

// verifyFlags are the verification flags of the detail commands.
type verifyFlags struct {
	rootKeys []string
	// roots are rootKeys parsed by validate.
	roots    []clusters.Key
	timeout  time.Duration
	noVerify bool
}

func (f *verifyFlags) register(cmd *cobra.Command, optional bool) {
	if optional {
		cmd.Flags().BoolVar(&f.noVerify, "no-verify", false, "skip gittuf verification")
	}
	cmd.Flags().DurationVar(&f.timeout, "timeout", defaultVerifyTimeout, "maximum time verification may take")
	cmd.Flags().StringArrayVar(&f.rootKeys, "root-key", nil, "also accept this root of trust, an ed25519 key as an \"ssh-ed25519 ...\" line or @file (repeatable)")
}

func (f *verifyFlags) validate() error {
	if f.timeout <= 0 {
		return usageError(fmt.Errorf("--timeout must be positive, got %s", f.timeout))
	}
	f.roots = nil
	for _, v := range f.rootKeys {
		k, err := parseRootKey(v)
		if err != nil {
			return usageError(fmt.Errorf("--root-key: %w", err))
		}
		f.roots = append(f.roots, k)
	}
	return nil
}

// maxKeyFile caps a --root-key @file.
const maxKeyFile = 64 << 10

// parseRootKey reads an ed25519 SSH public key line, or @path to a file
// holding one.
func parseRootKey(v string) (clusters.Key, error) {
	line := []byte(v)
	if path, ok := strings.CutPrefix(v, "@"); ok {
		data, err := readSmallFile(path, maxKeyFile)
		if err != nil {
			return clusters.Key{}, err
		}
		line = data
	}

	pub, _, _, _, err := ssh.ParseAuthorizedKey(line)
	if err != nil {
		return clusters.Key{}, fmt.Errorf("parse SSH public key: %w", err)
	}
	cryptoPub, ok := pub.(ssh.CryptoPublicKey)
	if !ok {
		return clusters.Key{}, errors.New("unsupported SSH public key")
	}
	edPub, ok := cryptoPub.CryptoPublicKey().(ed25519.PublicKey)
	if !ok {
		return clusters.Key{}, fmt.Errorf("only ed25519 root keys are supported, got %s", pub.Type())
	}
	return clusters.FromPublicKey(edPub, clusters.RoleFleetRoot, clusters.EnvironmentUser)
}

func readSmallFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // the user names their own key file
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("key file %s is larger than %d bytes", path, limit)
	}
	return data, nil
}

// verify runs full gittuf verification for ref, then entire-rsl's checks,
// with local as the local ref lookup.
func (a *app) verify(ctx context.Context, s *session, ref string, flags verifyFlags, local localTip) verify.Result {
	keys := slices.Concat(clusters.Known(), a.opts.TrustedKeys, flags.roots)
	return verify.Verify(ctx, s.repo.Store, ref, verify.Options{
		Logger:   a.logger,
		RepoPath: s.repo.Root,
		Timeout:  flags.timeout,
		LocalTip: local,
		Keys:     keys,
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
