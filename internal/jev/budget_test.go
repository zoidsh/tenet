package jev_test

import (
	"testing"

	"github.com/zoidsh/tenetlint/internal/jev"
)

func TestEstimateTokens(t *testing.T) {
	for _, c := range []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 1},
		{"0123456789", 3},
		{"1234567890123456789012345678901234", 10},
	} {
		if got := jev.EstimateTokens(c.s); got != c.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestRequestTokens(t *testing.T) {
	where, err := jev.Choice("where", map[string]any{"none": nil})
	if err != nil {
		t.Fatal(err)
	}
	questions := map[string]jev.Question{
		"verdict": jev.Noul("rule", "", ""),
		"loc":     where,
	}
	// {"type":"noul","instructions":"rule"} is 37 characters under the name
	// "verdict", and the choice is 65 characters under the name "loc".
	if got := jev.QuestionTokens("verdict", questions["verdict"]); got != 13 {
		t.Errorf("verdict question is %d tokens", got)
	}
	if got := jev.QuestionTokens("loc", questions["loc"]); got != 20 {
		t.Errorf("location question is %d tokens", got)
	}
	if got := jev.RequestTokens("0123456789", questions); got != 36 {
		t.Errorf("request is %d tokens, want the state's 3 plus 13 plus 20", got)
	}
	if got := jev.RequestTokens("0123456789", nil); got != 3 {
		t.Errorf("stateless request is %d tokens", got)
	}
}

func TestRequestBudgetLeavesHeadroom(t *testing.T) {
	if jev.RequestBudget >= jev.MaxRequestTokens {
		t.Errorf("budget %d leaves no headroom below %d", jev.RequestBudget, jev.MaxRequestTokens)
	}
}
