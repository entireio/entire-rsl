package refresolve

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsCheckpointID(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{"a3b2c4d5e6f7", "01JC0FFEE0BADC0DE00000007Z", "7ZZZZZZZZZZZZZZZZZZZZZZZZZ"} {
		assert.True(t, IsCheckpointID(ok), ok)
	}
	for _, bad := range []string{
		"", "a3b2c4d5e6f", "a3b2c4d5e6f78", "A3B2C4D5E6F7", "g3b2c4d5e6f7",
		"01jc0ffee0badc0de00000007z", "8ZZZZZZZZZZZZZZZZZZZZZZZZZ", "01JC0FFEE0BADC0DE0000000IL",
		"01JC0FFEE0BADC0DE00000007ZZ", "01JC0FFEE0BADC0DE0000007U",
	} {
		assert.False(t, IsCheckpointID(bad), bad)
	}
}

func TestCheckpointRefRoundTrip(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "refs/entire/checkpoints/7Z/01JC0FFEE0BADC0DE00000007Z", CheckpointRef("01JC0FFEE0BADC0DE00000007Z"))
	assert.Equal(t, "refs/entire/checkpoints/f7/a3b2c4d5e6f7", CheckpointRef("a3b2c4d5e6f7"))

	id, ok := CheckpointFromRef("refs/entire/checkpoints/7z/01JC0FFEE0BADC0DE00000007Z")
	assert.True(t, ok, "shard case may differ")
	assert.Equal(t, "01JC0FFEE0BADC0DE00000007Z", id)

	for _, bad := range []string{
		"refs/entire/checkpoints/aa/01JC0FFEE0BADC0DE00000007Z",
		"refs/entire/checkpoints/7Z/x/01JC0FFEE0BADC0DE00000007Z",
		"refs/entire/checkpoints/7Z/notanid",
		"refs/heads/main",
	} {
		_, ok := CheckpointFromRef(bad)
		assert.False(t, ok, bad)
	}
}

func TestCheckpointFromMessage(t *testing.T) {
	t.Parallel()

	got, err := CheckpointFromMessage("fix\n\nbody\n\nEntire-Checkpoint: 01JC0FFEE0BADC0DE00000007Z\nSigned-off-by: x\n")
	require.NoError(t, err)
	assert.Equal(t, "01JC0FFEE0BADC0DE00000007Z", got)

	got, err = CheckpointFromMessage("squash\n\nEntire-Checkpoint: a3b2c4d5e6f7\nEntire-Checkpoint: a3b2c4d5e6f7\n")
	require.NoError(t, err, "a repeated ID names one checkpoint")
	assert.Equal(t, "a3b2c4d5e6f7", got)

	_, err = CheckpointFromMessage("no trailer here")
	require.ErrorIs(t, err, ErrNoCheckpointTrailer)

	_, err = CheckpointFromMessage("quoted in body: Entire-Checkpoint: a3b2c4d5e6f7 was wrong")
	require.ErrorIs(t, err, ErrNoCheckpointTrailer)

	_, err = CheckpointFromMessage("squash\n\nEntire-Checkpoint: a3b2c4d5e6f7\nEntire-Checkpoint: 01JC0FFEE0BADC0DE00000007Z\n")
	require.ErrorIs(t, err, ErrManyCheckpointTrailers)
	require.ErrorContains(t, err, "a3b2c4d5e6f7, 01JC0FFEE0BADC0DE00000007Z")

	_, err = CheckpointFromMessage("x\n\nEntire-Checkpoint: NOTVALID\n")
	require.ErrorIs(t, err, ErrInvalidCheckpoint)
}

func FuzzCheckpointID(f *testing.F) {
	for _, seed := range []string{"a3b2c4d5e6f7", "01JC0FFEE0BADC0DE00000007Z", "", "../../x", "refs/heads/main"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if !IsCheckpointID(s) {
			return
		}
		ref := CheckpointRef(s)
		id, ok := CheckpointFromRef(ref)
		assert.True(t, ok)
		assert.Equal(t, s, id)
		assert.NotContains(t, s, "/")
		assert.NotContains(t, s, ".")
	})
}
