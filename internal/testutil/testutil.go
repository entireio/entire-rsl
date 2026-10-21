// Package testutil builds repositories with a gittuf root of trust, policy and
// signed RSL entries for tests. Fixtures are written through gittuf's own git
// binary backed store, never through entire-rsl's read-only store, and are
// deterministic: keys derive from fixed seeds and commits use gittuf's fixed
// test clock, so entry IDs are stable across runs.
package testutil

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/pem"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/entireio/entire-rsl/internal/clusters"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/gitinterface"
	"github.com/gittuf/gittuf/pkg/policy"
	"github.com/gittuf/gittuf/pkg/rsl"
	"golang.org/x/crypto/ssh"
)

// IsolateGitConfig points every git subprocess of the test binary away from
// the developer's global and system configuration, so settings such as
// commit.gpgsign cannot leak into fixtures. Call it from TestMain, before any
// test runs.
func IsolateGitConfig() {
	for key, value := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_COUNT":    "0",
		"GIT_TERMINAL_PROMPT": "0",
	} {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	for _, key := range []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE",
		"GIT_CEILING_DIRECTORIES", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG",
		"GIT_TEMPLATE_DIR", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_AUTHOR_DATE",
		"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_DATE",
	} {
		if err := os.Unsetenv(key); err != nil {
			panic(err)
		}
	}
}

// Key is a deterministic ed25519 key pair.
type Key struct {
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
	PEM     []byte
}

// NewKey derives a key pair from name, so the same name always yields the
// same key.
func NewKey(t testing.TB, name string) Key {
	t.Helper()

	seed := sha256.Sum256([]byte("entire-rsl fixture key " + name))
	priv := ed25519.NewKeyFromSeed(seed[:])
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal fixture key %q: %v", name, err)
	}

	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		t.Fatalf("fixture key %q: unexpected public key type", name)
	}

	return Key{Private: priv, Public: pub, PEM: pem.EncodeToMemory(block)}
}

// Fingerprint returns the key's SSH SHA256 fingerprint, which is also its
// gittuf principal ID.
func (k Key) Fingerprint(t testing.TB) string {
	t.Helper()

	pub, err := ssh.NewPublicKey(k.Public)
	if err != nil {
		t.Fatalf("fixture key: %v", err)
	}
	return ssh.FingerprintSHA256(pub)
}

// TrustedKeys describes the fixture root and cluster keys the way the
// built-in table describes Entire's, so verification checks accept them.
func TrustedKeys(t testing.TB) []clusters.Key {
	t.Helper()

	root, cluster := NewKey(t, "root"), NewKey(t, "cluster")
	return []clusters.Key{
		{PublicKey: base64.StdEncoding.EncodeToString(root.Public), Fingerprint: root.Fingerprint(t), Environment: "test", Role: clusters.RoleFleetRoot, Generation: 1},
		{PublicKey: base64.StdEncoding.EncodeToString(cluster.Public), Fingerprint: cluster.Fingerprint(t), Cluster: "test-cluster", Region: "local", Environment: "test", Role: clusters.RoleRSLSigner, Generation: 1},
	}
}

// Repo is a fixture repository.
type Repo struct {
	t testing.TB

	// Dir is the working tree root.
	Dir string
	// Git writes to the repository through gittuf's git binary backed store.
	Git *gitinterface.Repository

	Root    Key
	Cluster Key
	Rogue   Key
}

// DefaultRefPatterns are the patterns Entire's server authorizes its RSL
// signer for.
var DefaultRefPatterns = []string{"git:refs/heads/*", "git:refs/tags/*", "git:refs/entire/checkpoints/*"}

// NewRepo creates an empty repository with no gittuf policy.
func NewRepo(t *testing.T) *Repo {
	t.Helper()

	dir := t.TempDir()
	return &Repo{
		t:       t,
		Dir:     dir,
		Git:     gitinterface.CreateTestGitRepository(t, dir, false),
		Root:    NewKey(t, "root"),
		Cluster: NewKey(t, "cluster"),
		Rogue:   NewKey(t, "rogue"),
	}
}

