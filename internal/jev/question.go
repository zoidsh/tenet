package jev

import (
	"errors"
	"fmt"
)

// Label counts the API enforces.
const (
	MaxChoiceLabels = 255
	MinScoreLevels  = 2
	MaxScoreLevels  = 10
)

// Question asks one thing about the state. Criteria descriptions are typed as
// any because the API accepts a JSON object wherever it accepts a string; the
// constructors cover the string case.
type Question struct {
	Type         Kind   `json:"type"`
	Instructions string `json:"instructions,omitempty"`
	Criteria     any    `json:"criteria,omitempty"`
}

// NoulCriteria describes what a true and a false verdict mean.
type NoulCriteria struct {
	True  any `json:"true,omitempty"`
	False any `json:"false,omitempty"`
}

// Noul asks for the probability that a statement holds. Empty descriptions
// leave the criteria out, which the API treats as unspecified.
func Noul(instructions string, trueDesc, falseDesc string) Question {
	q := Question{Type: KindNoul, Instructions: instructions}
	if trueDesc != "" || falseDesc != "" {
		c := NoulCriteria{}
		if trueDesc != "" {
			c.True = trueDesc
		}
		if falseDesc != "" {
			c.False = falseDesc
		}
		q.Criteria = c
	}
	return q
}

// Choice asks for one label out of a set. A label maps to its description, or
// to nil when the label speaks for itself.
func Choice(instructions string, labels map[string]any) (Question, error) {
	q := Question{Type: KindChoice, Instructions: instructions, Criteria: labels}
	return q, q.Validate()
}

// Score asks for a level on a scale, described low to high.
func Score(instructions string, levels []string) (Question, error) {
	c := make([]any, len(levels))
	for i, l := range levels {
		c[i] = l
	}
	q := Question{Type: KindScore, Instructions: instructions, Criteria: c}
	return q, q.Validate()
}

// Validate reports whether the API would reject the question outright.
func (q Question) Validate() error {
	switch q.Type {
	case KindNoul:
		return nil
	case KindChoice:
		n, ok := mapLen(q.Criteria)
		if !ok {
			return fmt.Errorf("jev: choice criteria must be a map of labels, got %T", q.Criteria)
		}
		if n < 1 || n > MaxChoiceLabels {
			return fmt.Errorf("jev: choice needs 1 to %d labels, got %d", MaxChoiceLabels, n)
		}
		return nil
	case KindScore:
		n, ok := sliceLen(q.Criteria)
		if !ok {
			return fmt.Errorf("jev: score criteria must be a list of levels, got %T", q.Criteria)
		}
		if n < MinScoreLevels || n > MaxScoreLevels {
			return fmt.Errorf("jev: score needs %d to %d levels, got %d", MinScoreLevels, MaxScoreLevels, n)
		}
		return nil
	case "":
		return errors.New("jev: question has no type")
	default:
		return fmt.Errorf("jev: unknown question type %q", q.Type)
	}
}

func mapLen(criteria any) (int, bool) {
	switch c := criteria.(type) {
	case map[string]any:
		return len(c), true
	case map[string]string:
		return len(c), true
	default:
		return 0, false
	}
}

func sliceLen(criteria any) (int, bool) {
	switch c := criteria.(type) {
	case []any:
		return len(c), true
	case []string:
		return len(c), true
	default:
		return 0, false
	}
}

func validateQuestions(questions map[string]Question) error {
	if len(questions) == 0 {
		return errors.New("jev: no questions to ask")
	}
	for name, q := range questions {
		if err := q.Validate(); err != nil {
			return fmt.Errorf("question %q: %w", name, err)
		}
	}
	return nil
}
