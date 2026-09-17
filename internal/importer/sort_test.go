// The Asker interface exists so that the sort can be exercised without a key
// or the network, so these tests answer from a table instead: the substitute
// is the point of the seam, not a way around a real dependency.
// tenet:ignore-file no-mocking
package importer_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/cache"
	"github.com/zoidsh/tenetlint/internal/importer"
	"github.com/zoidsh/tenetlint/internal/jev"
)

// answers is what the fake asker replies for one candidate id.
type answers struct {
	kind      map[string]float64
	checkable float64
}

type call struct {
	state     string
	questions map[string]jev.Question
}

type fake struct {
	// byText keys the answers by the sentence, because a candidate's id is
	// only meaningful within the call it was asked in.
	byText map[string]answers
	calls  []call

	// drop is a question the fake leaves unanswered.
	drop string
}

func (f *fake) Ask(_ context.Context, state string, questions map[string]jev.Question) (*jev.Response, error) {
	f.calls = append(f.calls, call{state: state, questions: questions})
	out := map[string]jev.Answer{}
	for name, q := range questions {
		if name == f.drop {
			continue
		}
		a, ok := f.answerFor(q.Instructions)
		if !ok {
			return nil, fmt.Errorf("no answer prepared for %q", q.Instructions)
		}
		if strings.HasPrefix(name, "kind:") {
			out[name] = jev.Answer{Type: jev.KindChoice, Probabilities: a.kind}
			continue
		}
		out[name] = jev.Answer{Type: jev.KindNoul, Noul: a.checkable}
	}
	return &jev.Response{Model: "jev-1.13.0", Answers: out, Usage: jev.Usage{InputTokens: 50}}, nil
}

// answerFor finds the prepared answer by looking for its sentence inside the
// instructions, which holds however the sentence is punctuated.
func (f *fake) answerFor(instructions string) (answers, bool) {
	for text, a := range f.byText {
		if strings.Contains(instructions, text) {
			return a, true
		}
	}
	return answers{}, false
}

func candidates(texts ...string) []importer.Candidate {
	out := make([]importer.Candidate, len(texts))
	for i, text := range texts {
		out[i] = importer.Candidate{File: "CLAUDE.md", Line: i + 1, Heading: "Rules", Text: text}
	}
	return out
}

func TestSortAsksBothQuestionsPerCandidate(t *testing.T) {
	const rule = "A comment says why, not what."
	const process = "Ask before installing anything."
	asker := &fake{byText: map[string]answers{
		rule:    {kind: map[string]float64{importer.KindCodeRule: 0.9, importer.KindProcess: 0.1}, checkable: 0.84},
		process: {kind: map[string]float64{importer.KindProcess: 0.8, importer.KindCodeRule: 0.2}, checkable: 0.1},
	}}
	sorter := &importer.Sorter{Asker: asker, Model: "jev-1.13.0"}

	got, stats, err := sorter.Sort(context.Background(), candidates(rule, process))
	if err != nil {
		t.Fatal(err)
	}
	if len(asker.calls) != 1 {
		t.Fatalf("made %d calls", len(asker.calls))
	}
	c := asker.calls[0]
	for _, name := range []string{"kind:C001", "checkable:C001", "kind:C002", "checkable:C002"} {
		if _, ok := c.questions[name]; !ok {
			t.Errorf("no question named %s in %v", name, names(c.questions))
		}
	}
	if len(c.questions) != 4 {
		t.Errorf("questions are %v", names(c.questions))
	}
	for name, q := range c.questions {
		id := strings.TrimPrefix(strings.TrimPrefix(name, "kind:"), "checkable:")
		want := rule
		if id == "C002" {
			want = process
		}
		if !strings.Contains(q.Instructions, want) {
			t.Errorf("%s does not carry its sentence: %q", name, q.Instructions)
		}
	}
	if !strings.HasPrefix(c.state, "Sentences from a coding agent's instruction file.") {
		t.Errorf("state starts %q", c.state)
	}
	if !strings.Contains(c.state, "C001 Rules :: "+rule) {
		t.Errorf("state is %q", c.state)
	}

	if got[0].Kind != importer.KindCodeRule || got[0].KindProb != 0.9 || got[0].CheckableProb != 0.84 || !got[0].Accepted {
		t.Errorf("rule sorted as %#v", got[0])
	}
	if got[1].Kind != importer.KindProcess || got[1].Accepted {
		t.Errorf("process sorted as %#v", got[1])
	}
	if got[1].Candidate != candidates(rule, process)[1] {
		t.Errorf("candidate lost: %#v", got[1].Candidate)
	}
	if stats.Candidates != 2 || stats.Tenets != 1 || stats.Calls != 1 || stats.InputTokens != 50 {
		t.Errorf("stats are %#v", stats)
	}
}

func names(questions map[string]jev.Question) []string {
	var out []string
	for name := range questions {
		out = append(out, name)
	}
	return out
}

