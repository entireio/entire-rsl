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
)

// Key is a known Entire signing key.
type Key struct {
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
	if k.Role == RoleFleetRoot {
		return "Entire " + k.Environment + " root"
	}
	if k.Environment == prod {
		return "Entire " + k.Cluster
	}
	return "Entire " + k.Environment + " " + k.Cluster + " (" + k.Region + ")"
}

var known = []Key{
	{Fingerprint: "SHA256:FtO/MCrbqpsaeHSytjMYgM9xwISJ3EhjF9+FiGgHpwQ", Cluster: "aws-us-east-2", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{Fingerprint: "SHA256:LjKtcIV0yXKr3K12bhpMMKE8iJanDQgemgaC4+A53fc", Cluster: "aws-eu-central-1", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{Fingerprint: "SHA256:BvO0EOCw+W4PHsML0jcH4DN57MslwhohKIp61pwJjuw", Cluster: "aws-ap-southeast-2", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{Fingerprint: "SHA256:Mp+mUThsliFRfaN5k/yDj0wuCuAUJR+hg3pClmUL0NM", Cluster: "aws-ap-south-1", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{Fingerprint: "SHA256:aqzDRRL2FaTkQqhiQSVHbT7kBOl6xpiv6zfKzpkOPKQ", Cluster: "aws-eu-west-2", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{Fingerprint: "SHA256:83JIUjrI5cPfgM/500EkJ5JSdqTnfhiPj8CA3VWGi9Q", Cluster: "aws-ap-southeast-1", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{Fingerprint: "SHA256:pf/J036vywY55MQXHkqzbpJVC5WuaSOb0Wk81R/R590", Cluster: "aws-sa-east-1", Environment: prod, Role: RoleRSLSigner, Generation: 1},
	{Fingerprint: "SHA256:Ja0D6sYQVI7aSmdIucYfwpQeV57DlvY5EE006iNQaZc", Environment: prod, Role: RoleFleetRoot, Generation: 1},

	{Fingerprint: "SHA256:7Yrrdh7HdC4LlrMZbUqTiNFvk+A8l3R6gQAWTvoJ9w4", Cluster: "royalcanin", Region: "aws-us-west-2", Environment: staging, Role: RoleRSLSigner, Generation: 1},
	{Fingerprint: "SHA256:6YonDvEdWbz7X7ZbHLEOOUNHCVb0Ek5H7whO1IbmYGw", Cluster: "purina", Region: "aws-us-east-2", Environment: staging, Role: RoleRSLSigner, Generation: 1},
	{Fingerprint: "SHA256:Xv4SxJlyHqchFQFW5riMaIzR/8fY76HaVS4WGxQ1Pic", Cluster: "eukanuba", Region: "aws-eu-west-1", Environment: staging, Role: RoleRSLSigner, Generation: 2},
	{Fingerprint: "SHA256:Uj9e1T8scIZmHVv4vEFePqDzoVJ/OaR1/wQW1mdEtJ8", Environment: staging, Role: RoleFleetRoot, Generation: 1},
}

// Lookup returns the known key with fingerprint.
func Lookup(fingerprint string) (Key, bool) {
	for _, k := range known {
		if k.Fingerprint == fingerprint {
			return k, true
		}
	}
	return Key{}, false
}
