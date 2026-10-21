package rslread

import (
	"bytes"
	"strings"

	"github.com/entireio/entire-rsl/internal/store"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/hiddeco/sshsig"
	"golang.org/x/crypto/ssh"
)

// Signature methods.
const (
	MethodSSH     = "ssh"
	MethodGPG     = "gpg"
	MethodX509    = "x509"
	MethodNone    = "none"
	MethodUnknown = "unknown"
)

// sshNamespace and the hash match gittuf's SSH verifier
// (internal/signerverifier/ssh/ssh.go): git signs with namespace "git", and
// gittuf verifies with SHA-512.
const sshNamespace = "git"

// Signature describes the signature on an RSL entry commit.
//
// KeyID is set only when Valid: the key embedded in an SSH signature is chosen
// by whoever made it, so it identifies the signer only once the signature
// checks out against it. Whether that key may sign the entry is gittuf
// policy's decision, reported by verification, not here.
type Signature struct {
	Method string
	KeyID  string
	// Problem says why the signature is not attributed to a key.
	Problem string
	Valid   bool
}

// signatureOf reads and checks the signature on commit id.
func signatureOf(st *store.Store, id githash.Hash) (Signature, error) {
	payload, sig, err := st.GetObjectSignature(id)
	if err != nil {
		return Signature{}, err
	}
	return checkSignature(payload, sig), nil
}

func checkSignature(payload, sig []byte) Signature {
	if len(bytes.TrimSpace(sig)) == 0 {
		return Signature{Method: MethodNone, Problem: "unsigned"}
	}

	// gitinterface returns multi-block signatures verbatim and requires
	// verifiers to reject them (pkg/gitinterface/signature.go).
	if bytes.Count(sig, []byte("-----BEGIN ")) > 1 {
		return Signature{Method: MethodUnknown, Problem: "more than one signature block"}
	}

	text := strings.TrimSpace(string(sig))
	switch {
	case strings.HasPrefix(text, "-----BEGIN SSH SIGNATURE-----"):
		return checkSSH(payload, sig)
	case strings.HasPrefix(text, "-----BEGIN PGP SIGNATURE-----"):
		return Signature{Method: MethodGPG, Problem: "GPG signatures are not attributed by entire-rsl"}
	case strings.HasPrefix(text, "-----BEGIN SIGNED MESSAGE-----"):
		return Signature{Method: MethodX509, Problem: "x509 signatures are not attributed by entire-rsl"}
	default:
		return Signature{Method: MethodUnknown, Problem: "unrecognised signature format"}
	}
}

func checkSSH(payload, armored []byte) Signature {
	sig, err := sshsig.Unarmor(armored)
	if err != nil {
		return Signature{Method: MethodSSH, Problem: "malformed SSH signature"}
	}
	if err := sshsig.Verify(bytes.NewReader(payload), sig, sig.PublicKey, sshsig.HashSHA512, sshNamespace); err != nil {
		return Signature{Method: MethodSSH, Problem: "SSH signature does not verify"}
	}
	return Signature{Method: MethodSSH, KeyID: ssh.FingerprintSHA256(sig.PublicKey), Valid: true}
}
