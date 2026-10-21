package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/entireio/entire-rsl/internal/entirefields"
	"github.com/entireio/entire-rsl/internal/redact"
	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/entireio/entire-rsl/internal/sanitize"
	"github.com/gittuf/gittuf/pkg/githash"
)

// shortIDLen is the abbreviation length for IDs in tables.
const shortIDLen = 12

// none marks an empty cell.
const none = "-"

// labelRef labels a ref in detail views.
const labelRef = "Ref"

func shortID(h githash.Hash) string {
	if len(h) == 0 {
		return none
	}
	s := h.String()
	if len(s) > shortIDLen {
		return s[:shortIDLen]
	}
	return s
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// signerText describes a signature in a phrase, green when it checks out and
// red otherwise. Only a valid signature names a key.
func signerText(s rslread.Signature, st styles) string {
	if !s.Valid {
		return st.bad(s.Method + ": " + s.Problem)
	}
	if name := signerName(s); name != "" {
		return st.good(s.KeyID + " (" + name + "), signature valid")
	}
	return st.good(s.KeyID + ", signature valid")
}

// signerShort is signerText for a table cell.
func signerShort(s rslread.Signature, st styles) string {
	if !s.Valid {
		if s.Method == rslread.MethodNone {
			return st.bad("unsigned")
		}
		return st.bad("invalid")
	}
	if name := signerName(s); name != "" {
		return st.good(name)
	}
	return st.good(sanitize.Truncate(s.KeyID, 20))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// writeLogTable prints entries as a table, newest first.
func writeLogTable(w io.Writer, entries []rslread.Entry, st styles) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NUMBER\tENTRY\tTYPE\tREF\tTARGET\tTIME\t"+st.plain("SIGNER")+"\tSKIPPED")
	for _, e := range entries {
		ref := sanitize.Line(e.RefName)
		target := shortID(e.TargetID)
		if e.Kind == rslread.KindAnnotation {
			verb := "note on"
			if e.Skip {
				verb = "skip"
			}
			ref = verb + " " + plural(len(e.Annotates), "entry", "entries")
			target = none
		}

		skipped := none
		if e.Skipped() {
			skipped = "yes, by " + shortID(e.SkippedBy[0])
		}

		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			e.Number, shortID(e.ID), e.Kind, ref, target, formatTime(e.Time), signerShort(e.Signature, st), skipped)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write table: %w", err)
	}
	return nil
}

// field is one labelled line of a detail view.
type field struct{ label, value string }

func writeFields(w io.Writer, fields []field) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, f := range fields {
		lines := strings.Split(f.value, "\n")
		fmt.Fprintf(tw, "%s\t%s\n", f.label, lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(tw, "\t%s\n", l)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write fields: %w", err)
	}
	return nil
}

// entryFields describes one entry in full.
func entryFields(e rslread.Entry, st styles) []field {
	fields := []field{
		{"Entry", fmt.Sprintf("#%d %s", e.Number, e.ID.String())},
		{"Type", e.Kind},
	}

	switch e.Kind {
	case rslread.KindAnnotation:
		action := "note"
		if e.Skip {
			action = "skip (revokes the annotated entries)"
		}
		fields = append(fields, field{"Action", action})
		fields = append(fields, field{"Annotates", strings.Join(hashStrings(e.Annotates), "\n")})
		if e.Message != "" {
			fields = append(fields, field{"Message", sanitize.Lines(e.Message)})
		}
	default:
		fields = append(fields,
			field{labelRef, sanitize.Line(e.RefName)},
			field{"Target", e.TargetID.String()},
		)
		if e.Kind == rslread.KindPropagation {
			fields = append(fields,
				field{"Upstream", sanitize.Line(redact.URLs(e.UpstreamRepository))},
				field{"Upstream entry", e.UpstreamEntryID.String()},
			)
		}
	}

	fields = append(fields,
		field{"Time", formatTime(e.Time) + " (asserted by the signer)"},
		field{"Signer", signerText(e.Signature, st)},
		field{"Committer", sanitize.Line(e.Committer.Name) + " <" + sanitize.Line(e.Committer.Email) + "> (unverified)"},
	)

	if e.Skipped() {
		fields = append(fields, field{"Skipped", "yes, by " + strings.Join(hashStrings(e.SkippedBy), ", ")})
	}
	if len(e.AnnotatedBy) > 0 {
		fields = append(fields, field{"Annotated by", strings.Join(hashStrings(e.AnnotatedBy), "\n")})
	}

	return append(fields, entireFields(redact.Values(e.CustomFields))...)
}

// entireFields describes Entire's custom fields, then every field raw. The
// labels say who asserts them: the signer, not entire-rsl.
func entireFields(custom map[string]string) []field {
	if len(custom) == 0 {
		return nil
	}

	f := entirefields.Parse(custom)
	var fields []field
	if f.Pusher != nil {
		who := sanitize.Line(f.Pusher.Raw)
		if f.Pusher.Handle != "" {
			who = sanitize.Line(f.Pusher.Handle) + " (account " + f.Pusher.AccountID + ")"
		}
		fields = append(fields, field{"Pushed by", who + ", as recorded by Entire"})
	}
	if f.Actor != "" {
		label := f.ActorLabel
		if label == "" {
			label = sanitize.Line(f.Actor)
		}
		fields = append(fields, field{"Written by", label + ", as recorded by Entire"})
	}
	if f.ForcePush {
		fields = append(fields, field{"Force push", "yes"})
	}
	if f.Operation != "" {
		op := sanitize.Line(f.Operation)
		if f.OperationLabel != "" {
			op += " (" + f.OperationLabel + ")"
		}
		fields = append(fields, field{"Operation", op})
	}
	if f.SymrefTarget != "" {
		fields = append(fields, field{"Default branch", sanitize.Line(f.SymrefTarget)})
	}
	if f.SymrefPreviousTarget != "" {
		fields = append(fields, field{"Previous default", sanitize.Line(f.SymrefPreviousTarget)})
	}
	if f.Repository != "" {
		fields = append(fields, field{"Repository", sanitize.Line(f.Repository)})
	}

	keys := slices.Sorted(maps.Keys(custom))
	raw := make([]string, 0, len(keys))
	for _, k := range keys {
		raw = append(raw, sanitize.Line(k)+" = "+sanitize.Line(custom[k]))
	}
	return append(fields, field{"Custom fields", strings.Join(raw, "\n") + "\n(signed with the entry, asserted by the signer, unverified by entire-rsl)"})
}
