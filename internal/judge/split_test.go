// The fake asker of judge_test.go stands in for the API here too.
// tenet:ignore-file no-mocking
package judge_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/judge"
	"github.com/zoidsh/tenetlint/internal/source"
)

func tenetsConfig(n, criteria int) string {
	var b strings.Builder
	b.WriteString("version: 1\nmodel: jev-1.13.0\ntenets:\n")
	for i := range n {
		fmt.Fprintf(&b, "  - id: t%d\n    tenet: Rule %d holds.\n", i, i)
		if criteria > 0 {
			fmt.Fprintf(&b, "    criteria:\n      true: %s\n", strings.Repeat("a", criteria))
		}
	}
	return b.String()
}

func body(lines int, width int) string {
	var b strings.Builder
	for i := range lines {
		fmt.Fprintf(&b, "x%0*d\n", width-1, i)
	}
	return b.String()
}

func requestTokens(t *testing.T, state string, questions map[string]jev.Question) int {
	t.Helper()
	tokens, err := jev.RequestTokens(state, questions)
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func disjointNames(t *testing.T, calls []call) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, c := range calls {
		for name := range c.Questions {
			if names[name] {
				t.Errorf("%q was asked in two calls", name)
			}
			names[name] = true
		}
	}
	return names
}

func TestVerdictsSplitAcrossCalls(t *testing.T) {
	answer := func(f *fake) {
		f.verdict["t0"] = 0.9
		f.where["t0"] = map[string]float64{"L002": 0.9}
		for i := 1; i < 4; i++ {
			f.verdict[fmt.Sprintf("t%d", i)] = 0.1
		}
	}

	j, f, windows := fixtureWith(t, tenetsConfig(4, 30000), "x := 1\ny := 2\n", nil)
	answer(f)
	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	findings, stats := out.Findings, out.Stats
	verdicts := f.named("verdict")
	if len(verdicts) != 2 {
		t.Fatalf("made %d verdict calls, want the questions split in two", len(verdicts))
	}
	for _, c := range verdicts {
		if over := requestTokens(t, c.State, c.Questions); over > jev.RequestBudget {
			t.Errorf("a call is %d tokens, over the %d budget", over, jev.RequestBudget)
		}
	}
	names := disjointNames(t, verdicts)
	for i := range 4 {
		if !names[fmt.Sprintf("verdict:t%d", i)] {
			t.Errorf("t%d was never asked about", i)
		}
	}
	if stats.Calls != 3 {
		t.Errorf("stats counted %d calls, want two verdict calls and one location call", stats.Calls)
	}

	// The same tenets without the criteria fit one call, and must reach the
	// same verdict.
	j, f, windows = fixtureWith(t, tenetsConfig(4, 0), "x := 1\ny := 2\n", nil)
	answer(f)
	oneCall, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	whole := oneCall.Findings
	if len(f.named("verdict")) != 1 {
		t.Fatalf("the cheap questions took %d calls", len(f.named("verdict")))
	}
	if len(findings) != len(whole) {
		t.Fatalf("split run found %#v, one call found %#v", findings, whole)
	}
	for i := range findings {
		if findings[i] != whole[i] {
			t.Errorf("split finding %#v, one call gave %#v", findings[i], whole[i])
		}
	}
}

func TestVerboseLineCountsTokensAndCalls(t *testing.T) {
	j, f, windows := fixtureWith(t, tenetsConfig(4, 30000), "x := 1\ny := 2\n", nil)
	for i := range 4 {
		f.verdict[fmt.Sprintf("t%d", i)] = 0.1
	}
	var logged []string
	j.Log = func(line string) { logged = append(logged, line) }

	if _, err := j.Run(context.Background(), windows); err != nil {
		t.Fatal(err)
	}
	if len(logged) != 1 {
		t.Fatalf("logged %q, want a line for the window's one round", logged)
	}
	asked := map[string]jev.Question{}
	for i := range 4 {
		asked[fmt.Sprintf("verdict:t%d", i)] = jev.Noul(judge.VerdictInstructions(j.Tenets[i]), strings.Repeat("a", 30000), "")
	}
	want := fmt.Sprintf("a.go:1 verdict for 4 tenets, ~%d estimated tokens, 2 calls, 200 input tokens",
		requestTokens(t, judge.State(windows[0]), asked))
	if logged[0] != want {
		t.Errorf("logged %q, want %q", logged[0], want)
	}
}

