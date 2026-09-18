package importer_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/zoidsh/tenet/internal/importer"
	"github.com/zoidsh/tenet/internal/jev"
)

// pairAsker answers every pairing question by whether the two sentences it was
// asked about are in the same set, which lets a test say outright which
// rewordings the model should see.
type pairAsker struct {
	same  [][2]string
	calls int
}

func (a *pairAsker) Ask(_ context.Context, _ string, questions map[string]jev.Question) (*jev.Response, error) {
	a.calls++
	answers := map[string]jev.Answer{}
	for name, q := range questions {
		prob := 0.04
		for _, pair := range a.same {
			if strings.Contains(q.Instructions, pair[0]) && strings.Contains(q.Instructions, pair[1]) {
				prob = 0.96
			}
		}
		answers[name] = jev.Answer{Type: jev.KindNoul, Noul: prob}
	}
	return &jev.Response{Answers: answers, Usage: jev.Usage{InputTokens: 120}}, nil
}

func candidate(file string, line int, text string, accepted bool) importer.Sorted {
	kind := importer.KindProcess
	if accepted {
		kind = importer.KindCodeRule
	}
	return importer.Sorted{
		Candidate: importer.Candidate{File: file, Line: line, Text: text},
		Kind:      kind,
		Accepted:  accepted,
	}
}

func tenetEntry(id, text, source string) importer.Entry {
	return importer.Entry{Tenet: id, Text: text, Source: source}
}

// sync reads whichever files the candidates came from, which is what a run
// with no --from does.
func sync(t *testing.T, asker *pairAsker, sorted []importer.Sorted, entries []importer.Entry) (importer.Plan, importer.Stats) {
	t.Helper()
	var read []string
	for _, c := range sorted {
		if !slices.Contains(read, c.File) {
			read = append(read, c.File)
		}
	}
	return syncRead(t, asker, sorted, read, entries)
}

func syncRead(t *testing.T, asker *pairAsker, sorted []importer.Sorted, read []string, entries []importer.Entry) (importer.Plan, importer.Stats) {
	t.Helper()
	return syncHeld(t, asker, sorted, read, importer.Held{Entries: entries})
}

func syncHeld(t *testing.T, asker *pairAsker, sorted []importer.Sorted, read []string, config importer.Held) (importer.Plan, importer.Stats) {
	t.Helper()
	s := &importer.Syncer{Asker: asker, Model: jev.DefaultModel}
	var stats importer.Stats
	stats.Candidates = len(sorted)
	plan, err := s.Sync(t.Context(), sorted, read, config, &stats)
	if err != nil {
		t.Fatal(err)
	}
	return plan, stats
}

const (
	commentSentence = "A comment says why the code exists, not what it does."
	mockSentence    = "Never mock anything in tests."
	testsSentence   = "Run the tests before you call a branch done."
)

func TestSyncAddsWhatTheConfigDoesNotHold(t *testing.T) {
	sorted := []importer.Sorted{
		candidate("CLAUDE.md", 5, commentSentence, true),
		candidate("CLAUDE.md", 7, mockSentence, true),
		candidate("CLAUDE.md", 9, testsSentence, false),
	}
	entries := []importer.Entry{
		tenetEntry("comment-says-why", commentSentence, importer.SourceLine("CLAUDE.md", commentSentence)),
	}
	asker := &pairAsker{}
	plan, _ := sync(t, asker, sorted, entries)

	if len(plan.Added) != 1 || plan.Added[0].Text != mockSentence {
		t.Fatalf("added %#v", plan.Added)
	}
	if len(plan.Changed) != 0 || len(plan.Stale) != 0 {
		t.Errorf("changed %#v, stale %#v", plan.Changed, plan.Stale)
	}
	if asker.calls != 0 {
		t.Errorf("nothing was stale, so nothing should have been paired: %d calls", asker.calls)
	}
}

