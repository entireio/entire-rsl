// Package verify reports whether gittuf verification passes for a ref, as one
// of four states.
//
// Only gittuf decides: verified means gittuf's full verification of the ref's
// RSL history returned no error. entire-rsl adds only the checks that keep a
// pass from overstating what was proven. gittuf passes a ref no rule covers
// without checking anything (internal/policy/verify.go, "This target is not
// protected by gittuf policy"), so such a ref is unprotected, as is a ref with
// no policy or no RSL entries. Any error, timeout or panic is an error, never
// verified.
package verify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/entireio/entire-rsl/internal/clusters"
	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/entireio/entire-rsl/internal/store"
	"github.com/gittuf/gittuf/experimental/gittuf"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/gitstore"
	"github.com/gittuf/gittuf/pkg/policy"
	"github.com/gittuf/gittuf/pkg/rsl"
)

// State is a verification outcome.
type State string

// The four states. Only Verified is a pass.
const (
	Verified    State = "verified"
	Failed      State = "failed"
	Unprotected State = "unprotected"
	Error       State = "error"
)

// gitRuleScheme prefixes a gittuf rule pattern that protects a ref.
const gitRuleScheme = "git:"

// Result is the outcome of verifying one ref.
type Result struct {
	// Err is the underlying error for Failed and Error.
	Err error
	// ExpectedTip is the target of the latest RSL entry, which gittuf
	// verified, for Verified.
	ExpectedTip githash.Hash
	// FromEntry is the first RSL entry for the ref, where verification starts.
	FromEntry githash.Hash
	// FromNumber is FromEntry's number, 0 for an unnumbered entry.
	FromNumber uint64
	// Checked describes the RSL entries for the ref gittuf verified, from
	// FromEntry to the latest. Set only when gittuf's verification passed,
	// even if an Entire check then failed.
	Checked *Checked
	State   State
	Ref     string
	// Reason explains the state in a sentence.
	Reason string
	// Rules names the policy rules that cover the ref.
	Rules []string
	// Checks are entire-rsl's own checks, run once gittuf has evaluated the
	// ref.
	Checks   []Check
	Duration time.Duration
}

// Checked is the range of RSL entries for a ref that verification covers.
type Checked struct {
	First, Last rslread.Target
	// Count counts the ref's entries in the range, Skipped those an
	// annotation revokes.
	Count, Skipped int
}

// checkedRange summarises the ref's entries, newest first.
func checkedRange(targets []rslread.Target) *Checked {
	if len(targets) == 0 {
		return nil
	}
	c := &Checked{First: targets[len(targets)-1], Last: targets[0], Count: len(targets)}
	for _, t := range targets {
		if t.Skipped {
			c.Skipped++
		}
	}
	return c
}

// Options configure Verify.
type Options struct {
	Logger *slog.Logger
	// RepoPath is the repository's common git directory, used to list
	// policy rules through gittuf's git binary backed repository. gittuf
	// reads the config from the git directory it finds there, which a
	// linked worktree's own git directory does not hold.
	RepoPath string
	// LocalTip reads the local value of a ref, nil when it does not exist.
	LocalTip func(ref string) (githash.Hash, error)
	// Keys are the root and RSL signer keys the checks accept.
	Keys    []clusters.Key
	Timeout time.Duration
}

