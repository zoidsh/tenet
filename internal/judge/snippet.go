package judge

import (
	"fmt"
	"strings"

	"github.com/zoidsh/tenetlint/internal/jev"
	"github.com/zoidsh/tenetlint/internal/source"
	"github.com/zoidsh/tenetlint/internal/tenets"
)

// StateOf renders lines the way the model is shown a window, so that text
// which never came from a file on disk is judged in the same state a lint
// would have built for it. The kind decides the framing: a document read as
// source code is judged against what its code says rather than what it says.
func StateOf(kind, lang, path string, lines []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", header(kind, lang, path))
	for i, line := range lines {
		fmt.Fprintf(&b, "%s %s\n", lineID(i+1), line)
	}
	return b.String()
}

func header(kind, lang, path string) string {
	switch kind {
	case source.KindProse:
		return fmt.Sprintf("Document: %s. File: %s. Text excerpt:", lang, path)
	case source.KindData:
		return fmt.Sprintf("Data file: %s. File: %s. Excerpt:", lang, path)
	case source.KindCommit:
		return "Commit message. Text excerpt:"
	default:
		return fmt.Sprintf("Language: %s. File: %s. Source file excerpt:", lang, path)
	}
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
