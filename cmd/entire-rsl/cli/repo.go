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

// open opens the repository and its RSL. With --fetch it first brings the
// local gittuf refs up to date with the remote's. Without it, it offers to
// fetch the RSL when the repository has none.
func (a *app) open(ctx context.Context) (*session, error) {
	r, err := a.openRepo(ctx)
	if err != nil {
		return nil, err
	}

	if a.fetchFrom != "" {
		if err := a.fetchLatest(ctx, r); err != nil {
			return nil, err
		}
		// Reopen so the store sees the objects the fetch added.
		if r, err = a.openRepo(ctx); err != nil {
			return nil, err
		}
	}

	reader, err := rslread.Open(r.Store)
	if errors.Is(err, rslread.ErrNoRSL) && a.fetchFrom == "" {
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

// remotes lists the configured remotes, sorted, with their first URL.
func remotes(r *repo.Repo) ([]string, map[string]string, error) {
	list, err := r.Store.Repository().Remotes()
	if err != nil {
		return nil, nil, fmt.Errorf("list remotes: %w", err)
	}
	names := make([]string, 0, len(list))
	urls := map[string]string{}
	for _, rem := range list {
		cfg := rem.Config()
		names = append(names, cfg.Name)
		if len(cfg.URLs) > 0 {
			urls[cfg.Name] = cfg.URLs[0]
		}
	}
	sortRemotes(names)
	return names, urls, nil
}

// defaultRemote is the remote git clone creates, listed first.
const defaultRemote = "origin"

// sortRemotes orders remotes by name with origin, usually the default, first.
func sortRemotes(names []string) {
	slices.SortFunc(names, func(a, b string) int {
		switch {
		case a == b:
			return 0
		case a == defaultRemote:
			return -1
		case b == defaultRemote:
			return 1
		default:
			return strings.Compare(a, b)
		}
	})
}

// remoteChoice is how a remote was chosen.
type remoteChoice struct {
	name string
	url  string
	// picked is set when the user picked the remote at a prompt, which is
	// consent to fetch from it.
	picked bool
}

// chooseRemote returns the requested remote, the only remote, or one the
// user picks on a terminal. want is empty when none was requested. noneErr
// and severalErr explain the failure when no remote can be chosen.
func (a *app) chooseRemote(r *repo.Repo, want string, noneErr error, severalHint string) (remoteChoice, error) {
	names, urls, err := remotes(r)
	if err != nil {
		return remoteChoice{}, err
	}

	switch {
	case len(names) == 0:
		return remoteChoice{}, noneErr
	case want != "":
		if !slices.Contains(names, want) {
			return remoteChoice{}, usageError(fmt.Errorf("no remote named %q (remotes: %s)", want, strings.Join(names, ", ")))
		}
		return remoteChoice{name: want, url: urls[want]}, nil
	case len(names) == 1:
		return remoteChoice{name: names[0], url: urls[names[0]]}, nil
	case a.interactive():
		name, err := a.pickRemote(names, urls)
		if err != nil {
			return remoteChoice{}, err
		}
		return remoteChoice{name: name, url: urls[name], picked: true}, nil
	default:
		return remoteChoice{}, fmt.Errorf("%s (remotes: %s)", severalHint, strings.Join(names, ", "))
	}
}

// syncGittufRefs checks the repository's config is safe to fetch with, then
// syncs the gittuf refs from remote.
func (a *app) syncGittufRefs(ctx context.Context, r *repo.Repo, remote string, fastForward bool) ([]gitcli.RefUpdate, error) {
	cfg, err := r.Store.Repository().Config()
	if err != nil {
		return nil, fmt.Errorf("read repository config: %w", err)
	}
	if err := gitcli.CheckFetchConfig(cfg.Raw, remote); err != nil {
		return nil, err
	}

	git, err := gitcli.Find()
	if err != nil {
		return nil, err
	}
	a.logger.DebugContext(ctx, "fetching gittuf refs", slog.String("remote", remote), slog.Bool("fast_forward", fastForward))
	fmt.Fprintf(a.opts.Stderr, "Fetching gittuf refs from %s...\n", sanitize.Line(remote))
	return git.SyncGittufRefs(ctx, r.Root, remote, fastForward)
}

// fetchLatest syncs the gittuf refs from the remote --fetch names, or the
// only remote, as refresh does: missing local refs are created and existing
// ones fast-forwarded. A diverged ref is left alone with a warning, and the
// command goes on with the local refs.
func (a *app) fetchLatest(ctx context.Context, r *repo.Repo) error {
	want := a.fetchFrom
	if want == fetchAnyRemote {
		want = ""
	}
	choice, err := a.chooseRemote(r, want, errNoRemote, "several remotes are configured, run again with --fetch=<remote>")
	if err != nil {
		return err
	}

	updates, err := a.syncGittufRefs(ctx, r, choice.name, true)
	if err != nil {
		return err
	}
	for _, u := range updates {
		switch u.Status {
		case gitcli.RefCreated:
			fmt.Fprintf(a.opts.Stderr, "Created %s\n", sanitize.Line(u.Ref))
		case gitcli.RefUpdated:
			fmt.Fprintf(a.opts.Stderr, "Updated %s\n", sanitize.Line(u.Ref))
		case gitcli.RefDiverged:
			warnDiverged(a.opts.Stderr, u)
		}
	}
	return nil
}

// warnDiverged explains that a local gittuf ref was left as it was.
func warnDiverged(w io.Writer, u gitcli.RefUpdate) {
	fmt.Fprintf(w, "Warning: %s was left at %s: the remote's %s does not descend from it\n", sanitize.Line(u.Ref), u.Old, u.New)
}

// offerFetch fetches the gittuf refs from a remote when the repository has
// no RSL and --fetch was not given, with the user's consent: a yes or a pick
// at a prompt on a terminal.
func (a *app) offerFetch(ctx context.Context, r *repo.Repo) error {
	choice, err := a.chooseRemote(r, "",
		fmt.Errorf("%w: %w", rslread.ErrNoRSL, errNoRemote),
		rslread.ErrNoRSL.Error()+": several remotes could hold it, run again with --fetch=<remote>")
	if err != nil {
		return err
	}

	if !choice.picked {
		if !a.interactive() {
			return fmt.Errorf("%w: run again with --fetch to fetch it from %s", rslread.ErrNoRSL, choice.name)
		}
		ok, err := a.confirm(fmt.Sprintf("The repository has no local gittuf RSL. Fetch the gittuf refs from %s (%s)? [y/N] ",
			sanitize.Line(choice.name), sanitize.Line(redact.URLs(choice.url))))
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: %w", rslread.ErrNoRSL, errFetchDeclined)
		}
	}

	updates, err := a.syncGittufRefs(ctx, r, choice.name, false)
	if err != nil {
		return err
	}
	for _, u := range updates {
		if u.Status == gitcli.RefCreated {
			fmt.Fprintf(a.opts.Stderr, "Created %s\n", sanitize.Line(u.Ref))
		}
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
	fmt.Fprintln(a.opts.Stderr, "Several remotes are configured:")
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
