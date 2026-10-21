package entirefields

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fields  map[string]string
		want    Fields
		wantWho string
	}{
		{
			name:    "human push",
			fields:  map[string]string{KeyPusher: "01J9ZACCT000000000000000JD (jane)", KeyRepository: "01J9ZREPQ0000000000000000R"},
			want:    Fields{Pusher: &Pusher{AccountID: "01J9ZACCT000000000000000JD", Handle: "jane", Raw: "01J9ZACCT000000000000000JD (jane)"}, Repository: "01J9ZREPQ0000000000000000R"},
			wantWho: "jane",
		},
		{
			name:    "bare account id",
			fields:  map[string]string{KeyPusher: "01J9ZACCT000000000000000JD"},
			want:    Fields{Pusher: &Pusher{Raw: "01J9ZACCT000000000000000JD"}},
			wantWho: "01J9ZACCT000000000000000JD",
		},
		{
			name:    "mirror service",
			fields:  map[string]string{KeyActor: "entire-mirror-service"},
			want:    Fields{Actor: "entire-mirror-service", ActorLabel: "Entire mirror service (repository mirroring)"},
			wantWho: "Entire mirror service (repository mirroring)",
		},
		{
			name:    "unknown actor",
			fields:  map[string]string{KeyActor: "entire-future-service"},
			want:    Fields{Actor: "entire-future-service"},
			wantWho: "entire-future-service",
		},
		{
			name:   "force push",
			fields: map[string]string{KeyForcePush: "true"},
			want:   Fields{ForcePush: true},
		},
		{
			name:   "force push other value",
			fields: map[string]string{KeyForcePush: "yes"},
			want:   Fields{},
		},
		{
			name: "default branch change",
			fields: map[string]string{
				KeyOperation:            "default-branch-change",
				KeySymrefTarget:         "refs/heads/release%2F1.0",
				KeySymrefPreviousTarget: "refs/heads/main",
			},
			want: Fields{
				Operation:            "default-branch-change",
				OperationLabel:       "default branch changed",
				SymrefTarget:         "refs/heads/release/1.0",
				SymrefPreviousTarget: "refs/heads/main",
			},
		},
		{
			name:   "undecodable symref kept",
			fields: map[string]string{KeySymrefTarget: "refs/heads/bad%zz"},
			want:   Fields{SymrefTarget: "refs/heads/bad%zz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Parse(tt.fields)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantWho, got.Who())
		})
	}
}
