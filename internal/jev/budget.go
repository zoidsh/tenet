package jev

import (
	"encoding/json"
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

// QuestionTokens reports what one named question adds to a request.
func QuestionTokens(name string, q Question) int {
	encoded, err := json.Marshal(q)
	if err != nil {
		// A question the client cannot encode cannot be sent at all, so it is
		// charged the whole budget rather than slipped under it.
		return MaxRequestTokens
	}
	return EstimateTokens(name) + EstimateTokens(string(encoded))
}

// RequestTokens reports what a request is worth, state and questions together.
func RequestTokens(state string, questions map[string]Question) int {
	total := EstimateTokens(state)
	for name, q := range questions {
		total += QuestionTokens(name, q)
	}
	return total
}
