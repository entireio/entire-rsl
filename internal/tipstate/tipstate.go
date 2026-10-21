// Package tipstate relates a local ref to what the RSL records for it.
//
// A local ref that matches the RSL tip, or is behind it, holds a state the
// RSL history covers: an earlier recorded tip, or a commit inside a push the
// RSL recorded. A ref that is ahead holds commits the RSL never recorded, and
// one that diverged holds a history the RSL does not describe.
package tipstate

import (
	"context"
	"errors"
	"fmt"

	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/entireio/entire-rsl/internal/store"
	"github.com/gittuf/gittuf/pkg/githash"
)

// Kinds of relation.
const (
	Match    = "match"
	Behind   = "behind"
	Ahead    = "ahead"
	Diverged = "diverged"
)

// maxCoverSearch caps the entries searched for the push that introduced a
// commit behind the tip. Past it the commit is still behind, with the entry
// unknown.
const maxCoverSearch = 1000

// ErrNoEntries is returned when the RSL holds no unskipped entry for the ref.
var ErrNoEntries = errors.New("the RSL has no unskipped entry for the ref")

// Relation is how a local tip relates to the RSL record of its ref.
type Relation struct {
	// Entry is, for Behind, the entry that recorded the local tip, or the
	// push that introduced it. Nil when it was not found within the search
	// cap.
	Entry *rslread.Target
	// Tip is the newest unskipped entry, which records the RSL tip.
	Tip  rslread.Target
	Kind string
	// Exact is set for Behind when Entry records exactly the local tip.
	Exact bool
	// EntriesBehind counts, for Behind, the unskipped entries newer than
	// Entry.
	EntriesBehind int
	// Commits counts the commits between the local tip and the RSL tip: the
	// local ref lacks them for Behind, the RSL lacks them for Ahead.
	Commits int
}

// Relate compares local with the unskipped entries for ref.
func Relate(ctx context.Context, st *store.Store, reader *rslread.Reader, ref string, local githash.Hash) (Relation, error) {
	targets, err := reader.Targets(ctx, ref)
	if err != nil {
		return Relation{}, err
	}
	if len(targets) == 0 {
		return Relation{}, ErrNoEntries
	}

	rel := Relation{Tip: targets[0]}
	if targets[0].Target.Equal(local) {
		rel.Kind = Match
		return rel, nil
	}

	for i := 1; i < len(targets); i++ {
		if targets[i].Target.Equal(local) {
			entry := targets[i]
			rel.Kind, rel.Entry, rel.Exact, rel.EntriesBehind = Behind, &entry, true, i
			rel.Commits = countCommits(st, rel.Tip.Target, local)
			return rel, nil
		}
	}

	// KnowsCommit fails on anything but commits, such as tag objects, which
	// then only relate by exact match.
	behind, err := st.KnowsCommit(rel.Tip.Target, local)
	if err == nil && behind {
		rel.Kind = Behind
		rel.Commits = countCommits(st, rel.Tip.Target, local)
		rel.Entry, rel.EntriesBehind = introducer(st, targets, local)
		return rel, nil
	}
	if err := ctx.Err(); err != nil {
		return Relation{}, fmt.Errorf("compare the local tip with the RSL: %w", err)
	}

	ahead, err := st.KnowsCommit(local, rel.Tip.Target)
	if err == nil && ahead {
		rel.Kind = Ahead
		rel.Commits = countCommits(st, local, rel.Tip.Target)
		return rel, nil
	}
	if err := ctx.Err(); err != nil {
		return Relation{}, fmt.Errorf("compare the local tip with the RSL: %w", err)
	}

	rel.Kind = Diverged
	return rel, nil
}

// introducer finds the oldest entry whose target contains local, walking
// back from the tip while the targets still contain it.
func introducer(st *store.Store, targets []rslread.Target, local githash.Hash) (*rslread.Target, int) {
	idx := 0
	for i := 1; i < len(targets) && i <= maxCoverSearch; i++ {
		contains, err := st.KnowsCommit(targets[i].Target, local)
		if err != nil || !contains {
			entry := targets[idx]
			return &entry, idx
		}
		idx = i
	}
	if idx == len(targets)-1 {
		entry := targets[idx]
		return &entry, idx
	}
	return nil, 0
}

// countCommits counts the commits reachable from to and not from from, 0 when
// they cannot be read.
func countCommits(st *store.Store, to, from githash.Hash) int {
	ids, err := st.GetCommitsBetweenRange(to, from)
	if err != nil {
		return 0
	}
	return len(ids)
}

// Describe explains a relation in a sentence.
func (r Relation) Describe() string {
	switch r.Kind {
	case Match:
		return fmt.Sprintf("the local ref matches the tip the RSL records (%s, entry #%d)", r.Tip.Target.String(), r.Tip.Number)
	case Behind:
		where := "inside the history the RSL records"
		switch {
		case r.Entry != nil && r.Exact:
			where = fmt.Sprintf("at entry #%d", r.Entry.Number)
		case r.Entry != nil:
			where = fmt.Sprintf("in the push recorded by entry #%d", r.Entry.Number)
		}
		return fmt.Sprintf("the local ref is behind: it is %s, the RSL tip is entry #%d (%s, %s behind). Its state is covered by the verified history. Fetch to update it",
			where, r.Tip.Number, plural(r.EntriesBehind, "entry", "entries"), plural(r.Commits, "commit", "commits"))
	case Ahead:
		return fmt.Sprintf("the local ref is ahead of the RSL tip (entry #%d) by %s the RSL never recorded, so they are not verified", r.Tip.Number, plural(r.Commits, "commit", "commits"))
	default:
		return fmt.Sprintf("the local ref has diverged from the RSL tip (%s, entry #%d)", r.Tip.Target.String(), r.Tip.Number)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
