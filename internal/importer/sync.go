package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/zoidsh/tenet/internal/cache"
	"github.com/zoidsh/tenet/internal/jev"
)

// PairAccept is the probability that two sentences state the same rule has to
// reach before sync reads a stale entry and a new sentence as one rewording
// rather than a deletion and an addition.
const PairAccept = 0.5

// PairSimilarity is the share of significant words a pair has to have in
// common before the model is asked about it at all. Two rules that merely sit
// in the same file are not worth a question.
const PairSimilarity = 0.3

// pairSalt stands in for a tenet hash in the cache key, as cacheSalt does for
// the sort. The version retires the answers if the question is reworded.
const pairSalt = "tenet-sync-pair-v1"

const pairStateHeader = "Pairs of sentences. One of each pair is a rule a repository's config holds, the other a sentence its instruction file now has. Decide, for each pair, whether the two state the same rule."

const (
	pairTrue  = "The two sentences state one rule, the second a rewording, tightening or loosening of the first."
	pairFalse = "The two sentences state different rules, or one of them states no rule at all."
)

// PairInstructions asks whether a config entry and a new sentence are the same
// rule. Both sentences are carried in the question, because the state names
// every pair at once and the model is told which one this is nowhere else.
func PairInstructions(held, found string) string {
	return "Held: \"" + held + "\"\nFound: \"" + found + "\" These two sentences state the same rule."
}

// Entry is something a config holds that a sentence in a rule file stood
// behind: a tenet, or a comment above a built-in rule id.
type Entry struct {
	// Tenet is the tenet's id, empty for a comment above a rule.
	Tenet string `json:"tenet,omitempty"`

	// Rule is the built-in rule id the comment sits above, empty for a tenet.
	Rule string `json:"rule,omitempty"`

	// Source is the tenet's source field, or the comment's own text.
	Source string `json:"source"`

	// Text is the tenet's own sentence, empty for a comment.
	Text string `json:"-"`
}

// Change is an entry whose rule file reworded the sentence behind it.
type Change struct {
	Entry Entry

	// Old is the sentence the entry stood behind, New the one that replaced
	// it, read from File.
	Old  string
	New  string
	File string

	// TextUpdated is whether the tenet's own sentence was the old one word for
	// word, and so was reworded with it. A tenet somebody has since edited
	// keeps what they wrote.
	TextUpdated bool
}

// Source is what the entry's source field, or its comment, should now say.
func (c Change) Source() string { return SourceLine(c.File, c.New) }

// Restated is an entry whose source still named a line rather than a sentence,
// resolved to the sentence that line holds. Nothing about the rule changed.
type Restated struct {
	Entry  Entry
	Source string
}

// Held is what a config already has: the entries a rule file sentence stood
// behind, and every id that is spoken for, which is those entries' ids and the
// built-in rules a draft must not be named after.
type Held struct {
	Entries []Entry
	Taken   map[string]bool
}

// Plan is what a sync run makes of a config: nothing in it deletes anything.
type Plan struct {
	Added    []Sorted
	Changed  []Change
	Stale    []Entry
	Restated []Restated
}

// Syncer pairs what a config holds against what the rule files now say.
type Syncer struct {
	Asker Asker
	Cache *cache.Cache
	Model string

	// Log, when set, receives a line per call made.
	Log func(string)
}

var whitespace = regexp.MustCompile(`\s+`)

// Normalize is the form two sentences are compared in: everything a person can
// change about a sentence without changing the rule it states.
func Normalize(text string) string {
	text = whitespace.ReplaceAllString(strings.TrimSpace(text), " ")
	return strings.ToLower(strings.TrimRight(text, ".!?,;:"))
}

// bound is an entry with the rule file sentence it was resolved to.
type bound struct {
	Entry
	file     string
	sentence string

	// restate is whether the source named a line, which has to be traded for
	// the sentence that line holds the next time the file is written.
	restate bool

	// tracked is whether the entry says which rule file sentence it stands
	// behind. One that does not still covers what it says, so the sentence is
	// not drafted twice, but nothing about it can be called stale or reworded.
	tracked bool
}

