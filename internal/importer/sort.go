package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/zoidsh/tenetlint/internal/cache"
	"github.com/zoidsh/tenetlint/internal/jev"
)

// ChunkSize is how many candidates one call asks about. Every candidate costs
// two questions and a line of state, so the chunk keeps a request within a
// size the model answers well.
const ChunkSize = 80

// Accept is the probability a candidate's checkable question has to reach to
// become a tenet.
const Accept = 0.5

// The kinds a sentence in a rule file can be. KindContext and KindOther never
// become tenets; the rest do when a diff is enough to judge them, because the
// kind question reads "never do X" as process even where X is plain in the
// changed lines.
const (
	KindCodeRule  = "code-rule"
	KindProcess   = "process"
	KindContext   = "context"
	KindNeedsRepo = "needs-repo"
	KindOther     = "other"
)

const stateHeader = "Sentences from a coding agent's instruction file. Decide, for each, what kind of instruction it is."

const (
	checkableTrue  = "The sentence names a property of code, comments, tests, names, error handling, dependencies or prose that is visible in the changed lines themselves."
	checkableFalse = "Deciding requires running commands, seeing other files, knowing history or intent, or the sentence is not a rule at all."
)

// cacheSalt stands in for a tenet hash in the cache key: the importer asks the
// same two questions of every sentence, so the only thing besides the sentence
// that changes what an answer means is the model. The version is part of it so
// that rewording the questions retires the old answers.
const cacheSalt = "tenetlint-importer-v1"

func kindLabels() map[string]any {
	return map[string]any{
		KindCodeRule:  "a rule about how code, comments, tests, or docs must be written, checkable by reading a change to the files",
		KindProcess:   "an instruction about what the agent should do, run, ask, or avoid during its work",
		KindContext:   "a description of the project, its layout, commands, or facts, not an instruction",
		KindNeedsRepo: "a rule about code that can only be checked with knowledge of other files, history, or the running system",
		KindOther:     "none of these",
	}
}

// KindInstructions and CheckableInstructions carry the sentence itself,
// because the state names every candidate at once and the model is told which
// one this question is about nowhere else.
func KindInstructions(text string) string {
	return fmt.Sprintf("Sentence: %q What kind of instruction is this?", text)
}

// CheckableInstructions asks whether a diff alone settles the rule.
func CheckableInstructions(text string) string {
	return fmt.Sprintf("Sentence: %q A reviewer reading only a diff of source files, with no ability to run anything and no knowledge of the rest of the repository, could decide whether a change violates this.", text)
}

// Asker is the part of the jev client the sort needs, so that tests can answer
// without the network.
type Asker interface {
	Ask(ctx context.Context, state string, questions map[string]jev.Question) (*jev.Response, error)
}

// Sorted is one candidate with what the model made of it.
type Sorted struct {
	Candidate
	ID            string  `json:"tenet,omitempty"`
	Kind          string  `json:"kind"`
	KindProb      float64 `json:"kind_p"`
	CheckableProb float64 `json:"checkable_p"`
	Accepted      bool    `json:"accepted"`
}

// Stats is what an import cost.
type Stats struct {
	Candidates  int
	Tenets      int
	Calls       int
	CacheHits   int
	InputTokens int
	CostUSD     float64
	Duration    time.Duration
}

// Sorter decides which candidates are code rules.
type Sorter struct {
	Asker Asker
	Cache *cache.Cache
	Model string

	// Log, when set, receives a line per call made.
	Log func(string)
}

// Sort asks about every candidate and returns them in the order they were
// given, each with its kind and the two probabilities the report prints.
func (s *Sorter) Sort(ctx context.Context, candidates []Candidate) ([]Sorted, Stats, error) {
	started := time.Now()
	stats := Stats{Candidates: len(candidates)}

	out := make([]Sorted, len(candidates))
	var pending []int
	for i, c := range candidates {
		out[i] = Sorted{Candidate: c}
		if e, ok := s.Cache.Get(s.key(c)); ok && e.Sorted() {
			out[i].Kind, out[i].KindProb, out[i].CheckableProb = e.Kind, e.KindProb, e.Prob
			out[i].Accepted = accepted(e.Kind, e.Prob)
			stats.CacheHits++
			continue
		}
		pending = append(pending, i)
	}

	for start := 0; start < len(pending); start += ChunkSize {
		end := min(start+ChunkSize, len(pending))
		if err := s.ask(ctx, out, pending[start:end], &stats); err != nil {
			return nil, Stats{}, err
		}
	}

	for _, c := range out {
		if c.Accepted {
			stats.Tenets++
		}
	}
	stats.Duration = time.Since(started)
	return out, stats, nil
}

func accepted(kind string, checkable float64) bool {
	return checkable >= Accept && kind != KindContext && kind != KindOther
}

func (s *Sorter) key(c Candidate) string {
	h := sha256.New()
	_, _ = io.WriteString(h, cacheSalt)
	_, _ = h.Write([]byte{0})
	_, _ = io.WriteString(h, s.Model)
	return cache.Key(c.Text, hex.EncodeToString(h.Sum(nil)))
}

func candidateID(n int) string { return fmt.Sprintf("C%03d", n) }

func (s *Sorter) ask(ctx context.Context, out []Sorted, chunk []int, stats *Stats) error {
	var state strings.Builder
	state.WriteString(stateHeader + "\n")
	questions := map[string]jev.Question{}
	for n, i := range chunk {
		id, c := candidateID(n+1), out[i].Candidate
		fmt.Fprintf(&state, "%s %s\n", id, line(c))
		kind, err := jev.Choice(KindInstructions(c.Text), kindLabels())
		if err != nil {
			return err
		}
		questions["kind:"+id] = kind
		questions["checkable:"+id] = jev.Noul(CheckableInstructions(c.Text), checkableTrue, checkableFalse)
	}

	resp, err := s.Asker.Ask(ctx, state.String(), questions)
	if err != nil {
		return err
	}
	stats.Calls++
	stats.InputTokens += resp.Usage.InputTokens
	stats.CostUSD += jev.Cost(resp.Usage)
	if s.Log != nil {
		s.Log(fmt.Sprintf("sorted %d candidates, %d input tokens", len(chunk), resp.Usage.InputTokens))
	}

	for n, i := range chunk {
		id := candidateID(n + 1)
		kind, err := answerTo(resp, "kind:"+id)
		if err != nil {
			return err
		}
		checkable, err := answerTo(resp, "checkable:"+id)
		if err != nil {
			return err
		}
		label, prob := kind.Top()
		out[i].Kind, out[i].KindProb, out[i].CheckableProb = label, prob, checkable.Prob()
		out[i].Accepted = accepted(label, checkable.Prob())
		s.Cache.PutSort(s.key(out[i].Candidate), label, prob, checkable.Prob())
	}
	return nil
}

// answerTo insists on an answer to every question asked: a sentence the model
// skipped would otherwise be silently left out of the draft.
func answerTo(resp *jev.Response, name string) (jev.Answer, error) {
	a, ok := resp.Answers[name]
	if !ok {
		return jev.Answer{}, fmt.Errorf("jev did not answer %s", name)
	}
	return a, nil
}

func line(c Candidate) string {
	if c.Heading == "" {
		return ":: " + c.Text
	}
	return c.Heading + " :: " + c.Text
}
