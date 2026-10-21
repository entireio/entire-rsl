package store

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/storer"
)

// ErrTooManyReferences is returned when a listing exceeds its cap.
var ErrTooManyReferences = errors.New("too many references")

// maxReferences caps a reference listing.
const maxReferences = 1_000_000

// Person is a commit author or committer as recorded in the object. Nothing
// verifies it.
type Person struct {
	When  time.Time
	Name  string
	Email string
}

// CommitInfo is the unverified metadata of a commit.
type CommitInfo struct {
	Author    Person
	Committer Person
	Message   string
}

// CommitInfo returns a commit's author, committer and full message.
func (s *Store) CommitInfo(id githash.Hash) (CommitInfo, error) {
	commit, err := s.commit(id)
	if err != nil {
		return CommitInfo{}, err
	}
	return CommitInfo{
		Author:    Person{Name: commit.Author.Name, Email: commit.Author.Email, When: commit.Author.When},
		Committer: Person{Name: commit.Committer.Name, Email: commit.Committer.Email, When: commit.Committer.When},
		Message:   commit.Message,
	}, nil
}

// IsShallowBoundary reports whether id is a shallow boundary: a commit whose
// parents were cut off by a shallow fetch, so GetCommitParentIDs reports none.
func (s *Store) IsShallowBoundary(id githash.Hash) (bool, error) {
	h, err := toGoGit(id)
	if err != nil {
		return false, err
	}
	shallow, err := s.shallow()
	if err != nil {
		return false, err
	}
	return slices.Contains(shallow, h), nil
}

// Ref is a reference and the object it resolves to.
type Ref struct {
	Name   string
	Target githash.Hash
}

// References lists the references under prefix, sorted by name. Symbolic
// references are resolved, and ones that do not resolve are skipped.
func (s *Store) References(prefix string) ([]Ref, error) {
	iter, err := s.repo.References()
	if err != nil {
		return nil, &ObjectError{Op: "list references", Err: err}
	}
	defer iter.Close()

	var refs []Ref
	err = iter.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		if !strings.HasPrefix(name, prefix) {
			return nil
		}
		if len(refs) >= maxReferences {
			return ErrTooManyReferences
		}

		resolved := ref
		if ref.Type() == plumbing.SymbolicReference {
			resolved, err = storer.ResolveReference(s.repo.Storer, ref.Name())
			if err != nil {
				return nil //nolint:nilerr // a dangling symbolic reference names nothing to report
			}
		}

		target, err := fromGoGit(resolved.Hash())
		if err != nil {
			return err
		}
		refs = append(refs, Ref{Name: name, Target: target})
		return nil
	})
	if err != nil {
		return nil, &ObjectError{Op: "list references", Err: err}
	}

	slices.SortFunc(refs, func(a, b Ref) int { return strings.Compare(a.Name, b.Name) })
	return refs, nil
}