// resolve reads the sentence an entry stands behind. A source naming a line is
// resolved against the file as it is now, which is the only reading left of
// one: the line either still opens a sentence or the rule is gone.
func resolve(e Entry, byLine map[string]string) bound {
	b := bound{Entry: e, tracked: true}
	source, ok := ParseSource(e.Source)
	switch {
	case !ok:
		// A comment under rules: is only a record of a drafted sentence when
		// it is written as one. Any other line a person left there is prose
		// about the rule, and reporting it as a rule that went missing would
		// be noise nobody can act on.
		b.sentence, b.tracked = e.Text, e.Text != ""
		if e.Text == "" {
			b.sentence = strings.TrimSpace(e.Source)
		}
	case source.Line > 0:
		b.file = source.File
		if sentence, found := byLine[fmt.Sprintf("%s:%d", source.File, source.Line)]; found {
			b.sentence, b.restate = sentence, true
		} else {
			b.sentence = e.Text
		}
	default:
		b.file, b.sentence = source.File, source.Sentence
	}
	return b
}

// Sync decides what the rule files have gained, reworded and dropped since the
// config was last in step with them. Read names the files that were actually
// split, because a run narrowed by --from says nothing about the rules that
// came from the files it skipped. Stats gains what the pairing cost.
func (s *Syncer) Sync(ctx context.Context, sorted []Sorted, read []string, config Held, stats *Stats) (Plan, error) {
	if config.Taken == nil {
		config.Taken = map[string]bool{}
	}
	wasRead := make(map[string]bool, len(read))
	for _, file := range read {
		wasRead[file] = true
	}
	byLine := make(map[string]string, len(sorted))
	present := make(map[string]bool, len(sorted))
	for _, c := range sorted {
		byLine[fmt.Sprintf("%s:%d", c.File, c.Line)] = c.Text
		present[Normalize(c.Text)] = true
	}

	held := make([]bound, 0, len(config.Entries))
	// coveredBy names the tenet a sentence is already written down as, which
	// is empty when what covers it is a comment above a built-in rule.
	coveredBy := make(map[string]string, len(config.Entries)*2)
	for _, e := range config.Entries {
		b := resolve(e, byLine)
		held = append(held, b)
		for _, text := range []string{b.sentence, e.Text} {
			if text == "" {
				continue
			}
			key := Normalize(text)
			if id, seen := coveredBy[key]; !seen || (id == "" && e.Tenet != "") {
				coveredBy[key] = e.Tenet
			}
		}
	}

	var plan Plan
	var fresh []int
	for i, c := range sorted {
		if !c.Accepted {
			continue
		}
		if _, ok := coveredBy[Normalize(c.Text)]; !ok {
			fresh = append(fresh, i)
		}
	}
	added := make([]Sorted, 0, len(fresh))
	for _, i := range fresh {
		added = append(added, sorted[i])
	}
	var stale []bound
	for _, b := range held {
		switch {
		case b.sentence == "" || !b.tracked:
		case b.file != "" && !wasRead[b.file]:
		case present[Normalize(b.sentence)]:
			if b.restate {
				plan.Restated = append(plan.Restated, Restated{Entry: b.Entry, Source: SourceLine(b.file, b.sentence)})
			}
		default:
			stale = append(stale, b)
		}
	}

	paired, err := s.pair(ctx, stale, added, stats)
	if err != nil {
		return Plan{}, err
	}
	for i, b := range stale {
		j, ok := paired[i]
		if !ok {
			plan.Stale = append(plan.Stale, b.Entry)
			continue
		}
		plan.Changed = append(plan.Changed, Change{
			Entry:       b.Entry,
			Old:         b.sentence,
			New:         added[j].Text,
			File:        added[j].File,
			TextUpdated: b.Text != "" && Normalize(b.Text) == Normalize(b.sentence),
		})
	}
	reworded := make(map[int]bool, len(paired))
	for _, j := range paired {
		reworded[j] = true
	}

	// A candidate the config already writes down carries that entry's id, and
	// one that reworded a rule carries the id of the rule it reworded, so that
	// only a sentence actually being drafted spends a slug. Without this a new
	// tenet is handed a -2 because a sentence already in the file took the
	// name it would have had.
	for i, c := range sorted {
		if c.Accepted {
			sorted[i].ID = coveredBy[Normalize(c.Text)]
		}
	}
	for i, b := range stale {
		if j, ok := paired[i]; ok {
			sorted[fresh[j]].ID = b.Tenet
		}
	}
	drafts := make([]Sorted, 0, len(fresh))
	for j, i := range fresh {
		if !reworded[j] {
			drafts = append(drafts, sorted[i])
		}
	}
	AssignTaken(drafts, config.Taken)
	for n, j := 0, 0; j < len(fresh); j++ {
		if reworded[j] {
			continue
		}
		sorted[fresh[j]].ID = drafts[n].ID
		plan.Added = append(plan.Added, sorted[fresh[j]])
		n++
	}
	return plan, nil
}

