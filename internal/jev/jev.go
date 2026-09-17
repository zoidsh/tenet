// Package jev is a client for TypeSafe's jev model, which answers questions
// about a piece of text as calibrated probabilities rather than prose.
package jev

import "math"

// Kind is the shape of a question and of its matching answer.
type Kind string

// The three question kinds the API supports.
const (
	KindNoul   Kind = "noul"
	KindChoice Kind = "choice"
	KindScore  Kind = "score"
)

// Input tokens are the only billed unit, output tokens are free. The pricing
// page quotes $42 per billion input tokens, which is the $0.042 per million
// written here.
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

// RoundCost is a cost as a report states it. A sum of costs carries the
// float's last bits, and a millionth of a dollar is already finer than any
// decision made from the number.
func RoundCost(usd float64) float64 {
	return math.Round(usd*1e6) / 1e6
}
