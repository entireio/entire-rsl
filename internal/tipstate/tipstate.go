// Package tipstate relates a local ref to what the RSL records for it.
//
// A local ref matches the RSL tip, is behind it at an earlier recorded tip or
// inside a recorded push, is ahead of it with commits the RSL never recorded,
// or has diverged. The relation only states facts about the RSL record: it
// says nothing about whether that record verifies.
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
	Match  = "match"
	Behind = "behind"
	Ahead  = "ahead"
	// Revoked is a commit only a skipped entry's push brought in, even if a
	// later unskipped entry still contains it.
	Revoked  = "revoked"
	Diverged = "diverged"
	// TooFar is a commit behind the tip whose push is further back than the
	// search covers, so its coverage could not be established.
	TooFar = "too_far"
)

// maxCoverSearch caps the entries searched for the push that introduced a
// commit behind the tip.
const maxCoverSearch = 1000

// ErrNoEntries is returned when the RSL holds no unskipped entry for the ref.
var ErrNoEntries = errors.New("the RSL has no unskipped entry for the ref")

// Relation is how a local tip relates to the RSL record of its ref.
type Relation struct {
	// Entry is, for Behind, the unskipped entry that recorded the local tip,
	// or the push that introduced it.
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

// Relate compares local with the entries for ref. Errors reading the
// repository are returned, never read as a relation.
func Relate(ctx context.Context, st *store.Store, reader *rslread.Reader, ref string, local githash.Hash) (Relation, error) {
	all, err := reader.Targets(ctx, ref)
	if err != nil {
		return Relation{}, err
	}

	var unskipped, skipped []rslread.Target
	for _, t := range all {
		if t.Skipped {
			skipped = append(skipped, t)
		} else {
			unskipped = append(unskipped, t)
		}
	}
	if len(unskipped) == 0 {
		return Relation{}, ErrNoEntries
	}

	rel := Relation{Tip: unskipped[0]}
	if rel.Tip.Target.Equal(local) {
		rel.Kind = Match
		return rel, nil
	}

	for i := 1; i < len(unskipped); i++ {
		if unskipped[i].Target.Equal(local) {
			entry := unskipped[i]
			rel.Kind, rel.Entry, rel.Exact, rel.EntriesBehind = Behind, &entry, true, i
			if rel.Commits, err = commitsBetween(st, rel.Tip.Target, local); err != nil {
				return Relation{}, err
			}
			return rel, nil
		}
	}

	// Ancestry only applies to commits. Anything else, such as a tag
	// object, relates by exact match alone.
	commits, err := bothCommits(st, local, rel.Tip.Target)
	if err != nil {
		return Relation{}, err
	}
	if !commits {
		rel.Kind = Diverged
		return rel, nil
	}

	behind, err := st.KnowsCommit(rel.Tip.Target, local)
	if err != nil {
		return Relation{}, fmt.Errorf("compare the local tip with the RSL: %w", err)
	}
	if behind {
		return relateBehind(st, rel, unskipped, skipped, local)
	}

	ahead, err := st.KnowsCommit(local, rel.Tip.Target)
	if err != nil {
		return Relation{}, fmt.Errorf("compare the local tip with the RSL: %w", err)
	}
	if ahead {
		rel.Kind = Ahead
		if rel.Commits, err = commitsBetween(st, local, rel.Tip.Target); err != nil {
			return Relation{}, err
		}
		return rel, nil
	}

	rel.Kind = Diverged
	return rel, nil
}

// relateBehind places a commit the RSL tip contains: in the push of the
// oldest unskipped entry whose target contains it, unless a skipped entry's
// target also contains it. gittuf recovers from a skipped entry with a fix
// commit that may keep the skipped commits as ancestors, so containment by a
// later entry alone does not show the commit was verified.
func relateBehind(st *store.Store, rel Relation, unskipped, skipped []rslread.Target, local githash.Hash) (Relation, error) {
	for _, s := range skipped {
		isCommit, err := st.IsCommit(s.Target)
		if err != nil {
			return Relation{}, err
		}
		if !isCommit {
			continue
		}
		contains, err := st.KnowsCommit(s.Target, local)
		if err != nil {
			return Relation{}, fmt.Errorf("compare the local tip with skipped entry #%d: %w", s.Number, err)
		}
		if contains {
			entry := s
			rel.Kind, rel.Entry = Revoked, &entry
			return rel, nil
		}
	}

	idx := 0
	for i := 1; ; i++ {
		if i >= len(unskipped) {
			break
		}
		if i > maxCoverSearch {
			rel.Kind = TooFar
			return rel, nil
		}
		isCommit, err := st.IsCommit(unskipped[i].Target)
		if err != nil {
			return Relation{}, err
		}
		if !isCommit {
			break
		}
		contains, err := st.KnowsCommit(unskipped[i].Target, local)
		if err != nil {
			return Relation{}, fmt.Errorf("compare the local tip with entry #%d: %w", unskipped[i].Number, err)
		}
		if !contains {
			break
		}
		idx = i
	}

	entry := unskipped[idx]
	rel.Kind, rel.Entry, rel.EntriesBehind = Behind, &entry, idx
	var err error
	if rel.Commits, err = commitsBetween(st, rel.Tip.Target, local); err != nil {
		return Relation{}, err
	}
	return rel, nil
}

func bothCommits(st *store.Store, a, b githash.Hash) (bool, error) {
	for _, id := range []githash.Hash{a, b} {
		ok, err := st.IsCommit(id)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

// commitsBetween counts the commits reachable from to and not from from.
// Exact matches of non-commit objects have no commits between them.
func commitsBetween(st *store.Store, to, from githash.Hash) (int, error) {
	commits, err := bothCommits(st, to, from)
	if err != nil || !commits {
		return 0, err
	}
	ids, err := st.GetCommitsBetweenRange(to, from)
	if err != nil {
		return 0, fmt.Errorf("count commits between %s and %s: %w", from.String(), to.String(), err)
	}
	return len(ids), nil
}

// Describe states the relation as recorded facts, without any claim about
// verification.
func (r Relation) Describe() string {
	switch r.Kind {
	case Match:
		return fmt.Sprintf("the local ref matches the tip the RSL records (%s, entry #%d)", r.Tip.Target.String(), r.Tip.Number)
	case Behind:
		where := fmt.Sprintf("in the push recorded by entry #%d", r.Entry.Number)
		if r.Exact {
			where = fmt.Sprintf("at entry #%d", r.Entry.Number)
		}
		return fmt.Sprintf("the local ref is behind: it is %s, the RSL tip is entry #%d (%s, %s behind). Fetch to update it",
			where, r.Tip.Number, plural(r.EntriesBehind, "entry", "entries"), plural(r.Commits, "commit", "commits"))
	case Ahead:
		return fmt.Sprintf("the local ref is ahead of the RSL tip (entry #%d) by %s the RSL never recorded", r.Tip.Number, plural(r.Commits, "commit", "commits"))
	case Revoked:
		return fmt.Sprintf("the local ref is at a commit brought in by skipped entry #%d, which an annotation revoked", r.Entry.Number)
	case TooFar:
		return fmt.Sprintf("the local ref is behind the RSL tip (entry #%d) by more than %d entries, too far to find the push that recorded it", r.Tip.Number, maxCoverSearch)
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
