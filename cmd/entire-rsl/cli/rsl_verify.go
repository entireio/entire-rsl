package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/entireio/entire-rsl/internal/refname"
	"github.com/entireio/entire-rsl/internal/refresolve"
	"github.com/entireio/entire-rsl/internal/sanitize"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/spf13/cobra"
)

type verifyJSON struct {
	SchemaVersion int              `json:"schema_version"`
	Ref           string           `json:"ref"`
	Verification  verificationJSON `json:"verification"`
}

func newVerifyCmd(a *app) *cobra.Command {
	var (
		flags  verifyFlags
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "verify <target>",
		Short: "Verify a ref against gittuf policy",
		Long: `Run full gittuf verification for a branch, tag, full ref, checkpoint or
commit and report one state:

  verified     gittuf verified every RSL entry for the ref     exit 0
  failed       gittuf found a violation                        exit 3
  unprotected  no policy, no covering rule, or no RSL entries  exit 3
  error        verification could not complete                 exit 1

Short names resolve as git does, but a name matching more than one ref is an
error. A checkpoint is named by its ID.

A full commit ID verifies the commit: a branch or tag whose RSL record
contains it must verify, checked in name order until one does. A commit no
verified branch or tag contains is not verified, nor is one only a revoked
push brought in. When the commit has an Entire-Checkpoint trailer, the
checkpoint it names must verify too.`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.validate(); err != nil {
				return err
			}

			s, err := a.open(cmd.Context())
			if err != nil {
				return err
			}

			target, err := resolveVerifyTarget(cmd.Context(), s, args[0])
			if err != nil {
				return err
			}
			if len(target.commit) != 0 {
				return runVerifyCommit(cmd, a, s, target.commit, flags, asJSON)
			}
			ref, local := target.ref, target.local

			res := a.verify(cmd.Context(), s, ref, flags, local)
			code := exitCodeFor(res.State)

			if asJSON {
				if err := writeJSON(cmd.OutOrStdout(), verifyJSON{SchemaVersion: schemaVersion, Ref: ref, Verification: toVerificationJSON(res)}); err != nil {
					return err
				}
			} else {
				fields := append([]field{{labelRef, sanitize.Line(ref)}}, verificationFields(res, a.styles())...)
				if err := writeFields(cmd.OutOrStdout(), fields); err != nil {
					return err
				}
			}

			if code != ExitOK {
				return silentExit(code, fmt.Errorf("%s: %s", ref, res.State))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	flags.register(cmd, false)

	return cmd
}

// verifyTarget is what verify was asked for: a ref with the lookup of its
// local value, or a commit.
type verifyTarget struct {
	local  localTip
	ref    string
	commit githash.Hash
}

// resolveVerifyTarget turns a verify target into a full ref name and the
// lookup of its local value, or into a commit.
func resolveVerifyTarget(ctx context.Context, s *session, target string) (verifyTarget, error) {
	isCheckpoint := refresolve.IsCheckpointID(target)
	isCommit := refresolve.IsCommitID(target)

	var rslRefs map[string]bool
	exists := func(ref string) (bool, error) {
		tip, err := s.localRef(ref)
		if err != nil {
			return false, err
		}
		if len(tip) != 0 {
			return true, nil
		}
		if rslRefs == nil {
			if rslRefs, err = s.reader.RefNames(ctx); err != nil {
				return false, err
			}
		}
		return rslRefs[ref], nil
	}

	ref, err := refresolve.Resolve(target, exists)
	switch {
	case err == nil && (isCheckpoint || isCommit):
		return verifyTarget{}, usageError(fmt.Errorf("%w %q is both a ref (%s) and a checkpoint or commit ID. Name the ref in full", refresolve.ErrAmbiguous, target, ref))
	case err == nil:
		if id, ok := refresolve.CheckpointFromRef(ref); ok {
			return verifyTarget{ref: ref, local: s.localCheckpoint(id)}, nil
		}
		return verifyTarget{ref: ref, local: s.localRef}, nil
	case errors.Is(err, refresolve.ErrAmbiguous), errors.Is(err, refname.ErrInvalid):
		return verifyTarget{}, usageError(err)
	case !errors.Is(err, refresolve.ErrNotFound):
		return verifyTarget{}, err
	case isCheckpoint:
		return verifyTarget{ref: refresolve.CheckpointRef(target), local: s.localCheckpoint(target)}, nil
	case isCommit:
		commit, err := githash.NewHash(target)
		if err != nil {
			return verifyTarget{}, fmt.Errorf("parse commit ID: %w", err)
		}
		return verifyTarget{commit: commit}, nil
	default:
		return verifyTarget{}, err
	}
}

func runVerifyCommit(cmd *cobra.Command, a *app, s *session, commit githash.Hash, flags verifyFlags, asJSON bool) error {
	cv, err := a.verifyCommit(cmd.Context(), s, commit, flags)
	if err != nil {
		return err
	}

	if asJSON {
		if err := writeJSON(cmd.OutOrStdout(), cv.toJSON()); err != nil {
			return err
		}
	} else if err := writeCommitVerification(cmd.OutOrStdout(), cv, a.styles()); err != nil {
		return err
	}

	if code := exitCodeFor(cv.state); code != ExitOK {
		return silentExit(code, fmt.Errorf("%s: %s", commit.String(), cv.state))
	}
	return nil
}