// A sentence a rule file still holds is covered whichever of the three places
// it is written down in.
func TestSyncCoverage(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 5, commentSentence, true)}
	cases := []struct {
		name  string
		entry importer.Entry
	}{
		{"the sentence in a source", tenetEntry("x", "Something else entirely.", importer.SourceLine("CLAUDE.md", commentSentence))},
		{"the tenet's own text", tenetEntry("x", commentSentence, "")},
		{"a rules comment", importer.Entry{Rule: "comment-why", Source: importer.SourceLine("CLAUDE.md", commentSentence)}},
		{"a rules comment with no file", importer.Entry{Rule: "comment-why", Source: "the sentence this stands for: " + commentSentence}},
		// Whitespace, case and the full stop are not the rule.
		{"a loosely written source", tenetEntry("x", "", "CLAUDE.md:   a Comment says why the   code exists, not what it does")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			plan, _ := sync(t, &pairAsker{}, sorted, []importer.Entry{c.entry})
			if len(plan.Added) != 0 || len(plan.Stale) != 0 {
				t.Errorf("added %#v, stale %#v", plan.Added, plan.Stale)
			}
		})
	}
}

func TestSyncReportsAStaleEntry(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 5, testsSentence, false)}
	entries := []importer.Entry{
		tenetEntry("never-mock", mockSentence, importer.SourceLine("CLAUDE.md", mockSentence)),
		{Rule: "comment-why", Source: importer.SourceLine("CLAUDE.md", commentSentence)},
	}
	plan, _ := sync(t, &pairAsker{}, sorted, entries)

	if len(plan.Stale) != 2 {
		t.Fatalf("stale %#v", plan.Stale)
	}
	if plan.Stale[0].Tenet != "never-mock" || plan.Stale[1].Rule != "comment-why" {
		t.Errorf("stale %#v", plan.Stale)
	}
}

const reworded = "Never mock anything in a test, not even the clock."

func TestSyncPairsARewordedRule(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 7, reworded, true)}
	entries := []importer.Entry{
		tenetEntry("never-mock", mockSentence, importer.SourceLine("CLAUDE.md", mockSentence)),
	}
	asker := &pairAsker{same: [][2]string{{mockSentence, reworded}}}
	plan, stats := sync(t, asker, sorted, entries)

	if len(plan.Added) != 0 || len(plan.Stale) != 0 {
		t.Fatalf("a pairing should empty both lists: added %#v, stale %#v", plan.Added, plan.Stale)
	}
	if len(plan.Changed) != 1 {
		t.Fatalf("changed %#v", plan.Changed)
	}
	got := plan.Changed[0]
	if got.Entry.Tenet != "never-mock" || got.Old != mockSentence || got.New != reworded {
		t.Errorf("changed %#v", got)
	}
	if !got.TextUpdated {
		t.Error("the tenet said the old sentence word for word, so it should move with it")
	}
	if want := importer.SourceLine("CLAUDE.md", reworded); got.Source() != want {
		t.Errorf("source is %q, want %q", got.Source(), want)
	}
	if stats.Calls != 1 {
		t.Errorf("calls %d", stats.Calls)
	}
}

// A tenet somebody has since edited keeps the sentence they wrote, and the
// report has to say the rewording did not reach it.
func TestSyncKeepsATenetSomebodyEdited(t *testing.T) {
	const edited = "Never mock anything in tests; use a real clock."
	sorted := []importer.Sorted{candidate("CLAUDE.md", 7, reworded, true)}
	entries := []importer.Entry{
		tenetEntry("never-mock", edited, importer.SourceLine("CLAUDE.md", mockSentence)),
	}
	asker := &pairAsker{same: [][2]string{{mockSentence, reworded}}}
	plan, _ := sync(t, asker, sorted, entries)

	if len(plan.Changed) != 1 || plan.Changed[0].TextUpdated {
		t.Fatalf("changed %#v", plan.Changed)
	}
}

func TestSyncPairsARulesComment(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 7, reworded, true)}
	entries := []importer.Entry{
		{Rule: "no-mocking", Source: importer.SourceLine("CLAUDE.md", mockSentence)},
	}
	asker := &pairAsker{same: [][2]string{{mockSentence, reworded}}}
	plan, _ := sync(t, asker, sorted, entries)

	if len(plan.Changed) != 1 {
		t.Fatalf("changed %#v", plan.Changed)
	}
	if got := plan.Changed[0]; got.Entry.Rule != "no-mocking" || got.TextUpdated {
		t.Errorf("changed %#v", got)
	}
}

