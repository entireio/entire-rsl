package cli

import (
	"time"

	"github.com/entireio/entire-rsl/internal/clusters"
	"github.com/entireio/entire-rsl/internal/entirefields"
	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/gittuf/gittuf/pkg/githash"
)

// signerJSON describes an entry's signature. KeyID is present only when the
// signature checks out against that key. Whether the key may sign the entry
// is reported by verification, never here.
type signerJSON struct {
	Method         string `json:"method"`
	SignatureValid bool   `json:"signature_valid"`
	KeyID          string `json:"key_id,omitempty"`
	Name           string `json:"name,omitempty"`
	Problem        string `json:"problem,omitempty"`
}

// personJSON is a committer identity as recorded. Nothing verifies it.
type personJSON struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type entryJSON struct {
	Number             uint64               `json:"number"`
	ID                 string               `json:"id"`
	Type               string               `json:"type"`
	Ref                string               `json:"ref,omitempty"`
	Target             string               `json:"target,omitempty"`
	Time               string               `json:"time"`
	Signer             signerJSON           `json:"signer"`
	Committer          personJSON           `json:"committer_unverified"`
	Skipped            bool                 `json:"skipped"`
	SkippedBy          []string             `json:"skipped_by,omitempty"`
	AnnotatedBy        []string             `json:"annotated_by,omitempty"`
	Skip               *bool                `json:"skip,omitempty"`
	Annotates          []string             `json:"annotates,omitempty"`
	Message            string               `json:"message,omitempty"`
	UpstreamRepository string               `json:"upstream_repository,omitempty"`
	UpstreamEntryID    string               `json:"upstream_entry_id,omitempty"`
	Entire             *entirefields.Fields `json:"entire,omitempty"`
	CustomFields       map[string]string    `json:"custom_fields,omitempty"`
}

func toEntryJSON(e rslread.Entry) entryJSON {
	j := entryJSON{
		Number:             e.Number,
		ID:                 e.ID.String(),
		Type:               e.Kind,
		Ref:                e.RefName,
		Time:               e.Time.UTC().Format(time.RFC3339),
		Signer:             toSignerJSON(e.Signature),
		Committer:          personJSON{Name: e.Committer.Name, Email: e.Committer.Email},
		Skipped:            e.Skipped(),
		SkippedBy:          hashStrings(e.SkippedBy),
		AnnotatedBy:        hashStrings(e.AnnotatedBy),
		Annotates:          hashStrings(e.Annotates),
		Message:            e.Message,
		UpstreamRepository: e.UpstreamRepository,
		CustomFields:       e.CustomFields,
	}
	if len(e.TargetID) != 0 {
		j.Target = e.TargetID.String()
	}
	if len(e.UpstreamEntryID) != 0 {
		j.UpstreamEntryID = e.UpstreamEntryID.String()
	}
	if e.Kind == rslread.KindAnnotation {
		skip := e.Skip
		j.Skip = &skip
	}
	if len(e.CustomFields) != 0 {
		f := entirefields.Parse(e.CustomFields)
		j.Entire = &f
	}
	return j
}

func toSignerJSON(s rslread.Signature) signerJSON {
	return signerJSON{
		Method:         s.Method,
		KeyID:          s.KeyID,
		Name:           signerName(s),
		Problem:        s.Problem,
		SignatureValid: s.Valid,
	}
}

// signerName labels a verified key with the Entire cluster it belongs to.
func signerName(s rslread.Signature) string {
	if !s.Valid {
		return ""
	}
	if k, ok := clusters.Lookup(s.KeyID); ok {
		return k.Name()
	}
	return ""
}

func hashStrings(hs []githash.Hash) []string {
	if len(hs) == 0 {
		return nil
	}
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.String())
	}
	return out
}
