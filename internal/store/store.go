// Package store implements gittuf's gitstore.Storer over go-git, read-only.
//
// Every write method returns ErrReadOnly, so no gittuf code path reached
// through this store can modify the repository. The read methods mirror the
// semantics of gittuf's git binary backed gitinterface.Repository, and the
// object reads follow gittuf's own go-git backend (internal/gogitstore in the
// pjbgf/gittuf fork, Apache-2.0). Tests compare both implementations on the
// same repositories.
package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/gitinterface"
	"github.com/gittuf/gittuf/pkg/gitstore"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/filemode"
	"github.com/go-git/go-git/v6/plumbing/format/config"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
)

var (
	// ErrReadOnly is returned by every method that would write.
	ErrReadOnly = errors.New("entire-rsl is read-only and refuses to write to the repository")

	// ErrInvalidHash is returned when a hash is not a valid Git object ID.
	ErrInvalidHash = errors.New("hash is not a valid Git object ID")

	// ErrObjectTooLarge is returned when an object exceeds its size cap.
	ErrObjectTooLarge = errors.New("object exceeds the size entire-rsl reads into memory")

	// ErrNotFullReference is returned by GetReference for anything other than
	// HEAD or a full refs/ name. gitinterface accepts revision expressions
	// there, but gittuf only passes full names.
	ErrNotFullReference = errors.New("not a full reference name")
)

// Size caps for objects read into memory. RSL entries, policy commits and tag
// objects are small, and policy metadata blobs are kilobytes, so the caps sit
// far above legitimate sizes while bounding what a hostile repository can
// make the process allocate.
const (
	maxCommitSize = 1 << 20
	maxTagSize    = 1 << 20
	maxTreeSize   = 16 << 20
	maxBlobSize   = 16 << 20
)

// Git resolves the empty tree even when it is absent from the object database.
const (
	emptyTreeSHA1   = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	emptyTreeSHA256 = "6ef19b41225c5369f1c104d45d8d85efa9b057b53b14b4b9b939dd74decc5321"
)

var _ gitstore.Storer = (*Store)(nil)

// ObjectError reports a failure to read an object or reference from the
// repository, as opposed to a policy decision made over its contents.
type ObjectError struct {
	Err error
	Op  string
	ID  string
}

func (e *ObjectError) Error() string {
	if e.ID == "" {
		return fmt.Sprintf("%s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("%s %s: %v", e.Op, e.ID, e.Err)
}

func (e *ObjectError) Unwrap() error { return e.Err }

// opReadShallow names reads of the shallow boundary list in errors.
const opReadShallow = "read shallow boundaries"

// maxWalk caps the commits one history walk visits.
const maxWalk = 10_000_000

// ErrWalkLimit is returned when a history walk exceeds maxWalk commits.
var ErrWalkLimit = errors.New("history walk exceeded its limit")

// Store is a read-only gitstore.Storer over a go-git repository.
type Store struct {
	// ctx bounds the walks the Storer methods perform. gitstore.Storer methods
	// take no context, so the store carries the invocation's.
	ctx    context.Context //nolint:containedctx // gitstore.Storer methods take no context
	repo   *gogit.Repository
	limits limits
	sha256 bool
}

// limits are the per-type object size caps.
type limits struct {
	commit, tag, tree, blob int64
}

var defaultLimits = limits{commit: maxCommitSize, tag: maxTagSize, tree: maxTreeSize, blob: maxBlobSize}

// New wraps repo.
func New(repo *gogit.Repository) (*Store, error) {
	cfg, err := repo.Config()
	if err != nil {
		return nil, &ObjectError{Op: "read repository config", Err: err}
	}

	return &Store{ctx: context.Background(), repo: repo, limits: defaultLimits, sha256: cfg.Extensions.ObjectFormat == "sha256"}, nil
}

// WithContext returns a copy of the store whose walks stop when ctx is done.
func (s *Store) WithContext(ctx context.Context) *Store {
	c := *s
	c.ctx = ctx
	return &c
}

// Repository returns the underlying go-git repository for reads the Storer
// interface does not cover.
func (s *Store) Repository() *gogit.Repository { return s.repo }

// ZeroHash returns the all-zeroes hash of the repository's object format.
func (s *Store) ZeroHash() githash.Hash {
	if s.sha256 {
		return githash.ZeroHashSHA256
	}
	return githash.ZeroHash
}

// EmptyTree returns the empty tree ID of the repository's object format.
func (s *Store) EmptyTree() (githash.Hash, error) {
	id := emptyTreeSHA1
	if s.sha256 {
		id = emptyTreeSHA256
	}
	h, err := githash.NewHash(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidHash, err)
	}
	return h, nil
}

// GetReference returns the tip of a full reference name.
func (s *Store) GetReference(refName string) (githash.Hash, error) {
	if refName != "HEAD" && (!strings.HasPrefix(refName, "refs/") || plumbing.ReferenceName(refName).Validate() != nil) {
		return s.ZeroHash(), fmt.Errorf("%w: %q", ErrNotFullReference, refName)
	}

	ref, err := s.repo.Reference(plumbing.ReferenceName(refName), true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return s.ZeroHash(), gitstore.ErrReferenceNotFound
		}
		return s.ZeroHash(), &ObjectError{Op: "read reference", ID: refName, Err: err}
	}

	return fromGoGit(ref.Hash())
}

