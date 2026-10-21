package gitcli

import (
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

// FetchGittufRefs fetches the remote's gittuf refs into the repository at
// root. It creates missing local refs/gittuf/* refs without forcing, so an
// existing local ref that the remote's does not fast-forward makes the fetch
// fail instead of being overwritten, and it updates the remote-tracking refs
// refs/remotes/<remote>/gittuf/*. It is entire-rsl's only write path.
func (g *Git) FetchGittufRefs(ctx context.Context, root, remote string) error {
	if err := refname.Validate(remote); err != nil {
		return fmt.Errorf("remote name: %w", err)
	}

	args := []string{
		"-C", root,
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "protocol.ext.allow=never",
		"-c", "fetch.writeCommitGraph=false",
		"fetch",
		"--quiet",
		"--no-tags",
		"--no-write-fetch-head",
		"--no-auto-maintenance",
		"--no-recurse-submodules",
		"--",
		remote,
		"refs/gittuf/*:refs/gittuf/*",
		"+refs/gittuf/*:refs/remotes/" + remote + "/gittuf/*",
	}

	cmd := exec.CommandContext(ctx, g.path, args...)
	cmd.Cancel = func() error { return interrupt(cmd.Process) }
	cmd.WaitDelay = waitDelay
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("fetch from %s: %w", remote, ctx.Err())
		}
		return fmt.Errorf("fetch from %s: %w: %s", remote, err, redact.URLs(strings.TrimSpace(out.String())))
	}
	return nil
}
