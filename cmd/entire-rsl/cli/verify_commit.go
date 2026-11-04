package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/entireio/entire-rsl/internal/redact"
	"github.com/entireio/entire-rsl/internal/refresolve"
	"github.com/entireio/entire-rsl/internal/sanitize"
	"github.com/entireio/entire-rsl/internal/tipstate"
	"github.com/entireio/entire-rsl/internal/verify"
	"github.com/gittuf/gittuf/pkg/githash"
)

// containerPrefixes are the refs whose verified history can vouch for a
// commit. A checkpoint ref never vouches for the commit that names it.
var containerPrefixes = []string{"refs/heads/", "refs/tags/"}

// container is a branch or tag whose RSL record contains a commit.
type container struct {
	// res is set once the ref was verified.
	res *verify.Result
	ref string
	rel tipstate.Relation
}

// commitVerification is the outcome of verifying a commit: a branch or tag
// that contains it must verify, then the checkpoint its trailer names.
type commitVerification struct {
	commit     githash.Hash
	containers []container
	// revoked are refs that hold the commit only through a skipped entry's
	// push.
	revoked []container
	// unchecked are refs for which containment could not be established.
	unchecked    []string
	checkpointID string
	checkpoint   *verify.Result
	state        verify.State
	reason       string
}

// verifyCommit verifies the refs that contain commit until one passes, then
// the checkpoint its Entire-Checkpoint trailer names, if any.
func (a *app) verifyCommit(ctx context.Context, s *session, commit githash.Hash, flags verifyFlags) (commitVerification, error) {
	cv := commitVerification{commit: commit}

	info, err := s.repo.Store.CommitInfo(commit)
	if err != nil {
		return cv, fmt.Errorf("read commit %s: %w", commit.String(), err)
	}
	switch id, err := refresolve.CheckpointFromMessage(info.Message); {
	case err == nil:
		cv.checkpointID = id
	case !errors.Is(err, refresolve.ErrNoCheckpointTrailer):
		return cv, fmt.Errorf("commit %s: %w", commit.String(), err)
	}

	if cv.containers, cv.revoked, cv.unchecked, err = findContainers(ctx, s, commit); err != nil {
		return cv, err
	}

	verified := -1
	for i := range cv.containers {
		res := a.verify(ctx, s, cv.containers[i].ref, flags, s.localRef)
		cv.containers[i].res = &res
		if res.State == verify.Verified {
			verified = i
			break
		}
	}
	if verified < 0 {
		cv.state, cv.reason = cv.rejection()
		return cv, nil
	}

	cv.state = verify.Verified
	cv.reason = cv.containers[verified].ref + " contains the commit and gittuf verified it"
	if cv.checkpointID == "" {
		return cv, nil
	}

	res := a.verify(ctx, s, refresolve.CheckpointRef(cv.checkpointID), flags, s.localCheckpoint(cv.checkpointID))
	cv.checkpoint = &res
	if res.State != verify.Verified {
		cv.state = res.State
		cv.reason += fmt.Sprintf(", but the checkpoint its %s trailer names, %s, is %s", refresolve.CheckpointTrailer, cv.checkpointID, res.State)
		return cv, nil
	}
	cv.reason += fmt.Sprintf(", and the checkpoint its %s trailer names, %s", refresolve.CheckpointTrailer, cv.checkpointID)
	return cv, nil
}

// rejection explains why no ref vouches for the commit. A ref whose
// containment could not be established makes the outcome an error, since it
// may hold the commit.
func (cv commitVerification) rejection() (verify.State, string) {
	state, reason := cv.rejectionByContainers()
	if len(cv.unchecked) > 0 {
		state = verify.Error
		reason += fmt.Sprintf(". Whether %s the commit could not be established",
			plural(len(cv.unchecked), "more ref contains", "more refs contain"))
	}
	return state, reason
}

func (cv commitVerification) rejectionByContainers() (verify.State, string) {
	if len(cv.containers) == 0 {
		if len(cv.revoked) > 0 {
			return verify.Failed, "the commit is only in pushes an annotation revoked, so no branch or tag vouches for it"
		}
		return verify.Unprotected, "no branch or tag the RSL records contains the commit"
	}

	state := verify.Unprotected
	outcomes := make([]string, 0, len(cv.containers))
	for _, c := range cv.containers {
		outcomes = append(outcomes, c.ref+": "+string(c.res.State))
		switch {
		case c.res.State == verify.Error:
			state = verify.Error
		case c.res.State == verify.Failed && state != verify.Error:
			state = verify.Failed
		}
	}
	return state, "no branch or tag that contains the commit verified (" + strings.Join(outcomes, ", ") + ")"
}

