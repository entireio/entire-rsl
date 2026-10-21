package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

// schemaVersion is the version of every --json document. It changes only on
// an incompatible change. See docs/json-schema.md.
const schemaVersion = 1

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode JSON output: %w", err)
	}
	return nil
}
