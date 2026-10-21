package gitcli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/entireio/entire-rsl/internal/redact"
	"github.com/entireio/entire-rsl/internal/refname"
)

// ErrGitNotFound is returned when no git binary is on PATH.
var ErrGitNotFound = errors.New("git not found on PATH")

// waitDelay bounds how long a cancelled git process may take to exit after
// it is interrupted, matching the Entire CLI's grace period.
const waitDelay = 5 * time.Second

// gittufPrefix is the namespace gittuf keeps its refs in.
const gittufPrefix = "refs/gittuf/"

// Git runs one resolved git binary.
type Git struct {
	path string
}

// Find resolves git on PATH to an absolute path, once.
func Find() (*Git, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGitNotFound, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve git path: %w", err)
	}
	return &Git{path: abs}, nil
}

// RunError is a git process that exited unsuccessfully.
type RunError struct {
	Err    error
	Args   string
	Stderr string
}

func (e *RunError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("git %s: %v", e.Args, e.Err)
	}
	return fmt.Sprintf("git %s: %v: %s", e.Args, e.Err, e.Stderr)
}

func (e *RunError) Unwrap() error { return e.Err }

// TrackingPrefix is where a remote's gittuf refs are fetched to, gittuf's own
// remote-tracking layout (pkg/rsl remoteTrackerRef).
func TrackingPrefix(remote string) string {
	return "refs/remotes/" + remote + "/gittuf/"
}

// Sync statuses of a local gittuf ref.
const (
	// RefCreated: the ref did not exist locally and was created.
	RefCreated = "created"
	// RefUpdated: the remote's ref fast-forwarded the local one.
	RefUpdated = "updated"
	// RefUnchanged: the local ref already matched, or a fast-forward was not
	// requested.
	RefUnchanged = "unchanged"
	// RefDiverged: the remote's ref does not descend from the local one, or
	// the local ref is symbolic, so the local ref was left as it was.
	RefDiverged = "diverged"
)

// RefUpdate is what a sync did to one local gittuf ref.
type RefUpdate struct {
	Ref    string
	Old    string
	New    string
	Status string
	// Commits counts the commits in the remote's ref that the local one did
	// not have: all of them for a created ref, the fast-forward for an
	// updated one, the remote's side for a diverged one.
	Commits int
	// LocalCommits counts, for a diverged ref, the commits only the local
	// ref has.
	LocalCommits int
	// Symbolic is set when the local ref is symbolic, which is why it was
	// left as it was.
	Symbolic bool
}

// SyncGittufRefs fetches the remote's gittuf refs into
// refs/remotes/<remote>/gittuf/*, then brings the local refs/gittuf/* refs
// in line: missing refs are created and, when fastForward is set, existing
// refs the remote's descend from are updated. A local ref the remote's does
// not descend from is never changed and is reported as diverged. Every
// change happens in one update-ref transaction that checks each ref still
// holds the value it was read with. It is entire-rsl's only write path.
func (g *Git) SyncGittufRefs(ctx context.Context, root, remote string, fastForward bool) ([]RefUpdate, error) {
	if err := refname.Validate(remote); err != nil {
		return nil, fmt.Errorf("remote name: %w", err)
	}
	tracking := TrackingPrefix(remote)

	// --refmap= drops the remote's configured fetch mappings, so only the
	// refspec below is updated. --no-prune and --no-prune-tags stop
	// configured pruning from deleting refs.
	if _, err := g.run(ctx, root,
		"fetch",
		"--quiet",
		"--no-tags",
		"--refmap=",
		"--no-prune",
		"--no-prune-tags",
		"--no-write-fetch-head",
		"--no-auto-maintenance",
		"--no-recurse-submodules",
		"--",
		remote,
		"+"+gittufPrefix+"*:"+tracking+"*",
	); err != nil {
		return nil, fmt.Errorf("fetch from %s: %w", remote, err)
	}

	fetched, _, err := g.refs(ctx, root, tracking)
	if err != nil {
		return nil, err
	}
	local, symbolic, err := g.refs(ctx, root, gittufPrefix)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(fetched))
	for name := range fetched {
		names = append(names, name)
	}
	slices.Sort(names)

	var (
		updates []RefUpdate
		tx      strings.Builder
		changes int
	)
	tx.WriteString("start\n")
	for _, name := range names {
		id := fetched[name]
		localName := gittufPrefix + strings.TrimPrefix(name, tracking)
		if err := refname.Validate(localName); err != nil {
			return nil, fmt.Errorf("fetched ref: %w", err)
		}

		u := RefUpdate{Ref: localName, Old: local[localName], New: id}
		switch {
		case u.Old == "":
			u.Status = RefCreated
			if u.Commits, err = g.countCommits(ctx, root, "", id); err != nil {
				return nil, err
			}
			fmt.Fprintf(&tx, "create %s %s\n", localName, id)
			changes++
		case symbolic[localName]:
			// A symbolic gittuf ref would redirect the write to its target,
			// possibly outside refs/gittuf/, so it is never touched. The
			// counts compare with the object it resolves to.
			u.Status, u.Symbolic = RefDiverged, true
			if u.Commits, err = g.countCommits(ctx, root, u.Old, id); err != nil {
				return nil, err
			}
			if u.LocalCommits, err = g.countCommits(ctx, root, id, u.Old); err != nil {
				return nil, err
			}
		case u.Old == id || !fastForward:
			u.Status = RefUnchanged
		default:
			ff, err := g.isAncestor(ctx, root, u.Old, id)
			if err != nil {
				return nil, err
			}
			if u.Commits, err = g.countCommits(ctx, root, u.Old, id); err != nil {
				return nil, err
			}
			if !ff {
				u.Status = RefDiverged
				if u.LocalCommits, err = g.countCommits(ctx, root, id, u.Old); err != nil {
					return nil, err
				}
				break
			}
			u.Status = RefUpdated
			fmt.Fprintf(&tx, "update %s %s %s\n", localName, id, u.Old)
			changes++
		}
		updates = append(updates, u)
	}
	tx.WriteString("commit\n")

	if changes > 0 {
		if _, err := g.runInput(ctx, root, tx.String(), "update-ref", "--no-deref", "--stdin"); err != nil {
			return nil, fmt.Errorf("update local gittuf refs: %w", err)
		}
	}
	return updates, nil
}

