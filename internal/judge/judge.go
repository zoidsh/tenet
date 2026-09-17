// Package judge asks the model about every window and turns its answers into
// findings.
package judge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zoidsh/tenetlint/internal/cache"
	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/source"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

// DefaultConcurrency is how many windows are in flight at once.
const DefaultConcurrency = 8

// NoneLabel is the escape hatch the location question always offers, so that
// the model is never forced to name a line.
const NoneLabel = "none"

// Asker is the part of the jev client the judge needs, so that tests can
// answer without the network.
type Asker interface {
	Ask(ctx context.Context, state string, questions map[string]jev.Question) (*jev.Response, error)
}

// Finding is one violation. Fail is the cutoff it was judged against, so that
// a reader of the JSON can see how close the call was without the config.
type Finding struct {
	File        string  `json:"file"`
	Line        int     `json:"line"`
	Tenet       string  `json:"tenet"`
	Probability float64 `json:"probability"`
	Fail        float64 `json:"fail"`
	Message     string  `json:"message"`
}

// NearBand is how far under its cutoff a verdict still counts as a near miss.
const NearBand = 0.2

// NearMiss is a tenet that scored just under its cutoff on a window. It is no
// finding, and it is the only evidence a user tuning a cutoff has.
type NearMiss struct {
	File        string
	Line        int
	Tenet       string
	Probability float64
	Fail        float64
}

// Outcome is what one run decided.
type Outcome struct {
	Findings   []Finding
	NearMisses []NearMiss
	Stats      Stats
}

// Stats is what a run cost.
type Stats struct {
	Files       int
	Windows     int
	Calls       int
	CacheHits   int
	InputTokens int
	CostUSD     float64
	Duration    time.Duration
}

// Judge runs the tenets over windows.
type Judge struct {
	Asker       Asker
	Tenets      []*tenets.Tenet
	Cache       *cache.Cache
	Concurrency int

	// Log, when set, receives a line per round of questions asked.
	Log func(string)
}

// State is what the model is shown: the window's lines, each under the id the
// location question will answer with.
func State(w *source.Window) string {
	return StateOf(w.File.Kind(), w.File.Language(), w.Path(), w.Lines)
}

func lineID(n int) string { return fmt.Sprintf("L%03d", n) }

// ParseLineID reads the line a location answer names.
func ParseLineID(label string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(label, "L"))
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// VerdictInstructions and LocationInstructions carry the whole rule, because
// the model is given no other memory of what it is judging.
func VerdictInstructions(t *tenets.Tenet) string {
	return "Rule: " + t.Tenet + " Does this code violate the rule?"
}

// LocationInstructions asks where the violation is.
func LocationInstructions(t *tenets.Tenet) string {
	return "Rule: " + t.Tenet + " Which line most clearly violates the rule? Pick none if no line does."
}

// Run judges every window and returns the findings in file, line and tenet
// order. Any API error abandons the run: a partial verdict is worse than none.
func (j *Judge) Run(ctx context.Context, windows []*source.Window) (Outcome, error) {
	started := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	concurrency := j.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}

	var (
		mu       sync.Mutex
		findings []Finding
		near     []NearMiss
		stats    Stats
		firstErr error
		wg       sync.WaitGroup
	)
	slots := make(chan struct{}, concurrency)
	files := map[string]bool{}

	for _, w := range windows {
		files[w.Path()] = true
		wg.Add(1)
		slots <- struct{}{}
		go func(w *source.Window) {
			defer wg.Done()
			defer func() { <-slots }()
			found, missed, s, err := j.window(ctx, w)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			findings = append(findings, found...)
			near = append(near, missed...)
			stats.Calls += s.Calls
			stats.CacheHits += s.CacheHits
			stats.InputTokens += s.InputTokens
			stats.CostUSD += s.CostUSD
		}(w)
	}
	wg.Wait()

	if firstErr != nil {
		return Outcome{}, firstErr
	}
	stats.Files = len(files)
	stats.Windows = len(windows)
	stats.Duration = time.Since(started)
	sortFindings(findings)
	sort.Slice(near, func(a, b int) bool {
		x, y := near[a], near[b]
		if x.File != y.File {
			return x.File < y.File
		}
		if x.Line != y.Line {
			return x.Line < y.Line
		}
		return x.Tenet < y.Tenet
	})
	return Outcome{Findings: findings, NearMisses: near, Stats: stats}, nil
}

