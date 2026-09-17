package check_test

import (
	"math"
	"strings"
	"testing"

	"github.com/zoidsh/tenetlint/internal/check"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

func TestAUC(t *testing.T) {
	cases := []struct {
		name       string
		violations []float64
		oks        []float64
		want       float64
		defined    bool
	}{
		{"separated", []float64{0.9, 0.8}, []float64{0.2, 0.1}, 1, true},
		// One tied pair counts half: 3.5 of 4 pairs.
		{"tied pair", []float64{0.9, 0.5}, []float64{0.5, 0.1}, 0.875, true},
		{"all tied", []float64{0.5, 0.5}, []float64{0.5, 0.5}, 0.5, true},
		{"inverted", []float64{0.1, 0.2}, []float64{0.8, 0.9}, 0, true},
		// 17 of 20 pairs: the ok at 0.75 outscores three violations.
		{"one loud ok", []float64{0.5, 0.6, 0.7, 0.8}, []float64{0.1, 0.2, 0.3, 0.4, 0.75}, 0.85, true},
		{"only violations", []float64{0.9, 0.8}, nil, 0, false},
		{"only oks", nil, []float64{0.1}, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, defined := check.AUC(scores(c.violations, c.oks))
			if defined != c.defined {
				t.Fatalf("defined is %v, want %v", defined, c.defined)
			}
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("auc is %v, want %v", got, c.want)
			}
		})
	}
}

func scores(violations, oks []float64) []check.Score {
	var out []check.Score
	for _, p := range violations {
		out = append(out, check.Score{Prob: p, Violation: true})
	}
	for _, p := range oks {
		out = append(out, check.Score{Prob: p})
	}
	return out
}

func TestVerdictBoundaries(t *testing.T) {
	cases := []struct {
		name        string
		fail        float64
		violations  []float64
		oks         []float64
		minExamples int
		want        string
	}{
		{"separated", 0, []float64{0.95, 0.9, 0.85}, []float64{0.3, 0.2, 0.1}, 6, check.VerdictSharp},
		{"on the cutoff", 0, []float64{0.8, 0.85, 0.9}, []float64{0.3, 0.2, 0.1}, 6, check.VerdictSharp},
		{"auc exactly usable", 0, []float64{0.5, 0.6, 0.7, 0.8}, []float64{0.1, 0.2, 0.3, 0.4, 0.75}, 6, check.VerdictUsable},
		{"just under usable", 0, []float64{0.5, 0.6, 0.7, 0.8}, []float64{0.1, 0.2, 0.3, 0.4, 0.85}, 6, check.VerdictBlurry},
		{"one label only", 0, []float64{0.9, 0.8, 0.7, 0.6, 0.5, 0.4}, nil, 6, check.VerdictBlurry},
		{"one short", 0, []float64{0.95, 0.9, 0.85}, []float64{0.3, 0.2}, 6, check.VerdictTooFew},
		{"exactly enough", 0, []float64{0.95, 0.9, 0.85}, []float64{0.3, 0.2}, 5, check.VerdictSharp},
		// Every ok here is over 0.5 and under 0.7, so the tenet's own cut is
		// what decides that nothing is misjudged.
		{"cut where the tenet cuts", 0.7, []float64{0.75, 0.8, 0.85}, []float64{0.5, 0.55, 0.6}, 6, check.VerdictSharp},
		{"the same examples cut at the default", 0, []float64{0.75, 0.8, 0.85}, []float64{0.5, 0.55, 0.6}, 6, check.VerdictUsable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := check.Measure(tenetAt(c.fail), judged(c.violations, c.oks), c.minExamples)
			if r.Verdict != c.want {
				t.Errorf("verdict is %q, want %q (auc %.3f, %d misjudged)", r.Verdict, c.want, r.AUC, len(r.Misjudged))
			}
		})
	}
}

