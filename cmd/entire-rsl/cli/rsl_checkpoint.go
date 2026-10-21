package cli

import (
	"errors"
	"fmt"

	"github.com/entireio/entire-rsl/internal/refresolve"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/spf13/cobra"
)

// ErrCheckpointNotFound is returned when neither the repository nor the RSL
// has the checkpoint's ref.
var ErrCheckpointNotFound = errors.New("checkpoint not found")

type checkpointInfoJSON struct {
	ID           string `json:"id"`
	ResolvedFrom string `json:"resolved_from"`
	Commit       string `json:"commit,omitempty"`
}

type checkpointJSON struct {
	refReportJSON

	Checkpoint checkpointInfoJSON `json:"checkpoint"`
}

// checkpointTarget is a checkpoint and how the user named it.
type checkpointTarget struct {
	id     string
	commit githash.Hash
}

// resolveCheckpoint reads a checkpoint ID, or a full commit ID whose
// Entire-Checkpoint trailer names one.
func resolveCheckpoint(s *session, arg string) (checkpointTarget, error) {
	if refresolve.IsCheckpointID(arg) {
		return checkpointTarget{id: arg}, nil
	}

	commit, err := githash.NewHash(arg)
	if err != nil {
		return checkpointTarget{}, fmt.Errorf("parse commit ID: %w", err)
	}
	info, err := s.repo.Store.CommitInfo(commit)
	if err != nil {
		return checkpointTarget{}, fmt.Errorf("read commit %s: %w", arg, err)
	}
	id, err := refresolve.CheckpointFromMessage(info.Message)
	if err != nil {
		return checkpointTarget{}, fmt.Errorf("commit %s: %w", arg, err)
	}
	return checkpointTarget{id: id, commit: commit}, nil
}

// localCheckpoint finds a checkpoint's local ref, accepting a shard whose
// case differs from the canonical one.
func (s *session) localCheckpoint(id string) localTip {
	return func(ref string) (githash.Hash, error) {
		tip, err := s.localRef(ref)
		if err != nil || len(tip) != 0 {
			return tip, err
		}

		refs, err := s.repo.Store.References(refresolve.CheckpointsPrefix)
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			if got, ok := refresolve.CheckpointFromRef(r.Name); ok && got == id {
				return r.Target, nil
			}
		}
		return nil, nil
	}
}

func newCheckpointCmd(a *app) *cobra.Command {
	var flags refCommandFlags

	cmd := &cobra.Command{
		Use:   "checkpoint <id|sha>",
		Short: "Show what the RSL records for an Entire checkpoint",
		Long: `Show what the RSL records for an Entire checkpoint stored by the git-refs
backend under refs/entire/checkpoints/<shard>/<id>: the resolved ref and the
entries that wrote it.

Name the checkpoint by its ID (12 lowercase hex characters, or a 26 character
ULID), or by a full commit ID whose Entire-Checkpoint trailer names it.`,
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.validate(); err != nil {
				return err
			}
			if !refresolve.IsCheckpointID(args[0]) && !refresolve.IsCommitID(args[0]) {
				return usageError(fmt.Errorf("%q: %w", args[0], refresolve.ErrInvalidCheckpoint))
			}

			s, err := a.open(cmd.Context())
			if err != nil {
				return err
			}

			target, err := resolveCheckpoint(s, args[0])
			if err != nil {
				return err
			}

			ref := refresolve.CheckpointRef(target.id)
			report, err := buildRefReport(cmd.Context(), s, ref, flags.history, s.localCheckpoint(target.id))
			if err != nil {
				return err
			}
			if len(report.LocalTip) == 0 && report.History.Total == 0 {
				return fmt.Errorf("%w: %s has no local ref %s and no RSL entries", ErrCheckpointNotFound, target.id, ref)
			}
			a.addVerification(cmd.Context(), s, &report, flags.verify)

			if flags.asJSON {
				out := checkpointJSON{refReportJSON: report.toJSON(), Checkpoint: checkpointInfoJSON{ID: target.id, ResolvedFrom: "id"}}
				if len(target.commit) != 0 {
					out.Checkpoint.ResolvedFrom = "commit"
					out.Checkpoint.Commit = target.commit.String()
				}
				return writeJSON(cmd.OutOrStdout(), out)
			}

			fields := []field{{"Checkpoint", target.id}}
			if len(target.commit) != 0 {
				fields = append(fields, field{"From commit", target.commit.String() + " (Entire-Checkpoint trailer)"})
			}
			return writeRefReport(cmd.OutOrStdout(), report, fields...)
		},
	}
	flags.register(cmd)

	return cmd
}
