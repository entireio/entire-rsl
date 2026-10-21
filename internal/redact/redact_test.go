package redact

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURLs(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https://example.com/repo":                       "https://example.com/repo",
		"https://user:token@example.com/repo":            "https://redacted@example.com/repo",
		"https://token@example.com/repo":                 "https://redacted@example.com/repo",
		"ssh://git@github.com/org/repo":                  "ssh://redacted@github.com/org/repo",
		"fatal: unable to access 'https://u:p@h/r': 403": "fatal: unable to access 'https://redacted@h/r': 403",
		"two https://a:b@x/1 and http://c@y/2":           "two https://redacted@x/1 and http://redacted@y/2",
		"git@github.com:org/repo.git":                    "git@github.com:org/repo.git",
		"mail jane@example.com":                          "mail jane@example.com",
		"https://example.com/path@with-at":               "https://example.com/path@with-at",
	}

	for in, want := range tests {
		assert.Equal(t, want, URLs(in), in)
	}
}

func TestValues(t *testing.T) {
	t.Parallel()

	assert.Nil(t, Values(nil))
	assert.Equal(t, map[string]string{"k": "https://redacted@h/r", "o": "plain"}, Values(map[string]string{"k": "https://u:p@h/r", "o": "plain"}))
}
