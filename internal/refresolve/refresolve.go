// Package refresolve turns the names a user types into full ref names.
//
// Short names resolve the way git's DWIM rules do (refs/<name>,
// refs/tags/<name>, refs/heads/<name>, refs/remotes/<name>, then
// refs/remotes/<name>/HEAD), with one difference: when more than one
// candidate exists, resolution fails and lists them instead of picking one.
// A candidate exists when the ref exists locally or the RSL records it, so a
// ref that was pushed but never fetched still resolves.
package refresolve

import (
	"errors"
	"fmt"
	"strings"

	"github.com/entireio/entire-rsl/internal/refname"
)

// Ref name prefixes.
const (
	HeadsPrefix   = "refs/heads/"
	TagsPrefix    = "refs/tags/"
	remotesPrefix = "refs/remotes/"
)

var (
	// ErrAmbiguous is returned when a short name matches more than one ref.
	ErrAmbiguous = errors.New("ambiguous ref name")
	// ErrNotFound is returned when no candidate exists.
	ErrNotFound = errors.New("no ref matches")
	// ErrWrongNamespace is returned when a full name is outside the namespace
	// the command reads.
	ErrWrongNamespace = errors.New("ref is outside the expected namespace")
)

// Branch returns the full name of branch name, given short or as
// refs/heads/<name>.
func Branch(name string) (string, error) {
	return inNamespace(name, HeadsPrefix, "branch")
}

// Tag returns the full name of tag name, given short or as refs/tags/<name>.
func Tag(name string) (string, error) {
	return inNamespace(name, TagsPrefix, "tag")
}

func inNamespace(name, prefix, kind string) (string, error) {
	if err := refname.Validate(name); err != nil {
		return "", err
	}

	full := name
	if !strings.HasPrefix(name, prefix) {
		if refname.IsFull(name) {
			return "", fmt.Errorf("%w: %q is not a %s (%s...)", ErrWrongNamespace, name, kind, prefix)
		}
		full = prefix + name
	}

	if full == prefix {
		return "", fmt.Errorf("%w: empty %s name", refname.ErrInvalid, kind)
	}
	return full, refname.Validate(full)
}

// Exists reports whether a full ref name exists locally or in the RSL.
type Exists func(ref string) (bool, error)

// Resolve returns the full ref name for name. A full name is returned as is.
// A short name follows git's DWIM order and fails when more than one
// candidate exists.
func Resolve(name string, exists Exists) (string, error) {
	if err := refname.Validate(name); err != nil {
		return "", err
	}
	if refname.IsFull(name) {
		return name, nil
	}

	var found []string
	for _, candidate := range candidates(name) {
		ok, err := exists(candidate)
		if err != nil {
			return "", err
		}
		if ok {
			found = append(found, candidate)
		}
	}

	switch len(found) {
	case 0:
		return "", fmt.Errorf("%w %q locally or in the RSL", ErrNotFound, name)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("%w %q matches %s; name one in full", ErrAmbiguous, name, strings.Join(found, ", "))
	}
}

// candidates lists git's DWIM expansions of a short name, in order.
func candidates(name string) []string {
	return []string{
		"refs/" + name,
		TagsPrefix + name,
		HeadsPrefix + name,
		remotesPrefix + name,
		remotesPrefix + name + "/HEAD",
	}
}
