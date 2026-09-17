// The Asker interface exists so that the judge can be exercised without a key
// or the network, so these tests answer from a table instead: the substitute
// is the point of the seam, not a way around a real dependency.
// tenet:ignore-file no-mocking
package judge_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/zoidsh/tenet/internal/cache"
	"github.com/zoidsh/tenet/internal/jev"
	"github.com/zoidsh/tenet/internal/judge"
	"github.com/zoidsh/tenet/internal/source"
	"github.com/zoidsh/tenet/internal/tenets"
)

// call is one request the fake asker was given.
type call struct {
	State     string
	Questions map[string]jev.Question
}

type fake struct {
	mu      sync.Mutex
	calls   []call
	verdict map[string]float64
	where   map[string]map[string]float64
	err     error
}

func (f *fake) Ask(_ context.Context, state string, questions map[string]jev.Question) (*jev.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.calls = append(f.calls, call{State: state, Questions: questions})
	answers := map[string]jev.Answer{}
	for name := range questions {
		id := name[strings.Index(name, ":")+1:]
		if strings.HasPrefix(name, "verdict:") {
			answers[name] = jev.Answer{Type: jev.KindNoul, Noul: f.verdict[id]}
			continue
		}
		answers[name] = jev.Answer{Type: jev.KindChoice, Probabilities: f.where[id]}
	}
	return &jev.Response{Model: "jev-1.13.0", Answers: answers, Usage: jev.Usage{InputTokens: 100}}, nil
}

