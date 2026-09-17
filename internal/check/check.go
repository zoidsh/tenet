package check

import (
	"context"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/zoidsh/tenetlint/internal/cache"
	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/judge"
	"github.com/zoidsh/tenetlint/internal/source"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

// Question names within one example's call. Both are asked together so that
// an example costs one call however much is wanted from it.
const (
	verdictQuestion  = "verdict"
	locationQuestion = "where"
)

// Stats is what a check run cost.
type Stats struct {
	Examples    int
	Calls       int
	CacheHits   int
	InputTokens int
	CostUSD     float64
	Duration    time.Duration
}

// Checker judges every example of every tenet it is given.
type Checker struct {
	Asker       judge.Asker
	Cache       *cache.Cache
	Concurrency int
	MinExamples int

	// Log, when set, receives a line per call made.
	Log func(string)
}

// Run judges the examples and measures each tenet. Any API error abandons the
// run, as it does in a lint: half the examples say nothing about a tenet.
func (c *Checker) Run(ctx context.Context, ts []*tenets.Tenet) ([]Result, Stats, error) {
	started := time.Now()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	concurrency := c.Concurrency
	if concurrency <= 0 {
		concurrency = judge.DefaultConcurrency
	}
	minExamples := c.MinExamples
	if minExamples <= 0 {
		minExamples = DefaultMinExamples
	}

	judgeds := make([][]Judged, len(ts))
	var (
		mu       sync.Mutex
		stats    Stats
		firstErr error
		wg       sync.WaitGroup
	)
	slots := make(chan struct{}, concurrency)

	for i, t := range ts {
		judgeds[i] = make([]Judged, len(t.Examples))
		for k, e := range t.Examples {
			stats.Examples++
			wg.Add(1)
			slots <- struct{}{}
			go func(t *tenets.Tenet, e tenets.Example, out *Judged) {
				defer wg.Done()
				defer func() { <-slots }()
				judged, s, err := c.example(ctx, t, e)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
						cancel()
					}
					return
				}
				*out = judged
				stats.Calls += s.Calls
				stats.CacheHits += s.CacheHits
				stats.InputTokens += s.InputTokens
				stats.CostUSD += s.CostUSD
			}(t, e, &judgeds[i][k])
		}
	}
	wg.Wait()

	if firstErr != nil {
		return nil, Stats{}, firstErr
	}
	results := make([]Result, len(ts))
	for i, t := range ts {
		results[i] = Measure(t, judgeds[i], minExamples)
	}
	stats.Duration = time.Since(started)
	return results, stats, nil
}

func (c *Checker) example(ctx context.Context, t *tenets.Tenet, e tenets.Example) (Judged, Stats, error) {
	var stats Stats
	lines := e.CodeLines()
	state := judge.StateOf(exampleLang(t, e), exampleFile(t, e), lines)
	key := cache.Key(state, t.Hash())

	judged := Judged{Example: e}
	var located string
	entry, cached := c.Cache.Get(key)
	if cached {
		judged.Prob, located = entry.Prob, entry.Line
		stats.CacheHits++
	}

	questions := map[string]jev.Question{}
	if !cached {
		questions[verdictQuestion] = judge.VerdictQuestion(t)
	}
	if e.Lines.Set() && located == "" {
		q, err := judge.LocationQuestion(t, len(lines))
		if err != nil {
			return Judged{}, stats, err
		}
		questions[locationQuestion] = q
	}

	if len(questions) > 0 {
		resp, err := c.Asker.Ask(ctx, state, questions)
		if err != nil {
			return Judged{}, stats, err
		}
		stats.Calls++
		stats.InputTokens += resp.Usage.InputTokens
		stats.CostUSD += jev.Cost(resp.Usage)
		if c.Log != nil {
			c.Log(fmt.Sprintf("%s example %q, %d questions, %d input tokens", t.ID, firstLine(e), len(questions), resp.Usage.InputTokens))
		}
		if answer, ok := resp.Answers[verdictQuestion]; ok {
			judged.Prob = answer.Prob()
		}
		if answer, ok := resp.Answers[locationQuestion]; ok {
			located = judge.TopLine(answer)
		}
		if located != "" {
			c.Cache.PutLocation(key, judged.Prob, located)
		} else {
			c.Cache.PutVerdict(key, judged.Prob)
		}
	}

	if n, ok := judge.ParseLineID(located); ok && n <= len(lines) {
		judged.Line = n
	}
	return judged, stats, nil
}

func firstLine(e tenets.Example) string { return strings.TrimSpace(e.CodeLines()[0]) }

// exampleLang is the language the model is told the example is written in.
func exampleLang(t *tenets.Tenet, e tenets.Example) string {
	if e.Lang != "" {
		return e.Lang
	}
	if len(t.Include) > 0 {
		return source.LanguageForPath(t.Include[0])
	}
	return "text"
}

// exampleFile is the file name the example is shown under. It is taken from
// the tenet's own include glob, so that a tenet scoped to test files shows a
// test file name, which is part of what the model reads the code as.
func exampleFile(t *tenets.Tenet, e tenets.Example) string {
	if e.Lang == "" && len(t.Include) > 0 {
		name := strings.ReplaceAll(path.Base(t.Include[0]), "*", "example")
		if path.Ext(name) != "" {
			return name
		}
	}
	return "example" + source.ExtensionFor(exampleLang(t, e))
}
