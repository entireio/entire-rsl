package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/entireio/entire-rsl/internal/entirefields"
	"github.com/entireio/entire-rsl/internal/refresolve"
	"github.com/entireio/entire-rsl/internal/rslread"
	"github.com/entireio/entire-rsl/internal/sanitize"
	"github.com/gittuf/gittuf/pkg/githash"
	"github.com/gittuf/gittuf/pkg/gitstore"
	"github.com/spf13/cobra"
)

const (
	defaultHistory = 10
	maxHistory     = 1000
)

// Warning codes.
const (
	warnNoRSLEntries = "no_rsl_entries"
	warnAllSkipped   = "all_entries_skipped"
	warnLocalMissing = "local_ref_missing"
	warnLocalDrift   = "local_drift"
	warnTagMoved     = "tag_moved"
)

type warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// refReport is what the RSL records for one ref, next to the local ref.
type refReport struct {
	LocalTip githash.Hash
	History  rslread.History
	Ref      string
	Warnings []warning
}

// rslTip is the target the newest unskipped entry records.
func (r refReport) rslTip() githash.Hash {
	if r.History.Current == nil {
		return nil
	}
	return r.History.Current.TargetID
}

type refReportJSON struct {
	SchemaVersion int         `json:"schema_version"`
	Ref           string      `json:"ref"`
	LocalTip      *string     `json:"local_tip"`
	RSLTip        *string     `json:"rsl_tip"`
	Current       *entryJSON  `json:"current_entry"`
	History       []entryJSON `json:"history"`
	HistoryTotal  int         `json:"history_total"`
	Warnings      []warning   `json:"warnings"`
}

func optionalHash(h githash.Hash) *string {
	if len(h) == 0 {
		return nil
	}
	s := h.String()
	return &s
}

func (r refReport) toJSON() refReportJSON {
	j := refReportJSON{
		SchemaVersion: schemaVersion,
		Ref:           r.Ref,
		LocalTip:      optionalHash(r.LocalTip),
		RSLTip:        optionalHash(r.rslTip()),
		History:       make([]entryJSON, 0, len(r.History.Entries)),
		HistoryTotal:  r.History.Total,
		Warnings:      r.Warnings,
	}
	if j.Warnings == nil {
		j.Warnings = []warning{}
	}
	if r.History.Current != nil {
		c := toEntryJSON(*r.History.Current)
		j.Current = &c
	}
	for _, e := range r.History.Entries {
		j.History = append(j.History, toEntryJSON(e))
	}
	return j
}

// buildRefReport reads what the RSL and the local repository hold for ref.
func buildRefReport(ctx context.Context, s *session, ref string, historyLimit int) (refReport, error) {
	r := refReport{Ref: ref}

	local, err := s.repo.Store.GetReference(ref)
	switch {
	case err == nil:
		r.LocalTip = local
	case errors.Is(err, gitstore.ErrReferenceNotFound):
	default:
		return refReport{}, fmt.Errorf("read local %s: %w", ref, err)
	}

	r.History, err = s.reader.History(ctx, ref, historyLimit)
	if err != nil {
		return refReport{}, err
	}

	r.Warnings = refWarnings(r)
	return r, nil
}

func refWarnings(r refReport) []warning {
	var ws []warning
	ref := sanitize.Line(r.Ref)

	if r.History.Total == 0 {
		return append(ws, warning{Code: warnNoRSLEntries, Message: fmt.Sprintf("the RSL has no entries for %s. The repository's RSL coverage may exclude it, or it was never pushed through Entire", ref)})
	}
	if r.History.Current == nil {
		ws = append(ws, warning{Code: warnAllSkipped, Message: "every RSL entry for " + ref + " is skipped by an annotation"})
	}

	switch tip := r.rslTip(); {
	case len(r.LocalTip) == 0:
		ws = append(ws, warning{Code: warnLocalMissing, Message: ref + " does not exist locally. Fetch it to compare with the RSL"})
	case len(tip) != 0 && !r.LocalTip.Equal(tip):
		ws = append(ws, warning{Code: warnLocalDrift, Message: fmt.Sprintf("local %s is at %s but the RSL records %s", ref, shortID(r.LocalTip), shortID(tip))})
	}

	if strings.HasPrefix(r.Ref, refresolve.TagsPrefix) && r.History.Unskipped > 1 {
		ws = append(ws, warning{Code: warnTagMoved, Message: fmt.Sprintf("tag %s has %d unskipped RSL entries, so it was moved after it was first pushed", ref, r.History.Unskipped)})
	}

	return ws
}

func writeRefReport(w io.Writer, r refReport) error {
	local := "absent"
	if len(r.LocalTip) != 0 {
		local = r.LocalTip.String()
	}

	fields := []field{
		{"Ref", sanitize.Line(r.Ref)},
		{"Local tip", local},
	}

	if c := r.History.Current; c != nil {
		fields = append(fields, field{"RSL tip", fmt.Sprintf("%s (entry #%d)", c.TargetID.String(), c.Number)})
		if who := entirefields.Parse(c.CustomFields).Who(); who != "" {
			fields = append(fields, field{"Pushed by", sanitize.Line(who) + ", as recorded by Entire"})
		}
		fields = append(fields,
			field{"Time", formatTime(c.Time) + " (asserted by the signer)"},
			field{"Signer", signerText(c.Signature)},
		)
	} else {
		fields = append(fields, field{"RSL tip", "none"})
	}

	if err := writeFields(w, fields); err != nil {
		return err
	}

	if len(r.Warnings) > 0 {
		fmt.Fprintln(w)
		for _, warn := range r.Warnings {
			fmt.Fprintln(w, "Warning: "+warn.Message)
		}
	}

	if len(r.History.Entries) == 0 {
		return nil
	}

	fmt.Fprintf(w, "\nHistory (%d of %d entries, newest first)\n", len(r.History.Entries), r.History.Total)
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NUMBER\tENTRY\tTARGET\tTIME\tBY\tSIGNER\tSKIPPED")
	for _, e := range r.History.Entries {
		by := entirefields.Parse(e.CustomFields).Who()
		if by == "" {
			by = none
		}
		skipped := none
		if e.Skipped() {
			skipped = "yes, by " + shortID(e.SkippedBy[0])
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", e.Number, shortID(e.ID), shortID(e.TargetID), formatTime(e.Time), sanitize.Truncate(sanitize.Line(by), 40), signerShort(e.Signature), skipped)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("write history: %w", err)
	}
	return nil
}

// refCommandFlags are the flags shared by branch, tag and checkpoint.
type refCommandFlags struct {
	asJSON  bool
	history int
}

func (f *refCommandFlags) register(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&f.asJSON, "json", false, "print JSON")
	cmd.Flags().IntVar(&f.history, "history", defaultHistory, "number of RSL entries to list")
}

func (f *refCommandFlags) validate() error {
	if f.history < 0 || f.history > maxHistory {
		return usageError(fmt.Errorf("--history must be between 0 and %d", maxHistory))
	}
	return nil
}

// runRefReport resolves nothing: ref is already a full name.
func (a *app) runRefReport(cmd *cobra.Command, flags *refCommandFlags, ref string) error {
	s, err := a.open(cmd.Context())
	if err != nil {
		return err
	}

	report, err := buildRefReport(cmd.Context(), s, ref, flags.history)
	if err != nil {
		return err
	}

	if flags.asJSON {
		return writeJSON(cmd.OutOrStdout(), report.toJSON())
	}
	return writeRefReport(cmd.OutOrStdout(), report)
}
