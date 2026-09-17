package jev

import (
	"encoding/json"
	"fmt"
	"math"
)

// MaxRequestTokens is the budget the API documents for a request's state and
// questions together, at https://docs.typesafe.ai/primitives.md.
//
// RequestBudget is the headroom we keep below that limit, because the count is
// a character ratio and not the model's tokenizer: an estimate that lands
// under the real count would still be rejected.
const (
	MaxRequestTokens = 32000
	RequestBudget    = 28000
)

// The docs put 32,000 tokens at roughly 150,000 characters of English; 3.5
// characters per token is the conservative end of that.
const charsPerToken = 3.5

// EstimateTokens reports how many tokens a string is worth.
func EstimateTokens(s string) int {
	return int(math.Ceil(float64(len(s)) / charsPerToken))
}

// QuestionTokens reports what one named question adds to a request, measured
// on the JSON the request carries.
func QuestionTokens(name string, q Question) (int, error) {
	encoded, err := json.Marshal(q)
	if err != nil {
		return 0, fmt.Errorf("jev: question %q cannot be encoded: %w", name, err)
	}
	return EstimateTokens(name) + EstimateTokens(string(encoded)), nil
}

// RequestTokens reports what a request is worth, state and questions together.
func RequestTokens(state string, questions map[string]Question) (int, error) {
	total := EstimateTokens(state)
	for name, q := range questions {
		tokens, err := QuestionTokens(name, q)
		if err != nil {
			return 0, err
		}
		total += tokens
	}
	return total, nil
}
