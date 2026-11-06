// Package rslread walks the gittuf Reference State Log and builds the model
// entire-rsl renders: entries with their annotations, signer and asserted
// time.
//
// Every walk starts at the RSL tip, so an entry is only ever read when it is
// reachable from the log, and every annotation newer than an entry has been
// seen by the time the entry is. Walks stop when the context is done and
// after maxEntries entries.
package rslread

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/entireio/entire-rsl/internal/store"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/gitstore"
	"github.com/gittuf/gittuf/pkg/rsl"
)

// Entry kinds.
const (
	KindReference   = "reference"
	KindAnnotation  = "annotation"
	KindPropagation = "propagation"
)

// maxEntries caps the entries one walk visits.
const maxEntries = 5_000_000

// Errors.
var (
	ErrNoRSL          = errors.New("no gittuf RSL found (refs/gittuf/reference-state-log is missing)")
	ErrEntryNotFound  = errors.New("RSL entry not found")
	ErrAmbiguousEntry = errors.New("ambiguous RSL entry ID prefix")
	ErrWalkLimit      = errors.New("RSL walk exceeded its limit")
	ErrInvalidEntryID = errors.New("invalid RSL entry: use an entry number, or an entry ID of at least 7 hex characters")
	ErrShallowRSL     = errors.New("the RSL history is shallow: fetch its full history to read it")
)

// Entry is one RSL entry with everything entire-rsl shows about it.
type Entry struct {
	// Time is the entry commit's committer time. The signer asserts it.
	Time            time.Time
	CustomFields    map[string]string
	ID              githash.Hash
	TargetID        githash.Hash
	UpstreamEntryID githash.Hash
	Committer       store.Person
	Signature       Signature
	Kind            string
	RefName         string
	// UpstreamRepository is set on propagation entries.
	UpstreamRepository string
	// Message is an annotation's message.
	Message string
	// Annotates lists the entries an annotation refers to.
	Annotates []githash.Hash
	// AnnotatedBy lists the annotations that refer to this entry, newest first.
	AnnotatedBy []githash.Hash
	// SkippedBy lists the skip annotations that revoke this entry.
	SkippedBy []githash.Hash
	Number    uint64
	// Skip is set on an annotation that revokes the entries it refers to.
	Skip bool
}

// Skipped reports whether an annotation revokes the entry.
func (e Entry) Skipped() bool { return len(e.SkippedBy) > 0 }

// Reader reads the RSL of one repository.
type Reader struct {
	st  *store.Store
	tip githash.Hash
}

// Open returns a reader positioned at the RSL tip, or ErrNoRSL.
func Open(st *store.Store) (*Reader, error) {
	tip, err := st.GetReference(rsl.Ref)
	if err != nil {
		if errors.Is(err, gitstore.ErrReferenceNotFound) {
			return nil, ErrNoRSL
		}
		return nil, fmt.Errorf("read RSL tip: %w", err)
	}
	return &Reader{st: st, tip: tip}, nil
}

// Tip returns the ID of the newest entry.
func (r *Reader) Tip() githash.Hash { return r.tip }

// raw is an entry as gittuf parses it, with the annotations seen so far
// that refer to it.
type raw struct {
	entry       rsl.Entry
	annotations []*rsl.AnnotationEntry
}

func (w raw) id() githash.Hash { return w.entry.GetID() }

func (w raw) skipped() bool {
	for _, a := range w.annotations {
		if a.Skip {
			return true
		}
	}
	return false
}

func (w raw) refName() string {
	if u, ok := w.entry.(rsl.ReferenceUpdaterEntry); ok {
		return u.GetRefName()
	}
	return ""
}

// walk visits entries newest first from the tip until visit returns false.
// It parses each entry but reads nothing else, so a filtering walk stays
// cheap. build completes the entries a caller keeps.
func (r *Reader) walk(ctx context.Context, visit func(raw) (bool, error)) error {
	annotations := map[string][]*rsl.AnnotationEntry{}

	entry, err := rsl.GetEntry(r.st, r.tip)
	if err != nil {
		return fmt.Errorf("read RSL entry %s: %w", r.tip.String(), err)
	}

	for n := 0; ; n++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("walk RSL: %w", err)
		}
		if n >= maxEntries {
			return fmt.Errorf("%w: more than %d entries", ErrWalkLimit, maxEntries)
		}

		if a, ok := entry.(*rsl.AnnotationEntry); ok {
			for _, id := range a.RSLEntryIDs {
				annotations[id.String()] = append(annotations[id.String()], a)
			}
		}

		more, err := visit(raw{entry: entry, annotations: annotations[entry.GetID().String()]})
		if err != nil || !more {
			return err
		}

		parent, ok, err := r.parent(entry)
		if err != nil || !ok {
			return err
		}
		entry = parent
	}
}

