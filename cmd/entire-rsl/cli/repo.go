package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/entireio/entire-rsl/internal/repo"
	"github.com/entireio/entire-rsl/internal/rslread"
)

// session is an opened repository and its RSL, bound to one command's
// context.
type session struct {
	repo   *repo.Repo
	reader *rslread.Reader
}

// open opens the repository and its RSL.
func (a *app) open(ctx context.Context) (*session, error) {
	r, err := repo.Open(ctx, repo.Env{Getenv: a.opts.Getenv, Getwd: a.opts.Getwd}, a.logger)
	if err != nil {
		return nil, err
	}
	r.Store = r.Store.WithContext(ctx)
	a.logger.DebugContext(ctx, "opened repository", slog.String("root", r.Root))

	reader, err := rslread.Open(r.Store)
	if err != nil {
		if errors.Is(err, rslread.ErrNoRSL) {
			return nil, fmt.Errorf("%w\nThe repository has no gittuf Reference State Log locally. Fetch it from a remote and retry", err)
		}
		return nil, err
	}
	a.logger.DebugContext(ctx, "opened RSL", slog.String("tip", reader.Tip().String()))

	return &session{repo: r, reader: reader}, nil
}