func TestLocationsSplitAcrossCalls(t *testing.T) {
	const tenetCount = 40
	j, f, windows := fixtureWith(t, tenetsConfig(tenetCount, 0), body(source.MaxWindowLines, 8), nil)
	for i := range tenetCount {
		id := fmt.Sprintf("t%d", i)
		f.verdict[id] = 0.9
		f.where[id] = map[string]float64{"L001": 0.9}
	}
	if len(windows) != 1 {
		t.Fatalf("got %d windows", len(windows))
	}

	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	findings := out.Findings
	if len(f.named("verdict")) != 1 {
		t.Errorf("the verdicts took %d calls, want one", len(f.named("verdict")))
	}
	locations := f.named("where")
	if len(locations) < 2 {
		t.Fatalf("made %d location calls, want the labels to have split them", len(locations))
	}
	for _, c := range locations {
		if over := requestTokens(t, c.State, c.Questions); over > jev.RequestBudget {
			t.Errorf("a call is %d tokens, over the %d budget", over, jev.RequestBudget)
		}
	}
	names := disjointNames(t, locations)
	if len(names) != tenetCount {
		t.Errorf("the location calls asked %d questions, want %d", len(names), tenetCount)
	}
	if len(findings) != tenetCount {
		t.Fatalf("got %d findings, want one per tenet", len(findings))
	}
	for _, got := range findings {
		if got.Line != 1 {
			t.Errorf("finding %#v is not on the line the model named", got)
		}
	}
}

func TestOversizeQuestionHalvesTheWindow(t *testing.T) {
	// 203 lines of 200 characters is a full window, and ten of them give the
	// workers more than one window each to halve.
	j, f, windows := fixtureWith(t, tenetsConfig(1, 59500), body(10*203, 200), nil)
	j.Concurrency = 0
	f.verdict["t0"] = 0.9
	f.where["t0"] = map[string]float64{"L001": 0.9}
	// The windows are halved by workers running at once, so the log they share
	// needs a lock of its own.
	var mu sync.Mutex
	var logged []string
	j.Log = func(line string) {
		mu.Lock()
		defer mu.Unlock()
		logged = append(logged, line)
	}
	if len(windows) <= judge.DefaultConcurrency {
		t.Fatalf("got %d windows, want more than the %d workers", len(windows), judge.DefaultConcurrency)
	}
	whole := map[string]jev.Question{"verdict:t0": jev.Noul("", strings.Repeat("a", 59500), "")}
	if requestTokens(t, judge.State(windows[0]), whole) <= jev.RequestBudget {
		t.Fatal("the fixture fits the budget, so it cannot exercise halving")
	}

	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	findings, stats := out.Findings, out.Stats
	if len(findings) != 2*len(windows) {
		t.Fatalf("got %d findings, want one per half of %d windows", len(findings), len(windows))
	}
	for i, w := range windows {
		first, second := findings[2*i], findings[2*i+1]
		if first.Line != w.First || second.Line != w.First+len(w.Lines)/2 {
			t.Errorf("window %d gave findings on lines %d and %d, want %d and %d",
				i, first.Line, second.Line, w.First, w.First+len(w.Lines)/2)
		}
	}
	if stats.Calls != 4*len(windows) {
		t.Errorf("made %d calls, want a verdict and a location call per half", stats.Calls)
	}
	for _, c := range f.calls {
		if over := requestTokens(t, c.State, c.Questions); over > jev.RequestBudget {
			t.Errorf("a call is %d tokens, over the %d budget", over, jev.RequestBudget)
		}
	}
	var halved bool
	for _, line := range logged {
		if strings.Contains(line, "in halves") {
			halved = true
		}
	}
	if !halved {
		t.Errorf("nothing explained the halving: %q", logged)
	}
}