// Bootstrap applies a root of trust signed by Root that authorizes Cluster to
// sign RSL entries for patterns, the way Entire's server roots a repository.
func (r *Repo) Bootstrap(patterns ...string) {
	r.t.Helper()

	if len(patterns) == 0 {
		patterns = DefaultRefPatterns
	}

	_, err := policy.Bootstrap(context.Background(), r.Git, policy.BootstrapParams{
		RootSigners:         []crypto.Signer{r.Root.Private},
		RootPublicKeys:      []crypto.PublicKey{r.Root.Public},
		RootThreshold:       1,
		RSLSignerPublicKeys: []crypto.PublicKey{r.Cluster.Public},
		RefPatterns:         patterns,
		Expires:             time.Date(2100, time.January, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		r.t.Fatalf("bootstrap gittuf policy: %v", err)
	}
}

// Clone clones r into a new directory with git, under remote name. git clone
// fetches branches and tags only, so the clone has no gittuf refs.
func (r *Repo) Clone(t *testing.T, remote string) string {
	t.Helper()

	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "clone", "--quiet", "--origin", remote, "--", r.Dir, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clone %s: %v: %s", r.Dir, err, out)
	}
	return dir
}

// AddRemote adds a remote to the repository at dir.
func AddRemote(t *testing.T, dir, name, url string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", "-C", dir, "remote", "add", "--", name, url)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add remote %s: %v: %s", name, err, out)
	}
}

// Commit writes a commit holding one file and points ref at it. parent is
// the ref's current tip when it exists.
func (r *Repo) Commit(ref, message string) githash.Hash {
	r.t.Helper()

	blob, err := r.Git.WriteBlob([]byte(message + "\n"))
	if err != nil {
		r.t.Fatalf("write blob: %v", err)
	}
	tree, err := r.Git.WriteTree([]gitinterface.TreeEntry{gitinterface.NewEntryBlob("file.txt", blob)})
	if err != nil {
		r.t.Fatalf("write tree: %v", err)
	}
	id, err := r.Git.Commit(tree, ref, message, false)
	if err != nil {
		r.t.Fatalf("commit to %s: %v", ref, err)
	}
	return id
}

// SetRef points ref at id without recording anything in the RSL.
func (r *Repo) SetRef(ref string, id githash.Hash) {
	r.t.Helper()

	if err := r.Git.SetReference(ref, id); err != nil {
		r.t.Fatalf("set %s: %v", ref, err)
	}
}

// Record appends an RSL reference entry for ref at its current tip, signed
// by key, and returns the entry ID.
func (r *Repo) Record(key Key, ref string, fields rsl.CustomFields) githash.Hash {
	r.t.Helper()

	tip, err := r.Git.GetReference(ref)
	if err != nil {
		r.t.Fatalf("read %s: %v", ref, err)
	}
	return r.RecordAt(key, ref, tip, fields)
}

// RecordAt appends an RSL reference entry for ref at target, signed by key.
func (r *Repo) RecordAt(key Key, ref string, target githash.Hash, fields rsl.CustomFields) githash.Hash {
	r.t.Helper()

	var opts []rsl.EntryOption
	if len(fields) != 0 {
		opts = append(opts, rsl.WithCustomFields(fields))
	}
	if err := rsl.NewReferenceEntry(ref, target, opts...).CommitUsingSpecificKey(r.Git, key.PEM); err != nil {
		r.t.Fatalf("record RSL entry for %s: %v", ref, err)
	}
	return r.rslTip()
}

// Propagate appends an RSL propagation entry for ref at target, recording
// upstream as the repository it came from, signed by key.
func (r *Repo) Propagate(key Key, ref string, target githash.Hash, upstream string, upstreamEntry githash.Hash) githash.Hash {
	r.t.Helper()

	if err := rsl.NewPropagationEntry(ref, target, upstream, upstreamEntry).CommitUsingSpecificKey(r.Git, key.PEM); err != nil {
		r.t.Fatalf("record RSL propagation entry for %s: %v", ref, err)
	}
	return r.rslTip()
}

// Annotate appends an RSL annotation entry over ids, signed by key.
func (r *Repo) Annotate(key Key, skip bool, message string, ids ...githash.Hash) githash.Hash {
	r.t.Helper()

	if err := rsl.NewAnnotationEntry(ids, skip, message).CommitUsingSpecificKey(r.Git, key.PEM); err != nil {
		r.t.Fatalf("record RSL annotation: %v", err)
	}
	return r.rslTip()
}

func (r *Repo) rslTip() githash.Hash {
	r.t.Helper()

	tip, err := r.Git.GetReference(rsl.Ref)
	if err != nil {
		r.t.Fatalf("read RSL tip: %v", err)
	}
	return tip
}