func TestTooFewLeavesTheNumbersOut(t *testing.T) {
	r := check.Measure(tenet(), judged([]float64{0.9}, []float64{0.1}), 6)
	if r.Verdict != check.VerdictTooFew {
		t.Fatalf("verdict is %q", r.Verdict)
	}
	if r.AUC != 0 || r.Accuracy != 0 || r.Gap != 0 || r.Advice != "" || len(r.Misjudged) != 0 {
		t.Errorf("numbers were reported anyway: %#v", r)
	}
	if len(r.AccuracyAt) != 0 {
		t.Errorf("a tenet nobody can measure was compared at other cutoffs: %#v", r.AccuracyAt)
	}
}

func TestAdviceBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		fail       float64
		violations []float64
		oks        []float64
		want       string
	}{
		{"both means inside the band", 0, []float64{0.88, 0.88, 0.88}, []float64{0.72, 0.72, 0.72}, check.AdviceAmbiguous},
		{"the ok mean outside the band", 0, []float64{0.88, 0.88, 0.88}, []float64{0.68, 0.68, 0.68}, ""},
		{"an ok on the cutoff", 0, []float64{0.95, 0.95, 0.95}, []float64{0.1, 0.2, 0.8}, "raise fail above it"},
		{"an ok just under the cutoff", 0, []float64{0.95, 0.95, 0.95}, []float64{0.1, 0.2, 0.79}, ""},
		{"violations in the band under the cutoff", 0, []float64{0.72, 0.75, 0.95}, []float64{0.1, 0.1, 0.1}, "lower fail to 0.72"},
		{"violations under the floor", 0, []float64{0.65, 0.69, 0.95}, []float64{0.1, 0.1, 0.1}, check.AdviceViolationLow},
		{"the band moves with the cutoff", 0.6, []float64{0.66, 0.66, 0.66}, []float64{0.54, 0.54, 0.54}, check.AdviceAmbiguous},
		{"nothing to say", 0, []float64{0.95, 0.95, 0.95}, []float64{0.05, 0.05, 0.05}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := check.Measure(tenetAt(c.fail), judged(c.violations, c.oks), 6)
			if c.want == "" {
				if r.Advice != "" {
					t.Errorf("advice is %q, want none", r.Advice)
				}
				return
			}
			if !strings.Contains(r.Advice, c.want) {
				t.Errorf("advice is %q, want it to hold %q (mean violation %.3f, mean ok %.3f)",
					r.Advice, c.want, r.MeanViolation, r.MeanOK)
			}
		})
	}
}

func TestMeasureCountsAndMisjudged(t *testing.T) {
	examples := []check.Judged{
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a := 1\nb := 2\n", Lines: at(2, 2)}, Prob: 0.9, Line: 2},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "c := 3\n", Lines: at(1, 1)}, Prob: 0.75, Line: 1},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "d := 4\n"}, Prob: 0.8},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "e := 5\n"}, Prob: 0.1},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "f := 6\n"}, Prob: 0.2},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "g := 7\n"}, Prob: 0.95},
	}
	r := check.Measure(tenet(), examples, 6)

	if r.Examples != 6 || r.Violations != 3 || r.OKs != 3 {
		t.Errorf("counts are %d, %d, %d", r.Examples, r.Violations, r.OKs)
	}
	if math.Abs(r.Accuracy-4.0/6) > 1e-9 {
		t.Errorf("accuracy at the cutoff is %v", r.Accuracy)
	}
	// Lowering the cutoff to 0.7 would catch the violation at 0.75, and
	// raising it to 0.9 would lose the one at 0.8 as well.
	want := map[float64]float64{0.7: 5.0 / 6, 0.8: 4.0 / 6, 0.9: 3.0 / 6}
	if len(r.AccuracyAt) != len(want) {
		t.Fatalf("compared at %#v", r.AccuracyAt)
	}
	for _, a := range r.AccuracyAt {
		if math.Abs(a.Accuracy-want[a.Cutoff]) > 1e-9 {
			t.Errorf("accuracy at %.2f is %v, want %v", a.Cutoff, a.Accuracy, want[a.Cutoff])
		}
	}
	if math.Abs(r.Gap-(0.8166666666666667-0.4166666666666667)) > 1e-9 {
		t.Errorf("gap is %v from means %v and %v", r.Gap, r.MeanViolation, r.MeanOK)
	}
	if r.LocatedExamples != 2 || r.LocationHits != 2 || r.LocationRate != 1 {
		t.Errorf("location is %d/%d", r.LocationHits, r.LocatedExamples)
	}
	if len(r.Misjudged) != 2 {
		t.Fatalf("misjudged are %#v", r.Misjudged)
	}
	if r.Misjudged[0].Label != tenets.LabelViolation || r.Misjudged[0].Code != "c := 3" || !r.Misjudged[0].Borderline {
		t.Errorf("first misjudged is %#v", r.Misjudged[0])
	}
	if r.Misjudged[1].Label != tenets.LabelOK || r.Misjudged[1].Borderline {
		t.Errorf("second misjudged is %#v", r.Misjudged[1])
	}
}

