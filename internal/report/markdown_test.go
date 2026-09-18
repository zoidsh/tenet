package report_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/judge"
	"github.com/zoidsh/tenet/internal/report"
)

// TestMain keeps a test run inside GitHub Actions from appending every report
// it renders to the job's real summary.
func TestMain(m *testing.M) {
	if err := os.Unsetenv(report.StepSummaryEnv); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s is\n%s\nwant\n%s", name, got, want)
	}
}

func TestMarkdown(t *testing.T) {
	r := sample()
	r.NearMisses = []judge.NearMiss{
		{File: "internal/c.go", Line: 30, Tenet: "no-fallback", Probability: 0.71, Fail: 0.8},
	}
	var b strings.Builder
	if err := r.Markdown(&b); err != nil {
		t.Fatal(err)
	}
	golden(t, "summary.md", b.String())
}

func TestMarkdownWithoutFindings(t *testing.T) {
	var b strings.Builder
	if err := (report.Report{}).Markdown(&b); err != nil {
		t.Fatal(err)
	}
	want := "## 0 findings\n\n0 findings · 0 windows, 0 calls, 0 cached · $0.0000 · 0.0s\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

// A message is written by whoever wrote the tenet, so a pipe in one must not
// end the column it is sitting in.
func TestMarkdownKeepsTheTableIntact(t *testing.T) {
	r := report.Report{Findings: []judge.Finding{
		{File: "a.go", Line: 1, Tenet: "t", Probability: 0.9, Message: "one | two\nthree"},
	}}
	var b strings.Builder
	if err := r.Markdown(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `| one \| two three |`) {
		t.Errorf("the row is\n%s", b.String())
	}
}

func TestGitHubAppendsTheStepSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary.md")
	if err := os.WriteFile(path, []byte("earlier step\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(report.StepSummaryEnv, path)

	var b strings.Builder
	if err := sample().GitHub(&b); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(b.String(), "::error file=internal/a.go") {
		t.Errorf("the workflow commands are\n%s", b.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, "earlier step\n") {
		t.Errorf("the summary overwrote what the job had already written:\n%s", got)
	}
	if !strings.Contains(got, "## 2 findings") || !strings.Contains(got, "| internal/a.go | 12 |") {
		t.Errorf("the summary is\n%s", got)
	}
}

func TestGitHubWithoutAStepSummary(t *testing.T) {
	t.Setenv(report.StepSummaryEnv, "")
	var b strings.Builder
	if err := sample().GitHub(&b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "## 2 findings") {
		t.Errorf("the summary went to the step's output:\n%s", b.String())
	}
}
