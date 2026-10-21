package refname

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	t.Parallel()

	valid := []string{"main", "refs/heads/main", "refs/heads/feature/x-1", "refs/tags/v1.0.0", "HEAD", "refs/entire/checkpoints/7Z/01JC0FFEE0BADC0DE00000007Z", "a@b", "café"}
	for _, name := range valid {
		require.NoError(t, Validate(name), name)
	}

	invalid := []string{
		"", "-main", "--upload-pack=x", "@", "/main", "main/", "main.", "a//b", "a..b", "a@{1}",
		"a b", "a~1", "a^", "a:b", "a?", "a*", "a[b", "a\\b", "a\x00b", "a\x1bb", "a\x7fb",
		".hidden", "refs/.hidden", "refs/heads/x.lock", strings.Repeat("a", maxLen+1),
	}
	for _, name := range invalid {
		err := Validate(name)
		require.ErrorIs(t, err, ErrInvalid, name)
	}
}

func TestIsFull(t *testing.T) {
	t.Parallel()

	assert.True(t, IsFull("refs/heads/main"))
	assert.True(t, IsFull("HEAD"))
	assert.False(t, IsFull("main"))
	assert.False(t, IsFull("heads/main"))
}

func FuzzValidate(f *testing.F) {
	for _, seed := range []string{"main", "refs/heads/main", "-x", "a..b", "a\x1bb", "refs/heads/x.lock", "@{"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, name string) {
		if Validate(name) != nil {
			return
		}
		assert.NotEmpty(t, name)
		assert.False(t, strings.HasPrefix(name, "-"))
		assert.NotContains(t, name, "..")
		for _, r := range name {
			assert.False(t, r < 0x20 || r == 0x7f, "control character accepted")
		}
	})
}
