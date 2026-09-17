// Package report prints a run's findings and decides what the process exits
// with.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zoidsh/tenetlint/internal/judge"
	"github.com/zoidsh/tenetlint/internal/source"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

// Exit codes, which a pre-commit hook passes straight on.
const (
	ExitOK      = 0
	ExitFinding = 1
	ExitError   = 2
)

// FailNever is the --fail-on value that lets every finding through.
const FailNever = "never"

// Formats a report can be printed in.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// Report is one run's outcome.
type Report struct {
	Findings []judge.Finding
	Stats    judge.Stats
	Skipped  []source.Skip
}

// ExitCode is 1 when a finding at or above failOn stands. A low confidence
// finding is reported but never fails a run, because the point of the flag is
// that it is safe to gate a commit on.
func (r Report) ExitCode(failOn string) int {
	if failOn == FailNever {
		return ExitOK
	}
	level, err := tenets.ParseSeverity(failOn)
	if err != nil {
		return ExitOK
	}
	for _, f := range r.Findings {
		if f.LowConfidence {
			continue
		}
		if f.Severity.Rank() >= level.Rank() {
			return ExitFinding
		}
	}
	return ExitOK
}

// ColorEnabled reports whether output to w should be coloured.
func ColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	blue   = "\x1b[34m"
)

type painter bool

func (p painter) paint(color, text string) string {
	if !p {
		return text
	}
	return color + text + reset
}

func (p painter) severity(s tenets.Severity) string {
	switch s {
	case tenets.SeverityError:
		return p.paint(red, string(s))
	case tenets.SeverityWarn:
		return p.paint(yellow, string(s))
	default:
		return p.paint(blue, string(s))
	}
}

// Text writes the human-readable report.
func (r Report) Text(w io.Writer, color bool) error {
	p := painter(color)
	var b strings.Builder
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "%s:%d: %s %s (p=%.2f) %s",
			f.File, f.Line, p.severity(f.Severity), f.Tenet, f.Probability, f.Message)
		if f.LowConfidence {
			b.WriteString(p.paint(dim, " [low confidence]"))
		}
		b.WriteString("\n")
	}
	b.WriteString(p.paint(dim, r.summary()))
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func (r Report) summary() string {
	var errors, warnings, infos, low int
	for _, f := range r.Findings {
		switch f.Severity {
		case tenets.SeverityError:
			errors++
		case tenets.SeverityWarn:
			warnings++
		default:
			infos++
		}
		if f.LowConfidence {
			low++
		}
	}
	s := r.Stats
	return fmt.Sprintf("%d findings (%d errors, %d warnings, %d info), %d low confidence · %d windows, %d calls, %d cached · $%.4f · %.1fs",
		len(r.Findings), errors, warnings, infos, low,
		s.Windows, s.Calls, s.CacheHits, s.CostUSD, s.Duration.Seconds())
}

type jsonReport struct {
	Version  int             `json:"version"`
	Findings []judge.Finding `json:"findings"`
	Stats    jsonStats       `json:"stats"`
	Skipped  []source.Skip   `json:"skipped"`
}

type jsonStats struct {
	Files       int     `json:"files"`
	Windows     int     `json:"windows"`
	Calls       int     `json:"calls"`
	CacheHits   int     `json:"cache_hits"`
	InputTokens int     `json:"input_tokens"`
	CostUSD     float64 `json:"cost_usd"`
	DurationMS  int64   `json:"duration_ms"`
}

// JSON writes the machine-readable report.
func (r Report) JSON(w io.Writer) error {
	out := jsonReport{
		Version:  1,
		Findings: r.Findings,
		Stats: jsonStats{
			Files:       r.Stats.Files,
			Windows:     r.Stats.Windows,
			Calls:       r.Stats.Calls,
			CacheHits:   r.Stats.CacheHits,
			InputTokens: r.Stats.InputTokens,
			CostUSD:     r.Stats.CostUSD,
			DurationMS:  r.Stats.Duration.Milliseconds(),
		},
		Skipped: r.Skipped,
	}
	if out.Findings == nil {
		out.Findings = []judge.Finding{}
	}
	if out.Skipped == nil {
		out.Skipped = []source.Skip{}
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(out)
}

// Write prints the report in the named format.
func (r Report) Write(w io.Writer, format string, color bool) error {
	if format == FormatJSON {
		return r.JSON(w)
	}
	return r.Text(w, color)
}
