// Package clusters names the Entire clusters whose keys sign RSL entries and
// policy, so a verified signer can be shown with the cluster it belongs to.
//
// The table is best effort and only labels a key. It never decides trust:
// whether a key may sign an entry is gittuf policy's decision alone. A key
// missing from the table, for example after a rotation, is shown by its
// fingerprint.
//
// Source: entirehq/entiredb core/cluster/fixtures/clusters.prod.yaml and
// clusters.staging.yaml at e64a4f389a (2026-10-21). The same fingerprints are
// pinned per cluster as ENTIRE_RSL_EXPECTED_SIGNER_KEY_ID in entirehq/infra.
package clusters

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"slices"

	"golang.org/x/crypto/ssh"
)

// Role is what a key signs.
type Role string

const (
	// RoleRSLSigner keys sign the RSL entries a cluster records on push.
	RoleRSLSigner Role = "rsl-signer"
	// RoleFleetRoot keys sign the root of trust of every repository in an
	// environment.
	RoleFleetRoot Role = "fleet-root"
)

// Environments.
const (
	prod    = "prod"
	staging = "staging"
	// EnvironmentUser marks keys the user supplied, not Entire's.
	EnvironmentUser = "user"
)

// Key is a known signing key.
type Key struct {
	// PublicKey is the raw ed25519 public key, base64 encoded.
	PublicKey string
	// Fingerprint is the SSH SHA256 fingerprint, which is also the gittuf
	// principal ID.
	Fingerprint string
	// Cluster is the cluster slug, empty for a fleet root.
	Cluster string
	// Region is the cloud region a staging cluster runs in. Production
	// cluster slugs are their regions.
	Region      string
	Environment string
	Role        Role
	Generation  int
}

// Name is a short human label for the key.
func (k Key) Name() string {
	if k.Environment == EnvironmentUser {
		return "user-supplied " + string(k.Role) + " " + k.Fingerprint
	}
	if k.Role == RoleFleetRoot {
		return "Entire " + k.Environment + " root"
	}
	if k.Environment == prod {
		return "Entire " + k.Cluster
	}
	return "Entire " + k.Environment + " " + k.Cluster + " (" + k.Region + ")"
}

var known = []Key{
	{PublicKey: "iIDIqjqkDe0WmESf/ZQeuR+Yw52F+p0HFY4brKLmV/E=", Fingerprint: "SHA256:FtO/MCrbqpsaeHSytjMYgM9xwISJ3EhjF9+FiGgHpwQ", Cluster: "aws-us-east-2", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{PublicKey: "dATcLAm5+K/hZRn74EAabIph+2rEfBsbvkXTySViXvM=", Fingerprint: "SHA256:LjKtcIV0yXKr3K12bhpMMKE8iJanDQgemgaC4+A53fc", Cluster: "aws-eu-central-1", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{PublicKey: "dx6yGPJDy2gTrFfksolVAqxkWHcNswtgNQpqF923YH4=", Fingerprint: "SHA256:BvO0EOCw+W4PHsML0jcH4DN57MslwhohKIp61pwJjuw", Cluster: "aws-ap-southeast-2", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{PublicKey: "jXb5MbYanD+WVtptFeIsSgbiSeTqi5fhnR0Ay7mdll4=", Fingerprint: "SHA256:Mp+mUThsliFRfaN5k/yDj0wuCuAUJR+hg3pClmUL0NM", Cluster: "aws-ap-south-1", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{PublicKey: "TeBScj0/XmzqtigcD9MHK3UlAYcfRGtt33Bz9K2vo1I=", Fingerprint: "SHA256:aqzDRRL2FaTkQqhiQSVHbT7kBOl6xpiv6zfKzpkOPKQ", Cluster: "aws-eu-west-2", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{PublicKey: "mAorVibTTeszrO1YyLa8HYhK/K8FlYLyotr9GgTu1nI=", Fingerprint: "SHA256:83JIUjrI5cPfgM/500EkJ5JSdqTnfhiPj8CA3VWGi9Q", Cluster: "aws-ap-southeast-1", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{PublicKey: "AkSh7cCXNwLAohMDis7al8eg0QtkXHH9cfOoCeKegn4=", Fingerprint: "SHA256:pf/J036vywY55MQXHkqzbpJVC5WuaSOb0Wk81R/R590", Cluster: "aws-sa-east-1", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{PublicKey: "0YOabvC1LUqgoo/y9AEXIinCTW/Ge3hu+27th5Rxpas=", Fingerprint: "SHA256:Ja0D6sYQVI7aSmdIucYfwpQeV57DlvY5EE006iNQaZc", Environment: prod, Role: RoleFleetRoot, Generation: 1},

	{PublicKey: "4E0g/VFRhX8E58mqZFwPXGJ67tAGsOItMBr0nvSFJek=", Fingerprint: "SHA256:7Yrrdh7HdC4LlrMZbUqTiNFvk+A8l3R6gQAWTvoJ9w4", Cluster: "royalcanin", Region: "aws-us-west-2", Environment: staging, Role: RoleRSLSigner, Generation: 1},
	{PublicKey: "yVgWqGo32BfwdYJBvgFZTKv89k5lQJkGfLUSSLEUhPY=", Fingerprint: "SHA256:6YonDvEdWbz7X7ZbHLEOOUNHCVb0Ek5H7whO1IbmYGw", Cluster: "purina", Region: "aws-us-east-2", Environment: staging, Role: RoleRSLSigner, Generation: 1},
	{PublicKey: "KP5THuMh8wTdAUOF/PTC+pYcCehytlmNt40guTgVqMc=", Fingerprint: "SHA256:Xv4SxJlyHqchFQFW5riMaIzR/8fY76HaVS4WGxQ1Pic", Cluster: "eukanuba", Region: "aws-eu-west-1", Environment: staging, Role: RoleRSLSigner, Generation: 2},
	{PublicKey: "zmNlr5Y879WIiiYC14LMWYEQdQu9qAGttfip3jVGXTg=", Fingerprint: "SHA256:Uj9e1T8scIZmHVv4vEFePqDzoVJ/OaR1/wQW1mdEtJ8", Environment: staging, Role: RoleFleetRoot, Generation: 1},
}

// Known returns a copy of the built-in table.
func Known() []Key {
	return slices.Clone(known)
}

// Lookup returns the built-in key with fingerprint.
func Lookup(fingerprint string) (Key, bool) {
	return Find(known, fingerprint)
}

// Find returns the key in keys with fingerprint.
func Find(keys []Key, fingerprint string) (Key, bool) {
	idx := slices.IndexFunc(keys, func(k Key) bool { return k.Fingerprint == fingerprint })
	if idx < 0 {
		return Key{}, false
	}
	return keys[idx], true
}

// CryptoPublicKey decodes PublicKey.
func (k Key) CryptoPublicKey() (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(k.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("decode public key of %s: %w", k.Fingerprint, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("public key of %s is %d bytes, want %d", k.Fingerprint, len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// FromPublicKey describes a key that is not in the built-in table, such as a
// root of trust supplied on the command line.
func FromPublicKey(pub ed25519.PublicKey, role Role, environment string) (Key, error) {
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return Key{}, fmt.Errorf("encode public key: %w", err)
	}
	return Key{
		PublicKey:   base64.StdEncoding.EncodeToString(pub),
		Fingerprint: ssh.FingerprintSHA256(sshPub),
		Environment: environment,
		Role:        role,
	}, nil
}