// findContainers relates commit to every branch and tag the RSL
// records, in name order. A ref contains it when an unskipped entry records
// it or its push brought it in. A ref that holds it only through a revoked
// push does not.
func findContainers(ctx context.Context, s *session, commit githash.Hash) (containers, revoked []container, unchecked []string, err error) {
	names, err := s.reader.RefNames(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	for _, ref := range slices.Sorted(maps.Keys(names)) {
		if !slices.ContainsFunc(containerPrefixes, func(p string) bool { return strings.HasPrefix(ref, p) }) {
			continue
		}
		rel, err := tipstate.Relate(ctx, s.repo.Store, s.reader, ref, commit)
		switch {
		case errors.Is(err, tipstate.ErrNoEntries):
			continue
		case errors.Is(err, tipstate.ErrTipMissing):
			unchecked = append(unchecked, ref+" (the object its RSL tip records is not in the local repository. Fetch it to compare)")
			continue
		case err != nil:
			return nil, nil, nil, fmt.Errorf("relate the commit to %s: %w", ref, err)
		}

		switch rel.Kind {
		case tipstate.Match, tipstate.Behind:
			containers = append(containers, container{ref: ref, rel: rel})
		case tipstate.Revoked:
			revoked = append(revoked, container{ref: ref, rel: rel})
		case tipstate.TooFar:
			unchecked = append(unchecked, ref+" (the push that recorded the commit is too far back to find)")
		}
	}
	return containers, revoked, unchecked, nil
}

// containmentText says which entry of the ref records the commit.
func containmentText(c container) string {
	switch {
	case c.rel.Kind == tipstate.Match:
		return sanitize.Line(c.ref) + ", recorded by the RSL tip " + entryLabel(c.rel.Tip)
	case c.rel.Kind == tipstate.Revoked:
		return sanitize.Line(c.ref) + ", only in the push recorded by skipped " + entryLabel(*c.rel.Entry) + ", which an annotation revoked"
	case c.rel.Exact:
		return sanitize.Line(c.ref) + ", recorded by " + entryLabel(*c.rel.Entry)
	default:
		return sanitize.Line(c.ref) + ", in the push recorded by " + entryLabel(*c.rel.Entry)
	}
}

func containerEntry(c container) checkedEntryJSON {
	t := c.rel.Tip
	if c.rel.Entry != nil {
		t = *c.rel.Entry
	}
	return checkedEntryJSON{Number: t.Number, ID: t.ID.String()}
}

type containerJSON struct {
	Ref          string            `json:"ref"`
	Entry        checkedEntryJSON  `json:"entry"`
	Verification *verificationJSON `json:"verification,omitempty"`
}

type commitCheckpointJSON struct {
	ID  string `json:"id"`
	Ref string `json:"ref"`
	// Verification is nil when the commit was rejected before its
	// checkpoint was verified.
	Verification *verificationJSON `json:"verification"`
}

type commitStateJSON struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

type verifyCommitJSON struct {
	SchemaVersion int                   `json:"schema_version"`
	Commit        string                `json:"commit"`
	Verification  commitStateJSON       `json:"verification"`
	ContainedIn   []containerJSON       `json:"contained_in"`
	RevokedIn     []containerJSON       `json:"revoked_in,omitempty"`
	UncheckedRefs []string              `json:"unchecked_refs,omitempty"`
	Checkpoint    *commitCheckpointJSON `json:"checkpoint,omitempty"`
}

func (cv commitVerification) toJSON() verifyCommitJSON {
	out := verifyCommitJSON{
		SchemaVersion: schemaVersion,
		Commit:        cv.commit.String(),
		Verification:  commitStateJSON{State: string(cv.state), Reason: redact.URLs(cv.reason)},
		ContainedIn:   []containerJSON{},
		UncheckedRefs: cv.unchecked,
	}
	for _, c := range cv.containers {
		j := containerJSON{Ref: c.ref, Entry: containerEntry(c)}
		if c.res != nil {
			v := toVerificationJSON(*c.res)
			j.Verification = &v
		}
		out.ContainedIn = append(out.ContainedIn, j)
	}
	for _, c := range cv.revoked {
		out.RevokedIn = append(out.RevokedIn, containerJSON{Ref: c.ref, Entry: containerEntry(c)})
	}
	if cv.checkpoint != nil {
		v := toVerificationJSON(*cv.checkpoint)
		out.Checkpoint = &commitCheckpointJSON{ID: cv.checkpointID, Ref: cv.checkpoint.Ref, Verification: &v}
	} else if cv.checkpointID != "" {
		out.Checkpoint = &commitCheckpointJSON{ID: cv.checkpointID, Ref: refresolve.CheckpointRef(cv.checkpointID)}
	}
	return out
}

// writeCommitVerification prints the commit's outcome, then one block per
// ref that was verified for it.
func writeCommitVerification(w io.Writer, cv commitVerification, st styles) error {
	contained := none
	if len(cv.containers) > 0 {
		lines := make([]string, 0, len(cv.containers))
		for _, c := range cv.containers {
			lines = append(lines, containmentText(c))
		}
		contained = strings.Join(lines, "\n")
	}
	fields := []field{
		{"Commit", cv.commit.String()},
		{labelVerification, stateStyle(cv.state, st) + ": " + sanitize.Line(redact.URLs(cv.reason))},
		{"Contained in", contained},
	}
	if len(cv.revoked) > 0 {
		lines := make([]string, 0, len(cv.revoked))
		for _, c := range cv.revoked {
			lines = append(lines, containmentText(c))
		}
		fields = append(fields, field{"Revoked in", strings.Join(lines, "\n")})
	}
	if len(cv.unchecked) > 0 {
		lines := make([]string, 0, len(cv.unchecked))
		for _, u := range cv.unchecked {
			lines = append(lines, sanitize.Line(u))
		}
		fields = append(fields, field{"Not established", strings.Join(lines, "\n")})
	}
	if cv.checkpointID != "" {
		text := cv.checkpointID + " (" + refresolve.CheckpointTrailer + " trailer)"
		if cv.checkpoint == nil {
			text += ", not verified because no verified branch or tag contains the commit"
		}
		fields = append(fields, field{"Checkpoint", text})
	}
	if err := writeFields(w, fields); err != nil {
		return err
	}

	var blocks []verify.Result
	for _, c := range cv.containers {
		if c.res != nil {
			blocks = append(blocks, *c.res)
		}
	}
	if cv.checkpoint != nil {
		blocks = append(blocks, *cv.checkpoint)
	}
	for _, res := range blocks {
		fmt.Fprintln(w)
		if err := writeFields(w, append([]field{{labelRef, sanitize.Line(res.Ref)}}, verificationFields(res, st)...)); err != nil {
			return err
		}
	}
	return nil
}
