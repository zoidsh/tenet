// Package check measures how well a tenet's wording separates the labelled
// examples it was written against, so that an edit to the wording can be
// judged by the numbers moving.
package check

import (
	"fmt"
	"math"
	"sort"

	"github.com/zoidsh/tenet/internal/tenets"
)

// DefaultMinExamples is how many examples a tenet needs before its numbers
// mean anything.
const DefaultMinExamples = 6

// The words a tenet's separation is reported as.
const (
	VerdictSharp  = "sharp"
	VerdictUsable = "usable"
	VerdictBlurry = "blurry"
	VerdictTooFew = "too few examples"
)

// The AUC a verdict needs.
const (
	SharpAUC  = 0.95
	UsableAUC = 0.85
)

// Borderline is how close to the cutoff a misjudged example counts as one the
// wording nearly got right.
const Borderline = 0.1

// CompareAt are the cutoffs every tenet is also measured at, so that what
// moving its own cutoff would buy is on the screen beside it.
var CompareAt = []float64{0.7, 0.8, 0.9}

// LowerFloor is the lowest a cutoff is ever worth lowering to. Under it the
// model is not saying much, and the answer is to sharpen the wording rather
// than to accept what it is unsure of.
const LowerFloor = 0.7

// The advice lines, one of which is chosen by the shape of the numbers.
const (
	AdviceOKHigh       = "the ok examples score high: add a false criterion naming what they have in common."
	AdviceViolationLow = "the violations score low: add a true criterion naming what makes them violations."
	AdviceAmbiguous    = "both labels land in the middle: the tenet sentence is ambiguous, name the observable property it is about."
)

// AmbiguousBand is how far either side of the tenet's own cutoff both means
// have to sit for the sentence itself to be the suspect. It follows the
// cutoff rather than the middle of the scale, because a tenet cut at 0.8 is
// undecided about what scores 0.8, not about what scores 0.5.
const AmbiguousBand = 0.1

// Score is one example's probability under the label it is known to carry.
type Score struct {
	Prob      float64
	Violation bool
}

// AUC is the probability that a violation drawn at random scores above an ok
// one, a tie counting half. Ranks are averaged over ties, which is what makes
// that reading of the rank sum hold. It is undefined without one of each
// label, which the second result reports.
func AUC(scores []Score) (float64, bool) {
	var violations, oks int
	for _, s := range scores {
		if s.Violation {
			violations++
		} else {
			oks++
		}
	}
	if violations == 0 || oks == 0 {
		return 0, false
	}

	ordered := make([]Score, len(scores))
	copy(ordered, scores)
	sort.Slice(ordered, func(a, b int) bool { return ordered[a].Prob < ordered[b].Prob })

	var rankSum float64
	for i := 0; i < len(ordered); {
		j := i
		for j < len(ordered) && ordered[j].Prob == ordered[i].Prob {
			j++
		}
		averageRank := float64(i+j+1) / 2
		for k := i; k < j; k++ {
			if ordered[k].Violation {
				rankSum += averageRank
			}
		}
		i = j
	}

	n := float64(violations)
	return (rankSum - n*(n+1)/2) / (n * float64(oks)), true
}

// AccuracyAt is what the examples would come out at under a cutoff the tenet
// does not carry.
type AccuracyAt struct {
	Cutoff   float64 `json:"cutoff"`
	Accuracy float64 `json:"accuracy"`
}

// Misjudged is an example the tenet's cutoff puts on the wrong side.
type Misjudged struct {
	Label      tenets.Label `json:"label"`
	Prob       float64      `json:"probability"`
	Code       string       `json:"code"`
	Borderline bool         `json:"borderline"`
}

