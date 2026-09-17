package judge

import (
	"fmt"
	"strings"

	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

// StateOf renders lines the way the model is shown a window, so that text
// which never came from a file on disk is judged in the same state a lint
// would have built for it.
func StateOf(lang, path string, lines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Language: %s. File: %s. Source file excerpt:\n", lang, path)
	for i, line := range lines {
		fmt.Fprintf(&b, "%s %s\n", lineID(i+1), line)
	}
	return b.String()
}

// VerdictQuestion asks whether the state violates the tenet.
func VerdictQuestion(t *tenets.Tenet) jev.Question {
	var trueDesc, falseDesc string
	if c := t.Criteria; c != nil {
		trueDesc, falseDesc = c.True, c.False
	}
	return jev.Noul(VerdictInstructions(t), trueDesc, falseDesc)
}

// LocationQuestion asks which of the first lines lines of the state violates
// the tenet.
func LocationQuestion(t *tenets.Tenet, lines int) (jev.Question, error) {
	labels := map[string]any{NoneLabel: "no line violates the rule"}
	for i := 1; i <= lines; i++ {
		labels[lineID(i)] = nil
	}
	return jev.Choice(LocationInstructions(t), labels)
}

// ParseLineID reads the line a location answer names.
func ParseLineID(label string) (int, bool) { return parseLineID(label) }

// TopLine is the line a location answer names, with none taken out.
func TopLine(a jev.Answer) string { return topLine(a) }