func TestLocationMissCountsAgainstTheRate(t *testing.T) {
	examples := []check.Judged{
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\nc\n", Lines: at(2, 2)}, Prob: 0.9, Line: 3},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\nc\n", Lines: at(2, 2)}, Prob: 0.9, Line: 2},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\n", Lines: at(1, 1)}, Prob: 0.9, Line: 1},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "a\n"}, Prob: 0.1},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "b\n"}, Prob: 0.1},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "c\n"}, Prob: 0.1},
	}
	r := check.Measure(tenet(), examples, 6)
	if r.LocatedExamples != 3 || r.LocationHits != 2 {
		t.Errorf("location is %d/%d", r.LocationHits, r.LocatedExamples)
	}
	if math.Abs(r.LocationRate-2.0/3) > 1e-9 {
		t.Errorf("location rate is %v", r.LocationRate)
	}
}

func at(first, last int) tenets.LineRange {
	return tenets.LineRange{First: first, Last: last}
}

func TestASpanCountsAnyLineInsideIt(t *testing.T) {
	examples := []check.Judged{
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\nc\nd\n", Lines: at(2, 3)}, Prob: 0.9, Line: 3},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\nc\nd\n", Lines: at(2, 3)}, Prob: 0.9, Line: 2},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\nc\nd\n", Lines: at(2, 3)}, Prob: 0.9, Line: 4},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "a\n"}, Prob: 0.1},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "b\n"}, Prob: 0.1},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "c\n"}, Prob: 0.1},
	}
	r := check.Measure(tenet(), examples, 6)
	if r.LocatedExamples != 3 || r.LocationHits != 2 {
		t.Errorf("location is %d of %d, want the two lines inside the span", r.LocationHits, r.LocatedExamples)
	}
}

func TestAnUnaskedLocationIsAMiss(t *testing.T) {
	examples := []check.Judged{
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\n", Lines: at(1, 2)}, Prob: 0.9},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\n", Lines: at(1, 2)}, Prob: 0.9, Line: 1},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\n"}, Prob: 0.9},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "a\n"}, Prob: 0.1},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "b\n"}, Prob: 0.1},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "c\n"}, Prob: 0.1},
	}
	r := check.Measure(tenet(), examples, 6)
	if r.LocatedExamples != 2 || r.LocationHits != 1 {
		t.Errorf("location is %d of %d", r.LocationHits, r.LocatedExamples)
	}
}

func tenet() *tenets.Tenet {
	return &tenets.Tenet{ID: "comment-why", Tenet: "A comment says why."}
}

// tenetAt is the same tenet cut somewhere other than the default, which a
// cutoff of 0 asks for.
func tenetAt(fail float64) *tenets.Tenet {
	t := tenet()
	if fail > 0 {
		t.Fail = &fail
	}
	return t
}

func judged(violations, oks []float64) []check.Judged {
	var out []check.Judged
	for _, p := range violations {
		out = append(out, check.Judged{Example: tenets.Example{Label: tenets.LabelViolation, Code: "x := 1\n"}, Prob: p})
	}
	for _, p := range oks {
		out = append(out, check.Judged{Example: tenets.Example{Label: tenets.LabelOK, Code: "y := 2\n"}, Prob: p})
	}
	return out
}