func (f *fake) named(kind string) []call {
	var out []call
	for _, c := range f.calls {
		for name := range c.Questions {
			if strings.HasPrefix(name, kind+":") {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

const config = `
version: 1
model: jev-1.13.0
tenets:
  - id: comment-why
    tenet: A comment says why.
    criteria:
      true: It restates the code.
      false: It gives a reason.
  - id: no-fallback
    tenet: No silent fallbacks.
`

func fixture(t *testing.T, body string, reportable map[int]bool) (*judge.Judge, *fake, []*source.Window) {
	t.Helper()
	return fixtureWith(t, config, body, reportable)
}

func fixtureWith(t *testing.T, config, body string, reportable map[int]bool) (*judge.Judge, *fake, []*source.Window) {
	t.Helper()
	cfg, err := tenets.Parse([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, t := range cfg.Tenets {
		known[t.ID] = true
	}
	file, err := source.NewFile("a.go", []byte(body), reportable, known)
	if err != nil {
		t.Fatal(err)
	}
	var windows []*source.Window
	for _, w := range file.Windows() {
		if w.HasReportable() {
			windows = append(windows, w)
		}
	}
	f := &fake{verdict: map[string]float64{}, where: map[string]map[string]float64{}}
	j := &judge.Judge{Asker: f, Tenets: cfg.Tenets, Concurrency: 1}
	return j, f, windows
}

func TestVerdictQuestionsCarryTheRule(t *testing.T) {
	j, f, windows := fixture(t, "x := 1\ny := 2\n", nil)
	f.verdict["comment-why"] = 0.1
	f.verdict["no-fallback"] = 0.1

	if _, err := j.Run(context.Background(), windows); err != nil {
		t.Fatal(err)
	}
	calls := f.named("verdict")
	if len(calls) != 1 {
		t.Fatalf("made %d verdict calls", len(calls))
	}
	c := calls[0]
	if !strings.HasPrefix(c.State, "Language: go. File: a.go. Source file excerpt:\n") {
		t.Errorf("state header is %q", c.State)
	}
	if !strings.Contains(c.State, "L001 x := 1\nL002 y := 2\n") {
		t.Errorf("state body is %q", c.State)
	}
	q, ok := c.Questions["verdict:comment-why"]
	if !ok {
		t.Fatalf("questions are %v", c.Questions)
	}
	if q.Type != jev.KindNoul {
		t.Errorf("question type is %q", q.Type)
	}
	if q.Instructions != "Rule: A comment says why. Does this code violate the rule?" {
		t.Errorf("instructions are %q", q.Instructions)
	}
	crit, ok := q.Criteria.(jev.NoulCriteria)
	if !ok || crit.True != "It restates the code." || crit.False != "It gives a reason." {
		t.Errorf("criteria are %#v", q.Criteria)
	}
	if _, ok := f.calls[0].Questions["verdict:no-fallback"]; !ok {
		t.Error("the two tenets did not share one call")
	}
	if len(f.named("where")) != 0 {
		t.Error("a location was asked for although no verdict passed")
	}
}

func TestLocationAskedOnlyForFindings(t *testing.T) {
	j, f, windows := fixture(t, "x := 1\ny := 2\n", nil)
	f.verdict["comment-why"] = 0.9
	f.verdict["no-fallback"] = 0.4
	f.where["comment-why"] = map[string]float64{"L001": 0.2, "L002": 0.5, "none": 0.9}

	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	findings, stats := out.Findings, out.Stats
	calls := f.named("where")
	if len(calls) != 1 {
		t.Fatalf("made %d location calls", len(calls))
	}
	if _, ok := calls[0].Questions["where:no-fallback"]; ok {
		t.Error("a location was asked for a tenet that found nothing")
	}
	q := calls[0].Questions["where:comment-why"]
	if q.Instructions != "Rule: A comment says why. Which line most clearly violates the rule? Pick none if no line does." {
		t.Errorf("instructions are %q", q.Instructions)
	}
	labels, ok := q.Criteria.(map[string]any)
	if !ok {
		t.Fatalf("labels are %#v", q.Criteria)
	}
	if len(labels) != 3 || labels["none"] != "no line violates the rule" {
		t.Errorf("labels are %#v", labels)
	}
	if _, ok := labels["L002"]; !ok {
		t.Error("a line has no label")
	}

	if len(findings) != 1 {
		t.Fatalf("got %d findings: %#v", len(findings), findings)
	}
	got := findings[0]
	// none holds the highest probability, but the verdict has already settled
	// that the window violates the rule.
	if got.Line != 2 || got.Tenet != "comment-why" {
		t.Errorf("finding is %#v", got)
	}
	if got.Probability != 0.9 || got.Fail != tenets.DefaultFail {
		t.Errorf("finding probability is %#v", got)
	}
	if got.Message != "A comment says why." {
		t.Errorf("finding fields are %#v", got)
	}
	if stats.Calls != 2 || stats.Windows != 1 || stats.Files != 1 || stats.InputTokens != 200 {
		t.Errorf("stats are %#v", stats)
	}
	if stats.CostUSD <= 0 || stats.Duration <= 0 {
		t.Errorf("stats are %#v", stats)
	}
}

func TestLocationIsTheTopLine(t *testing.T) {
	j, f, windows := fixture(t, "x := 1\ny := 2\n", nil)
	f.verdict["comment-why"] = 0.85
	f.verdict["no-fallback"] = 0
	f.where["comment-why"] = map[string]float64{"L001": 0.1, "L002": 0.7}

	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	findings := out.Findings
	if len(findings) != 1 || findings[0].Line != 2 {
		t.Fatalf("findings are %#v", findings)
	}
}

// The cutoff is where the tenet says it is, and a verdict that lands exactly
// on it is a finding: fail is what fails, not what nearly does.
func TestTheCutoffIsInclusive(t *testing.T) {
	cases := []struct {
		prob float64
		want bool
	}{
		{tenets.DefaultFail - 0.01, false},
		{tenets.DefaultFail, true},
		{tenets.DefaultFail + 0.01, true},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%.2f", c.prob), func(t *testing.T) {
			j, f, windows := fixture(t, "x := 1\ny := 2\n", nil)
			f.verdict["comment-why"] = c.prob
			f.verdict["no-fallback"] = 0
			f.where["comment-why"] = map[string]float64{"L001": 0.9}

			out, err := j.Run(context.Background(), windows)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(out.Findings) == 1; got != c.want {
				t.Errorf("p=%.2f gave %d findings, want %v", c.prob, len(out.Findings), c.want)
			}
		})
	}
}

func TestNearMissesAreCollected(t *testing.T) {
	j, f, windows := fixture(t, "x := 1\ny := 2\n", nil)
	f.verdict["comment-why"] = tenets.DefaultFail - 0.05
	f.verdict["no-fallback"] = tenets.DefaultFail - judge.NearBand - 0.01

	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Findings) != 0 {
		t.Fatalf("findings are %#v", out.Findings)
	}
	if len(out.NearMisses) != 1 {
		t.Fatalf("near misses are %#v", out.NearMisses)
	}
	got := out.NearMisses[0]
	if got.Tenet != "comment-why" || got.File != "a.go" || got.Line != 1 {
		t.Errorf("near miss is %#v", got)
	}
	if got.Probability != tenets.DefaultFail-0.05 || got.Fail != tenets.DefaultFail {
		t.Errorf("near miss numbers are %#v", got)
	}
}

func TestCacheHitsSkipCalls(t *testing.T) {
	dir := t.TempDir()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	j, f, windows := fixture(t, "x := 1\ny := 2\n", nil)
	j.Cache = c
	f.verdict["comment-why"] = 0.9
	f.verdict["no-fallback"] = 0.1
	f.where["comment-why"] = map[string]float64{"L001": 0.9}

	first, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	if first.Stats.Calls != 2 || first.Stats.CacheHits != 0 {
		t.Fatalf("first run stats are %#v", first.Stats)
	}

	f.calls = nil
	second, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	stats := second.Stats
	if stats.Calls != 0 {
		t.Errorf("second run made %d calls", stats.Calls)
	}
	if stats.CacheHits != 2 {
		t.Errorf("second run hit the cache %d times", stats.CacheHits)
	}
	if len(second.Findings) != len(first.Findings) || second.Findings[0] != first.Findings[0] {
		t.Errorf("cached run gave %#v, want %#v", second.Findings, first.Findings)
	}
}

func TestSuppressedAndUnreportableLinesAreDropped(t *testing.T) {
	j, f, windows := fixture(t, "x := 1 // tenet\x3aignore comment-why\ny := 2\n", nil)
	f.verdict["comment-why"] = 0.9
	f.verdict["no-fallback"] = 0.9
	f.where["comment-why"] = map[string]float64{"L001": 0.9}
	f.where["no-fallback"] = map[string]float64{"L001": 0.9}

	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	findings := out.Findings
	if len(findings) != 1 || findings[0].Tenet != "no-fallback" {
		t.Fatalf("findings are %#v", findings)
	}

	j, f, windows = fixture(t, "x := 1\ny := 2\n", map[int]bool{2: true})
	f.verdict["comment-why"] = 0.9
	f.verdict["no-fallback"] = 0
	f.where["comment-why"] = map[string]float64{"L001": 0.9}
	out, err = j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	findings = out.Findings
	if len(findings) != 0 {
		t.Fatalf("a finding on an unchanged line survived: %#v", findings)
	}
}

// A directive on one line must not shadow a second violation of the same
// tenet in the same window, which it would if the location landed on the line
// the directive exempts.
func TestAnExemptLineIsNotOffered(t *testing.T) {
	j, f, windows := fixture(t, "x := 1 // tenet\x3aignore comment-why\ny := 2\n", nil)
	f.verdict["comment-why"] = 0.9
	f.verdict["no-fallback"] = 0.1
	f.where["comment-why"] = map[string]float64{"L002": 0.6, "none": 0.4}

	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	q := f.named("where")[0].Questions["where:comment-why"]
	labels, ok := q.Criteria.(map[string]any)
	if !ok {
		t.Fatalf("labels are %#v", q.Criteria)
	}
	if _, offered := labels["L001"]; offered {
		t.Errorf("the exempt line was offered: %#v", labels)
	}
	if len(labels) != 2 {
		t.Errorf("labels are %#v", labels)
	}
	findings := out.Findings
	if len(findings) != 1 || findings[0].Line != 2 || findings[0].Tenet != "comment-why" {
		t.Fatalf("findings are %#v", findings)
	}
}

// The sibling of the case above: with a line held back, none is an answer
// again, and the model taking it means the violation is on the exempt line.
func TestNoneOverTheOpenLinesIsNoFinding(t *testing.T) {
	j, f, windows := fixture(t, "x := 1 // tenet\x3aignore comment-why\ny := 2\n", nil)
	f.verdict["comment-why"] = 0.9
	f.verdict["no-fallback"] = 0.1
	f.where["comment-why"] = map[string]float64{"L002": 0.3, "none": 0.7}

	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Findings) != 0 {
		t.Errorf("findings are %#v", out.Findings)
	}
}

// A location cached by a run that could report on the whole file is no answer
// for a run whose diff touched one line of it.
func TestACachedLineOutsideTheOpenSetIsAskedAgain(t *testing.T) {
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const body = "x := 1\ny := 2\n"

	whole, first, windows := fixture(t, body, nil)
	whole.Cache = store
	first.verdict["comment-why"] = 0.9
	first.verdict["no-fallback"] = 0.1
	first.where["comment-why"] = map[string]float64{"L001": 0.9}
	out, err := whole.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Findings) != 1 || out.Findings[0].Line != 1 {
		t.Fatalf("the first run found %#v", out.Findings)
	}

	changed, second, windows := fixture(t, body, map[int]bool{2: true})
	changed.Cache = store
	second.verdict["comment-why"] = 0.9
	second.verdict["no-fallback"] = 0.1
	second.where["comment-why"] = map[string]float64{"L002": 0.9}
	out, err = changed.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.named("where")) != 1 {
		t.Errorf("the location was not asked again: %d calls", len(second.named("where")))
	}
	if len(second.named("verdict")) != 0 {
		t.Errorf("the cached verdict was asked again: %d calls", len(second.named("verdict")))
	}
	if len(out.Findings) != 1 || out.Findings[0].Line != 2 {
		t.Errorf("findings are %#v", out.Findings)
	}
}

