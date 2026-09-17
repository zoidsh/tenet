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
)

// Exit codes, which a pre-commit hook passes straight on.
const (
	ExitOK      = 0
	ExitFinding = 1
	ExitError   = 2
)

// Formats a report can be printed in.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// FormatEnv names the format whatever the terminal says, for a caller that
// pipes the output somewhere and still wants to read it.
const FormatEnv = "TENETLINT_FORMAT"

// Next is what a user does about the findings above it. It names the
// directive that silences one, so that the way out is on the screen.
const Next = "fix the lines above or mark one with a tenet\x3aignore <id> directive, then commit again"

// DefaultFormat is text for a person at a terminal and JSON for everything
// else, because what reads a pipe is a script or an agent.
func DefaultFormat(w io.Writer) string {
	switch os.Getenv(FormatEnv) {
	case FormatText:
		return FormatText
	case FormatJSON:
		return FormatJSON
	}
	if isTerminal(w) {
		return FormatText
	}
	return FormatJSON
}

func isTerminal(w io.Writer) bool {
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

// Report is one run's outcome.
type Report struct {
	Findings []judge.Finding
	Stats    judge.Stats
	Skipped  []source.Skip

	// Quiet drops the summary line, leaving only the findings themselves.
	Quiet bool
}

// ExitCode is 1 when anything was found. Every finding blocks: a tenet that
// is not worth failing a commit over is one whose cutoff is in the wrong
// place, which is what check is for.
func (r Report) ExitCode() int {
	if len(r.Findings) > 0 {
		return ExitFinding
	}
	return ExitOK
}

// ColorEnabled reports whether output to w should be coloured.
func ColorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTerminal(w)
}

const (
	reset = "\x1b[0m"
	dim   = "\x1b[2m"
	red   = "\x1b[31m"
)

type painter bool

func (p painter) paint(color, text string) string {
	if !p {
		return text
	}
	return color + text + reset
}

// Text writes the human-readable report: the lines to go and look at, then
// what each tenet that fired says, then what the run cost.
func (r Report) Text(w io.Writer, color bool) error {
	p := painter(color)
	var b strings.Builder
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "%s:%d: %s %s\n",
			f.File, f.Line, p.paint(red, f.Tenet), p.paint(dim, fmt.Sprintf("(p=%.2f)", f.Probability)))
	}
	if legend := r.legend(); legend != "" {
		b.WriteString("\n" + legend)
	}
	if !r.Quiet {
		if len(r.Findings) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(p.paint(dim, r.summary()) + "\n")
		if len(r.Findings) > 0 {
			b.WriteString(Next + "\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// legend says once what each tenet that fired asks for, so that the findings
// themselves stay one short line each however long the tenet is.
func (r Report) legend() string {
	var ids []string
	said := map[string]string{}
	for _, f := range r.Findings {
		if _, ok := said[f.Tenet]; ok {
			continue
		}
		said[f.Tenet] = f.Message
		ids = append(ids, f.Tenet)
	}
	var width int
	for _, id := range ids {
		width = max(width, len(id))
	}
	var b strings.Builder
	for _, id := range ids {
		fmt.Fprintf(&b, "%-*s  %s\n", width, id, said[id])
	}
	return b.String()
}

func (r Report) summary() string {
	s := r.Stats
	return fmt.Sprintf("%d findings · %d windows, %d calls, %d cached · $%.4f · %.1fs",
		len(r.Findings), s.Windows, s.Calls, s.CacheHits, s.CostUSD, s.Duration.Seconds())
}

type jsonReport struct {
	Version  int             `json:"version"`
	Findings []judge.Finding `json:"findings"`
	Next     string          `json:"next"`
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
	if len(out.Findings) > 0 {
		out.Next = Next
	} else {
		out.Findings = []judge.Finding{}
	}
	if out.Skipped == nil {
		out.Skipped = []source.Skip{}
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	// Nothing here is going into a web page, and a tenet full of < is
	// unreadable to the person who has to act on it.
	encoder.SetEscapeHTML(false)
	return encoder.Encode(out)
}

// Write prints the report in the named format.
func (r Report) Write(w io.Writer, format string, color bool) error {
	if format == FormatJSON {
		return r.JSON(w)
	}
	return r.Text(w, color)
}