// A pair that shares almost no words is not worth a question, so the model is
// never asked and both entries stand on their own.
func TestSyncAsksAboutNothingFarApart(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 7, "Prefer table driven tests over helpers.", true)}
	entries := []importer.Entry{
		tenetEntry("comment-says-why", commentSentence, importer.SourceLine("CLAUDE.md", commentSentence)),
	}
	asker := &pairAsker{}
	plan, _ := sync(t, asker, sorted, entries)

	if asker.calls != 0 {
		t.Errorf("the model was asked about a pair with nothing in common")
	}
	if len(plan.Added) != 1 || len(plan.Stale) != 1 {
		t.Errorf("added %#v, stale %#v", plan.Added, plan.Stale)
	}
}

// A rewording in one file never claims a stale rule from another.
func TestSyncPairsOnlyWithinAFile(t *testing.T) {
	sorted := []importer.Sorted{candidate("AGENTS.md", 7, reworded, true)}
	entries := []importer.Entry{
		tenetEntry("never-mock", mockSentence, importer.SourceLine("CLAUDE.md", mockSentence)),
	}
	asker := &pairAsker{same: [][2]string{{mockSentence, reworded}}}
	plan, _ := syncRead(t, asker, sorted, []string{"AGENTS.md", "CLAUDE.md"}, entries)

	if asker.calls != 0 {
		t.Errorf("the model was asked across two files")
	}
	if len(plan.Added) != 1 || len(plan.Stale) != 1 || len(plan.Changed) != 0 {
		t.Errorf("plan %#v", plan)
	}
}

// Two stale rules that both look like one new sentence cannot both claim it.
func TestSyncGivesOneSentenceToOneEntry(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 7, reworded, true)}
	entries := []importer.Entry{
		tenetEntry("never-mock", mockSentence, importer.SourceLine("CLAUDE.md", mockSentence)),
		tenetEntry("never-mock-2", "Never mock anything in tests at all.", importer.SourceLine("CLAUDE.md", "Never mock anything in tests at all.")),
	}
	asker := &pairAsker{same: [][2]string{{mockSentence, reworded}, {"Never mock anything in tests at all.", reworded}}}
	plan, _ := sync(t, asker, sorted, entries)

	if len(plan.Changed) != 1 || len(plan.Stale) != 1 || len(plan.Added) != 0 {
		t.Errorf("changed %#v, stale %#v, added %#v", plan.Changed, plan.Stale, plan.Added)
	}
}

func TestSyncRestatesAnOldSource(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 5, commentSentence, true)}
	entries := []importer.Entry{tenetEntry("comment-says-why", commentSentence, "CLAUDE.md:5")}
	plan, _ := sync(t, &pairAsker{}, sorted, entries)

	if len(plan.Restated) != 1 {
		t.Fatalf("restated %#v", plan.Restated)
	}
	if want := importer.SourceLine("CLAUDE.md", commentSentence); plan.Restated[0].Source != want {
		t.Errorf("source is %q, want %q", plan.Restated[0].Source, want)
	}
	if len(plan.Added) != 0 || len(plan.Stale) != 0 || len(plan.Changed) != 0 {
		t.Errorf("plan %#v", plan)
	}
}

// A source naming a line the file no longer has falls back to the tenet's own
// sentence, which is the only other thing that says what the rule was.
func TestSyncAnOldSourcePastTheEndOfTheFile(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 5, commentSentence, true)}
	entries := []importer.Entry{tenetEntry("never-mock", mockSentence, "CLAUDE.md:400")}
	plan, _ := sync(t, &pairAsker{}, sorted, entries)

	if len(plan.Stale) != 1 || plan.Stale[0].Tenet != "never-mock" {
		t.Errorf("stale %#v", plan.Stale)
	}
	if len(plan.Restated) != 0 {
		t.Errorf("restated %#v", plan.Restated)
	}
}

