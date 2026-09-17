// Package check measures how well a tenet's wording separates the labelled
// examples it was written against, so that an edit to the wording can be
// judged by the numbers moving.
package check

import (
	"math"
	"sort"

	"github.com/zoidsh/tenetlint/internal/tenets"
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

// Borderline is how close to the threshold a misjudged example counts as one
// the wording nearly got right.
const Borderline = 0.1

// The advice lines, one of which is chosen by the shape of the numbers.
const (
	AdviceOKHigh       = "the ok examples score high: add a false criterion naming what they have in common."
	AdviceViolationLow = "the violations score low: add a true criterion naming what makes them violations."
	AdviceAmbiguous    = "both labels land in the middle: the tenet sentence is ambiguous, name the observable property it is about."
)

// The band both means sitting inside makes the tenet itself the suspect.
const (
	ambiguousLow  = 0.4
	ambiguousHigh = 0.6
)

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

// Misjudged is an example the tenet's threshold puts on the wrong side.
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
	Threshold  float64 `json:"threshold"`
	Confident  float64 `json:"confident"`

	// Separated is false when the examples carry only one label, which leaves
	// the AUC and the gap with nothing to measure.
	Separated         bool    `json:"separated"`
	AUC               float64 `json:"auc"`
	AccuracyThreshold float64 `json:"accuracy_at_threshold"`
	AccuracyConfident float64 `json:"accuracy_at_confident"`
	MeanViolation     float64 `json:"mean_violation"`
	MeanOK            float64 `json:"mean_ok"`
	Gap               float64 `json:"gap"`

	LocatedExamples int     `json:"located_examples"`
	LocationHits    int     `json:"location_hits"`
	LocationRate    float64 `json:"location_rate"`

	Misjudged []Misjudged `json:"misjudged"`
	Advice    string      `json:"advice"`
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
		Threshold: t.ThresholdValue(),
		Confident: t.ConfidentValue(),
		Misjudged: []Misjudged{},
	}
	if len(judged) < minExamples {
		r.Verdict = VerdictTooFew
		return r
	}

	scores := make([]Score, 0, len(judged))
	var violationSum, okSum float64
	var correctThreshold, correctConfident int
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
		if (j.Prob >= r.Threshold) == violation {
			correctThreshold++
		} else {
			r.Misjudged = append(r.Misjudged, Misjudged{
				Label:      j.Example.Label,
				Prob:       j.Prob,
				Code:       j.Example.Lines()[0],
				Borderline: math.Abs(j.Prob-r.Threshold) <= Borderline,
			})
		}
		if (j.Prob >= r.Confident) == violation {
			correctConfident++
		}
		if j.Example.Line > 0 {
			r.LocatedExamples++
			if j.Line == j.Example.Line {
				r.LocationHits++
			}
		}
	}

	r.AccuracyThreshold = float64(correctThreshold) / float64(len(judged))
	r.AccuracyConfident = float64(correctConfident) / float64(len(judged))
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
	r.Advice = advice(r)
	return r
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
func advice(r Result) string {
	middle := func(mean float64) bool { return mean >= ambiguousLow && mean <= ambiguousHigh }
	switch {
	case r.Violations > 0 && r.OKs > 0 && middle(r.MeanViolation) && middle(r.MeanOK):
		return AdviceAmbiguous
	case r.OKs > 0 && r.MeanOK >= r.Threshold:
		return AdviceOKHigh
	case r.Violations > 0 && r.MeanViolation < r.Threshold:
		return AdviceViolationLow
	default:
		return ""
	}
}