// Result is what check reports about one tenet.
type Result struct {
	Tenet      string  `json:"tenet"`
	Verdict    string  `json:"verdict"`
	Examples   int     `json:"examples"`
	Violations int     `json:"violations"`
	OKs        int     `json:"oks"`
	Fail       float64 `json:"fail"`

	// Separated is false when the examples carry only one label, which leaves
	// the AUC and the gap with nothing to measure.
	Separated     bool         `json:"separated"`
	AUC           float64      `json:"auc"`
	Accuracy      float64      `json:"accuracy"`
	AccuracyAt    []AccuracyAt `json:"accuracy_at"`
	MeanViolation float64      `json:"mean_violation"`
	MeanOK        float64      `json:"mean_ok"`
	Gap           float64      `json:"gap"`

	LocatedExamples int     `json:"located_examples"`
	LocationHits    int     `json:"location_hits"`
	LocationRate    float64 `json:"location_rate"`

	Misjudged []Misjudged `json:"misjudged"`
	Advice    string      `json:"advice"`

	// Stability is nil unless the run judged the examples more than once.
	Stability *Stability `json:"stability,omitempty"`
}

// Stability is how far the model's answers moved when the same examples were
// judged again, reported only when a run asked for more than one pass.
type Stability struct {
	Runs      int        `json:"runs"`
	MaxStdDev float64    `json:"max_std_dev"`
	Crossed   []Crossing `json:"crossed"`
}

// Crossing is an example the passes did not agree about: its probabilities
// fall on both sides of the tenet's cutoff, so the verdict it gets is the run
// it was asked in.
type Crossing struct {
	Label tenets.Label `json:"label"`
	Code  string       `json:"code"`
	Min   float64      `json:"min_probability"`
	Max   float64      `json:"max_probability"`
}

// StabilityOf compares the passes of one tenet, which hold the same examples
// in the same order. The deviation is the sample one, over n-1: the passes are
// draws from what the model would answer, not the whole of it.
func StabilityOf(t *tenets.Tenet, passes [][]Judged) *Stability {
	if len(passes) < 2 {
		return nil
	}
	fail := t.FailValue()
	s := &Stability{Runs: len(passes), Crossed: []Crossing{}}
	probs := make([]float64, len(passes))
	for i := range passes[0] {
		for p, pass := range passes {
			probs[p] = pass[i].Prob
		}
		s.MaxStdDev = math.Max(s.MaxStdDev, stdDev(probs))
		low, high := probs[0], probs[0]
		for _, p := range probs {
			low, high = math.Min(low, p), math.Max(high, p)
		}
		if low < fail && high >= fail {
			e := passes[0][i].Example
			s.Crossed = append(s.Crossed, Crossing{Label: e.Label, Code: e.CodeLines()[0], Min: low, Max: high})
		}
	}
	return s
}

func stdDev(values []float64) float64 {
	var sum float64
	same := true
	for _, v := range values {
		sum += v
		same = same && v == values[0]
	}
	// The mean of equal values is not exactly that value in binary floating
	// point, which would leave an example nothing moved on reporting a
	// deviation of 1e-17 rather than none.
	if same {
		return 0
	}
	mean := sum / float64(len(values))
	var squares float64
	for _, v := range values {
		squares += (v - mean) * (v - mean)
	}
	return math.Sqrt(squares / float64(len(values)-1))
}

// Judged is one example with what the model answered about it: the
// probability that it violates its tenet, and the line the location question
// named, zero when that question was not asked.
type Judged struct {
	Example tenets.Example
	Prob    float64
	Line    int
}

