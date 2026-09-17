package check_test

import (
	"math"
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
		violations  []float64
		oks         []float64
		minExamples int
		want        string
	}{
		{"separated", []float64{0.9, 0.8, 0.7}, []float64{0.3, 0.2, 0.1}, 6, check.VerdictSharp},
		{"on the threshold", []float64{0.5, 0.6, 0.7}, []float64{0.3, 0.2, 0.1}, 6, check.VerdictSharp},
		{"auc exactly usable", []float64{0.5, 0.6, 0.7, 0.8}, []float64{0.1, 0.2, 0.3, 0.4, 0.75}, 6, check.VerdictUsable},
		{"just under usable", []float64{0.5, 0.6, 0.7, 0.8}, []float64{0.1, 0.2, 0.3, 0.4, 0.85}, 6, check.VerdictBlurry},
		{"one label only", []float64{0.9, 0.8, 0.7, 0.6, 0.5, 0.4}, nil, 6, check.VerdictBlurry},
		{"one short", []float64{0.9, 0.8, 0.7}, []float64{0.3, 0.2}, 6, check.VerdictTooFew},
		{"exactly enough", []float64{0.9, 0.8, 0.7}, []float64{0.3, 0.2}, 5, check.VerdictSharp},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := check.Measure(tenet(), judged(c.violations, c.oks), c.minExamples)
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
	if r.AUC != 0 || r.AccuracyThreshold != 0 || r.Gap != 0 || r.Advice != "" || len(r.Misjudged) != 0 {
		t.Errorf("numbers were reported anyway: %#v", r)
	}
}

func TestAdviceBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		violations []float64
		oks        []float64
		want       string
	}{
		{"both on the edges of the band", []float64{0.6, 0.6, 0.6}, []float64{0.4, 0.4, 0.4}, check.AdviceAmbiguous},
		{"violations just above the band", []float64{0.61, 0.61, 0.61}, []float64{0.4, 0.4, 0.4}, ""},
		{"ok mean on the threshold", []float64{0.95, 0.95, 0.95}, []float64{0.4, 0.5, 0.6}, check.AdviceOKHigh},
		{"ok mean just under", []float64{0.95, 0.95, 0.95}, []float64{0.4, 0.5, 0.59}, ""},
		{"violation mean just under the threshold", []float64{0.49, 0.49, 0.49}, []float64{0.05, 0.05, 0.05}, check.AdviceViolationLow},
		{"violation mean on the threshold", []float64{0.5, 0.5, 0.5}, []float64{0.05, 0.05, 0.05}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := check.Measure(tenet(), judged(c.violations, c.oks), 6)
			if r.Advice != c.want {
				t.Errorf("advice is %q, want %q (mean violation %.3f, mean ok %.3f)", r.Advice, c.want, r.MeanViolation, r.MeanOK)
			}
		})
	}
}

func TestMeasureCountsAndMisjudged(t *testing.T) {
	examples := []check.Judged{
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a := 1\nb := 2\n", Line: 2}, Prob: 0.9, Line: 2},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "c := 3\n", Line: 1}, Prob: 0.45, Line: 1},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "d := 4\n"}, Prob: 0.8},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "e := 5\n"}, Prob: 0.1},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "f := 6\n"}, Prob: 0.2},
		{Example: tenets.Example{Label: tenets.LabelOK, Code: "g := 7\n"}, Prob: 0.95},
	}
	r := check.Measure(tenet(), examples, 6)

	if r.Examples != 6 || r.Violations != 3 || r.OKs != 3 {
		t.Errorf("counts are %d, %d, %d", r.Examples, r.Violations, r.OKs)
	}
	if math.Abs(r.AccuracyThreshold-4.0/6) > 1e-9 {
		t.Errorf("accuracy at the threshold is %v", r.AccuracyThreshold)
	}
	// At 0.7 the violation at 0.45 and the ok at 0.95 are the ones that land
	// on the wrong side.
	if math.Abs(r.AccuracyConfident-4.0/6) > 1e-9 {
		t.Errorf("accuracy at confident is %v", r.AccuracyConfident)
	}
	if math.Abs(r.Gap-(0.7166666666666667-0.4166666666666667)) > 1e-9 {
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
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\nc\n", Line: 2}, Prob: 0.9, Line: 3},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\nb\nc\n", Line: 2}, Prob: 0.9, Line: 2},
		{Example: tenets.Example{Label: tenets.LabelViolation, Code: "a\n", Line: 1}, Prob: 0.9, Line: 1},
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

func tenet() *tenets.Tenet {
	return &tenets.Tenet{ID: "comment-why", Tenet: "A comment says why."}
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