// A source nobody wrote as init writes them says nothing about a rule file, so
// it is never called stale; the tenet's own sentence still covers.
func TestSyncLeavesAHandWrittenSourceAlone(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 5, commentSentence, true)}
	entries := []importer.Entry{tenetEntry("comment-says-why", commentSentence, "a conversation with Tim")}
	plan, _ := sync(t, &pairAsker{}, sorted, entries)

	if len(plan.Added) != 0 || len(plan.Stale) != 0 || len(plan.Restated) != 0 {
		t.Errorf("plan %#v", plan)
	}
}

// mockTwin slugs exactly as mockSentence does, so the two compete for one id.
const mockTwin = "Never mock, anything in the tests!"

// The slug a covered sentence would have had is not spent on it: the entry
// that already holds that sentence lends the candidate its own id, so a new
// draft whose words slug the same way gets the plain name.
func TestSyncDoesNotSpendASlugOnACoveredSentence(t *testing.T) {
	sorted := []importer.Sorted{
		candidate("CLAUDE.md", 5, mockSentence, true),
		candidate("CLAUDE.md", 9, mockTwin, true),
	}
	config := importer.Held{
		Entries: []importer.Entry{tenetEntry("no-mock", mockSentence, importer.SourceLine("CLAUDE.md", mockSentence))},
		Taken:   map[string]bool{"no-mock": true},
	}
	plan, _ := syncHeld(t, &pairAsker{}, sorted, []string{"CLAUDE.md"}, config)

	if len(plan.Added) != 1 || plan.Added[0].Text != mockTwin {
		t.Fatalf("added %#v", plan.Added)
	}
	if got := plan.Added[0].ID; got != "never-mock-anything-tests" {
		t.Errorf("the draft is %q, want the plain slug", got)
	}
	if sorted[0].ID != "no-mock" {
		t.Errorf("the covered candidate is %q, want the id of the tenet holding it", sorted[0].ID)
	}
	if sorted[1].ID != "never-mock-anything-tests" {
		t.Errorf("the drafted candidate is %q", sorted[1].ID)
	}
}

// When an entry really does hold the id, the draft still steps around it.
func TestSyncStepsAroundAnIdAnEntryHolds(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 9, mockTwin, true)}
	config := importer.Held{
		Entries: []importer.Entry{tenetEntry("never-mock-anything-tests", mockSentence, importer.SourceLine("CLAUDE.md", mockSentence))},
		Taken:   map[string]bool{"never-mock-anything-tests": true},
	}
	plan, _ := syncHeld(t, &pairAsker{}, sorted, []string{"CLAUDE.md"}, config)

	if len(plan.Added) != 1 || plan.Added[0].ID != "never-mock-anything-tests-2" {
		t.Errorf("added %#v", plan.Added)
	}
}

// A sentence a rules comment covers belongs to no tenet, so the candidate
// carries no id rather than one nothing in the file answers to.
func TestSyncLeavesACommentCoveredCandidateUnnamed(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 5, mockSentence, true)}
	config := importer.Held{
		Entries: []importer.Entry{{Rule: "no-mocking", Source: importer.SourceLine("CLAUDE.md", mockSentence)}},
		Taken:   map[string]bool{"no-mocking": true},
	}
	plan, _ := syncHeld(t, &pairAsker{}, sorted, []string{"CLAUDE.md"}, config)

	if len(plan.Added) != 0 {
		t.Fatalf("added %#v", plan.Added)
	}
	if sorted[0].ID != "" {
		t.Errorf("the candidate is named %q, but no tenet holds it", sorted[0].ID)
	}
}

// A reworded rule's new sentence carries the id of the tenet it reworded.
func TestSyncNamesAChangedCandidateAfterItsTenet(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 7, reworded, true)}
	config := importer.Held{
		Entries: []importer.Entry{tenetEntry("no-mock", mockSentence, importer.SourceLine("CLAUDE.md", mockSentence))},
		Taken:   map[string]bool{"no-mock": true},
	}
	asker := &pairAsker{same: [][2]string{{mockSentence, reworded}}}
	plan, _ := syncHeld(t, asker, sorted, []string{"CLAUDE.md"}, config)

	if len(plan.Changed) != 1 || len(plan.Added) != 0 {
		t.Fatalf("changed %#v, added %#v", plan.Changed, plan.Added)
	}
	if sorted[0].ID != "no-mock" {
		t.Errorf("the reworded candidate is %q", sorted[0].ID)
	}
}

