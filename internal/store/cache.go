package store

import (
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// Cache bounds. An RSL walk reads each entry commit several times in a row
// (parents, message, signature), so a small window removes most repeated
// decoding. The byte budget bounds what a hostile history of large commits
// can pin in memory: commits above maxCachedCommit are never kept, and the
// oldest entries are evicted until both limits hold.
const (
	commitCacheSize   = 4096
	commitCacheBudget = 64 << 20
	maxCachedCommit   = 64 << 10
)

type cachedCommit struct {
	commit *object.Commit
	hash   plumbing.Hash
	size   int64
}

// cache holds what the store reads repeatedly. Copies made by WithContext
// share it. Decoded commits are treated as immutable.
type cache struct {
	commits  map[plumbing.Hash]*object.Commit
	err      error
	shallow  []plumbing.Hash
	order    []cachedCommit
	bytes    int64
	entries  int
	budget   int64
	mu       sync.Mutex
	loadOnce sync.Once
}

func newCache(entries int, budget int64) *cache {
	return &cache{commits: make(map[plumbing.Hash]*object.Commit, entries), entries: entries, budget: budget}
}

func (c *cache) commit(h plumbing.Hash) (*object.Commit, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	commit, ok := c.commits[h]
	return commit, ok
}

// addCommit keeps commit, whose encoded size is size, evicting the oldest
// entries until the entry and byte limits hold. A commit larger than
// maxCachedCommit is not kept.
func (c *cache) addCommit(h plumbing.Hash, commit *object.Commit, size int64) {
	if size > maxCachedCommit {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.commits[h]; ok {
		return
	}
	for len(c.order) > 0 && (len(c.order) >= c.entries || c.bytes+size > c.budget) {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.commits, oldest.hash)
		c.bytes -= oldest.size
	}
	c.order = append(c.order, cachedCommit{hash: h, commit: commit, size: size})
	c.commits[h] = commit
	c.bytes += size
}

// shallowOnce loads the shallow boundaries on first use. They do not change
// while a command runs: a fetch adds refs and objects, not boundaries.
func (c *cache) shallowOnce(load func() ([]plumbing.Hash, error)) ([]plumbing.Hash, error) {
	c.loadOnce.Do(func() {
		c.shallow, c.err = load()
	})
	return c.shallow, c.err
}
