// Package repo locates the repository entire-rsl inspects and opens it
// through go-git.
package repo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/entireio/entire-rsl/internal/store"
	gogit "github.com/go-git/go-git/v6"
)

// EnvRepoRoot is set by the Entire CLI to the repository root when it runs
// inside one.
const EnvRepoRoot = "ENTIRE_REPO_ROOT"

// ErrNotRepository is returned when no repository can be found.
var ErrNotRepository = errors.New("not a git repository (run inside one, or set " + EnvRepoRoot + ")")

// Repo is an opened repository.
type Repo struct {
	Store *store.Store
	// Root is the directory the repository was opened from.
	Root string
}

// Env is how Open reads the process environment.
type Env struct {
	Getenv func(key string) string
	Getwd  func() (string, error)
}

// ProcessEnv reads the real process environment.
func ProcessEnv(getenv func(string) string) Env {
	return Env{
		Getenv: getenv,
		Getwd:  os.Getwd, //nolint:forbidigo // this package is the one place the working directory is resolved
	}
}

// Open opens the repository named by ENTIRE_REPO_ROOT, or else the one
// containing the working directory. ENTIRE_REPO_ROOT wins when both are
// available and differ, and the difference is logged at debug level.
func Open(ctx context.Context, env Env, logger *slog.Logger) (*Repo, error) {
	cwd, cwdErr := env.Getwd()

	root := env.Getenv(EnvRepoRoot)
	if root == "" {
		if cwdErr != nil {
			return nil, fmt.Errorf("resolve working directory: %w", cwdErr)
		}
		return open(cwd, true)
	}

	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("%s must be an absolute path, got %q", EnvRepoRoot, root)
	}

	r, err := open(root, false)
	if err != nil {
		return nil, fmt.Errorf("open %s=%q: %w", EnvRepoRoot, root, err)
	}

	if logger.Enabled(ctx, slog.LevelDebug) && cwdErr == nil {
		if discovered, err := discoverRoot(cwd); err == nil && !sameDir(discovered, root) {
			logger.DebugContext(ctx, "ENTIRE_REPO_ROOT differs from the repository containing the working directory, using ENTIRE_REPO_ROOT",
				slog.String("entire_repo_root", root),
				slog.String("cwd_repository", discovered))
		}
	}

	return r, nil
}

func open(path string, detect bool) (*Repo, error) {
	g, err := gogit.PlainOpenWithOptions(path, &gogit.PlainOpenOptions{DetectDotGit: detect})
	if err != nil {
		if errors.Is(err, gogit.ErrRepositoryNotExists) {
			return nil, ErrNotRepository
		}
		return nil, fmt.Errorf("open repository: %w", err)
	}

	s, err := store.New(g)
	if err != nil {
		return nil, fmt.Errorf("open repository: %w", err)
	}

	root := path
	if wt, err := g.Worktree(); err == nil {
		root = wt.Filesystem().Root()
	}

	return &Repo{Store: s, Root: root}, nil
}

func discoverRoot(cwd string) (string, error) {
	r, err := open(cwd, true)
	if err != nil {
		return "", err
	}
	return r.Root, nil
}

func sameDir(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return os.SameFile(ai, bi)
}
