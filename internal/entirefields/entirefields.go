// Package entirefields interprets the RSL custom fields Entire's server
// stamps on the entries it records.
//
// The fields are covered by the entry's signature, so once gittuf verifies an
// entry they are what the signing cluster asserted. Nothing else vouches for
// them: they never influence a verification state or an exit code. The field
// set follows entirehq/entiredb rslstore/writer.go and rslstore/operation.go
// at e64a4f389a.
package entirefields

import (
	"net/url"
	"regexp"
	"strings"
)

// Field keys.
const (
	KeyRepository             = "custom.entire.io/repository"
	KeyPusher                 = "custom.entire.io/pusher"
	KeyActor                  = "custom.entire.io/actor"
	KeyForcePush              = "custom.entire.io/force-push"
	KeyOperation              = "custom.entire.io/operation"
	KeySymrefTarget           = "custom.entire.io/symref-target"
	KeySymrefPreviousTarget   = "custom.entire.io/symref-previous-target"
	forcePushTrue             = "true"
	operationDefaultBranch    = "default-branch-change"
	operationDefaultBaseline  = "default-branch-baseline"
	actorMirrorService        = "entire-mirror-service"
	actorTrustService         = "entire-trust-service"
	actorAutomationService    = "entire-automation-service"
	actorServiceAccount       = "entire-service-account"
	actorInternalService      = "entire-internal-service"
	defaultBranchChangeMeans  = "default branch changed"
	defaultBranchBaselineMean = "baseline for the new default branch (not a push)"
)

var actorMeanings = map[string]string{
	actorMirrorService:     "Entire mirror service (repository mirroring)",
	actorTrustService:      "Entire trust service (policy publication)",
	actorAutomationService: "Entire automation",
	actorServiceAccount:    "an Entire service account",
	actorInternalService:   "an internal Entire service",
}

// pusherPattern matches "<account ULID> (<handle>)" as the server writes it.
var pusherPattern = regexp.MustCompile(`^([0-7][0-9A-HJKMNP-TV-Z]{25}) \((.+)\)$`)

// Pusher is the end user Entire observed pushing.
type Pusher struct {
	AccountID string `json:"account_id,omitempty"`
	Handle    string `json:"handle,omitempty"`
	// Raw is the field value as recorded.
	Raw string `json:"raw"`
}

// Fields is the Entire interpretation of an entry's custom fields.
type Fields struct {
	Pusher     *Pusher `json:"pusher,omitempty"`
	Actor      string  `json:"actor,omitempty"`
	ActorLabel string  `json:"actor_label,omitempty"`
	Repository string  `json:"repository,omitempty"`
	ForcePush  bool    `json:"force_push,omitempty"`
	Operation  string  `json:"operation,omitempty"`
	// OperationLabel explains Operation in words.
	OperationLabel       string `json:"operation_label,omitempty"`
	SymrefTarget         string `json:"symref_target,omitempty"`
	SymrefPreviousTarget string `json:"symref_previous_target,omitempty"`
}

// Parse interprets fields. Unknown keys are ignored here and still shown raw
// by callers.
func Parse(fields map[string]string) Fields {
	var f Fields

	if v, ok := fields[KeyPusher]; ok {
		p := &Pusher{Raw: v}
		if m := pusherPattern.FindStringSubmatch(v); m != nil {
			p.AccountID, p.Handle = m[1], m[2]
		}
		f.Pusher = p
	}

	if v, ok := fields[KeyActor]; ok {
		f.Actor = v
		f.ActorLabel = actorMeanings[v]
	}

	f.Repository = fields[KeyRepository]
	f.ForcePush = fields[KeyForcePush] == forcePushTrue

	if v, ok := fields[KeyOperation]; ok {
		f.Operation = v
		switch v {
		case operationDefaultBranch:
			f.OperationLabel = defaultBranchChangeMeans
		case operationDefaultBaseline:
			f.OperationLabel = defaultBranchBaselineMean
		}
	}

	f.SymrefTarget = decodeRef(fields[KeySymrefTarget])
	f.SymrefPreviousTarget = decodeRef(fields[KeySymrefPreviousTarget])

	return f
}

// Who describes who made the change in one phrase: the pusher's handle, an
// actor's meaning, or empty when neither field is present.
func (f Fields) Who() string {
	switch {
	case f.Pusher != nil && f.Pusher.Handle != "":
		return f.Pusher.Handle
	case f.Pusher != nil:
		return f.Pusher.Raw
	case f.ActorLabel != "":
		return f.ActorLabel
	default:
		return f.Actor
	}
}

// decodeRef reverses the server's %XX encoding of ref names. A value that
// does not decode is returned as recorded.
func decodeRef(v string) string {
	if v == "" || !strings.Contains(v, "%") {
		return v
	}
	decoded, err := url.PathUnescape(v)
	if err != nil {
		return v
	}
	return decoded
}
