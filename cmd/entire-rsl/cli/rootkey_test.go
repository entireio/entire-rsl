package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entireio/entire-rsl/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestRootKeyFlag(t *testing.T) {
	t.Parallel()

	r := testutil.NewRepo(t)
	r.Root = testutil.NewKey(t, "self-managed root")
	r.Bootstrap()
	r.Commit("refs/heads/main", "only")
	r.Record(r.Cluster, "refs/heads/main", nil)

	unknown := execute(t, inRepo(r.Dir), "verify", "--json", "refs/heads/main")
	assert.Equal(t, ExitVerification, unknown.code, unknown.stderr)
	assert.Contains(t, unknown.stdout, "not signed by a known root key")

	pub, err := ssh.NewPublicKey(r.Root.Public)
	require.NoError(t, err)
	keyFile := filepath.Join(t.TempDir(), "root.pub")
	require.NoError(t, os.WriteFile(keyFile, ssh.MarshalAuthorizedKey(pub), 0o600))

	for _, value := range []string{"@" + keyFile, string(ssh.MarshalAuthorizedKey(pub))} {
		res := execute(t, inRepo(r.Dir), "verify", "--json", "--root-key", value, "refs/heads/main")
		require.Equal(t, ExitOK, res.code, res.stdout+res.stderr)

		var doc verifyJSON
		require.NoError(t, json.Unmarshal([]byte(res.stdout), &doc))
		statuses := map[string]string{}
		for _, c := range doc.Verification.Checks {
			statuses[c.Name] = c.Status
		}
		assert.Equal(t, map[string]string{"tip": "pass", "root_of_trust": "pass", "signer": "skip"}, statuses)
	}
}

func TestRootKeyFlagRejectsBadKeys(t *testing.T) {
	t.Parallel()

	sc := testutil.NewScenario(t)

	for name, value := range map[string]string{
		"garbage":      "not a key",
		"missing file": "@/nonexistent/key.pub",
		"rsa":          "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAgQDDHr/jh2Jy4yALcK4JyWbVkPRaWmhck3IgCoeOO3z1e2dBowLh64QAM+Qb72pxekALga2oi4GvT+TlWNhzPH4V4ffbsXbnKrDrW3kJy7wfPZhVdAjQABq9j5wq0gu8zP+5gqNWQUhIgaRm7cGT3Cd3gJDAOBwM6Ojj0iBNkzT5QQ==",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			res := execute(t, inRepo(sc.Dir), "verify", "--root-key", value, "main")

			assert.Equal(t, ExitUsage, res.code, res.stderr)
		})
	}
}