// parent returns entry's parent, and false at the first entry of the RSL.
//
// gittuf's GetParentForEntry reports a parent it cannot read with an error
// that also matches ErrRSLEntryNotFound (pkg/rsl GetEntry joins them), which
// is indistinguishable from reaching the first entry. The parent IDs are read
// first so only a commit with no parents ends the walk, and a shallow
// boundary inside the RSL is reported instead of read as its start.
func (r *Reader) parent(entry rsl.Entry) (rsl.Entry, bool, error) {
	id := entry.GetID()

	info, err := r.st.GetCommitInfo(id)
	if err != nil {
		return nil, false, fmt.Errorf("read parents of RSL entry %s: %w", id.String(), err)
	}
	if len(info.ParentIDs) == 0 {
		shallow, err := r.st.IsShallowBoundary(id)
		if err != nil {
			return nil, false, err
		}
		if shallow {
			return nil, false, fmt.Errorf("%w (entry %s)", ErrShallowRSL, id.String())
		}
		return nil, false, nil
	}

	parent, err := rsl.GetParentForEntry(r.st, entry)
	if err != nil {
		return nil, false, fmt.Errorf("read parent of RSL entry %s: %w", id.String(), err)
	}
	return parent, true, nil
}

// Target is an entry's record of a ref, without the commit metadata and
// signature check a full Entry carries.
type Target struct {
	ID      githash.Hash
	Target  githash.Hash
	Number  uint64
	Skipped bool
}

