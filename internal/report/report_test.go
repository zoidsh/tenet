package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zoidsh/tenetlint/internal/judge"
	"github.com/zoidsh/tenetlint/internal/report"
	"github.com/zoidsh/tenetlint/internal/source"
)

func sample() report.Report {
	return report.Report{
		Findings: []judge.Finding{
			{
				File: "internal/a.go", Line: 12, Tenet: "comment-why",
				Probability: 0.83, Fail: 0.8,
				Message: "A comment says why.",
			},
			{
				File: "internal/b.go", Line: 4, Tenet: "no-fallback",
				Probability: 0.91, Fail: 0.8,
				Message: "No silent fallbacks.",
			},
		},
		Stats: judge.Stats{
			Files: 2, Windows: 3, Calls: 4, CacheHits: 1, InputTokens: 1200,
			CostUSD: 0.0012, Duration: 800 * time.Millisecond,
		},
		Skipped: []source.Skip{{File: ".env", Reason: "may hold a secret"}},
	}
}

const wantText = `internal/a.go:12: comment-why (p=0.83)
internal/b.go:4: no-fallback (p=0.91)

comment-why  A comment says why.
no-fallback  No silent fallbacks.

2 findings · 3 windows, 4 calls, 1 cached · $0.0012 · 0.8s
` + report.Next + "\n"

func TestText(t *testing.T) {
	var b strings.Builder
	if err := sample().Text(&b, false); err != nil {
		t.Fatal(err)
	}
	if b.String() != wantText {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), wantText)
	}
}

func TestTextWithoutFindings(t *testing.T) {
	var b strings.Builder
	r := report.Report{Stats: judge.Stats{}}
	if err := r.Text(&b, false); err != nil {
		t.Fatal(err)
	}
	want := "0 findings · 0 windows, 0 calls, 0 cached · $0.0000 · 0.0s\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

// The legend names each tenet once however many lines it fired on, and the
// ids line up under each other.
func TestTextLegendPadsTheIds(t *testing.T) {
	r := sample()
	r.Findings = append(r.Findings, judge.Finding{
		File: "internal/c.go", Line: 9, Tenet: "comment-why",
		Probability: 0.95, Fail: 0.8, Message: "A comment says why.",
	})
	var b strings.Builder
	if err := r.Text(&b, false); err != nil {
		t.Fatal(err)
	}
	if strings.Count(b.String(), "A comment says why.") != 1 {
		t.Errorf("the tenet is said more than once:\n%s", b.String())
	}
	if !strings.Contains(b.String(), "comment-why  A comment") {
		t.Errorf("the ids are not padded to the longest:\n%s", b.String())
	}
}

func TestQuietDropsTheSummaryAndTheAdvice(t *testing.T) {
	r := sample()
	r.Quiet = true
	var b strings.Builder
	if err := r.Text(&b, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "findings ·") || strings.Contains(b.String(), report.Next) {
		t.Errorf("quiet printed the summary:\n%s", b.String())
	}
}

func TestTextColour(t *testing.T) {
	var b strings.Builder
	if err := sample().Text(&b, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\x1b[31mcomment-why\x1b[0m") {
		t.Errorf("the tenet is not coloured: %q", b.String())
	}
}

const wantJSON = `{
  "version": 1,
  "findings": [
    {
      "file": "internal/a.go",
      "line": 12,
      "tenet": "comment-why",
      "probability": 0.83,
      "fail": 0.8,
      "message": "A comment says why."
    },
    {
      "file": "internal/b.go",
      "line": 4,
      "tenet": "no-fallback",
      "probability": 0.91,
      "fail": 0.8,
      "message": "No silent fallbacks."
    }
  ],
  "next": "fix the lines above or mark one with a tenet` + "\x3a" + `ignore <id> directive, then commit again",
  "stats": {
    "files": 2,
    "windows": 3,
    "calls": 4,
    "cache_hits": 1,
    "input_tokens": 1200,
    "cost_usd": 0.0012,
    "duration_ms": 800
  },
  "skipped": [
    {
      "file": ".env",
      "reason": "may hold a secret"
    }
  ]
}
`

func TestJSON(t *testing.T) {
	var b strings.Builder
	if err := sample().JSON(&b); err != nil {
		t.Fatal(err)
	}
	if b.String() != wantJSON {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), wantJSON)
	}
}

func TestJSONEmptyListsStayLists(t *testing.T) {
	var b strings.Builder
	if err := (report.Report{}).JSON(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"findings": []`) || !strings.Contains(b.String(), `"skipped": []`) {
		t.Errorf("got %s", b.String())
	}
	if !strings.Contains(b.String(), `"next": ""`) {
		t.Errorf("a run with nothing to fix still says to fix something: %s", b.String())
	}
}

func TestExitCode(t *testing.T) {
	if got := (report.Report{}).ExitCode(); got != report.ExitOK {
		t.Errorf("a clean run exits %d", got)
	}
	if got := sample().ExitCode(); got != report.ExitFinding {
		t.Errorf("a run with findings exits %d", got)
	}
}

func TestDefaultFormat(t *testing.T) {
	var b strings.Builder
	if got := report.DefaultFormat(&b); got != report.FormatJSON {
		t.Errorf("what is not a terminal gets %q", got)
	}
	t.Setenv(report.FormatEnv, report.FormatText)
	if got := report.DefaultFormat(&b); got != report.FormatText {
		t.Errorf("%s was not obeyed: %q", report.FormatEnv, got)
	}
	t.Setenv(report.FormatEnv, "loud")
	if got := report.DefaultFormat(&b); got != report.FormatJSON {
		t.Errorf("a format nobody knows should leave the terminal to decide, got %q", got)
	}
}

// The progress line is a half-written line taken back again, so anything that
// keeps what it is sent must never see it.
func TestProgressStaysOffAPipe(t *testing.T) {
	var b strings.Builder
	p := report.NewProgress(&b, true)
	p.Start(12, 2)
	p.Clear()
	if b.String() != "" {
		t.Errorf("a pipe was written to: %q", b.String())
	}
}