// Cached counts the windows a run would not have to ask about at all, for the
// progress line, at the price of hashing every window before judging it.
func (j *Judge) Cached(windows []*source.Window) int {
	var cached int
	for _, w := range windows {
		state := State(w)
		asked, all := 0, true
		for _, t := range j.Tenets {
			if !t.Applies(w.Path()) || w.File.Sup.File(t.ID) {
				continue
			}
			asked++
			if _, ok := j.Cache.Get(cache.Key(state, t.Hash())); !ok {
				all = false
				break
			}
		}
		if asked == 0 || all {
			cached++
		}
	}
	return cached
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(a, b int) bool {
		x, y := findings[a], findings[b]
		if x.File != y.File {
			return x.File < y.File
		}
		if x.Line != y.Line {
			return x.Line < y.Line
		}
		return x.Tenet < y.Tenet
	})
}

// pending is one tenet's state while its window is judged.
type pending struct {
	tenet *tenets.Tenet
	key   string
	prob  float64
	line  string
	asked bool
}

func (j *Judge) window(ctx context.Context, w *source.Window) ([]Finding, []NearMiss, Stats, error) {
	var stats Stats
	findings, near, err := j.judge(ctx, w, &stats)
	return findings, near, stats, err
}

func (j *Judge) judge(ctx context.Context, w *source.Window, stats *Stats) ([]Finding, []NearMiss, error) {
	state := State(w)

	var work []*pending
	for _, t := range j.Tenets {
		if !t.Applies(w.Path()) || w.File.Sup.File(t.ID) {
			continue
		}
		p := &pending{tenet: t, key: cache.Key(state, t.Hash())}
		if e, ok := j.Cache.Get(p.key); ok {
			p.prob, p.line, p.asked = e.Prob, e.Line, true
			stats.CacheHits++
		}
		work = append(work, p)
	}
	if len(work) == 0 {
		return nil, nil, nil
	}

	if err := j.askVerdicts(ctx, w, state, work, stats); err != nil {
		return j.inHalves(ctx, w, stats, err)
	}
	if err := j.askLocations(ctx, w, state, work, stats); err != nil {
		return j.inHalves(ctx, w, stats, err)
	}

	var findings []Finding
	var near []NearMiss
	for _, p := range work {
		cutoff := p.tenet.FailValue()
		if p.prob < cutoff {
			if p.prob >= cutoff-NearBand {
				near = append(near, NearMiss{
					File:        w.Path(),
					Line:        w.First,
					Tenet:       p.tenet.ID,
					Probability: p.prob,
					Fail:        cutoff,
				})
			}
			continue
		}
		id, ok := ParseLineID(p.line)
		if !ok || id > len(w.Lines) {
			continue
		}
		line := w.Line(id)
		if !w.File.Reportable(line) || w.File.Sup.Line(line, p.tenet.ID) {
			continue
		}
		findings = append(findings, Finding{
			File:        w.Path(),
			Line:        line,
			Tenet:       p.tenet.ID,
			Probability: p.prob,
			Fail:        cutoff,
			Message:     p.tenet.Tenet,
		})
	}
	return findings, near, nil
}

// inHalves judges the window in two, which is the only way left when a single
// question and the window together are over budget. Any answer the window
// already had is dropped: the halves ask about a different state, so nothing
// carries over.
func (j *Judge) inHalves(ctx context.Context, w *source.Window, stats *Stats, err error) ([]Finding, []NearMiss, error) {
	if !errors.Is(err, errOversize) {
		return nil, nil, err
	}
	if len(w.Lines) < 2 {
		return nil, nil, fmt.Errorf("%s:%d: %w", w.Path(), w.First, err)
	}
	j.logf("%s:%d %v, judging its %d lines in halves", w.Path(), w.First, err, len(w.Lines))

	var findings []Finding
	var near []NearMiss
	for _, half := range halves(w) {
		found, missed, err := j.judge(ctx, half, stats)
		if err != nil {
			return nil, nil, err
		}
		findings = append(findings, found...)
		near = append(near, missed...)
	}
	return findings, near, nil
}

func halves(w *source.Window) []*source.Window {
	mid := len(w.Lines) / 2
	return []*source.Window{
		{File: w.File, First: w.First, Lines: w.Lines[:mid]},
		{File: w.File, First: w.First + mid, Lines: w.Lines[mid:]},
	}
}