// Targets returns the reference and propagation entries for ref, newest
// first, skipped ones marked.
func (r *Reader) Targets(ctx context.Context, ref string) ([]Target, error) {
	var out []Target
	err := r.walk(ctx, func(w raw) (bool, error) {
		if w.refName() != ref {
			return true, nil
		}
		u, ok := w.entry.(rsl.ReferenceUpdaterEntry)
		if !ok {
			return true, nil
		}
		out = append(out, Target{ID: u.GetID(), Target: u.GetTargetID(), Number: u.GetNumber(), Skipped: w.skipped()})
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RefNames returns every ref the RSL holds a reference or propagation entry
// for.
func (r *Reader) RefNames(ctx context.Context) (map[string]bool, error) {
	refs := map[string]bool{}
	err := r.walk(ctx, func(w raw) (bool, error) {
		if name := w.refName(); name != "" {
			refs[name] = true
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// CheckComplete walks the whole RSL and fails if any entry cannot be read or
// the history stops at a shallow boundary. gittuf's own walks read an
// unreadable parent as the first entry, so a verification that starts at the
// "first" entry is only full once this passes.
func (r *Reader) CheckComplete(ctx context.Context) error {
	return r.walk(ctx, func(raw) (bool, error) { return true, nil })
}

// Walk visits complete entries newest first from the tip until visit returns
// false.
func (r *Reader) Walk(ctx context.Context, visit func(Entry) (bool, error)) error {
	return r.walk(ctx, func(w raw) (bool, error) {
		e, err := r.build(w)
		if err != nil {
			return false, err
		}
		return visit(e)
	})
}

func (r *Reader) build(w raw) (Entry, error) {
	e := Entry{ID: w.entry.GetID(), Number: w.entry.GetNumber()}

	switch v := w.entry.(type) {
	case *rsl.ReferenceEntry:
		e.Kind, e.RefName, e.TargetID, e.CustomFields = KindReference, v.RefName, v.TargetID, v.CustomFields
	case *rsl.PropagationEntry:
		e.Kind, e.RefName, e.TargetID, e.CustomFields = KindPropagation, v.RefName, v.TargetID, v.CustomFields
		e.UpstreamRepository, e.UpstreamEntryID = v.UpstreamRepository, v.UpstreamEntryID
	case *rsl.AnnotationEntry:
		e.Kind, e.Annotates, e.Skip, e.Message, e.CustomFields = KindAnnotation, v.RSLEntryIDs, v.Skip, v.Message, v.CustomFields
	default:
		return Entry{}, fmt.Errorf("RSL entry %s has an unknown type %T", e.ID.String(), w.entry)
	}

	for _, a := range w.annotations {
		e.AnnotatedBy = append(e.AnnotatedBy, a.ID)
		if a.Skip {
			e.SkippedBy = append(e.SkippedBy, a.ID)
		}
	}

	info, err := r.st.CommitInfo(e.ID)
	if err != nil {
		return Entry{}, fmt.Errorf("read RSL entry %s: %w", e.ID.String(), err)
	}
	e.Time, e.Committer = info.Committer.When.UTC(), info.Committer

	e.Signature, err = signatureOf(r.st, e.ID)
	if err != nil {
		return Entry{}, fmt.Errorf("read signature of RSL entry %s: %w", e.ID.String(), err)
	}

	return e, nil
}

// LogOptions selects entries for Log.
type LogOptions struct {
	// Ref keeps only reference and propagation entries for this ref.
	Ref string
	// Before starts the listing below this entry.
	Before *Selector
	// Limit caps the entries returned.
	Limit int
}

// Log returns up to opts.Limit entries newest first.
func (r *Reader) Log(ctx context.Context, opts LogOptions) ([]Entry, error) {
	// A prefix is resolved to one entry first, so an ambiguous prefix fails
	// as it does for Find instead of starting below whichever match comes
	// first.
	if opts.Before != nil && opts.Before.isPrefix() {
		e, err := r.Find(ctx, *opts.Before)
		if err != nil {
			return nil, err
		}
		exact := Selector{idPrefix: e.ID.String()}
		opts.Before = &exact
	}

	var out []Entry
	started := opts.Before == nil

	err := r.walk(ctx, func(w raw) (bool, error) {
		if !started {
			started = opts.Before.matches(w)
			return true, nil
		}
		if opts.Ref != "" && w.refName() != opts.Ref {
			return true, nil
		}

		e, err := r.build(w)
		if err != nil {
			return false, err
		}
		out = append(out, e)
		return len(out) < opts.Limit, nil
	})
	if err != nil {
		return nil, err
	}
	if !started {
		return nil, fmt.Errorf("%w: %s", ErrEntryNotFound, opts.Before)
	}
	return out, nil
}

// Find returns the entry sel names. A prefix that matches more than one entry
// is an error listing the matches.
func (r *Reader) Find(ctx context.Context, sel Selector) (Entry, error) {
	var matches []raw
	err := r.walk(ctx, func(w raw) (bool, error) {
		if sel.matches(w) {
			matches = append(matches, w)
		}
		// A number or full ID names one entry. Keep walking a prefix to
		// detect ambiguity.
		return len(matches) == 0 || sel.isPrefix(), nil
	})
	if err != nil {
		return Entry{}, err
	}

	switch len(matches) {
	case 0:
		return Entry{}, fmt.Errorf("%w: %s", ErrEntryNotFound, sel)
	case 1:
		return r.build(matches[0])
	default:
		ids := make([]string, 0, len(matches))
		for _, m := range matches {
			ids = append(ids, m.id().String())
		}
		return Entry{}, fmt.Errorf("%w %q matches %s", ErrAmbiguousEntry, sel.String(), strings.Join(ids, ", "))
	}
}

// History is the RSL record of one ref.
type History struct {
	// Entries are the newest reference and propagation entries, newest first.
	Entries []Entry
	// Total counts every such entry in the RSL, beyond the limit too.
	Total int
	// Current is the newest entry no skip annotation revokes, which records
	// the state the ref should have. Nil when every entry is skipped.
	Current *Entry
	// Unskipped counts the entries no skip annotation revokes.
	Unskipped int
}

// History returns up to limit entries for ref with counts over the whole RSL.
func (r *Reader) History(ctx context.Context, ref string, limit int) (History, error) {
	var h History
	err := r.walk(ctx, func(w raw) (bool, error) {
		if w.refName() != ref {
			return true, nil
		}
		h.Total++
		skipped := w.skipped()
		if !skipped {
			h.Unskipped++
		}

		keep := len(h.Entries) < limit
		current := h.Current == nil && !skipped
		if !keep && !current {
			return true, nil
		}

		e, err := r.build(w)
		if err != nil {
			return false, err
		}
		if keep {
			h.Entries = append(h.Entries, e)
		}
		if current {
			h.Current = &e
		}
		return true, nil
	})
	if err != nil {
		return History{}, err
	}
	return h, nil
}

// Selector names an entry by number or by ID or ID prefix.
type Selector struct {
	idPrefix string
	number   uint64
	byNumber bool
}

// minPrefix is the shortest ID prefix accepted, as git's default abbreviation.
const minPrefix = 7

// ParseSelector reads an entry number (decimal digits) or an entry ID or ID
// prefix (at least seven hex characters, not all digits).
func ParseSelector(s string) (Selector, error) {
	if s == "" || len(s) > 64 {
		return Selector{}, ErrInvalidEntryID
	}

	if isDigits(s) {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil || n == 0 {
			return Selector{}, ErrInvalidEntryID
		}
		return Selector{number: n, byNumber: true}, nil
	}

	lower := strings.ToLower(s)
	if len(lower) < minPrefix || !isHex(lower) {
		return Selector{}, ErrInvalidEntryID
	}
	return Selector{idPrefix: lower}, nil
}

func (s Selector) matches(w raw) bool {
	if s.byNumber {
		return w.entry.GetNumber() == s.number
	}
	return strings.HasPrefix(w.id().String(), s.idPrefix)
}

func (s Selector) isPrefix() bool {
	return !s.byNumber && len(s.idPrefix) != 40 && len(s.idPrefix) != 64
}

func (s Selector) String() string {
	if s.byNumber {
		return "#" + strconv.FormatUint(s.number, 10)
	}
	return s.idPrefix
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
