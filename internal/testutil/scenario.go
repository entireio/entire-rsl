package testutil

import (
	"testing"

	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/rsl"
)

// Fixture checkpoint IDs. CheckpointULID's shard is "7Z" and CheckpointHex's
// is "f7", the last two characters of each.
const (
	CheckpointULID = "01JC0FFEE0BADC0DE00000007Z"
	CheckpointHex  = "a3b2c4d5e6f7"
	RepositoryID   = "01J9ZREPQ0000000000000000R"
	PusherJane     = "01J9ZACCT000000000000000JD (jane)"
	PusherBob      = "01J9ZACCT000000000000000BB (bob)"
)

// Scenario is a repository shaped like one Entire's server records: a root
// of trust authorizing the cluster key, branches, a moved tag, checkpoint refs,
// a rogue entry and a revoked one.
type Scenario struct {
	*Repo

	MainFirst, MainSecond         githash.Hash // commits on main
	MainFirstEntry, MainLastEntry githash.Hash // RSL entries for main
	TrailerCommit                 githash.Hash // main tip, carries Entire-Checkpoint
	FeatureEntry                  githash.Hash // recorded by the mirror service
	TagFirstEntry, TagMovedEntry  githash.Hash // v1.0.0 recorded twice
	CheckpointEntry               githash.Hash // refs/entire/checkpoints/7Z/<ULID>
	RogueEntry                    githash.Hash // refs/heads/rogue, signed by Rogue
	RevokedEntry, RevokeAnnot     githash.Hash // refs/heads/fixed, skipped
	FixedEntry                    githash.Hash // refs/heads/fixed, re-recorded
}

// CheckpointRef returns the git-refs backend ref for a checkpoint ID.
func CheckpointRef(id string) string {
	return "refs/entire/checkpoints/" + id[len(id)-2:] + "/" + id
}

func pushedBy(pusher string, extra ...string) rsl.CustomFields {
	fields := rsl.CustomFields{
		"custom.entire.io/repository": RepositoryID,
		"custom.entire.io/pusher":     pusher,
	}
	for i := 0; i+1 < len(extra); i += 2 {
		fields[extra[i]] = extra[i+1]
	}
	return fields
}

// NewScenario builds the Scenario repository.
func NewScenario(t *testing.T) *Scenario {
	t.Helper()

	s := &Scenario{Repo: NewRepo(t)}
	s.Bootstrap()

	s.MainFirst = s.Commit("refs/heads/main", "first")
	s.MainFirstEntry = s.Record(s.Cluster, "refs/heads/main", pushedBy(PusherJane))
	s.MainSecond = s.Commit("refs/heads/main", "second")
	s.Record(s.Cluster, "refs/heads/main", pushedBy(PusherJane))

	s.Commit("refs/heads/feature", "feature work")
	s.FeatureEntry = s.Record(s.Cluster, "refs/heads/feature", rsl.CustomFields{
		"custom.entire.io/repository": RepositoryID,
		"custom.entire.io/actor":      "entire-mirror-service",
	})

	s.SetRef("refs/tags/v1.0.0", s.MainFirst)
	s.TagFirstEntry = s.Record(s.Cluster, "refs/tags/v1.0.0", pushedBy(PusherJane, "custom.entire.io/force-push", "true"))
	s.SetRef("refs/tags/v1.0.0", s.MainSecond)
	s.TagMovedEntry = s.Record(s.Cluster, "refs/tags/v1.0.0", pushedBy(PusherJane, "custom.entire.io/force-push", "true"))

	s.Commit(CheckpointRef(CheckpointULID), "checkpoint "+CheckpointULID)
	s.CheckpointEntry = s.Record(s.Cluster, CheckpointRef(CheckpointULID), pushedBy(PusherJane))

	s.TrailerCommit = s.Commit("refs/heads/main", "third\n\nEntire-Checkpoint: "+CheckpointULID)
	s.MainLastEntry = s.Record(s.Cluster, "refs/heads/main", pushedBy(PusherJane))

	s.Commit("refs/heads/rogue", "rogue work")
	s.RogueEntry = s.Record(s.Rogue, "refs/heads/rogue", pushedBy(PusherJane))

	s.Commit("refs/heads/fixed", "fixed work")
	s.RevokedEntry = s.Record(s.Rogue, "refs/heads/fixed", nil)
	s.RevokeAnnot = s.Annotate(s.Cluster, true, "revoked: signed by an unknown key", s.RevokedEntry)
	s.FixedEntry = s.Record(s.Cluster, "refs/heads/fixed", pushedBy(PusherBob))

	return s
}