// Verify runs full gittuf verification for ref.
func Verify(ctx context.Context, st *store.Store, ref string, opts Options) (res Result) {
	start := time.Now()
	res = Result{Ref: ref}

	defer func() {
		if p := recover(); p != nil {
			res = Result{Ref: ref, State: Error, Reason: "verification panicked", Err: fmt.Errorf("panic: %v", p)}
		}
		res.Duration = time.Since(start)
		opts.Logger.DebugContext(ctx, "verification finished",
			slog.String("ref", ref), slog.String("state", string(res.State)), slog.Duration("duration", res.Duration))
	}()

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}
	st = st.WithContext(ctx)

	// Every later step walks the RSL through gittuf, which reads a gap in the
	// history as its start. A truncated RSL could then hide the policy or the
	// ref's earliest entries, so completeness is established first.
	reader, err := rslread.Open(st)
	if errors.Is(err, rslread.ErrNoRSL) {
		res.State, res.Reason = Unprotected, "the repository has no gittuf RSL"
		return res
	}
	if err != nil {
		return errorResult(ctx, ref, "could not read the RSL", err)
	}
	if err := reader.CheckComplete(ctx); err != nil {
		return errorResult(ctx, ref, "the local RSL history is incomplete", err)
	}

	hasPolicy, err := hasPolicy(st)
	if err != nil {
		return errorResult(ctx, ref, "could not read the gittuf policy", err)
	}
	if !hasPolicy {
		res.State, res.Reason = Unprotected, "the repository has no applied gittuf policy"
		return res
	}

	first, _, err := rsl.GetFirstReferenceUpdaterEntryForRef(st, ref)
	if errors.Is(err, rsl.ErrRSLEntryNotFound) {
		res.State, res.Reason = Unprotected, "the RSL has no entries for the ref, so nothing protects it. The repository's RSL coverage may exclude it"
		return res
	}
	if err != nil {
		return errorResult(ctx, ref, "could not read the ref's RSL entries", err)
	}
	res.FromEntry, res.FromNumber = first.GetID(), first.GetNumber()

	targets, err := reader.Targets(ctx, ref)
	if err != nil {
		return errorResult(ctx, ref, "could not read the ref's RSL entries", err)
	}

	res.Rules, err = coveringRules(ctx, opts.RepoPath, ref)
	if err != nil {
		return errorResult(ctx, ref, "could not read the gittuf policy rules", err)
	}
	if len(res.Rules) == 0 {
		res.State, res.Reason = Unprotected, "no gittuf policy rule covers the ref"
		return res
	}

	opts.Logger.DebugContext(ctx, "running gittuf verification",
		slog.String("ref", ref), slog.String("from_entry", first.GetID().String()))

	tip, err := policy.VerifyRefFromEntry(ctx, st, ref, first.GetID())
	if err != nil {
		if isOperational(ctx, err) {
			return errorResult(ctx, ref, "verification could not complete", err)
		}
		res.State, res.Reason, res.Err = Failed, "gittuf verification failed", err
	} else {
		res.State, res.ExpectedTip = Verified, tip
		res.Reason = "gittuf verified every RSL entry for the ref against the policy, and every check passed"
		// gittuf stops at the first unrevoked violation, so the range is
		// only known to have been checked when it passed.
		res.Checked = checkedRange(targets)
	}

	res.Checks, err = runChecks(ctx, st, reader, ref, res.State == Verified, opts)
	if err != nil {
		return errorResult(ctx, ref, "verification checks could not complete", err)
	}
	if failed, ok := failedCheck(res.Checks); ok && res.State == Verified {
		res.State, res.ExpectedTip = Failed, nil
		res.Reason = "gittuf verified the ref, but the " + failed.Name + " check failed: " + failed.Detail
	}
	if res.State == Verified && slices.ContainsFunc(res.Checks, func(c Check) bool { return c.Status == CheckBehind }) {
		res.Reason += ". The local ref is behind the RSL tip, at a state the verified history covers"
	}
	return res
}

// hasPolicy reports whether the RSL records an applied policy, as gittuf's
// own HasPolicy does (experimental/gittuf/policy.go).
func hasPolicy(st gitstore.Storer) (bool, error) {
	_, _, err := rsl.GetLatestReferenceUpdaterEntry(st, rsl.ForReference(policy.Ref), rsl.IsUnskipped())
	if errors.Is(err, rsl.ErrRSLEntryNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find the applied policy: %w", err)
	}
	return true, nil
}

// matcher is the part of gittuf's rule interfaces coverage needs.
type matcher interface {
	Matches(path string) bool
}

// coveringRules lists the rules gittuf would apply to ref: top-level rules
// whose pattern matches it, gittuf's catch-all allow rule excluded, and
// global rules that match it. A rule deeper in the delegation graph is only
// reached through a matching top-level rule, so top-level matches decide
// whether any rule applies (internal/policy/policy.go,
// findVerifiersForPathIfProtected).
func coveringRules(ctx context.Context, repoPath, ref string) ([]string, error) {
	// gittuf starts git without a context here, so the work runs aside and
	// the caller stops waiting when the context is done. An abandoned git
	// process only reads, and ends with the command.
	type result struct {
		names []string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		names, err := listCoveringRules(ctx, repoPath, ref)
		done <- result{names, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("list policy rules: %w", ctx.Err())
	case r := <-done:
		return r.names, r.err
	}
}

func listCoveringRules(ctx context.Context, repoPath, ref string) ([]string, error) {
	repo, err := gittuf.LoadRepository(repoPath)
	if err != nil {
		return nil, fmt.Errorf("load repository for policy rules: %w", err)
	}

	target := gitRuleScheme + ref
	var names []string

	rules, err := repo.ListRules(ctx, policy.Ref)
	if err != nil {
		return nil, fmt.Errorf("list policy rules: %w", err)
	}
	for _, r := range rules {
		if r.Depth == 0 && r.Delegation.Matches(target) {
			names = append(names, r.Delegation.ID())
		}
	}

	globals, err := repo.ListGlobalRules(ctx, policy.Ref)
	if err != nil {
		return nil, fmt.Errorf("list global policy rules: %w", err)
	}
	for _, g := range globals {
		if m, ok := g.(matcher); ok && m.Matches(target) {
			names = append(names, "global:"+g.GetName())
		}
	}

	return names, nil
}

// isOperational reports whether err stopped verification from completing,
// as opposed to gittuf finding a violation.
func isOperational(ctx context.Context, err error) bool {
	var objErr *store.ObjectError
	return ctx.Err() != nil ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, store.ErrReadOnly) ||
		errors.Is(err, store.ErrWalkLimit) ||
		errors.Is(err, gitstore.ErrReferenceNotFound) ||
		errors.As(err, &objErr)
}

func errorResult(ctx context.Context, ref, reason string, err error) Result {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		reason = "verification timed out"
	}
	return Result{Ref: ref, State: Error, Reason: reason, Err: err}
}
