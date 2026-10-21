package store

import (
	"sync"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

// commitCacheSize bounds the decoded commits kept per store. An RSL walk
// reads each entry commit several times in a row (parents, message,
// signature), so a small window removes most repeated decoding.
const commitCacheSize = 4096

// cache holds what the store reads repeatedly. Copies made by WithContext
// share it. Decoded commits are treated as immutable.
type cache struct {
	commits  map[plumbing.Hash]*object.Commit
	shallow  []plumbing.Hash
	order    []plumbing.Hash
	err      error
	size     int
	next     int
	mu       sync.Mutex
	loadOnce sync.Once
}

func newCache(size int) *cache {
	return &cache{commits: make(map[plumbing.Hash]*object.Commit, size), order: make([]plumbing.Hash, size), size: size}
}

func (c *cache) commit(h plumbing.Hash) (*object.Commit, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	commit, ok := c.commits[h]
	return commit, ok
}

// addCommit keeps commit, evicting the oldest entry once the cache is full.
func (c *cache) addCommit(h plumbing.Hash, commit *object.Commit) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.commits[h]; ok {
		return
	}
	if old := c.order[c.next]; !old.IsZero() {
		delete(c.commits, old)
	}
	c.order[c.next] = h
	c.next = (c.next + 1) % c.size
	c.commits[h] = commit
}

// shallowOnce loads the shallow boundaries on first use. They do not change
// while a command runs: a fetch adds refs and objects, not boundaries.
func (c *cache) shallowOnce(load func() ([]plumbing.Hash, error)) ([]plumbing.Hash, error) {
	c.loadOnce.Do(func() {
		c.shallow, c.err = load()
	})
	return c.shallow, c.err
}
