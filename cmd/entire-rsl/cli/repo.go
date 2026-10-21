package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/entireio/entire-rsl/internal/gitcli"
	"github.com/entireio/entire-rsl/internal/redact"
	"github.com/entireio/entire-rsl/internal/repo"
	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/entireio/entire-rsl/internal/sanitize"
)

// maxAnswer caps how much of a typed answer is kept.
const maxAnswer = 1024

// fetchAnyRemote is --fetch given without a remote.
const fetchAnyRemote = "\x00any"

var (
	errFetchDeclined = errors.New("the RSL was not fetched")
	errNoRemote      = errors.New("no remote is configured to fetch it from")
)

// session is an opened repository and its RSL, bound to one command's
// context.
type session struct {
	repo   *repo.Repo
	reader *rslread.Reader
}

// open opens the repository and its RSL, offering to fetch the RSL when the
// repository has none.
func (a *app) open(ctx context.Context) (*session, error) {
	r, err := a.openRepo(ctx)
	if err != nil {
		return nil, err
	}

	reader, err := rslread.Open(r.Store)
	if errors.Is(err, rslread.ErrNoRSL) {
		if err := a.offerFetch(ctx, r); err != nil {
			return nil, err
		}
		reader, err = rslread.Open(r.Store)
	}
	if err != nil {
		return nil, err
	}
	a.logger.DebugContext(ctx, "opened RSL", slog.String("tip", reader.Tip().String()))

	return &session{repo: r, reader: reader}, nil
}

func (a *app) openRepo(ctx context.Context) (*repo.Repo, error) {
	r, err := repo.Open(ctx, repo.Env{Getenv: a.opts.Getenv, Getwd: a.opts.Getwd}, a.logger)
	if err != nil {
		return nil, err
	}
	r.Store = r.Store.WithContext(ctx)
	a.logger.DebugContext(ctx, "opened repository", slog.String("root", r.Root))
	return r, nil
}

// offerFetch fetches the gittuf refs from a remote, with the user's consent:
// --fetch, or a yes at a prompt on a terminal.
func (a *app) offerFetch(ctx context.Context, r *repo.Repo) error {
	remotes, err := r.Store.Repository().Remotes()
	if err != nil {
		return fmt.Errorf("list remotes: %w", err)
	}
	names := make([]string, 0, len(remotes))
	urls := map[string]string{}
	for _, rem := range remotes {
		cfg := rem.Config()
		names = append(names, cfg.Name)
		if len(cfg.URLs) > 0 {
			urls[cfg.Name] = cfg.URLs[0]
		}
	}
	slices.Sort(names)

	if len(names) == 0 {
		return fmt.Errorf("%w: %w", rslread.ErrNoRSL, errNoRemote)
	}

	consented := a.fetchFrom != ""
	remote := a.fetchFrom
	switch {
	case remote != "" && remote != fetchAnyRemote:
		if !slices.Contains(names, remote) {
			return usageError(fmt.Errorf("--fetch: no remote named %q (remotes: %s)", remote, strings.Join(names, ", ")))
		}
	case len(names) == 1:
		remote = names[0]
	case a.interactive():
		// Picking a remote is the consent.
		remote, err = a.pickRemote(names, urls)
		if err != nil {
			return err
		}
		consented = true
	default:
		return fmt.Errorf("%w: several remotes could hold it, run again with --fetch=<remote> (remotes: %s)", rslread.ErrNoRSL, strings.Join(names, ", "))
	}

	if !consented {
		if !a.interactive() {
			return fmt.Errorf("%w: run again with --fetch to fetch it from %s", rslread.ErrNoRSL, remote)
		}
		ok, err := a.confirm(fmt.Sprintf("The repository has no local gittuf RSL. Fetch the gittuf refs from %s (%s)? [y/N] ",
			sanitize.Line(remote), sanitize.Line(redact.URLs(urls[remote]))))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: %w", rslread.ErrNoRSL, errFetchDeclined)
		}
	}

	cfg, err := r.Store.Repository().Config()
	if err != nil {
		return fmt.Errorf("read repository config: %w", err)
	}
	if err := gitcli.CheckFetchConfig(cfg.Raw, remote); err != nil {
		return err
	}

	git, err := gitcli.Find()
	if err != nil {
		return err
	}
	a.logger.DebugContext(ctx, "fetching gittuf refs", slog.String("remote", remote))
	fmt.Fprintf(a.opts.Stderr, "Fetching gittuf refs from %s...\n", sanitize.Line(remote))
	created, err := git.FetchGittufRefs(ctx, r.Root, remote)
	if err != nil {
		return err
	}
	for _, ref := range created {
		fmt.Fprintf(a.opts.Stderr, "Created %s\n", sanitize.Line(ref))
	}
	return nil
}

func (a *app) interactive() bool {
	return a.opts.Interactive != nil && a.opts.Interactive()
}

// confirm asks a yes or no question on stderr and reads the answer.
func (a *app) confirm(question string) (bool, error) {
	fmt.Fprint(a.opts.Stderr, question)
	answer, err := a.readLine()
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

// pickRemote asks which remote to fetch from.
func (a *app) pickRemote(names []string, urls map[string]string) (string, error) {
	fmt.Fprintln(a.opts.Stderr, "The repository has no local gittuf RSL. Several remotes could hold it:")
	for i, name := range names {
		fmt.Fprintf(a.opts.Stderr, "  %d) %s  %s\n", i+1, sanitize.Line(name), sanitize.Line(redact.URLs(urls[name])))
	}
	fmt.Fprintf(a.opts.Stderr, "Fetch from which remote? [1-%d, empty to cancel] ", len(names))

	answer, err := a.readLine()
	if err != nil {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", fmt.Errorf("%w: %w", rslread.ErrNoRSL, errFetchDeclined)
	}
	n, err := strconv.Atoi(answer)
	if err != nil || n < 1 || n > len(names) {
		return "", usageError(fmt.Errorf("no remote numbered %q", answer))
	}
	return names[n-1], nil
}

func (a *app) readLine() (string, error) {
	if a.stdin == nil {
		a.stdin = bufio.NewReader(a.opts.Stdin)
	}
	line, err := a.stdin.ReadString('\n')
	if len(line) > maxAnswer {
		line = line[:maxAnswer]
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read answer: %w", err)
	}
	return line, nil
}