// A comment under rules: that is not written as a source line is prose about
// the rule. It still covers what it says, so the sentence is not drafted a
// second time, but it is never reported as a rule that went missing.
func TestSyncNeverCallsALooseRulesCommentStale(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 5, commentSentence, true)}
	entries := []importer.Entry{
		{Rule: "no-mocking", Source: "we lean on this one instead of writing it out"},
	}
	plan, _ := sync(t, &pairAsker{}, sorted, entries)

	if len(plan.Stale) != 0 || len(plan.Changed) != 0 {
		t.Errorf("stale %#v, changed %#v", plan.Stale, plan.Changed)
	}
}

// A comment written as a source line is reported like any other entry.
func TestSyncReportsASourceShapedRulesComment(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 5, commentSentence, true)}
	entries := []importer.Entry{
		{Rule: "no-mocking", Source: importer.SourceLine("CLAUDE.md", mockSentence)},
	}
	plan, _ := sync(t, &pairAsker{}, sorted, entries)

	if len(plan.Stale) != 1 || plan.Stale[0].Rule != "no-mocking" {
		t.Errorf("stale %#v", plan.Stale)
	}
}

// A run narrowed to one file says nothing about the rules that came from the
// files it never opened, so none of them is stale and none is restated.
func TestSyncLeavesTheFilesItDidNotReadAlone(t *testing.T) {
	sorted := []importer.Sorted{candidate("AGENTS.md", 3, testsSentence, true)}
	entries := []importer.Entry{
		tenetEntry("never-mock", mockSentence, importer.SourceLine("CLAUDE.md", mockSentence)),
		tenetEntry("drafted-long-ago", commentSentence, "CLAUDE.md:5"),
		{Rule: "comment-why", Source: importer.SourceLine("CLAUDE.md", commentSentence)},
	}
	plan, _ := syncRead(t, &pairAsker{}, sorted, []string{"AGENTS.md"}, entries)

	if len(plan.Stale) != 0 {
		t.Errorf("a file that was never read left %#v stale", plan.Stale)
	}
	if len(plan.Restated) != 0 {
		t.Errorf("restated %#v", plan.Restated)
	}
	if len(plan.Added) != 1 || plan.Added[0].Text != testsSentence {
		t.Errorf("added %#v", plan.Added)
	}
}

// Whitespace and a full stop are not the rule, so a tenet that says the old
// sentence in all but those still moves with the rewording.
func TestSyncMovesATenetThatOnlyDiffersInPunctuation(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 7, reworded, true)}
	entries := []importer.Entry{
		tenetEntry("never-mock", "Never mock  anything in tests", importer.SourceLine("CLAUDE.md", mockSentence)),
	}
	asker := &pairAsker{same: [][2]string{{mockSentence, reworded}}}
	plan, _ := sync(t, asker, sorted, entries)

	if len(plan.Changed) != 1 || !plan.Changed[0].TextUpdated {
		t.Fatalf("changed %#v", plan.Changed)
	}
}

func TestNormalize(t *testing.T) {
	cases := [][2]string{
		{"  A comment  says\twhy. ", "a comment says why"},
		{"Never mock anything in tests!", "never mock anything in tests"},
		{"Run the tests;", "run the tests"},
	}
	for _, c := range cases {
		if got := importer.Normalize(c[0]); got != c[1] {
			t.Errorf("Normalize(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

func TestAssignTakenAvoidsIdsAlreadySpokenFor(t *testing.T) {
	sorted := []importer.Sorted{candidate("CLAUDE.md", 5, commentSentence, true)}
	importer.AssignTaken(sorted, map[string]bool{"comment-says-why-code-exists": true})
	if sorted[0].ID != "comment-says-why-code-exists-2" {
		t.Errorf("id is %q", sorted[0].ID)
	}
}
