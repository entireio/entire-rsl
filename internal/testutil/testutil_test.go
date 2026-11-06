package testutil

import (
	"os"
	"testing"

	"github.com/gittuf/gittuf/experimental/gittuf"
	verifyopts "github.com/gittuf/gittuf/experimental/gittuf/options/verify"
	"github.com/gittuf/gittuf/pkg/rsl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	cleanup := IsolateGitConfig()
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func TestFixtureVerifiesWithGittuf(t *testing.T) {
	t.Parallel()

	r := NewRepo(t)
	r.Bootstrap()

	commit := r.Commit("refs/heads/main", "first")
	entry := r.Record(r.Cluster, "refs/heads/main", rsl.CustomFields{"custom.entire.io/pusher": "01J9ZAAAAAAAAAAAAAAAAAAAAA (jane)"})

	tip, err := gittuf.NewStorerRepository(r.Git).VerifyRef(t.Context(), "refs/heads/main", verifyopts.WithLatestOnly())
	require.NoError(t, err)
	assert.Equal(t, commit, tip)

	got, err := rsl.GetEntry(r.Git, entry)
	require.NoError(t, err)
	refEntry, ok := got.(*rsl.ReferenceEntry)
	require.True(t, ok)
	value, ok := refEntry.GetCustomField("custom.entire.io/pusher")
	require.True(t, ok)
	assert.Equal(t, "01J9ZAAAAAAAAAAAAAAAAAAAAA (jane)", value)
}

func TestFixtureRogueSignatureFailsGittuf(t *testing.T) {
	t.Parallel()

	r := NewRepo(t)
	r.Bootstrap()
	r.Commit("refs/heads/main", "first")
	r.Record(r.Rogue, "refs/heads/main", nil)

	_, err := gittuf.NewStorerRepository(r.Git).VerifyRef(t.Context(), "refs/heads/main", verifyopts.WithLatestOnly())
	require.Error(t, err)
}

func TestFixtureIsDeterministic(t *testing.T) {
	t.Parallel()

	build := func() string {
		r := NewRepo(t)
		r.Bootstrap()
		r.Commit("refs/heads/main", "first")
		return r.Record(r.Cluster, "refs/heads/main", nil).String()
	}

	first, second := build(), build()

	assert.Equal(t, first, second)
}

func TestKeyFingerprintIsStable(t *testing.T) {
	t.Parallel()

	a := NewKey(t, "cluster")
	b := NewKey(t, "cluster")

	assert.Equal(t, a.Fingerprint(t), b.Fingerprint(t))
	assert.NotEqual(t, a.Fingerprint(t), NewKey(t, "rogue").Fingerprint(t))
}

func TestScenarioVerificationOutcomes(t *testing.T) {
	t.Parallel()

	s := NewScenario(t)

	for _, ref := range []string{"refs/heads/main", "refs/heads/feature", "refs/heads/fixed", CheckpointRef(CheckpointULID)} {
		_, err := gittuf.NewStorerRepository(s.Git).VerifyRef(t.Context(), ref, verifyopts.WithLatestOnly())
		require.NoError(t, err, ref)
	}

	_, err := gittuf.NewStorerRepository(s.Git).VerifyRef(t.Context(), "refs/heads/rogue", verifyopts.WithLatestOnly())
	assert.Error(t, err, "rogue")
}

func TestAppendedEntriesVerifyWithGittuf(t *testing.T) {
	t.Parallel()

	r := NewRepo(t)
	r.Bootstrap()
	target := r.Commit("refs/heads/main", "only")
	first := r.Record(r.Cluster, "refs/heads/main", nil)
	r.AppendEntries(t, r.Cluster, "refs/heads/main", target, 25)

	tip, err := gittuf.NewStorerRepository(r.Git).VerifyRef(t.Context(), "refs/heads/main", verifyopts.WithLatestOnly())
	require.NoError(t, err)
	assert.Equal(t, target, tip)

	_, err = gittuf.NewStorerRepository(r.Git).VerifyRefFromEntry(t.Context(), "refs/heads/main", first)
	require.NoError(t, err)

	latest, err := rsl.GetLatestEntry(r.Git)
	require.NoError(t, err)
	assert.Equal(t, uint64(28), latest.GetNumber())
}
