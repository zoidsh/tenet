package jev

// Answer is the union of the three answer shapes; Type says which fields the
// API filled in.
type Answer struct {
	Type Kind `json:"type"`

	Noul float64 `json:"noul,omitempty"`

	Choice string  `json:"choice,omitempty"`
	Score  float64 `json:"score,omitempty"`

	Confidence    float64            `json:"confidence,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// Prob reports the probability a noul answer assigns to its statement being
// true. It is zero for the other kinds.
func (a Answer) Prob() float64 {
	if a.Type != KindNoul {
		return 0
	}
	return a.Noul
}

// Top reports the most probable label of a choice answer and its probability,
// read from the distribution rather than the confidence field, which measures
// how concentrated the distribution is and not how likely the winner is.
func (a Answer) Top() (string, float64) {
	var label string
	var best float64
	found := false
	for l, p := range a.Probabilities {
		// Ties are broken by label so that the result does not depend on Go's
		// map iteration order.
		if !found || p > best || (p == best && l < label) {
			label, best, found = l, p, true
		}
	}
	return label, best
}

// Response is one answered request.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`

	// RequestID comes from the x-typesafe-request-id header, not the body.
	RequestID string `json:"-"`
}
