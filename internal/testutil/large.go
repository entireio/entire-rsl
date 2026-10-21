package testutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"testing"
	"time"

	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/rsl"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"
)

// AppendEntries appends n RSL reference entries for ref at target, signed by
// key, writing objects directly with go-git. It is for benchmarks: writing
// through gittuf's git binary backed store costs several processes per entry.
// The entries are what gittuf writes: the same message format and an SSH
// signature in namespace git over the commit.
func (r *Repo) AppendEntries(t testing.TB, key Key, ref string, target githash.Hash, n int) {
	t.Helper()

	repo, err := gogit.PlainOpen(r.Dir)
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key.Private)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	tip, err := r.Git.GetReference(rsl.Ref)
	if err != nil {
		t.Fatalf("read RSL tip: %v", err)
	}
	parent := plumbing.NewHash(tip.String())
	number := r.entryNumber(tip)

	emptyTree := &object.Tree{}
	treeObj := repo.Storer.NewEncodedObject()
	if err := emptyTree.Encode(treeObj); err != nil {
		t.Fatalf("encode empty tree: %v", err)
	}
	treeHash, err := repo.Storer.SetEncodedObject(treeObj)
	if err != nil {
		t.Fatalf("write empty tree: %v", err)
	}

	when := time.Date(1995, time.October, 26, 9, 0, 0, 0, time.UTC)
	for range n {
		number++
		commit := &object.Commit{
			Author:       object.Signature{Name: committerName, Email: committerEmail, When: when},
			Committer:    object.Signature{Name: committerName, Email: committerEmail, When: when},
			Message:      fmt.Sprintf("%s\n\n%s: %s\n%s: %s\n%s: %d\n", rsl.ReferenceEntryHeader, rsl.RefKey, ref, rsl.TargetIDKey, target.String(), rsl.NumberKey, number),
			TreeHash:     treeHash,
			ParentHashes: []plumbing.Hash{parent},
		}

		payload := repo.Storer.NewEncodedObject()
		if err := commit.EncodeWithoutSignature(payload); err != nil {
			t.Fatalf("encode commit: %v", err)
		}
		reader, err := payload.Reader()
		if err != nil {
			t.Fatalf("read commit: %v", err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read commit: %v", err)
		}
		sig, err := sshsig.Sign(bytes.NewReader(data), signer, sshsig.HashSHA512, "git")
		if err != nil {
			t.Fatalf("sign commit: %v", err)
		}
		commit.Signature = string(sshsig.Armor(sig))

		obj := repo.Storer.NewEncodedObject()
		if err := commit.Encode(obj); err != nil {
			t.Fatalf("encode signed commit: %v", err)
		}
		parent, err = repo.Storer.SetEncodedObject(obj)
		if err != nil {
			t.Fatalf("write commit: %v", err)
		}
	}

	if err := repo.Storer.SetReference(plumbing.NewHashReference(plumbing.ReferenceName(rsl.Ref), parent)); err != nil {
		t.Fatalf("set RSL tip: %v", err)
	}

	// Real repositories keep history in packfiles, not loose objects.
	if out, err := exec.CommandContext(context.Background(), "git", "-C", r.Dir, "repack", "-adq").CombinedOutput(); err != nil {
		t.Fatalf("repack: %v: %s", err, out)
	}
}

func (r *Repo) entryNumber(id githash.Hash) uint64 {
	r.t.Helper()

	entry, err := rsl.GetEntry(r.Git, id)
	if err != nil {
		r.t.Fatalf("read RSL entry: %v", err)
	}
	return entry.GetNumber()
}
