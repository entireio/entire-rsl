package clusters

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// publicKeys are the raw ed25519 public keys from entiredb's cluster fixtures,
// in fixture order. The test proves each fingerprint above derives from its key.
var publicKeys = map[string]string{
	"aws-us-east-2":      "iIDIqjqkDe0WmESf/ZQeuR+Yw52F+p0HFY4brKLmV/E=",
	"aws-eu-central-1":   "dATcLAm5+K/hZRn74EAabIph+2rEfBsbvkXTySViXvM=",
	"aws-ap-southeast-2": "dx6yGPJDy2gTrFfksolVAqxkWHcNswtgNQpqF923YH4=",
	"aws-ap-south-1":     "jXb5MbYanD+WVtptFeIsSgbiSeTqi5fhnR0Ay7mdll4=",
	"aws-eu-west-2":      "TeBScj0/XmzqtigcD9MHK3UlAYcfRGtt33Bz9K2vo1I=",
	"aws-ap-southeast-1": "mAorVibTTeszrO1YyLa8HYhK/K8FlYLyotr9GgTu1nI=",
	"aws-sa-east-1":      "AkSh7cCXNwLAohMDis7al8eg0QtkXHH9cfOoCeKegn4=",
	"prod root":          "0YOabvC1LUqgoo/y9AEXIinCTW/Ge3hu+27th5Rxpas=",
	"royalcanin":         "4E0g/VFRhX8E58mqZFwPXGJ67tAGsOItMBr0nvSFJek=",
	"purina":             "yVgWqGo32BfwdYJBvgFZTKv89k5lQJkGfLUSSLEUhPY=",
	"eukanuba":           "KP5THuMh8wTdAUOF/PTC+pYcCehytlmNt40guTgVqMc=",
	"staging root":       "zmNlr5Y879WIiiYC14LMWYEQdQu9qAGttfip3jVGXTg=",
}

func TestFingerprintsDeriveFromFixtureKeys(t *testing.T) {
	t.Parallel()

	for _, k := range known {
		name := k.Cluster
		if k.Role == RoleFleetRoot {
			name = k.Environment + " root"
		}

		raw, err := base64.StdEncoding.DecodeString(publicKeys[name])
		require.NoError(t, err, name)
		pub, err := ssh.ParsePublicKey(sshWireKey(raw))
		require.NoError(t, err, name)

		assert.Equal(t, k.Fingerprint, ssh.FingerprintSHA256(pub), name)
	}
}

// sshWireKey wraps a raw ed25519 public key in the SSH wire format.
func sshWireKey(raw []byte) []byte {
	field := func(b []byte) []byte {
		n := len(b)
		return append([]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}, b...)
	}
	return append(field([]byte(ssh.KeyAlgoED25519)), field(raw)...)
}

func TestLookupAndName(t *testing.T) {
	t.Parallel()

	k, ok := Lookup("SHA256:FtO/MCrbqpsaeHSytjMYgM9xwISJ3EhjF9+FiGgHpwQ")
	require.True(t, ok)
	assert.Equal(t, "Entire aws-us-east-2", k.Name())

	k, ok = Lookup("SHA256:Xv4SxJlyHqchFQFW5riMaIzR/8fY76HaVS4WGxQ1Pic")
	require.True(t, ok)
	assert.Equal(t, "Entire staging eukanuba (aws-eu-west-1)", k.Name())

	k, ok = Lookup("SHA256:Ja0D6sYQVI7aSmdIucYfwpQeV57DlvY5EE006iNQaZc")
	require.True(t, ok)
	assert.Equal(t, "Entire prod root", k.Name())

	_, ok = Lookup("SHA256:unknown")
	assert.False(t, ok)
}