// countCommits counts the commits reachable from to and not from from, or
// all commits reachable from to when from is empty.
func (g *Git) countCommits(ctx context.Context, root, from, to string) (int, error) {
	rng := to
	if from != "" {
		rng = from + ".." + to
	}
	out, err := g.run(ctx, root, "rev-list", "--count", rng, "--")
	if err != nil {
		return 0, fmt.Errorf("count commits in %s: %w", rng, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("count commits in %s: %w", rng, err)
	}
	return n, nil
}

// isAncestor reports whether ancestor is new or one of its ancestors.
func (g *Git) isAncestor(ctx context.Context, root, ancestor, commit string) (bool, error) {
	_, err := g.run(ctx, root, "merge-base", "--is-ancestor", ancestor, commit)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("compare %s with %s: %w", ancestor, commit, err)
}

// refs lists the refs under prefix with their object IDs, and which of them
// are symbolic.
func (g *Git) refs(ctx context.Context, root, prefix string) (map[string]string, map[string]bool, error) {
	out, err := g.run(ctx, root, "for-each-ref", "--format=%(refname) %(objectname) %(symref)", "--", prefix)
	if err != nil {
		return nil, nil, fmt.Errorf("list %s: %w", prefix, err)
	}

	refs := map[string]string{}
	symbolic := map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		refs[fields[0]] = fields[1]
		if len(fields) > 2 {
			symbolic[fields[0]] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("list %s: %w", prefix, err)
	}
	return refs, symbolic, nil
}

func (g *Git) run(ctx context.Context, root string, args ...string) ([]byte, error) {
	return g.runInput(ctx, root, "", args...)
}

// runInput runs git in root with the execution vectors of repository config
// neutralised, and returns its stdout.
func (g *Git) runInput(ctx context.Context, root, stdin string, args ...string) ([]byte, error) {
	// --no-replace-objects and an empty graft file make git read the stored
	// commit graph, so refs/replace/* or grafts cannot fake an ancestry.
	full := append([]string{
		"--no-replace-objects",
		"-C", root,
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "protocol.ext.allow=never",
		"-c", "fetch.writeCommitGraph=false",
	}, args...)

	cmd := exec.CommandContext(ctx, g.path, full...)
	cmd.Cancel = func() error { return interrupt(cmd.Process) }
	cmd.WaitDelay = waitDelay
	cmd.Env = append(os.Environ(), "GIT_GRAFT_FILE="+os.DevNull)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		return nil, &RunError{Args: args[0], Err: err, Stderr: redact.URLs(strings.TrimSpace(stderr.String()))}
	}
	return stdout.Bytes(), nil
}
