package store

import (
	"fmt"
	"testing"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/assert"
)

func hash(i int) plumbing.Hash {
	h, _ := plumbing.FromHex(fmt.Sprintf("%040x", i+1))
	return h
}

func TestCacheHoldsItsByteBudget(t *testing.T) {
	t.Parallel()

	c := newCache(100, 3*1024)
	for i := range 10 {
		c.addCommit(hash(i), &object.Commit{}, 1024)
		assert.LessOrEqual(t, c.bytes, int64(3*1024))
	}

	_, oldest := c.commit(hash(0))
	_, newest := c.commit(hash(9))
	assert.False(t, oldest, "the oldest commits are evicted")
	assert.True(t, newest)
	assert.Len(t, c.commits, 3)
}

func TestCacheHoldsItsEntryLimit(t *testing.T) {
	t.Parallel()

	c := newCache(2, 1<<20)
	for i := range 5 {
		c.addCommit(hash(i), &object.Commit{}, 10)
	}

	assert.Len(t, c.commits, 2)
	assert.Len(t, c.order, 2)
}

func TestCacheSkipsLargeCommits(t *testing.T) {
	t.Parallel()

	c := newCache(100, 1<<30)
	c.addCommit(hash(1), &object.Commit{}, maxCachedCommit+1)

	_, ok := c.commit(hash(1))
	assert.False(t, ok)
	assert.Zero(t, c.bytes)
}
