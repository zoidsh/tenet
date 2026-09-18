package configedit

import (
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

var errTenetsNotASequence = errors.New("tenets: is not a list of tenets")

// RuleComment is one comment line above a built-in rule id under rules:, where
// the agent instructions say to keep the source line of the drafted sentence
// the rule stood in for.
type RuleComment struct {
	// Rule is the id the comment sits above.
	Rule string

	// Text is the line without its leading hash.
	Text string

	item *yaml.Node
	line int
}

// RuleComments are every comment line above every rule id, in file order.
func (f *File) RuleComments() []*RuleComment {
	seq := f.value("rules")
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil
	}
	var out []*RuleComment
	for _, item := range seq.Content {
		for i, line := range strings.Split(item.HeadComment, "\n") {
			text, ok := strings.CutPrefix(strings.TrimSpace(line), "#")
			if !ok {
				continue
			}
			out = append(out, &RuleComment{
				Rule: item.Value,
				Text: strings.TrimSpace(text),
				item: item,
				line: i,
			})
		}
	}
	return out
}

// Set rewrites the comment line, keeping the indentation the encoder gives the
// block and every other line of it as it was.
func (c *RuleComment) Set(text string) {
	c.Text = text
	lines := strings.Split(c.item.HeadComment, "\n")
	lines[c.line] = "# " + text
	c.item.HeadComment = strings.Join(lines, "\n")
}
