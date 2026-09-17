// The Asker interface exists so that check can be exercised without a key or
// the network, so these tests answer from a table instead: the substitute is
// the point of the seam, not a way around a real dependency.
// tenet:ignore-file no-mocking
//
// The examples in the config below are labelled violations of comment-why, so
// the comment that restates the code is the fixture and not a slip.
// tenet:ignore-file comment-why
package check_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zoidsh/tenetlint/internal/cache"
	"github.com/zoidsh/tenetlint/internal/check"
	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/report"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

// answer is what the table says about the example whose code holds a token.
type answer struct {
	prob  float64
	where string
}

type table struct {
	mu      sync.Mutex
	answers map[string]answer
	states  []string
	calls   int
}

func (t *table) Ask(_ context.Context, state string, questions map[string]jev.Question) (*jev.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls++
	t.states = append(t.states, state)

	a := answer{}
	for token, found := range t.answers {
		if strings.Contains(state, token) {
			a = found
		}
	}
	answers := map[string]jev.Answer{}
	for name := range questions {
		if name == "verdict" {
			answers[name] = jev.Answer{Type: jev.KindNoul, Noul: a.prob}
			continue
		}
		answers[name] = jev.Answer{
			Type:          jev.KindChoice,
			Probabilities: map[string]float64{a.where: 0.7, "none": 0.3},
		}
	}
	return &jev.Response{Model: "jev-1.13.0", Answers: answers, Usage: jev.Usage{InputTokens: 100}}, nil
}

const config = `version: 1
model: jev-1.13.0
tenets:
  - id: comment-why
    tenet: A comment says why.
    criteria:
      true: It restates the code.
      false: It gives a reason.
    include: ["**/*.go"]
    examples:
      - label: violation
        lines: 2
        code: |
          func add(a, b int) int {
              // add a and b
              return a + b
          }
      - label: violation
        lines: [1, 2]
        code: |
          // bump the counter
          counter++
      - label: violation
        lines: 2
        code: |
          // step one
          start()
      - label: ok
        code: |
          // The API caps a page at 500.
          page(500)
      - label: ok
        code: |
          // Keep this in step with the schema.
          migrate()
      - label: ok
        code: |
          // The vendor SDK requires this order.
          connect()
  - id: no-fallback
    tenet: No silent fallbacks.
    include: ["**/*.go"]
    examples:
      - label: violation
        code: |
          if err != nil { return cached, nil } // one
      - label: violation
        code: |
          if err != nil { return cached, nil } // two
      - label: violation
        code: |
          if err != nil { return cached, nil } // three
      - label: ok
        code: |
          if err != nil { return err } // four
      - label: ok
        code: |
          if err != nil { return err } // five
      - label: ok
        code: |
          if err != nil { return err } // six
  - id: no-mocking
    tenet: Never mock.
    include: ["**/*_test.go"]
    examples:
      - label: violation
        code: |
          patch(fetch)
      - label: ok
        code: |
          server := httptest.NewServer(handler)
`

func answers() map[string]answer {
	return map[string]answer{
		"add a and b":     {prob: 0.92, where: "L002"},
		"bump the":        {prob: 0.88, where: "L002"},
		"step one":        {prob: 0.45, where: "L001"},
		"caps a page":     {prob: 0.05},
		"step with the":   {prob: 0.12},
		"vendor SDK":      {prob: 0.75},
		"// one":          {prob: 0.9},
		"// two":          {prob: 0.85},
		"// three":        {prob: 0.8},
		"// four":         {prob: 0.8},
		"// five":         {prob: 0.7},
		"// six":          {prob: 0.6},
		"patch(fetch)":    {prob: 0.9},
		"httptest.NewSer": {prob: 0.1},
	}
}

