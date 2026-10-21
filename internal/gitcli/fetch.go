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

// TrackingPrefix is where a remote's gittuf refs are fetched to, gittuf's own
// remote-tracking layout (pkg/rsl remoteTrackerRef).
func TrackingPrefix(remote string) string {
	return "refs/remotes/" + remote + "/gittuf/"
}

// FetchGittufRefs fetches the remote's gittuf refs into
// refs/remotes/<remote>/gittuf/*, then creates each local refs/gittuf/* ref
// that does not exist yet from its tracking ref, in one transaction that
// fails if any of them appeared meanwhile. Existing local refs are never
// changed. It returns the local refs it created. It is entire-rsl's only
// write path.
func (g *Git) FetchGittufRefs(ctx context.Context, root, remote string) ([]string, error) {
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

	fetched, err := g.refs(ctx, root, tracking)
	if err != nil {
		return nil, err
	}
	local, err := g.refs(ctx, root, gittufPrefix)
	if err != nil {
		return nil, err
	}

	var tx strings.Builder
	var created []string
	tx.WriteString("start\n")
	for name, id := range fetched {
		localName := gittufPrefix + strings.TrimPrefix(name, tracking)
		if err := refname.Validate(localName); err != nil {
			return nil, fmt.Errorf("fetched ref: %w", err)
		}
		if _, exists := local[localName]; exists {
			continue
		}
		fmt.Fprintf(&tx, "create %s %s\n", localName, id)
		created = append(created, localName)
	}
	tx.WriteString("commit\n")

	if len(created) == 0 {
		return nil, nil
	}
	if _, err := g.runInput(ctx, root, tx.String(), "update-ref", "--stdin"); err != nil {
		return nil, fmt.Errorf("create local gittuf refs: %w", err)
	}
	return created, nil
}

// refs lists the refs under prefix and their object IDs.
func (g *Git) refs(ctx context.Context, root, prefix string) (map[string]string, error) {
	out, err := g.run(ctx, root, "for-each-ref", "--format=%(refname) %(objectname)", "--", prefix)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}

	refs := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		name, id, ok := strings.Cut(scanner.Text(), " ")
		if !ok {
			continue
		}
		refs[name] = id
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}
	return refs, nil
}

func (g *Git) run(ctx context.Context, root string, args ...string) ([]byte, error) {
	return g.runInput(ctx, root, "", args...)
}

// runInput runs git in root with the execution vectors of repository config
// neutralised, and returns its stdout.
func (g *Git) runInput(ctx context.Context, root, stdin string, args ...string) ([]byte, error) {
	full := append([]string{
		"-C", root,
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "protocol.ext.allow=never",
		"-c", "fetch.writeCommitGraph=false",
	}, args...)

	cmd := exec.CommandContext(ctx, g.path, full...)
	cmd.Cancel = func() error { return interrupt(cmd.Process) }
	cmd.WaitDelay = waitDelay
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, redact.URLs(strings.TrimSpace(stderr.String())))
	}
	return stdout.Bytes(), nil
}