func (j *Judge) askVerdicts(ctx context.Context, w *source.Window, state string, work []*pending, stats *Stats) error {
	questions := map[string]jev.Question{}
	for _, p := range work {
		if p.asked {
			continue
		}
		questions["verdict:"+p.tenet.ID] = VerdictQuestion(p.tenet)
	}
	if len(questions) == 0 {
		return nil
	}
	resp, err := j.ask(ctx, w, "verdict", state, questions, stats)
	if err != nil {
		return err
	}
	for _, p := range work {
		answer, ok := resp.Answers["verdict:"+p.tenet.ID]
		if !ok {
			continue
		}
		p.prob = answer.Prob()
		j.Cache.PutVerdict(p.key, p.prob)
	}
	return nil
}

func (j *Judge) askLocations(ctx context.Context, w *source.Window, state string, work []*pending, stats *Stats) error {
	questions := map[string]jev.Question{}
	var asking []*pending
	for _, p := range work {
		if p.prob < p.tenet.FailValue() || p.line != "" {
			continue
		}
		q, err := LocationQuestion(p.tenet, len(w.Lines))
		if err != nil {
			return err
		}
		questions["where:"+p.tenet.ID] = q
		asking = append(asking, p)
	}
	if len(questions) == 0 {
		return nil
	}
	resp, err := j.ask(ctx, w, "where", state, questions, stats)
	if err != nil {
		return err
	}
	for _, p := range asking {
		answer, ok := resp.Answers["where:"+p.tenet.ID]
		if !ok {
			continue
		}
		p.line = TopLine(answer)
		if p.line != "" {
			j.Cache.PutLocation(p.key, p.prob, p.line)
		}
	}
	return nil
}

// TopLine is the most probable line, read from the distribution with none
// taken out: the verdict has already decided that the window violates the
// rule, so the question left is only which line shows it best.
func TopLine(a jev.Answer) string {
	trimmed := jev.Answer{Probabilities: map[string]float64{}}
	for label, p := range a.Probabilities {
		if label == NoneLabel {
			continue
		}
		trimmed.Probabilities[label] = p
	}
	label, _ := trimmed.Top()
	return label
}

// ask answers every question, over as many calls as the token budget needs,
// and merges the answers as if one call had been made.
func (j *Judge) ask(ctx context.Context, w *source.Window, kind, state string, questions map[string]jev.Question, stats *Stats) (*jev.Response, error) {
	groups, tokens, err := split(state, questions)
	if err != nil {
		return nil, err
	}
	merged := &jev.Response{Answers: make(map[string]jev.Answer, len(questions))}
	for _, group := range groups {
		resp, err := j.Asker.Ask(ctx, state, group)
		if err != nil {
			return nil, err
		}
		stats.Calls++
		stats.InputTokens += resp.Usage.InputTokens
		stats.CostUSD += jev.Cost(resp.Usage)
		merged.Usage.InputTokens += resp.Usage.InputTokens
		merged.Usage.OutputTokens += resp.Usage.OutputTokens
		for name, answer := range resp.Answers {
			merged.Answers[name] = answer
		}
	}
	j.logf("%s:%d %s for %d tenets, ~%d estimated tokens, %d calls, %d input tokens",
		w.Path(), w.First, kind, len(questions), tokens, len(groups), merged.Usage.InputTokens)
	return merged, nil
}

// errOversize says that no grouping of the questions can fit the window into
// one request, which leaves only a smaller window.
var errOversize = errors.New("does not fit the request budget")

// split partitions the questions into requests that each stay within the
// budget, in name order so that the same window always splits the same way. It
// also reports what the questions are worth together.
func split(state string, questions map[string]jev.Question) ([]map[string]jev.Question, int, error) {
	base := jev.EstimateTokens(state)
	names := make([]string, 0, len(questions))
	for name := range questions {
		names = append(names, name)
	}
	sort.Strings(names)

	var groups []map[string]jev.Question
	group := map[string]jev.Question{}
	total, tokens := base, base
	for _, name := range names {
		cost, err := jev.QuestionTokens(name, questions[name])
		if err != nil {
			return nil, 0, err
		}
		if base+cost > jev.RequestBudget {
			return nil, 0, fmt.Errorf("%q alone %w: about %d tokens against %d", name, errOversize, base+cost, jev.RequestBudget)
		}
		if len(group) > 0 && tokens+cost > jev.RequestBudget {
			groups = append(groups, group)
			group, tokens = map[string]jev.Question{}, base
		}
		group[name] = questions[name]
		tokens += cost
		total += cost
	}
	return append(groups, group), total, nil
}

func (j *Judge) logf(format string, args ...any) {
	if j.Log != nil {
		j.Log(fmt.Sprintf(format, args...))
	}
}
