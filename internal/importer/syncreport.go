package importer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/zoidsh/tenet/internal/jev"
)

// SyncReport is what one sync decided, ready to print.
type SyncReport struct {
	Plan       Plan
	Candidates []Sorted
	Stats      Stats

	// Written is the config the edits went to, empty when nothing was written.
	Written string

	// DryRun says the edits were withheld rather than there being nothing to
	// write, which the summary has to tell apart.
	DryRun bool
}

// Text writes a line per entry and the summary.
func (r SyncReport) Text(w io.Writer) error {
	var b strings.Builder
	table := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range r.Plan.Added {
		_, _ = fmt.Fprintf(table, "added\t%s\t%s\t\n", c.ID, SourceLine(c.File, c.Text))
	}
	for _, c := range r.Plan.Changed {
		_, _ = fmt.Fprintf(table, "changed\t%s\t%s\t%s\n", subject(c.Entry), c.Source(), c.note())
	}
	for _, e := range r.Plan.Restated {
		_, _ = fmt.Fprintf(table, "restated\t%s\t%s\tthe source named a line, which now names the sentence\n", subject(e.Entry), e.Source)
	}
	for _, e := range r.Plan.Stale {
		_, _ = fmt.Fprintf(table, "stale\t%s\t%s\t\n", subject(e), e.Source)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	body := strings.TrimRight(b.String(), " \n")
	if body != "" {
		body += "\n\n"
	}
	_, err := io.WriteString(w, body+r.Summary()+"\n")
	return err
}

// subject is what the report calls an entry: a tenet by its id, a comment by
// the rule it stands above.
func subject(e Entry) string {
	if e.Tenet != "" {
		return e.Tenet
	}
	return "rules " + e.Rule
}

// note says what a person still has to do about a reworded rule. Criteria,
// examples and a cutoff calibrated against the old sentence are kept, so the
// check has to be run again to say whether they still hold.
func (c Change) note() string {
	if c.Entry.Tenet == "" {
		return "the rule file reworded this"
	}
	if !c.TextUpdated {
		return "re-run tenet check " + c.Entry.Tenet + "; the tenet sentence stays as you wrote it"
	}
	return "re-run tenet check " + c.Entry.Tenet
}

// Summary is the one line that says what the run found, cost and wrote.
func (r SyncReport) Summary() string {
	s := r.Stats
	var wrote string
	switch {
	case r.Written != "":
		wrote = ", written to " + r.Written
	case r.DryRun:
		wrote = ", nothing written (--dry-run)"
	default:
		wrote = ", nothing to write"
	}
	return fmt.Sprintf("%d added, %d changed, %d restated, %d stale%s · %d sentences, %d calls, %d cached · $%.4f · %.1fs",
		len(r.Plan.Added), len(r.Plan.Changed), len(r.Plan.Restated), len(r.Plan.Stale), wrote,
		s.Candidates, s.Calls, s.CacheHits, s.CostUSD, s.Duration.Seconds())
}

type jsonAdded struct {
	Tenet  string   `json:"tenet"`
	Source string   `json:"source"`
	Kind   []string `json:"kind"`
}

type jsonChanged struct {
	Tenet       string `json:"tenet,omitempty"`
	Rule        string `json:"rule,omitempty"`
	Old         string `json:"old"`
	New         string `json:"new"`
	TextUpdated bool   `json:"text_updated"`
}

// jsonRestated says what the source now reads, for a rule nothing else about
// which changed.
type jsonRestated struct {
	Tenet  string `json:"tenet,omitempty"`
	Rule   string `json:"rule,omitempty"`
	Source string `json:"source"`
}

type jsonSyncReport struct {
	Added      []jsonAdded    `json:"added"`
	Changed    []jsonChanged  `json:"changed"`
	Restated   []jsonRestated `json:"restated"`
	Stale      []Entry        `json:"stale"`
	Candidates []Sorted       `json:"candidates"`
	Stats      jsonStats      `json:"stats"`
	Written    *string        `json:"written"`
	DryRun     bool           `json:"dry_run"`
}

// JSON writes the machine-readable report.
func (r SyncReport) JSON(w io.Writer) error {
	out := jsonSyncReport{
		Added:      []jsonAdded{},
		Changed:    []jsonChanged{},
		Restated:   []jsonRestated{},
		Stale:      []Entry{},
		Candidates: r.Candidates,
		DryRun:     r.DryRun,
		Stats: jsonStats{
			Candidates:  r.Stats.Candidates,
			Tenets:      r.Stats.Tenets,
			Calls:       r.Stats.Calls,
			CacheHits:   r.Stats.CacheHits,
			InputTokens: r.Stats.InputTokens,
			CostUSD:     jev.RoundCost(r.Stats.CostUSD),
			DurationMS:  r.Stats.Duration.Milliseconds(),
		},
	}
	for _, c := range r.Plan.Added {
		kind := DraftKind(c.Kind)
		if kind == nil {
			kind = []string{}
		}
		out.Added = append(out.Added, jsonAdded{Tenet: c.ID, Source: SourceLine(c.File, c.Text), Kind: kind})
	}
	for _, c := range r.Plan.Changed {
		out.Changed = append(out.Changed, jsonChanged{
			Tenet:       c.Entry.Tenet,
			Rule:        c.Entry.Rule,
			Old:         c.Old,
			New:         c.New,
			TextUpdated: c.TextUpdated,
		})
	}
	for _, e := range r.Plan.Restated {
		out.Restated = append(out.Restated, jsonRestated{Tenet: e.Entry.Tenet, Rule: e.Entry.Rule, Source: e.Source})
	}
	out.Stale = append(out.Stale, r.Plan.Stale...)
	if r.Written != "" {
		out.Written = &r.Written
	}
	if out.Candidates == nil {
		out.Candidates = []Sorted{}
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}