// With every line of the window exempt there is nothing left to report on, so
// the tenet costs no call at all.
func TestATenetExemptOnEveryLineIsNotAsked(t *testing.T) {
	j, f, windows := fixture(t, "x := 1 // tenet\x3aignore comment-why\n", nil)
	f.verdict["no-fallback"] = 0.1

	if _, err := j.Run(context.Background(), windows); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.calls[0].Questions["verdict:comment-why"]; ok {
		t.Error("a tenet with no line left to report on was still asked about")
	}
}

func TestFileSuppressionSkipsTheTenetEntirely(t *testing.T) {
	j, f, windows := fixture(t, "// tenet\x3aignore-file comment-why\nx := 1\n", nil)
	f.verdict["no-fallback"] = 0.1

	if _, err := j.Run(context.Background(), windows); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.calls[0].Questions["verdict:comment-why"]; ok {
		t.Error("a file-suppressed tenet was still asked about")
	}
}

func TestRunAtDefaultConcurrency(t *testing.T) {
	// Every line differs, so that no window is a cache hit for another and the
	// count of calls is the same whatever order the workers run in.
	var b strings.Builder
	for i := range 12 * source.MaxWindowLines {
		fmt.Fprintf(&b, "x := %d\n", i)
	}
	body := b.String()
	j, f, windows := fixture(t, body, nil)
	j.Concurrency = 0
	c, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j.Cache = c
	var logged int
	var mu sync.Mutex
	j.Log = func(string) {
		mu.Lock()
		defer mu.Unlock()
		logged++
	}
	f.verdict["comment-why"] = 0.9
	f.verdict["no-fallback"] = 0.1
	f.where["comment-why"] = map[string]float64{"L001": 0.9}

	if len(windows) < judge.DefaultConcurrency+1 {
		t.Fatalf("got %d windows, want more than the %d workers", len(windows), judge.DefaultConcurrency)
	}
	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	findings, stats := out.Findings, out.Stats
	if len(findings) != len(windows) {
		t.Errorf("got %d findings from %d windows", len(findings), len(windows))
	}
	if stats.Calls != 2*len(windows) || logged != stats.Calls {
		t.Errorf("stats are %#v, logged %d", stats, logged)
	}
	for i := 1; i < len(findings); i++ {
		if findings[i-1].Line >= findings[i].Line {
			t.Fatalf("findings came back out of order: %d then %d", findings[i-1].Line, findings[i].Line)
		}
	}
}

