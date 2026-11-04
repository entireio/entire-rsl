package refresolve

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// CheckpointsPrefix is the git-refs checkpoint backend namespace:
// refs/entire/checkpoints/<shard>/<id>, where the shard is the ID's last two
// characters (entireio/cli cmd/entire/cli/checkpoint/refs_naming.go).
const CheckpointsPrefix = "refs/entire/checkpoints/"

// CheckpointTrailer is the commit trailer linking a commit to its checkpoint
// (entireio/cli cmd/entire/cli/trailers/trailers.go).
const CheckpointTrailer = "Entire-Checkpoint"

var (
	// legacyIDPattern is a legacy checkpoint ID: 12 lowercase hex characters.
	legacyIDPattern = regexp.MustCompile(`^[0-9a-f]{12}$`)
	// ulidPattern is a canonical ULID: 26 uppercase Crockford base32
	// characters, the first at most 7 so the timestamp cannot overflow. It
	// accepts exactly what entireio/cli's ulid.ParseStrict plus canonical
	// round trip accepts.
	ulidPattern = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)
	// commitIDPattern is a full SHA-1 or SHA-256 object ID.
	commitIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	// trailerPattern matches an Entire-Checkpoint trailer line. It is
	// anchored to a whole line, so an ID quoted in the body does not count.
	trailerPattern = regexp.MustCompile(`(?m)^` + CheckpointTrailer + `:[ \t]*([0-9A-Za-z]+)[ \t]*$`)
)

var (
	// ErrInvalidCheckpoint is returned for input that is neither a checkpoint
	// ID nor a full commit ID.
	ErrInvalidCheckpoint = errors.New("not a checkpoint ID (12 lowercase hex characters, or a 26 character ULID) or a full commit ID")
	// ErrNoCheckpointTrailer is returned for a commit with no checkpoint.
	ErrNoCheckpointTrailer = errors.New("commit has no " + CheckpointTrailer + " trailer")
	// ErrManyCheckpointTrailers is returned for a commit naming several.
	ErrManyCheckpointTrailers = errors.New("commit has several " + CheckpointTrailer + " trailers")
)

// IsCheckpointID reports whether s is a valid checkpoint ID.
func IsCheckpointID(s string) bool {
	return legacyIDPattern.MatchString(s) || ulidPattern.MatchString(s)
}

// IsCommitID reports whether s is a full lowercase commit ID.
func IsCommitID(s string) bool {
	return commitIDPattern.MatchString(s)
}

// CheckpointRef returns the git-refs backend ref for a valid checkpoint ID.
func CheckpointRef(id string) string {
	return CheckpointsPrefix + id[len(id)-2:] + "/" + id
}

// CheckpointFromRef returns the checkpoint ID a ref names, accepting a shard
// whose case differs from the ID's, as entireio/cli's ParseRef does: on a
// case-insensitive file system a ULID's shard directory may take the case of
// an existing legacy one.
func CheckpointFromRef(ref string) (string, bool) {
	tail, ok := strings.CutPrefix(ref, CheckpointsPrefix)
	if !ok {
		return "", false
	}
	shard, id, ok := strings.Cut(tail, "/")
	if !ok || strings.Contains(id, "/") || !IsCheckpointID(id) {
		return "", false
	}
	return id, strings.EqualFold(shard, id[len(id)-2:])
}

// CheckpointFromMessage returns the checkpoint a commit message's
// Entire-Checkpoint trailer names. No trailer, an invalid ID, or several
// distinct IDs is an error naming what was found.
func CheckpointFromMessage(message string) (string, error) {
	var ids []string
	for _, m := range trailerPattern.FindAllStringSubmatch(message, -1) {
		if !IsCheckpointID(m[1]) {
			return "", fmt.Errorf("%w: %q is not a valid checkpoint ID", ErrInvalidCheckpoint, m[1])
		}
		if !slices.Contains(ids, m[1]) {
			ids = append(ids, m[1])
		}
	}

	switch len(ids) {
	case 0:
		return "", ErrNoCheckpointTrailer
	case 1:
		return ids[0], nil
	default:
		return "", fmt.Errorf("%w: %s", ErrManyCheckpointTrailers, strings.Join(ids, ", "))
	}
}
