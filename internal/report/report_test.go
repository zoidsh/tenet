package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zoidsh/tenetlint/internal/judge"
	"github.com/zoidsh/tenetlint/internal/report"
	"github.com/zoidsh/tenetlint/internal/source"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

func sample() report.Report {
	return report.Report{
		Findings: []judge.Finding{
			{
				File: "internal/a.go", Line: 12, Tenet: "comment-why",
				Severity: tenets.SeverityWarn, Probability: 0.83,
				Message: "A comment says why.",
			},
			{
				File: "internal/b.go", Line: 4, Tenet: "no-fallback",
				Severity: tenets.SeverityError, Probability: 0.61, LowConfidence: true,
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

const wantText = `internal/a.go:12: warn comment-why (p=0.83) A comment says why.
internal/b.go:4: error no-fallback (p=0.61) No silent fallbacks. [low confidence]
2 findings (1 errors, 1 warnings, 0 info), 1 low confidence · 3 windows, 4 calls, 1 cached · $0.0012 · 0.8s
`

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
	want := "0 findings (0 errors, 0 warnings, 0 info), 0 low confidence · 0 windows, 0 calls, 0 cached · $0.0000 · 0.0s\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestTextColour(t *testing.T) {
	var b strings.Builder
	if err := sample().Text(&b, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "\x1b[33mwarn\x1b[0m") {
		t.Errorf("warn is not coloured: %q", b.String())
	}
	if !strings.Contains(b.String(), "\x1b[31merror\x1b[0m") {
		t.Errorf("error is not coloured: %q", b.String())
	}
}

const wantJSON = `{
  "version": 1,
  "findings": [
    {
      "file": "internal/a.go",
      "line": 12,
      "tenet": "comment-why",
      "severity": "warn",
      "probability": 0.83,
      "low_confidence": false,
      "message": "A comment says why."
    },
    {
      "file": "internal/b.go",
      "line": 4,
      "tenet": "no-fallback",
      "severity": "error",
      "probability": 0.61,
      "low_confidence": true,
      "message": "No silent fallbacks."
    }
  ],
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
}

func TestExitCode(t *testing.T) {
	warn := judge.Finding{Severity: tenets.SeverityWarn}
	info := judge.Finding{Severity: tenets.SeverityInfo}
	lowError := judge.Finding{Severity: tenets.SeverityError, LowConfidence: true}

	cases := []struct {
		name     string
		findings []judge.Finding
		failOn   string
		want     int
	}{
		{"nothing", nil, "warn", report.ExitOK},
		{"warn at warn", []judge.Finding{warn}, "warn", report.ExitFinding},
		{"warn at error", []judge.Finding{warn}, "error", report.ExitOK},
		{"info at warn", []judge.Finding{info}, "warn", report.ExitOK},
		{"info at info", []judge.Finding{info}, "info", report.ExitFinding},
		{"low confidence never fails", []judge.Finding{lowError}, "error", report.ExitOK},
		{"never", []judge.Finding{warn}, report.FailNever, report.ExitOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			failOn, err := report.ParseFailOn(c.failOn)
			if err != nil {
				t.Fatal(err)
			}
			got := report.Report{Findings: c.findings}.ExitCode(failOn)
			if got != c.want {
				t.Errorf("exit %d, want %d", got, c.want)
			}
		})
	}
}

func TestParseFailOnRejectsNonsense(t *testing.T) {
	if _, err := report.ParseFailOn("loud"); err == nil {
		t.Fatal("want an error")
	}
}