// Measure turns one tenet's judged examples into its numbers.
func Measure(t *tenets.Tenet, judged []Judged, minExamples int) Result {
	r := Result{
		Tenet:     t.ID,
		Examples:  len(judged),
		Fail:      t.FailValue(),
		Misjudged: []Misjudged{},
	}
	if len(judged) < minExamples {
		r.Verdict = VerdictTooFew
		return r
	}

	scores := make([]Score, 0, len(judged))
	var violationSum, okSum float64
	var correct int
	for _, j := range judged {
		violation := j.Example.Label == tenets.LabelViolation
		scores = append(scores, Score{Prob: j.Prob, Violation: violation})
		if violation {
			r.Violations++
			violationSum += j.Prob
		} else {
			r.OKs++
			okSum += j.Prob
		}
		if (j.Prob >= r.Fail) == violation {
			correct++
		} else {
			r.Misjudged = append(r.Misjudged, Misjudged{
				Label:      j.Example.Label,
				Prob:       j.Prob,
				Code:       j.Example.CodeLines()[0],
				Borderline: math.Abs(j.Prob-r.Fail) <= Borderline,
			})
		}
		if j.Example.Lines.Set() {
			r.LocatedExamples++
			if j.Example.Lines.Contains(j.Line) {
				r.LocationHits++
			}
		}
	}

	r.Accuracy = float64(correct) / float64(len(judged))
	for _, cutoff := range CompareAt {
		r.AccuracyAt = append(r.AccuracyAt, AccuracyAt{Cutoff: cutoff, Accuracy: accuracyAt(scores, cutoff)})
	}
	if r.Violations > 0 {
		r.MeanViolation = violationSum / float64(r.Violations)
	}
	if r.OKs > 0 {
		r.MeanOK = okSum / float64(r.OKs)
	}
	r.Gap = r.MeanViolation - r.MeanOK
	if r.LocatedExamples > 0 {
		r.LocationRate = float64(r.LocationHits) / float64(r.LocatedExamples)
	}
	r.AUC, r.Separated = AUC(scores)
	r.Verdict = verdict(r)
	r.Advice = advice(r, scores)
	return r
}

func accuracyAt(scores []Score, cutoff float64) float64 {
	var correct int
	for _, s := range scores {
		if (s.Prob >= cutoff) == s.Violation {
			correct++
		}
	}
	return float64(correct) / float64(len(scores))
}

func verdict(r Result) string {
	switch {
	case !r.Separated:
		return VerdictBlurry
	case r.AUC >= SharpAUC && len(r.Misjudged) == 0:
		return VerdictSharp
	case r.AUC >= UsableAUC:
		return VerdictUsable
	default:
		return VerdictBlurry
	}
}

// advice names the one change most likely to move the numbers. The ambiguous
// band is tested first because a tenet whose two means both sit in the middle
// satisfies the other two rules as well, and rewriting the sentence is what
// such a tenet needs before either criterion is worth writing.
func advice(r Result, scores []Score) string {
	middle := func(mean float64) bool {
		return math.Abs(mean-r.Fail) <= AmbiguousBand
	}
	highestOK, inBand, lowestInBand := bands(r, scores)
	switch {
	case r.Violations > 0 && r.OKs > 0 && middle(r.MeanViolation) && middle(r.MeanOK):
		return AdviceAmbiguous
	case highestOK >= r.Fail:
		return fmt.Sprintf("an ok example scores %.2f, at or above the cutoff: raise fail above it, or add a false criterion naming what the ok examples have in common.", highestOK)
	case inBand > 0 && lowestInBand > highestOK:
		return fmt.Sprintf("%s score under the cutoff but above %.2f: lower fail to %.2f for this rule.",
			plural(inBand, "violation"), LowerFloor, lowestInBand)
	case r.OKs > 0 && r.MeanOK >= r.Fail:
		return AdviceOKHigh
	case r.Violations > 0 && r.MeanViolation < r.Fail:
		return AdviceViolationLow
	default:
		return ""
	}
}

// bands are what the advice is chosen by: the highest an ok example scored,
// and the violations sitting between the floor and the cutoff, which are the
// ones a lower cutoff would catch.
func bands(r Result, scores []Score) (highestOK float64, inBand int, lowestInBand float64) {
	lowestInBand = 1
	for _, s := range scores {
		if !s.Violation {
			highestOK = max(highestOK, s.Prob)
			continue
		}
		if s.Prob >= LowerFloor && s.Prob < r.Fail {
			inBand++
			lowestInBand = min(lowestInBand, s.Prob)
		}
	}
	return highestOK, inBand, lowestInBand
}
