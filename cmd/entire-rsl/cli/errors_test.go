package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantCode   int
		wantStderr string
	}{
		{name: "nil", err: nil, wantCode: ExitOK, wantStderr: ""},
		{name: "plain error", err: errors.New("boom"), wantCode: ExitFailure, wantStderr: "Error: boom\n"},
		{name: "usage", err: usageError(errors.New("bad flag")), wantCode: ExitUsage, wantStderr: "Error: bad flag\nRun 'entire rsl --help' for usage.\n"},
		{name: "silent verification", err: silentExit(ExitVerification, errors.New("failed")), wantCode: ExitVerification, wantStderr: ""},
		{name: "wrapped exit error", err: fmt.Errorf("outer: %w", silentExit(ExitVerification, nil)), wantCode: ExitVerification, wantStderr: ""},
		{name: "cobra unknown command", err: errors.New(`unknown command "x" for "entire-rsl"`), wantCode: ExitUsage, wantStderr: "Error: unknown command \"x\" for \"entire-rsl\"\nRun 'entire rsl --help' for usage.\n"},
		{name: "cancelled", err: fmt.Errorf("walk: %w", context.Canceled), wantCode: exitInterrupted, wantStderr: "Interrupted.\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			code := Report(&stderr, tt.err)

			assert.Equal(t, tt.wantCode, code)
			assert.Equal(t, tt.wantStderr, stderr.String())
		})
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	t.Parallel()

	res := execute(t, testEnv{}, "nope")

	assert.Equal(t, ExitUsage, res.code)
	assert.Empty(t, res.stdout)
	assert.Contains(t, res.stderr, `unknown command "nope"`)
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	t.Parallel()

	res := execute(t, testEnv{}, "version", "--nope")

	assert.Equal(t, ExitUsage, res.code)
	assert.Contains(t, res.stderr, "unknown flag: --nope")
}

func TestExtraArgumentIsUsageError(t *testing.T) {
	t.Parallel()

	res := execute(t, testEnv{}, "version", "extra")

	assert.Equal(t, ExitUsage, res.code)
}