func run(t *testing.T, c *check.Checker) ([]check.Result, check.Stats) {
	t.Helper()
	cfg, err := tenets.Parse([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	results, stats, err := c.Run(context.Background(), cfg.Tenets)
	if err != nil {
		t.Fatal(err)
	}
	return results, stats
}

func TestGoldenOutput(t *testing.T) {
	asker := &table{answers: answers()}
	results, stats := run(t, &check.Checker{Asker: asker})
	// The duration is the one number that is not the same twice.
	stats.Duration = 0

	r := check.Report{Results: results, Stats: stats}
	for _, format := range []string{report.FormatText, report.FormatJSON} {
		var got bytes.Buffer
		if err := r.Write(&got, format); err != nil {
			t.Fatal(err)
		}
		golden(t, "check."+format, got.String())
	}
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

func TestOneCallPerExample(t *testing.T) {
	asker := &table{answers: answers()}
	_, stats := run(t, &check.Checker{Asker: asker})
	if asker.calls != 14 || stats.Calls != 14 {
		t.Errorf("made %d calls for 14 examples (stats say %d)", asker.calls, stats.Calls)
	}
	if stats.Examples != 14 || stats.InputTokens != 1400 {
		t.Errorf("stats are %#v", stats)
	}
}

func TestTheStateIsTheOneALintWouldBuild(t *testing.T) {
	asker := &table{answers: answers()}
	run(t, &check.Checker{Asker: asker})

	var adder, mocking string
	for _, state := range asker.states {
		switch {
		case strings.Contains(state, "add a and b"):
			adder = state
		case strings.Contains(state, "patch(fetch)"):
			mocking = state
		}
	}
	want := "Language: go. File: example.go. Source file excerpt:\n" +
		"L001 func add(a, b int) int {\n" +
		"L002     // add a and b\n" +
		"L003     return a + b\n" +
		"L004 }\n"
	if adder != want {
		t.Errorf("state is\n%q\nwant\n%q", adder, want)
	}
	// The tenet is about test files, so its examples are shown as one.
	if !strings.HasPrefix(mocking, "Language: go. File: example_test.go.") {
		t.Errorf("state is %q", mocking)
	}
}

func TestExampleLangNamesItsOwnFile(t *testing.T) {
	cfg, err := tenets.Parse([]byte(`version: 1
tenets:
  - id: comment-why
    tenet: A comment says why.
    include: ["**/*.go"]
    examples:
      - label: ok
        lang: python
        code: |
          PAGE = 500
`))
	if err != nil {
		t.Fatal(err)
	}
	asker := &table{answers: map[string]answer{"PAGE": {prob: 0.1}}}
	if _, _, err := (&check.Checker{Asker: asker}).Run(context.Background(), cfg.Tenets); err != nil {
		t.Fatal(err)
	}
	want := "Language: python. File: example.py. Source file excerpt:\nL001 PAGE = 500\n"
	if asker.states[0] != want {
		t.Errorf("state is %q, want %q", asker.states[0], want)
	}
}

func TestCachedExamplesCostNothing(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	first := &table{answers: answers()}
	run(t, &check.Checker{Asker: first, Cache: c})

	second := &table{answers: answers()}
	results, stats := run(t, &check.Checker{Asker: second, Cache: c})
	if second.calls != 0 {
		t.Errorf("the second run made %d calls", second.calls)
	}
	if stats.CacheHits != 14 {
		t.Errorf("cache hits are %d", stats.CacheHits)
	}
	if results[0].Verdict != check.VerdictUsable || results[0].LocationHits != 2 {
		t.Errorf("the cached run measured %#v", results[0])
	}
}

func TestAnAPIErrorAbandonsTheRun(t *testing.T) {
	cfg, err := tenets.Parse([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	c := &check.Checker{Asker: &broken{}}
	if _, _, err := c.Run(context.Background(), cfg.Tenets); err == nil {
		t.Fatal("want the error the API returned")
	}
}

type broken struct{}

func (broken) Ask(context.Context, string, map[string]jev.Question) (*jev.Response, error) {
	return nil, errBroken
}

var errBroken = &jev.APIError{Status: 500, Message: "upstream is down"}
