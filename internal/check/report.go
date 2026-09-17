package check

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/zoidsh/tenetlint/internal/report"
)

// Report is one check run.
type Report struct {
	Results     []Result
	Stats       Stats
	MinExamples int
}

const labelWidth = 18

// Text writes the table a user reads while editing a tenet.
func (r Report) Text(w io.Writer) error {
	var b strings.Builder
	for i, result := range r.Results {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s\n", result.Tenet, result.Verdict)
		row(&b, "examples", r.examples(result))
		if result.Verdict != VerdictTooFew {
			row(&b, "auc", auc(result))
			row(&b, "accuracy", fmt.Sprintf("%.2f at fail %.2f · %s", result.Accuracy, result.Fail, comparison(result)))
			row(&b, "mean probability", fmt.Sprintf("violation %.2f, ok %.2f, gap %.2f",
				result.MeanViolation, result.MeanOK, result.Gap))
			if result.LocatedExamples > 0 {
				row(&b, "location", fmt.Sprintf("%d of %d lines named (%.2f)",
					result.LocationHits, result.LocatedExamples, result.LocationRate))
			}
			stability(&b, result)
			if result.Advice != "" {
				row(&b, "advice", result.Advice)
			}
			misjudged(&b, result)
		}
	}
	if len(r.Results) > 0 {
		b.WriteString("\n")
	}
	b.WriteString(r.summary())
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func (r Report) examples(result Result) string {
	if result.Verdict == VerdictTooFew {
		return fmt.Sprintf("%d, %d needed", result.Examples, r.minExamples())
	}
	return fmt.Sprintf("%d (%d violation, %d ok)", result.Examples, result.Violations, result.OKs)
}

func (r Report) minExamples() int {
	if r.MinExamples <= 0 {
		return DefaultMinExamples
	}
	return r.MinExamples
}

func auc(result Result) string {
	if !result.Separated {
		return "not measurable: every example carries the same label"
	}
	return fmt.Sprintf("%.2f", result.AUC)
}

// comparison is what the same examples would come out at under the other
// cutoffs, so that lowering or raising this tenet's own is an informed move.
func comparison(result Result) string {
	parts := make([]string, 0, len(result.AccuracyAt))
	for _, a := range result.AccuracyAt {
		parts = append(parts, fmt.Sprintf("%.2f at %.2f", a.Accuracy, a.Cutoff))
	}
	return strings.Join(parts, ", ")
}

func stability(b *strings.Builder, result Result) {
	s := result.Stability
	if s == nil {
		return
	}
	row(b, "stability", fmt.Sprintf("%s, max sd %.3f, %s crossed fail %.2f",
		plural(s.Runs, "run"), s.MaxStdDev, plural(len(s.Crossed), "example"), result.Fail))
	for _, c := range s.Crossed {
		fmt.Fprintf(b, "    %-9s p=%.2f to %.2f  %s\n", c.Label, c.Min, c.Max, strings.TrimSpace(c.Code))
	}
}

func misjudged(b *strings.Builder, result Result) {
	if len(result.Misjudged) == 0 {
		return
	}
	row(b, "misjudged", fmt.Sprintf("%d of %d", len(result.Misjudged), result.Examples))
	for _, m := range result.Misjudged {
		note := ""
		if m.Borderline {
			note = " [near the cutoff]"
		}
		fmt.Fprintf(b, "    %-9s p=%.2f  %s%s\n", m.Label, m.Prob, strings.TrimSpace(m.Code), note)
	}
}

func row(b *strings.Builder, label, value string) {
	fmt.Fprintf(b, "  %-*s%s\n", labelWidth, label, value)
}

func (r Report) summary() string {
	s := r.Stats
	return fmt.Sprintf("%s over %s · %d calls, %d cached · $%.4f · %.1fs",
		plural(len(r.Results), "tenet"), plural(s.Examples, "example"),
		s.Calls, s.CacheHits, s.CostUSD, s.Duration.Seconds())
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

type jsonReport struct {
	Version     int       `json:"version"`
	Tenets      []Result  `json:"tenets"`
	Stats       jsonStats `json:"stats"`
	MinExamples int       `json:"min_examples"`
}

type jsonStats struct {
	Examples    int     `json:"examples"`
	Calls       int     `json:"calls"`
	CacheHits   int     `json:"cache_hits"`
	InputTokens int     `json:"input_tokens"`
	CostUSD     float64 `json:"cost_usd"`
	DurationMS  int64   `json:"duration_ms"`
}

// JSON writes the same numbers for a tool to read.
func (r Report) JSON(w io.Writer) error {
	out := jsonReport{
		Version:     1,
		Tenets:      r.Results,
		MinExamples: r.minExamples(),
		Stats: jsonStats{
			Examples:    r.Stats.Examples,
			Calls:       r.Stats.Calls,
			CacheHits:   r.Stats.CacheHits,
			InputTokens: r.Stats.InputTokens,
			CostUSD:     r.Stats.CostUSD,
			DurationMS:  r.Stats.Duration.Milliseconds(),
		},
	}
	if out.Tenets == nil {
		out.Tenets = []Result{}
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

// Write prints the report in the named format.
func (r Report) Write(w io.Writer, format string) error {
	if format == report.FormatJSON {
		return r.JSON(w)
	}
	return r.Text(w)
}