func TestSortQuotesTheSentenceAsWritten(t *testing.T) {
	const text = "Say \"no\" to a `fallback\\default` path."
	asker := &fake{byText: map[string]answers{
		text: {kind: map[string]float64{importer.KindCodeRule: 0.9}, checkable: 0.8},
	}}
	if _, _, err := (&importer.Sorter{Asker: asker}).Sort(context.Background(), candidates(text)); err != nil {
		t.Fatal(err)
	}
	want := `Sentence: "` + text + `" What kind`
	if got := asker.calls[0].questions["kind:C001"].Instructions; !strings.HasPrefix(got, want) {
		t.Errorf("instructions are %q, want them to start %q", got, want)
	}
}

func TestSortAcceptsAtExactlyTheThreshold(t *testing.T) {
	cases := []struct {
		name      string
		kind      string
		checkable float64
		want      bool
	}{
		{"just under", importer.KindCodeRule, 0.49, false},
		{"exactly", importer.KindCodeRule, importer.Accept, true},
		{"above", importer.KindCodeRule, 0.99, true},
		{"process at the threshold", importer.KindProcess, importer.Accept, true},
		{"needs-repo but checkable", importer.KindNeedsRepo, 0.9, true},
		{"context, however checkable", importer.KindContext, 0.9, false},
		{"other, however checkable", importer.KindOther, 0.9, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			const text = "Keep the error path explicit here."
			asker := &fake{byText: map[string]answers{
				text: {kind: map[string]float64{c.kind: 0.7, importer.KindOther: 0.3}, checkable: c.checkable},
			}}
			sorter := &importer.Sorter{Asker: asker}
			got, stats, err := sorter.Sort(context.Background(), candidates(text))
			if err != nil {
				t.Fatal(err)
			}
			if got[0].Accepted != c.want {
				t.Errorf("accepted is %v, want %v", got[0].Accepted, c.want)
			}
			if (stats.Tenets == 1) != c.want {
				t.Errorf("stats count %d tenets", stats.Tenets)
			}
		})
	}
}

func TestSortChunksAtEighty(t *testing.T) {
	texts := make([]string, importer.ChunkSize+1)
	byText := map[string]answers{}
	for i := range texts {
		texts[i] = fmt.Sprintf("Rule number %d applies to every file.", i)
		byText[texts[i]] = answers{
			kind:      map[string]float64{importer.KindCodeRule: 0.6, importer.KindOther: 0.4},
			checkable: 0.8,
		}
	}
	asker := &fake{byText: byText}
	sorter := &importer.Sorter{Asker: asker}

	got, stats, err := sorter.Sort(context.Background(), candidates(texts...))
	if err != nil {
		t.Fatal(err)
	}
	if len(asker.calls) != 2 || stats.Calls != 2 {
		t.Fatalf("made %d calls", len(asker.calls))
	}
	if len(asker.calls[0].questions) != 2*importer.ChunkSize || len(asker.calls[1].questions) != 2 {
		t.Errorf("chunks hold %d and %d questions", len(asker.calls[0].questions), len(asker.calls[1].questions))
	}
	// Each call numbers its own candidates, so the last chunk starts over.
	if !strings.Contains(asker.calls[1].state, "C001 Rules :: "+texts[importer.ChunkSize]) {
		t.Errorf("second state is %q", asker.calls[1].state)
	}
	if len(got) != len(texts) || stats.Tenets != len(texts) {
		t.Errorf("got %d sorted, %d tenets", len(got), stats.Tenets)
	}
}

func TestSortFailsOnAnUnansweredQuestion(t *testing.T) {
	const text = "A comment says why, not what."
	asker := &fake{
		byText: map[string]answers{text: {kind: map[string]float64{importer.KindCodeRule: 0.9}, checkable: 0.8}},
		drop:   "checkable:C001",
	}
	_, _, err := (&importer.Sorter{Asker: asker}).Sort(context.Background(), candidates(text))
	if err == nil || !strings.Contains(err.Error(), "checkable:C001") {
		t.Errorf("error is %v", err)
	}
}

func TestSortReusesCachedSentences(t *testing.T) {
	const kept = "A comment says why, not what."
	const added = "Never mock anything in tests."
	byText := map[string]answers{
		kept:  {kind: map[string]float64{importer.KindCodeRule: 0.9}, checkable: 0.8},
		added: {kind: map[string]float64{importer.KindCodeRule: 0.7}, checkable: 0.6},
	}
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	first := &fake{byText: byText}
	if _, _, err := (&importer.Sorter{Asker: first, Cache: store}).Sort(context.Background(), candidates(kept)); err != nil {
		t.Fatal(err)
	}

	second := &fake{byText: byText}
	got, stats, err := (&importer.Sorter{Asker: second, Cache: store}).Sort(context.Background(), candidates(kept, added))
	if err != nil {
		t.Fatal(err)
	}
	if stats.CacheHits != 1 || stats.Calls != 1 {
		t.Errorf("stats are %#v", stats)
	}
	if len(second.calls[0].questions) != 2 {
		t.Errorf("asked %d questions, want only the new sentence", len(second.calls[0].questions))
	}
	if got[0].Kind != importer.KindCodeRule || got[0].KindProb != 0.9 || got[0].CheckableProb != 0.8 || !got[0].Accepted {
		t.Errorf("cached candidate is %#v", got[0])
	}
}
