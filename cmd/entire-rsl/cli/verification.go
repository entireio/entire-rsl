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
	"github.com/entireio/entire-rsl/internal/rslread"
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

type checkedEntryJSON struct {
	Number uint64 `json:"number,omitempty"`
	ID     string `json:"id"`
}

type entriesCheckedJSON struct {
	First   checkedEntryJSON `json:"first"`
	Last    checkedEntryJSON `json:"last"`
	Count   int              `json:"count"`
	Skipped int              `json:"skipped"`
}

type verificationJSON struct {
	State       string              `json:"state"`
	Reason      string              `json:"reason"`
	Error       string              `json:"error,omitempty"`
	VerifiedTip string              `json:"verified_tip,omitempty"`
	FromEntry   string              `json:"from_entry,omitempty"`
	FromNumber  uint64              `json:"from_entry_number,omitempty"`
	Checked     *entriesCheckedJSON `json:"entries_checked,omitempty"`
	Rules       []string            `json:"rules,omitempty"`
	Checks      []checkJSON         `json:"checks,omitempty"`
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
		j.FromEntry, j.FromNumber = r.FromEntry.String(), r.FromNumber
	}
	if c := r.Checked; c != nil {
		j.Checked = &entriesCheckedJSON{
			First:   checkedEntryJSON{Number: c.First.Number, ID: c.First.ID.String()},
			Last:    checkedEntryJSON{Number: c.Last.Number, ID: c.Last.ID.String()},
			Count:   c.Count,
			Skipped: c.Skipped,
		}
	}
	for _, c := range r.Checks {
		j.Checks = append(j.Checks, checkJSON{Name: c.Name, Status: string(c.Status), Detail: redact.URLs(c.Detail)})
	}
	return j
}

// verificationFields describes a result for a detail view, the state green
// when verified, red when failed or in error, yellow when unprotected.
func verificationFields(r verify.Result, st styles) []field {
	fields := []field{{labelVerification, stateStyle(r.State, st) + ": " + sanitize.Line(redact.URLs(r.Reason))}}
	if r.Err != nil {
		fields = append(fields, field{"Detail", sanitize.Lines(redact.URLs(r.Err.Error()))})
	}
	if len(r.ExpectedTip) != 0 {
		fields = append(fields, field{"Verified tip", r.ExpectedTip.String()})
	}
	if r.Checked != nil {
		fields = append(fields, field{"RSL entries checked", checkedText(*r.Checked)})
	}
	if len(r.Rules) != 0 {
		fields = append(fields, field{"Rules", sanitize.Line(strings.Join(r.Rules, ", "))})
	}
	for _, c := range r.Checks {
		fields = append(fields, field{"Check " + c.Name, checkStyle(c.Status, st) + ": " + sanitize.Line(redact.URLs(c.Detail))})
	}
	return fields
}

// checkedText describes the range of RSL entries verification covered, such
// as "#3 d424aa2b5963 -> #9 2f3106dcfec6 (3 entries)".
func checkedText(c verify.Checked) string {
	text := entryLabel(c.First)
	if !c.First.ID.Equal(c.Last.ID) {
		text += " \u2192 " + entryLabel(c.Last)
	}
	text += " (" + plural(c.Count, "entry", "entries")
	if c.Skipped > 0 {
		text += fmt.Sprintf(", %d skipped", c.Skipped)
	}
	return text + ")"
}

// entryLabel is an entry's number and short ID, the ID alone when the entry
// is unnumbered.
func entryLabel(t rslread.Target) string {
	if t.Number == 0 {
		return shortID(t.ID)
	}
	return fmt.Sprintf("#%d %s", t.Number, shortID(t.ID))
}

func stateStyle(state verify.State, st styles) string {
	switch state {
	case verify.Verified:
		return st.good(string(state))
	case verify.Unprotected:
		return st.warn(string(state))
	case verify.Failed, verify.Error:
		return st.bad(string(state))
	default:
		return string(state)
	}
}

func checkStyle(status verify.CheckStatus, st styles) string {
	switch status {
	case verify.CheckPass:
		return st.good(string(status))
	case verify.CheckFail:
		return st.bad(string(status))
	case verify.CheckBehind:
		return st.warn(string(status))
	case verify.CheckSkip:
		return string(status)
	default:
		return string(status)
	}
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
		RepoPath: s.repo.CommonDir,
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
