package jev_test

import (
	"testing"

	"github.com/zoidsh/tenet/internal/jev"
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

	got, err := jev.QuestionTokens("verdict", questions["verdict"])
	if err != nil {
		t.Fatal(err)
	}
	if got != 13 {
		t.Errorf(`the verdict question is %d tokens, want the 37 characters of {"type":"noul","instructions":"rule"} and the 7 of its name`, got)
	}
	if got, err = jev.QuestionTokens("loc", questions["loc"]); err != nil {
		t.Fatal(err)
	}
	if got != 20 {
		t.Errorf("the location question is %d tokens, want the 65 characters of its JSON and the 3 of its name", got)
	}
	if got, err = jev.RequestTokens("0123456789", questions); err != nil {
		t.Fatal(err)
	}
	if got != 36 {
		t.Errorf("the request is %d tokens, want the state's 3 plus 13 plus 20", got)
	}
	if got, err = jev.RequestTokens("0123456789", nil); err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("a request with no questions is %d tokens, want the state's 3", got)
	}
}

func TestRequestTokensRejectsUnencodableCriteria(t *testing.T) {
	q := jev.Question{Type: jev.KindNoul, Instructions: "rule", Criteria: make(chan int)}
	if _, err := jev.RequestTokens("state", map[string]jev.Question{"verdict": q}); err == nil {
		t.Error("a question the request cannot carry was given a size anyway")
	}
}

func TestRequestBudgetLeavesHeadroom(t *testing.T) {
	if jev.RequestBudget >= jev.MaxRequestTokens {
		t.Errorf("budget %d leaves no headroom below %d", jev.RequestBudget, jev.MaxRequestTokens)
	}
}