func TestATenetIsNotAskedOfAnotherKind(t *testing.T) {
	j, f, windows := fixtureWith(t, `
version: 1
model: jev-1.13.0
tenets:
  - id: comment-why
    tenet: A comment says why.
    kind: [code]
  - id: plain-english
    tenet: Write plainly.
    kind: [prose]
`, "x := 1\n", nil)
	f.verdict["comment-why"] = 0.1
	f.verdict["plain-english"] = 0.9

	out, err := j.Run(context.Background(), windows)
	if err != nil {
		t.Fatal(err)
	}
	if out.Findings != nil {
		t.Errorf("a prose tenet found something in a Go file: %#v", out.Findings)
	}
	calls := f.named("verdict")
	if len(calls) != 1 {
		t.Fatalf("made %d verdict calls", len(calls))
	}
	if _, ok := calls[0].Questions["verdict:plain-english"]; ok {
		t.Errorf("the prose tenet was asked: %v", calls[0].Questions)
	}
	if _, ok := calls[0].Questions["verdict:comment-why"]; !ok {
		t.Errorf("the code tenet was not asked: %v", calls[0].Questions)
	}
}

func TestAPIErrorAbortsTheRun(t *testing.T) {
	j, f, windows := fixture(t, "x := 1\n", nil)
	f.err = errors.New("boom")

	out, err := j.Run(context.Background(), windows)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error is %v", err)
	}
	if out.Findings != nil {
		t.Errorf("partial findings survived: %#v", out.Findings)
	}
}
