package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/entireio/entire-rsl/internal/refname"
	"github.com/entireio/entire-rsl/internal/refresolve"
	"github.com/entireio/entire-rsl/internal/sanitize"
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
		Long: `Run full gittuf verification for a branch, tag, full ref or checkpoint and
report one state:

  verified     gittuf verified every RSL entry for the ref     exit 0
  failed       gittuf found a violation                        exit 3
  unprotected  no policy, no covering rule, or no RSL entries  exit 3
  error        verification could not complete                 exit 1

Short names resolve as git does, but a name matching more than one ref is an
error. A checkpoint is named by its ID, or by a full commit ID whose
Entire-Checkpoint trailer names it.`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.validate(); err != nil {
				return err
			}

			s, err := a.open(cmd.Context())
			if err != nil {
				return err
			}

			ref, local, err := resolveVerifyTarget(cmd.Context(), s, args[0])
			if err != nil {
				return err
			}

			res := a.verify(cmd.Context(), s, ref, flags, local)
			code := exitCodeFor(res.State)

			if asJSON {
				if err := writeJSON(cmd.OutOrStdout(), verifyJSON{SchemaVersion: schemaVersion, Ref: ref, Verification: toVerificationJSON(res)}); err != nil {
					return err
				}
			} else {
				fields := append([]field{{labelRef, sanitize.Line(ref)}}, verificationFields(res)...)
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

// resolveVerifyTarget turns a verify target into a full ref name and the
// lookup of its local value.
func resolveVerifyTarget(ctx context.Context, s *session, target string) (string, localTip, error) {
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
		return "", nil, usageError(fmt.Errorf("%w %q is both a ref (%s) and a checkpoint or commit ID. Name the ref in full", refresolve.ErrAmbiguous, target, ref))
	case err == nil:
		if id, ok := refresolve.CheckpointFromRef(ref); ok {
			return ref, s.localCheckpoint(id), nil
		}
		return ref, s.localRef, nil
	case errors.Is(err, refresolve.ErrAmbiguous), errors.Is(err, refname.ErrInvalid):
		return "", nil, usageError(err)
	case !errors.Is(err, refresolve.ErrNotFound):
		return "", nil, err
	case isCheckpoint || isCommit:
		cp, err := resolveCheckpoint(s, target)
		if err != nil {
			return "", nil, err
		}
		return refresolve.CheckpointRef(cp.id), s.localCheckpoint(cp.id), nil
	default:
		return "", nil, err
	}
}