// candidatePair is one stale entry and one new sentence worth asking about.
type candidatePair struct {
	stale int
	added int
	prob  float64
}

// pair matches each stale entry to the new sentence that states the same rule,
// at most one each way. The cheap similarity comes first because every pair
// past it costs a question, and two rules in one file share little.
func (s *Syncer) pair(ctx context.Context, stale []bound, added []Sorted, stats *Stats) (map[int]int, error) {
	var pairs []candidatePair
	for i, b := range stale {
		for j, c := range added {
			// An entry that names no file pairs with nothing: there is no
			// telling which file's rewording it would be.
			if b.file != c.File {
				continue
			}
			if similarity(b.sentence, c.Text) < PairSimilarity {
				continue
			}
			pairs = append(pairs, candidatePair{stale: i, added: j})
		}
	}
	if err := s.ask(ctx, stale, added, pairs, stats); err != nil {
		return nil, err
	}

	// The likeliest rewording wins its entry and its sentence, so that two
	// stale rules competing for one new sentence do not both claim it.
	slices.SortStableFunc(pairs, func(a, b candidatePair) int {
		if a.prob != b.prob {
			return cmpDesc(a.prob, b.prob)
		}
		return a.stale - b.stale
	})
	out := map[int]int{}
	takenAdded := map[int]bool{}
	for _, p := range pairs {
		if p.prob < PairAccept || takenAdded[p.added] {
			continue
		}
		if _, ok := out[p.stale]; ok {
			continue
		}
		out[p.stale], takenAdded[p.added] = p.added, true
	}
	return out, nil
}

func cmpDesc(a, b float64) int {
	if a > b {
		return -1
	}
	return 1
}

func (s *Syncer) ask(ctx context.Context, stale []bound, added []Sorted, pairs []candidatePair, stats *Stats) error {
	var pending []int
	for i, p := range pairs {
		key := s.key(stale[p.stale].sentence, added[p.added].Text)
		if e, ok := s.Cache.Get(key); ok {
			pairs[i].prob = e.Prob
			stats.CacheHits++
			continue
		}
		pending = append(pending, i)
	}
	for start := 0; start < len(pending); start += ChunkSize {
		end := min(start+ChunkSize, len(pending))
		if err := s.askChunk(ctx, stale, added, pairs, pending[start:end], stats); err != nil {
			return err
		}
	}
	return nil
}

func (s *Syncer) askChunk(ctx context.Context, stale []bound, added []Sorted, pairs []candidatePair, chunk []int, stats *Stats) error {
	var state strings.Builder
	state.WriteString(pairStateHeader + "\n")
	questions := map[string]jev.Question{}
	for n, i := range chunk {
		id := candidateID(n + 1)
		held, found := stale[pairs[i].stale].sentence, added[pairs[i].added].Text
		fmt.Fprintf(&state, "%s held :: %s\n%s found :: %s\n", id, held, id, found)
		questions["same:"+id] = jev.Noul(PairInstructions(held, found), pairTrue, pairFalse)
	}

	resp, err := s.Asker.Ask(ctx, state.String(), questions)
	if err != nil {
		return err
	}
	stats.Calls++
	stats.InputTokens += resp.Usage.InputTokens
	stats.CostUSD += jev.Cost(resp.Usage)
	if s.Log != nil {
		s.Log(fmt.Sprintf("paired %d sentences, %d input tokens", len(chunk), resp.Usage.InputTokens))
	}

	for n, i := range chunk {
		answer, err := answerTo(resp, "same:"+candidateID(n+1))
		if err != nil {
			return err
		}
		pairs[i].prob = answer.Prob()
		s.Cache.PutVerdict(s.key(stale[pairs[i].stale].sentence, added[pairs[i].added].Text), answer.Prob())
	}
	return nil
}

func (s *Syncer) key(held, found string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, pairSalt)
	_, _ = h.Write([]byte{0})
	_, _ = io.WriteString(h, s.Model)
	return cache.Key(held+"\x00"+found, hex.EncodeToString(h.Sum(nil)))
}

// similarity is the share of the significant words two sentences share. The
// stop words are left out because every pair of English sentences has them in
// common, which would let anything past.
func similarity(a, b string) float64 {
	left, right := significant(a), significant(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	both := 0
	for word := range left {
		if right[word] {
			both++
		}
	}
	return float64(both) / float64(len(left)+len(right)-both)
}

func significant(text string) map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.Fields(strings.ToLower(text)) {
		if word = notSlug.ReplaceAllString(word, ""); word != "" && !stopWords[word] {
			out[word] = true
		}
	}
	return out
}
