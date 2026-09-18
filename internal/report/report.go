// Package report prints a run's findings and decides what the process exits
// with.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/judge"
	"github.com/zoidsh/tenet/internal/source"
)

// Exit codes, which a pre-commit hook passes straight on.
const (
	ExitOK      = 0
	ExitFinding = 1
	ExitError   = 2
	// A hook that runs on every commit in every repository needs to tell a
	// repository that does not use tenet from a run that broke.
	ExitNoConfig = 3
)

// Formats a report can be printed in.
const (
	FormatText   = "text"
	FormatJSON   = "json"
	FormatGitHub = "github"
)

// FormatEnv names the format whatever the terminal says, for a caller that
// pipes the output somewhere and still wants to read it.
const FormatEnv = "TENET_FORMAT"

// Next is what a user does about the findings above it. It leaves the ignore
// directive unnamed, because a directive is a person's decision about a
// confirmed false positive and an agent reading this line would take it for
// the way past a finding.
const Next = "fix the lines above"

// NextStaged is Next for a run that was about to commit what it judged. A
// lint of paths or of a base ref commits nothing, so it says nothing about
// committing again.
const NextStaged = Next + ", then commit again"

// NextCommitMsg is what a user does about a finding in a commit message,
// which git has already taken out of the editor by the time a commit-msg hook
// prints this.
const NextCommitMsg = "reword the message, which git kept in .git/COMMIT_EDITMSG, then commit again"

// NextPR is what a user does about a finding in a pull request's title or
// description, neither of which is in the branch a push would change.
const NextPR = "edit the pull request title or description, then push again"

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

	// Baselined are the findings an accepted baseline took out of Findings.
	// They are counted, and printed only when ShowBaselined, but they never
	// fail a run: the point of accepting them was to stop them blocking.
	Baselined     []judge.Finding
	ShowBaselined bool

	// Quiet drops the summary line, leaving only the findings themselves.
	Quiet bool

	// Next is what to do about the findings, Next above when it is empty.
	Next string
}

func (r Report) next() string {
	if r.Next != "" {
		return r.Next
	}
	return Next
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

// The choices --color takes.
const (
	ColorAuto   = "auto"
	ColorAlways = "always"
	ColorNever  = "never"
)

// Colored settles the colour for one run: always and never say so outright,
// and auto asks the terminal and NO_COLOR.
func Colored(w io.Writer, choice string) (bool, error) {
	switch choice {
	case ColorAlways:
		return true, nil
	case ColorNever:
		return false, nil
	case ColorAuto:
		return ColorEnabled(w), nil
	default:
		return false, fmt.Errorf("--color must be %s, %s or %s, got %q", ColorAuto, ColorAlways, ColorNever, choice)
	}
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
	listed := r.listed()
	var b strings.Builder
	for _, l := range listed {
		fmt.Fprintf(&b, "%s:%d: %s %s",
			l.File, l.Line, p.paint(red, l.Tenet), p.paint(dim, fmt.Sprintf("(p=%.2f)", l.Probability)))
		if l.baselined {
			b.WriteString(" " + p.paint(dim, "[baselined]"))
		}
		b.WriteString("\n")
	}
	if legend := r.legend(); legend != "" {
		b.WriteString("\n" + legend)
	}
	if !r.Quiet {
		if len(listed) > 0 {
			b.WriteString("\n")
		}
		b.WriteString(p.paint(dim, r.summary()) + "\n")
		if len(r.Findings) > 0 {
			b.WriteString(r.next() + "\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// listed are the findings the text report prints, in the one order it prints
// them in whether or not the baselined ones are among them.
func (r Report) listed() []listing {
	out := make([]listing, 0, len(r.Findings)+len(r.Baselined))
	for _, f := range r.Findings {
		out = append(out, listing{Finding: f})
	}
	if r.ShowBaselined {
		for _, f := range r.Baselined {
			out = append(out, listing{Finding: f, baselined: true})
		}
		sort.Slice(out, func(a, b int) bool { return judge.Less(out[a].Finding, out[b].Finding) })
	}
	return out
}

type listing struct {
	judge.Finding
	baselined bool
}

// legend says once what each tenet that fired asks for, so that the findings
// themselves stay one short line each however long the tenet is.
func (r Report) legend() string {
	var ids []string
	said := map[string]string{}
	for _, f := range r.listed() {
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
	var baselined string
	if len(r.Baselined) > 0 {
		baselined = fmt.Sprintf(", %d baselined", len(r.Baselined))
	}
	return fmt.Sprintf("%d findings%s · %d windows, %d calls, %d cached · $%.4f · %.1fs",
		len(r.Findings), baselined, s.Windows, s.Calls, s.CacheHits, s.CostUSD, s.Duration.Seconds())
}

type jsonReport struct {
	Version  int             `json:"version"`
	Findings []judge.Finding `json:"findings"`
	Next     string          `json:"next"`
	Stats    jsonStats       `json:"stats"`
	Skipped  []source.Skip   `json:"skipped"`

	// Baselined is written only when it was asked for, so that a reader can
	// tell an empty list from a run that never looked.
	Baselined *[]judge.Finding `json:"baselined,omitempty"`
}

type jsonStats struct {
	Baselined   int     `json:"baselined"`
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
			Baselined:   len(r.Baselined),
			Files:       r.Stats.Files,
			Windows:     r.Stats.Windows,
			Calls:       r.Stats.Calls,
			CacheHits:   r.Stats.CacheHits,
			InputTokens: r.Stats.InputTokens,
			CostUSD:     jev.RoundCost(r.Stats.CostUSD),
			DurationMS:  r.Stats.Duration.Milliseconds(),
		},
		Skipped: r.Skipped,
	}
	if len(out.Findings) > 0 {
		out.Next = r.next()
	} else {
		out.Findings = []judge.Finding{}
	}
	if out.Skipped == nil {
		out.Skipped = []source.Skip{}
	}
	if r.ShowBaselined {
		baselined := r.Baselined
		if baselined == nil {
			baselined = []judge.Finding{}
		}
		out.Baselined = &baselined
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	// Nothing here is going into a web page, and a tenet full of < is
	// unreadable to the person who has to act on it.
	encoder.SetEscapeHTML(false)
	return encoder.Encode(out)
}

// GitHub writes the findings as workflow commands, which GitHub reads off a
// step's output and turns into annotations on the lines they name. Baselined
// findings are left out: accepting one was the decision not to put it in
// front of anyone again.
func (r Report) GitHub(w io.Writer) error {
	var b strings.Builder
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "::error file=%s,line=%d,title=%s::%s\n",
			escapeProperty(f.File), f.Line, escapeProperty(f.Tenet), escapeData(f.Message))
	}
	if !r.Quiet {
		b.WriteString(r.summary() + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// The escapes a workflow command needs: a raw newline would end the command
// early, and an unescaped separator inside a property would start another one.
var (
	dataEscapes     = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	propertyEscapes = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
)

func escapeData(s string) string { return dataEscapes.Replace(s) }

func escapeProperty(s string) string { return propertyEscapes.Replace(s) }

// Write prints the report in the named format.
func (r Report) Write(w io.Writer, format string, color bool) error {
	switch format {
	case FormatJSON:
		return r.JSON(w)
	case FormatGitHub:
		return r.GitHub(w)
	}
	return r.Text(w, color)
}
