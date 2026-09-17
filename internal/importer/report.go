package importer

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/zoidsh/tenetlint/internal/jev"
)

// SentenceWidth is how much of a candidate the table shows before it is cut,
// which keeps a row on one terminal line next to its probabilities.
const SentenceWidth = 60

// Report is what one import decided, ready to print.
type Report struct {
	Candidates []Sorted
	Stats      Stats

	// Written is the file the draft went to, empty when nothing was written.
	Written string

	// DryRun says the draft was withheld rather than there being nothing to
	// write, which the summary has to tell apart.
	DryRun bool
}

// Text writes the per-file tables and the summary.
func (r Report) Text(w io.Writer) error {
	var b strings.Builder
	var file string
	table := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, c := range r.Candidates {
		if c.File != file {
			if file != "" {
				if err := table.Flush(); err != nil {
					return err
				}
				b.WriteString("\n")
			}
			file = c.File
			_, _ = fmt.Fprintf(table, "%s\n", file)
			_, _ = fmt.Fprint(table, "  line\tkind\tkind p\tcheckable p\ttenet\tsentence\n")
		}
		_, _ = fmt.Fprintf(table, "  %d\t%s\t%.2f\t%.2f\t%s\t%s\n",
			c.Line, c.Kind, c.KindProb, c.CheckableProb, tenetOrDash(c.ID), cut(c.Text))
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if len(r.Candidates) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(r.Summary() + "\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func tenetOrDash(id string) string {
	if id == "" {
		return "-"
	}
	return id
}

func cut(text string) string {
	runes := []rune(text)
	if len(runes) <= SentenceWidth {
		return text
	}
	return string(runes[:SentenceWidth-1]) + "…"
}

// Summary is the one line that says what the run cost and where it went.
func (r Report) Summary() string {
	s := r.Stats
	var wrote string
	switch {
	case r.Written != "":
		wrote = fmt.Sprintf("%d tenets written to %s", s.Tenets, r.Written)
	case r.DryRun:
		wrote = fmt.Sprintf("%d tenets, nothing written (--dry-run)", s.Tenets)
	default:
		wrote = fmt.Sprintf("%d tenets, nothing to write", s.Tenets)
	}
	return fmt.Sprintf("%d candidates, %s · %d calls, %d cached · $%.4f · %.1fs",
		s.Candidates, wrote, s.Calls, s.CacheHits, s.CostUSD, s.Duration.Seconds())
}

type jsonReport struct {
	Candidates []Sorted  `json:"candidates"`
	Written    *string   `json:"written"`
	Stats      jsonStats `json:"stats"`
}

type jsonStats struct {
	Candidates  int     `json:"candidates"`
	Tenets      int     `json:"tenets"`
	Calls       int     `json:"calls"`
	CacheHits   int     `json:"cache_hits"`
	InputTokens int     `json:"input_tokens"`
	CostUSD     float64 `json:"cost_usd"`
	DurationMS  int64   `json:"duration_ms"`
}

// JSON writes the machine-readable report.
func (r Report) JSON(w io.Writer) error {
	out := jsonReport{
		Candidates: r.Candidates,
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
