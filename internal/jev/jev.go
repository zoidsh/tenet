// Package jev is a client for TypeSafe's jev model, which answers questions
// about a piece of text as calibrated probabilities rather than prose.
package jev

// Kind is the shape of a question and of its matching answer.
type Kind string

// The three question kinds the API supports.
const (
	KindNoul   Kind = "noul"
	KindChoice Kind = "choice"
	KindScore  Kind = "score"
)

// Input tokens are the only billed unit: $42 per billion, output free.
const usdPerInputToken = 0.042 / 1e6

// Usage counts the tokens a request was billed for.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Cost reports what a request cost in US dollars.
func Cost(u Usage) float64 {
	return float64(u.InputTokens) * usdPerInputToken
}