// ReadBlob returns the contents of a blob.
func (s *Store) ReadBlob(blobID githash.Hash) ([]byte, error) {
	obj, err := s.encoded(blobID, plumbing.BlobObject, s.limits.blob)
	if err != nil {
		return nil, err
	}

	blob, err := object.DecodeBlob(obj)
	if err != nil {
		return nil, &ObjectError{Op: "decode blob", ID: blobID.String(), Err: err}
	}

	reader, err := blob.Reader()
	if err != nil {
		return nil, &ObjectError{Op: "read blob", ID: blobID.String(), Err: err}
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, &ObjectError{Op: "read blob", ID: blobID.String(), Err: err}
	}
	return data, nil
}

// GetCommitMessage returns a commit's message, trimmed as gitinterface does.
func (s *Store) GetCommitMessage(commitID githash.Hash) (string, error) {
	commit, err := s.commit(commitID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(commit.Message), nil
}

// GetCommitTreeID returns a commit's tree.
func (s *Store) GetCommitTreeID(commitID githash.Hash) (githash.Hash, error) {
	commit, err := s.commit(commitID)
	if err != nil {
		return s.ZeroHash(), err
	}
	return fromGoGit(commit.TreeHash)
}

// GetCommitParentIDs returns a commit's parents, nil for a root commit or a
// shallow boundary, as gitinterface does.
func (s *Store) GetCommitParentIDs(commitID githash.Hash) ([]githash.Hash, error) {
	commit, err := s.commit(commitID)
	if err != nil {
		return nil, err
	}

	shallow, err := s.repo.Storer.Shallow()
	if err != nil {
		return nil, &ObjectError{Op: opReadShallow, Err: err}
	}
	if slices.Contains(shallow, commit.Hash) || len(commit.ParentHashes) == 0 {
		return nil, nil
	}

	parents := make([]githash.Hash, 0, len(commit.ParentHashes))
	for _, p := range commit.ParentHashes {
		id, err := fromGoGit(p)
		if err != nil {
			return nil, err
		}
		parents = append(parents, id)
	}
	return parents, nil
}

// GetEntriesInTree returns a tree's immediate entries, nil for an empty tree.
func (s *Store) GetEntriesInTree(treeID githash.Hash) ([]gitstore.TreeEntry, error) {
	tree, err := s.tree(treeID)
	if err != nil {
		return nil, err
	}
	if len(tree.Entries) == 0 {
		return nil, nil
	}

	entries := make([]gitstore.TreeEntry, 0, len(tree.Entries))
	for _, e := range tree.Entries {
		id, err := fromGoGit(e.Hash)
		if err != nil {
			return nil, err
		}

		// ls-tree reports only trees as "tree". Everything else, including
		// submodule gitlinks, counts as a blob.
		kind := gitstore.KindBlob
		if e.Mode == filemode.Dir {
			kind = gitstore.KindSubtree
		}
		entries = append(entries, gitstore.TreeEntry{Path: e.Name, ID: id, Kind: kind})
	}
	return entries, nil
}

// GetAllFilesInTree returns the recursively flattened path to blob mapping of
// a tree, nil for an empty tree.
func (s *Store) GetAllFilesInTree(treeID githash.Hash) (map[string]githash.Hash, error) {
	h, err := toGoGit(treeID)
	if err != nil {
		return nil, err
	}

	leaves := map[string]leaf{}
	if err := s.collectLeaves(h, "", leaves); err != nil {
		return nil, err
	}
	if len(leaves) == 0 {
		return nil, nil //nolint:nilnil // gitstore.Storer contract: an empty tree has no files, as gitinterface returns
	}

	files := make(map[string]githash.Hash, len(leaves))
	for path, l := range leaves {
		id, err := fromGoGit(l.hash)
		if err != nil {
			return nil, err
		}
		files[path] = id
	}
	return files, nil
}

// leaf is a non-tree entry of a flattened tree.
type leaf struct {
	hash plumbing.Hash
	mode filemode.FileMode
}

// collectLeaves records every non-tree entry under treeID. Submodule gitlinks
// are leaves, matching ls-tree -r. Every subtree is read through treeObject,
// so each one is size-checked.
func (s *Store) collectLeaves(treeID plumbing.Hash, prefix string, leaves map[string]leaf) error {
	if err := s.ctx.Err(); err != nil {
		return fmt.Errorf("read tree: %w", err)
	}

	tree, err := s.treeObject(treeID)
	if err != nil {
		return err
	}

	for _, e := range tree.Entries {
		path := e.Name
		if prefix != "" {
			path = prefix + "/" + e.Name
		}

		if e.Mode == filemode.Dir {
			if err := s.collectLeaves(e.Hash, path, leaves); err != nil {
				return err
			}
			continue
		}
		leaves[path] = leaf{hash: e.Hash, mode: e.Mode}
	}
	return nil
}

// GetPathIDInTree resolves a slash separated path within a tree. An
// intermediate path returns that subtree's ID.
func (s *Store) GetPathIDInTree(treeID githash.Hash, treePath string) (githash.Hash, error) {
	current := treeID
	for component := range strings.SplitSeq(strings.TrimSuffix(treePath, "/"), "/") {
		entries, err := s.GetEntriesInTree(current)
		if err != nil {
			return nil, err
		}

		idx := slices.IndexFunc(entries, func(e gitstore.TreeEntry) bool { return e.Path == component })
		if idx < 0 {
			return nil, fmt.Errorf("%w: %s", gitinterface.ErrTreeDoesNotHavePath, treePath)
		}
		current = entries[idx].ID
	}
	return current, nil
}

// GetTagTarget peels chained tags down to a commit, as gitinterface's
// rev-list -n 1 does.
func (s *Store) GetTagTarget(tagID githash.Hash) (githash.Hash, error) {
	id, err := toGoGit(tagID)
	if err != nil {
		return s.ZeroHash(), err
	}

	// A tag chain longer than this is not something git produces.
	for range 16 {
		tag, err := s.tagObject(id)
		if err != nil {
			break
		}
		id = tag.Target
	}

	commit, err := s.commitObject(id)
	if err != nil {
		return s.ZeroHash(), err
	}
	return fromGoGit(commit.Hash)
}

// GetObjectSignature returns the signed payload and detached signature of a
// commit or tag. The signature is empty for an unsigned object.
func (s *Store) GetObjectSignature(objectID githash.Hash) ([]byte, []byte, error) {
	id, err := toGoGit(objectID)
	if err != nil {
		return nil, nil, err
	}

	obj, err := s.repo.Storer.EncodedObject(plumbing.AnyObject, id)
	if err != nil {
		return nil, nil, &ObjectError{Op: "read object", ID: objectID.String(), Err: err}
	}

	if obj.Type() == plumbing.CommitObject {
		commit, err := s.commitObject(id)
		if err != nil {
			return nil, nil, err
		}
		payload, err := encodeWithoutSignature(commit)
		if err != nil {
			return nil, nil, &ObjectError{Op: "encode commit", ID: objectID.String(), Err: err}
		}

		// SHA-256 commits sign under gpgsig-sha256, which go-git keeps apart
		// from the SHA-1 gpgsig header.
		signature := commit.Signature
		if objectID.IsSHA256() {
			signature = commit.SignatureSHA256
		}
		return payload, []byte(signature), nil
	}

	if obj.Type() == plumbing.TagObject {
		tag, err := s.tagObject(id)
		if err != nil {
			return nil, nil, err
		}
		payload, err := encodeWithoutSignature(tag)
		if err != nil {
			return nil, nil, &ObjectError{Op: "encode tag", ID: objectID.String(), Err: err}
		}
		// Tag signatures are appended to the payload in both object formats.
		return payload, []byte(tag.Signature), nil
	}

	return nil, nil, gitinterface.ErrNotCommitOrTag
}

// LookupConfig reads a key from the repository's own config. ok is false when
// the key is unset. Global and system config are not consulted.
func (s *Store) LookupConfig(key gitstore.ConfigKey) (string, bool, error) {
	cfg, err := s.repo.Config()
	if err != nil {
		return "", false, &ObjectError{Op: "read repository config", Err: err}
	}

	parts := strings.Split(string(key), ".")
	if len(parts) < 2 {
		return "", false, nil
	}

	section := cfg.Raw.Section(parts[0])
	options := section.Options
	if len(parts) > 2 {
		sub := strings.Join(parts[1:len(parts)-1], ".")
		if !section.HasSubsection(sub) {
			return "", false, nil
		}
		options = section.Subsection(sub).Options
	}

	name := parts[len(parts)-1]
	if !options.Has(name) {
		return "", false, nil
	}
	return strings.TrimSpace(lastValue(options, name)), true, nil
}

// lastValue returns the last value set for name, matching git config --get.
func lastValue(options config.Options, name string) string {
	values := options.GetAll(name)
	return values[len(values)-1]
}

// GetCommitsBetweenRange returns the commits reachable from commitNewID but not
// from commitOldID, sorted by ID as gitinterface does. A zero commitOldID
// means no lower bound.
func (s *Store) GetCommitsBetweenRange(commitNewID, commitOldID githash.Hash) ([]githash.Hash, error) {
	excluded := map[plumbing.Hash]struct{}{}
	if !commitOldID.IsZero() {
		if err := s.walk(commitOldID, func(h plumbing.Hash) bool {
			excluded[h] = struct{}{}
			return true
		}); err != nil {
			return nil, err
		}
	}

	// Non-nil when empty, as gitinterface returns.
	ids := []githash.Hash{}
	err := s.walk(commitNewID, func(h plumbing.Hash) bool {
		if _, skip := excluded[h]; skip {
			return false
		}
		id, err := fromGoGit(h)
		if err == nil {
			ids = append(ids, id)
		}
		return true
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(ids, func(a, b githash.Hash) int { return strings.Compare(a.String(), b.String()) })
	return ids, nil
}

// walk visits from and its ancestors once each, not descending past a commit
// for which visit returns false, nor past shallow boundaries.
func (s *Store) walk(from githash.Hash, visit func(plumbing.Hash) bool) error {
	start, err := toGoGit(from)
	if err != nil {
		return err
	}

	shallow, err := s.repo.Storer.Shallow()
	if err != nil {
		return &ObjectError{Op: opReadShallow, Err: err}
	}

	seen := map[plumbing.Hash]struct{}{}
	queue := []plumbing.Hash{start}
	for len(queue) > 0 {
		h := queue[0]
		queue = queue[1:]
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}

		if err := s.ctx.Err(); err != nil {
			return fmt.Errorf("walk history: %w", err)
		}
		if len(seen) > maxWalk {
			return fmt.Errorf("%w: more than %d commits from %s", ErrWalkLimit, maxWalk, from.String())
		}

		if !visit(h) {
			continue
		}
		if slices.Contains(shallow, h) {
			continue
		}

		commit, err := s.commitObject(h)
		if err != nil {
			return err
		}
		queue = append(queue, commit.ParentHashes...)
	}
	return nil
}

// GetFilePathsChangedByCommit returns the paths a commit changes relative to
// its parents, following gitinterface: every path for a root commit, the diff
// against the single parent, and for a merge the union of the diffs against
// each parent, unless the merge's tree matches its last parent's.
func (s *Store) GetFilePathsChangedByCommit(commitID githash.Hash) ([]string, error) {
	commit, err := s.commit(commitID)
	if err != nil {
		return nil, err
	}

	parents, err := s.GetCommitParentIDs(commitID)
	if err != nil {
		return nil, err
	}

	if len(parents) == 0 {
		treeID, err := fromGoGit(commit.TreeHash)
		if err != nil {
			return nil, err
		}
		files, err := s.GetAllFilesInTree(treeID)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			return nil, nil
		}
		paths := make([]string, 0, len(files))
		for p := range files {
			paths = append(paths, p)
		}
		slices.Sort(paths)
		return paths, nil
	}

	if len(parents) == 1 {
		return s.changedPaths(parents[0], commit)
	}

	last, err := s.changedPaths(parents[len(parents)-1], commit)
	if err != nil || len(last) == 0 {
		return nil, err
	}

	set := map[string]struct{}{}
	for _, parent := range parents {
		paths, err := s.changedPaths(parent, commit)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			set[p] = struct{}{}
		}
	}

	paths := make([]string, 0, len(set))
	for p := range set {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return paths, nil
}

// changedPaths lists the paths whose mode or object differ between parent's
// tree and commit's, like diff-tree -r --name-only without rename detection.
// Both trees are flattened through collectLeaves rather than go-git's
// DiffTree, which would load subtrees without the size checks.
func (s *Store) changedPaths(parentID githash.Hash, commit *object.Commit) ([]string, error) {
	parent, err := s.commit(parentID)
	if err != nil {
		return nil, err
	}

	from := map[string]leaf{}
	if err := s.collectLeaves(parent.TreeHash, "", from); err != nil {
		return nil, err
	}
	to := map[string]leaf{}
	if err := s.collectLeaves(commit.TreeHash, "", to); err != nil {
		return nil, err
	}

	var paths []string
	for path, l := range from {
		if other, ok := to[path]; !ok || other != l {
			paths = append(paths, path)
		}
	}
	for path := range to {
		if _, ok := from[path]; !ok {
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}

	slices.Sort(paths)
	return paths, nil
}

// KnowsCommit reports whether ancestorID is commitID or one of its ancestors.
// Both must be commits, as in gitinterface.
func (s *Store) KnowsCommit(commitID, ancestorID githash.Hash) (bool, error) {
	if _, err := s.commit(commitID); err != nil {
		return false, err
	}
	ancestor, err := toGoGit(ancestorID)
	if err != nil {
		return false, err
	}
	if _, err := s.commitObject(ancestor); err != nil {
		return false, err
	}

	found := false
	err = s.walk(commitID, func(h plumbing.Hash) bool {
		if h == ancestor {
			found = true
		}
		return !found
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

// GetMergeTree would write a merge result to the object database.
func (s *Store) GetMergeTree(_, _ githash.Hash) (githash.Hash, error) {
	return nil, ErrReadOnly
}

// SetReference refuses to write.
func (s *Store) SetReference(_ string, _ githash.Hash) error { return ErrReadOnly }

// DeleteReference refuses to write.
func (s *Store) DeleteReference(_ string) error { return ErrReadOnly }

// CheckAndSetReference refuses to write.
func (s *Store) CheckAndSetReference(_ string, _, _ githash.Hash) error { return ErrReadOnly }

// CheckAndDeleteReference refuses to write.
func (s *Store) CheckAndDeleteReference(_ string, _ githash.Hash) error { return ErrReadOnly }

// WriteBlob refuses to write.
func (s *Store) WriteBlob(_ []byte) (githash.Hash, error) { return nil, ErrReadOnly }

// WriteTree refuses to write.
func (s *Store) WriteTree(_ []gitstore.TreeEntry) (githash.Hash, error) { return nil, ErrReadOnly }

// Commit refuses to write.
func (s *Store) Commit(_ githash.Hash, _, _ string, _ bool) (githash.Hash, error) {
	return nil, ErrReadOnly
}

// CommitUsingSpecificKey refuses to write.
func (s *Store) CommitUsingSpecificKey(_ githash.Hash, _, _ string, _ []byte) (githash.Hash, error) {
	return nil, ErrReadOnly
}

// ResetDueToError refuses to reset and returns cause alongside ErrReadOnly.
func (s *Store) ResetDueToError(cause error, _ string, _ githash.Hash) error {
	return errors.Join(cause, ErrReadOnly)
}

// encoded loads an object of type want, refusing it before decoding when it
// is larger than limit.
func (s *Store) encoded(id githash.Hash, want plumbing.ObjectType, limit int64) (plumbing.EncodedObject, error) {
	h, err := toGoGit(id)
	if err != nil {
		return nil, err
	}
	return s.encodedGoGit(h, want, limit)
}

func (s *Store) encodedGoGit(h plumbing.Hash, want plumbing.ObjectType, limit int64) (plumbing.EncodedObject, error) {
	op := "read " + want.String()

	// The size comes from the object header, so an oversized object is refused
	// before it is inflated.
	size, err := s.repo.Storer.EncodedObjectSize(h)
	if err != nil {
		return nil, &ObjectError{Op: op, ID: h.String(), Err: err}
	}
	if size > limit {
		return nil, &ObjectError{Op: op, ID: h.String(), Err: fmt.Errorf("%w: %d bytes, limit %d", ErrObjectTooLarge, size, limit)}
	}

	obj, err := s.repo.Storer.EncodedObject(want, h)
	if err != nil {
		return nil, &ObjectError{Op: op, ID: h.String(), Err: err}
	}
	return obj, nil
}

func (s *Store) commit(id githash.Hash) (*object.Commit, error) {
	h, err := toGoGit(id)
	if err != nil {
		return nil, err
	}
	return s.commitObject(h)
}

func (s *Store) commitObject(h plumbing.Hash) (*object.Commit, error) {
	obj, err := s.encodedGoGit(h, plumbing.CommitObject, s.limits.commit)
	if err != nil {
		return nil, err
	}
	commit, err := object.DecodeCommit(s.repo.Storer, obj)
	if err != nil {
		return nil, &ObjectError{Op: "decode commit", ID: h.String(), Err: err}
	}
	return commit, nil
}

func (s *Store) tagObject(h plumbing.Hash) (*object.Tag, error) {
	obj, err := s.encodedGoGit(h, plumbing.TagObject, s.limits.tag)
	if err != nil {
		return nil, err
	}
	tag, err := object.DecodeTag(s.repo.Storer, obj)
	if err != nil {
		return nil, &ObjectError{Op: "decode tag", ID: h.String(), Err: err}
	}
	return tag, nil
}

func (s *Store) tree(id githash.Hash) (*object.Tree, error) {
	h, err := toGoGit(id)
	if err != nil {
		return nil, err
	}
	return s.treeObject(h)
}

func (s *Store) treeObject(h plumbing.Hash) (*object.Tree, error) {
	if s.isEmptyTree(h.String()) {
		return &object.Tree{}, nil
	}

	obj, err := s.encodedGoGit(h, plumbing.TreeObject, s.limits.tree)
	if err != nil {
		return nil, err
	}
	tree, err := object.DecodeTree(s.repo.Storer, obj)
	if err != nil {
		return nil, &ObjectError{Op: "decode tree", ID: h.String(), Err: err}
	}
	return tree, nil
}

// isEmptyTree reports whether id is the empty tree of the repository's own
// object format. The other format's ID does not count, as in gitinterface.
func (s *Store) isEmptyTree(id string) bool {
	if s.sha256 {
		return id == emptyTreeSHA256
	}
	return id == emptyTreeSHA1
}

// signatureless is implemented by go-git commits and tags, which can re-encode
// themselves as the bytes their signature covers.
type signatureless interface {
	EncodeWithoutSignature(o plumbing.EncodedObject) error
}

func encodeWithoutSignature(obj signatureless) ([]byte, error) {
	encoded := memory.NewStorage().NewEncodedObject()
	if err := obj.EncodeWithoutSignature(encoded); err != nil {
		return nil, fmt.Errorf("encode without signature: %w", err)
	}

	reader, err := encoded.Reader()
	if err != nil {
		return nil, fmt.Errorf("read encoded object: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read encoded object: %w", err)
	}
	return data, nil
}

func toGoGit(h githash.Hash) (plumbing.Hash, error) {
	id, ok := plumbing.FromHex(h.String())
	if !ok {
		return plumbing.ZeroHash, fmt.Errorf("%w: %q", ErrInvalidHash, h.String())
	}
	return id, nil
}

func fromGoGit(id plumbing.Hash) (githash.Hash, error) {
	h, err := githash.NewHash(id.String())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidHash, err)
	}
	return h, nil
}
