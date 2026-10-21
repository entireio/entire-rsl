package verify

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/entireio/entire-rsl/internal/clusters"
	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/entireio/entire-rsl/internal/store"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/policy"
)

// CheckStatus is the outcome of one check.
type CheckStatus string

// Check outcomes. A failed check turns verified into failed. A skipped check
// did not apply.
const (
	CheckPass CheckStatus = "pass"
	CheckFail CheckStatus = "fail"
	CheckSkip CheckStatus = "skip"
)

// Check names.
const (
	CheckTip    = "tip"
	CheckRoot   = "root_of_trust"
	CheckSigner = "signer"
)

// Check is one verification check entire-rsl runs on top of gittuf's.
type Check struct {
	Name   string
	Status CheckStatus
	Detail string
}

// runChecks runs the tip, root of trust and signer checks.
func runChecks(ctx context.Context, st *store.Store, reader *rslread.Reader, ref string, res Result, opts Options) []Check {
	root, rootKey := checkRoot(ctx, st, opts.Keys)
	return []Check{
		checkTip(ctx, reader, ref, res, opts.LocalTip),
		root,
		checkSigner(ctx, reader, ref, rootKey, opts.Keys),
	}
}

// checkTip compares the tip the RSL records, which the server signed, with
// the local ref.
func checkTip(ctx context.Context, reader *rslread.Reader, ref string, res Result, localTip func(string) (githash.Hash, error)) Check {
	c := Check{Name: CheckTip}

	recorded := res.ExpectedTip
	if len(recorded) == 0 {
		h, err := reader.History(ctx, ref, 1)
		if err != nil || len(h.Entries) == 0 {
			c.Status, c.Detail = CheckFail, "could not read the tip the RSL records"
			return c
		}
		recorded = h.Entries[0].TargetID
	}

	if localTip == nil {
		c.Status, c.Detail = CheckSkip, "the local ref was not read"
		return c
	}
	local, err := localTip(ref)
	switch {
	case err != nil:
		c.Status, c.Detail = CheckFail, "could not read the local ref: "+err.Error()
	case len(local) == 0:
		c.Status, c.Detail = CheckSkip, "the ref does not exist locally, so there is nothing to compare"
	case !local.Equal(recorded):
		c.Status, c.Detail = CheckFail, fmt.Sprintf("the RSL records %s but the local ref is at %s", recorded.String(), local.String())
	default:
		c.Status, c.Detail = CheckPass, "the local ref matches the tip the RSL records ("+recorded.String()+")"
	}
	return c
}

// checkRoot requires the policy's history to start from a known root key and
// its current root keys to all be known. It returns the anchoring key.
func checkRoot(ctx context.Context, st *store.Store, keys []clusters.Key) (Check, *clusters.Key) {
	c := Check{Name: CheckRoot}

	info, err := policy.InspectRoot(ctx, st)
	if err != nil {
		c.Status, c.Detail = CheckFail, "could not inspect the root of trust: "+err.Error()
		return c, nil
	}

	roots := byRole(keys, clusters.RoleFleetRoot)
	var anchor *clusters.Key
	for i := range roots {
		pub, err := roots[i].CryptoPublicKey()
		if err != nil {
			continue
		}
		// gittuf requires every initial root key it is given to have signed
		// the first root, so each known root is tried on its own.
		if _, err := policy.InspectRoot(ctx, st, policy.WithInitialRootKeys(pub)); err == nil {
			anchor = &roots[i]
			break
		}
	}
	if anchor == nil {
		c.Status = CheckFail
		c.Detail = "the first root of trust is not signed by a known root key (current root keys: " + strings.Join(info.RootKeyIDs, ", ") + ")"
		return c, nil
	}

	var unknown []string
	for _, id := range info.RootKeyIDs {
		if _, ok := clusters.Find(roots, id); !ok {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		c.Status = CheckFail
		c.Detail = fmt.Sprintf("the root of trust starts from %s but now trusts unknown root keys: %s", anchor.Name(), strings.Join(unknown, ", "))
		return c, nil
	}

	c.Status = CheckPass
	c.Detail = fmt.Sprintf("anchored to %s (%s), root version %d", anchor.Name(), anchor.Fingerprint, info.Version)
	return c, anchor
}

// checkSigner requires the ref's latest entry, in a repository rooted by
// Entire, to be signed by a known cluster key of the same environment.
func checkSigner(ctx context.Context, reader *rslread.Reader, ref string, root *clusters.Key, keys []clusters.Key) Check {
	c := Check{Name: CheckSigner}

	if root == nil {
		c.Status, c.Detail = CheckSkip, "the root of trust is not known, so no signer is expected"
		return c
	}
	if root.Environment == clusters.EnvironmentUser {
		c.Status, c.Detail = CheckSkip, "the root of trust is not Entire's"
		return c
	}

	h, err := reader.History(ctx, ref, 1)
	if err != nil || len(h.Entries) == 0 {
		c.Status, c.Detail = CheckFail, "could not read the ref's latest RSL entry"
		return c
	}
	sig := h.Entries[0].Signature
	if !sig.Valid {
		c.Status, c.Detail = CheckFail, "the latest RSL entry has no valid signature ("+sig.Problem+")"
		return c
	}

	k, ok := clusters.Find(byRole(keys, clusters.RoleRSLSigner), sig.KeyID)
	switch {
	case !ok:
		c.Status, c.Detail = CheckFail, "the latest RSL entry is signed by "+sig.KeyID+", which is not a known Entire cluster key"
	case k.Environment != root.Environment:
		c.Status = CheckFail
		c.Detail = fmt.Sprintf("the latest RSL entry is signed by %s, a %s key, but the repository is rooted in %s", k.Name(), k.Environment, root.Environment)
	default:
		c.Status, c.Detail = CheckPass, fmt.Sprintf("signed by %s (%s)", k.Name(), k.Fingerprint)
	}
	return c
}

func byRole(keys []clusters.Key, role clusters.Role) []clusters.Key {
	var out []clusters.Key
	for _, k := range keys {
		if k.Role == role {
			out = append(out, k)
		}
	}
	return out
}

// failedCheck returns the first failed check.
func failedCheck(checks []Check) (Check, bool) {
	idx := slices.IndexFunc(checks, func(c Check) bool { return c.Status == CheckFail })
	if idx < 0 {
		return Check{}, false
	}
	return checks[idx], true
}
